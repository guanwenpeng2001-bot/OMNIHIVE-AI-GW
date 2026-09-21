package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// User billing timeline kinds. One timeline merges the money movements the
// platform records for a user, so recharges, refunds, rebates, redeemed cards,
// administrative adjustments and pay-as-you-go consumption can be reconciled on
// a single axis.
const (
	UserBillingKindRecharge   = "recharge"
	UserBillingKindRefund     = "refund"
	UserBillingKindRebate     = "rebate"
	UserBillingKindRedeem     = "redeem"
	UserBillingKindAdjustment = "adjustment"
	UserBillingKindCharge     = "charge"
)

// UserBillingTimelineEntry is one recorded money movement.
//
// Amount is always read from the column the platform wrote when the event
// happened (payment_orders.amount, payment_orders.refund_amount,
// redeem_codes.value, user_affiliate_ledger.amount and usage_logs.actual_cost).
// The timeline never re-derives an amount from the current pricing card or the
// current rate multiplier: historical entries keep the facts they were recorded
// with.
type UserBillingTimelineEntry struct {
	Kind   string  `json:"kind"`
	Amount float64 `json:"amount"` // signed: credits > 0, debits < 0
	// AffectsBalance marks entries that moved the wallet when they were
	// recorded. Rebate accruals stay in the affiliate quota until transferred,
	// so they are visible in the timeline without being wallet movements.
	AffectsBalance bool      `json:"affects_balance"`
	OccurredAt     time.Time `json:"occurred_at"`
	Reference      string    `json:"reference"`
	Detail         string    `json:"detail,omitempty"`
	// RequestCount is set on the aggregated daily consumption entry only.
	RequestCount int64 `json:"request_count,omitempty"`
}

// UserBillingTimelineTotals reconciles the window: Credits + Debits == Net, and
// every component equals the sum of the recorded source rows.
type UserBillingTimelineTotals struct {
	Credits            float64 `json:"credits"`
	Debits             float64 `json:"debits"` // negative
	Net                float64 `json:"net"`
	AccruedRebate      float64 `json:"accrued_rebate"` // accrued but not yet transferred
	ChargeRequestCount int64   `json:"charge_request_count"`
}

// UserBillingTimelinePage is the paginated timeline of one window.
type UserBillingTimelinePage struct {
	Entries  []UserBillingTimelineEntry `json:"entries"`
	Totals   UserBillingTimelineTotals  `json:"totals"`
	Total    int64                      `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
	StartAt  *time.Time                 `json:"start_at,omitempty"`
	EndAt    *time.Time                 `json:"end_at,omitempty"`
	Timezone string                     `json:"timezone"`
}

// UserBillingTimelineQuery describes the requested slice of a user's timeline.
type UserBillingTimelineQuery struct {
	Page     int
	PageSize int
	StartAt  *time.Time
	EndAt    *time.Time
	// Timezone buckets the daily consumption entries. Empty or unknown values
	// fall back to the configured server timezone.
	Timezone string
}

const (
	defaultUserBillingTimelinePageSize = 20
	maxUserBillingTimelinePageSize     = 100
	// The timeline merges several sources, each read with a page-sized bound;
	// deep paging is refused instead of silently truncating the result.
	maxUserBillingTimelinePage = 1000
)

// ErrUserBillingTimelinePageOutOfRange rejects page numbers that would force an
// unbounded merge over every source.
var ErrUserBillingTimelinePageOutOfRange = infraerrors.BadRequest(
	"BILLING_TIMELINE_PAGE_OUT_OF_RANGE",
	"billing timeline page is out of range",
)

// UserBillingTimeline returns the unified, paginated billing timeline of one
// user. Amounts are historical facts; nothing here recomputes past entries.
func (s *UsageService) UserBillingTimeline(ctx context.Context, userID int64, query UserBillingTimelineQuery) (*UserBillingTimelinePage, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("user billing timeline: usage service is not available")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("user billing timeline: invalid user id")
	}
	query = normalizeUserBillingTimelineQuery(query)
	if query.Page > maxUserBillingTimelinePage {
		return nil, ErrUserBillingTimelinePageOutOfRange
	}

	loaded, err := s.loadUserBillingTimelineEntries(ctx, userID, query)
	if err != nil {
		return nil, err
	}
	entries := append(loaded.paged, loaded.charges...)
	sortUserBillingTimelineEntries(entries)

	totals, total, err := s.loadUserBillingTimelineTotals(ctx, userID, query)
	if err != nil {
		return nil, err
	}
	// Charge rows are day buckets that were loaded in full, so their entry count
	// comes from the loaded rows while every other kind is counted in SQL.
	total += int64(len(loaded.charges))

	return &UserBillingTimelinePage{
		Entries:  paginateUserBillingTimeline(entries, query),
		Totals:   totals,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
		StartAt:  query.StartAt,
		EndAt:    query.EndAt,
		Timezone: query.Timezone,
	}, nil
}

func normalizeUserBillingTimelineQuery(query UserBillingTimelineQuery) UserBillingTimelineQuery {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = defaultUserBillingTimelinePageSize
	}
	if query.PageSize > maxUserBillingTimelinePageSize {
		query.PageSize = maxUserBillingTimelinePageSize
	}
	query.Timezone = resolveUserBillingTimelineTimezone(query.Timezone)
	return query
}

// resolveUserBillingTimelineTimezone returns a Postgres-acceptable time zone
// name, falling back to the configured server zone for empty/unknown input.
func resolveUserBillingTimelineTimezone(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return timezone.Name()
	}
	if _, err := time.LoadLocation(name); err != nil {
		return timezone.Name()
	}
	return name
}

// sortUserBillingTimelineEntries orders newest first with a deterministic
// tie-break so paging stays stable when several entries share a timestamp.
func sortUserBillingTimelineEntries(entries []UserBillingTimelineEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if !left.OccurredAt.Equal(right.OccurredAt) {
			return left.OccurredAt.After(right.OccurredAt)
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Reference < right.Reference
	})
}

// paginateUserBillingTimeline slices the already-merged and sorted timeline.
func paginateUserBillingTimeline(entries []UserBillingTimelineEntry, query UserBillingTimelineQuery) []UserBillingTimelineEntry {
	offset := (query.Page - 1) * query.PageSize
	if offset >= len(entries) {
		return []UserBillingTimelineEntry{}
	}
	end := offset + query.PageSize
	if end > len(entries) {
		end = len(entries)
	}
	return entries[offset:end]
}

// userBillingTimelineLoadedEntries separates the paged money movements from the
// fully loaded daily consumption buckets.
type userBillingTimelineLoadedEntries struct {
	paged   []UserBillingTimelineEntry
	charges []UserBillingTimelineEntry
}

func (s *UsageService) loadUserBillingTimelineEntries(ctx context.Context, userID int64, query UserBillingTimelineQuery) (userBillingTimelineLoadedEntries, error) {
	// Every paged source is read with the same bound, so the merged head of each
	// source is enough to serve any page inside the window.
	limit := query.Page * query.PageSize

	statements := []string{
		userBillingTimelineRechargesSQL,
		userBillingTimelineRefundsSQL,
		userBillingTimelineRebatesSQL,
		userBillingTimelineRedeemCodesSQL,
	}
	loaded := userBillingTimelineLoadedEntries{paged: make([]UserBillingTimelineEntry, 0, limit*len(statements))}
	for _, statement := range statements {
		entries, err := s.queryUserBillingTimelineEntries(ctx, statement, userID, query, limit)
		if err != nil {
			return userBillingTimelineLoadedEntries{}, err
		}
		loaded.paged = append(loaded.paged, entries...)
	}

	charges, err := s.queryUserBillingTimelineChargeDays(ctx, userID, query)
	if err != nil {
		return userBillingTimelineLoadedEntries{}, err
	}
	loaded.charges = charges
	return loaded, nil
}

// queryUserBillingTimelineEntries runs one source query whose result shape is
// (kind, amount, affects_balance, occurred_at, reference, detail).
func (s *UsageService) queryUserBillingTimelineEntries(ctx context.Context, statement string, userID int64, query UserBillingTimelineQuery, limit int) ([]UserBillingTimelineEntry, error) {
	rows, err := s.entClient.QueryContext(ctx, statement, userID, query.StartAt, query.EndAt, limit)
	if err != nil {
		return nil, fmt.Errorf("query user billing timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []UserBillingTimelineEntry
	for rows.Next() {
		var entry UserBillingTimelineEntry
		if err := rows.Scan(&entry.Kind, &entry.Amount, &entry.AffectsBalance, &entry.OccurredAt, &entry.Reference, &entry.Detail); err != nil {
			return nil, fmt.Errorf("scan user billing timeline: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query user billing timeline: %w", err)
	}
	return entries, nil
}

// queryUserBillingTimelineChargeDays aggregates the recorded consumption per
// day. actual_cost is what was charged when the request happened; summing
// recorded rows is not a recomputation of historical pricing.
//
// Day buckets are not paged: they are bounded by the calendar span of the
// window, and the totals need all of them anyway.
func (s *UsageService) queryUserBillingTimelineChargeDays(ctx context.Context, userID int64, query UserBillingTimelineQuery) ([]UserBillingTimelineEntry, error) {
	rows, err := s.entClient.QueryContext(ctx, userBillingTimelineChargeDaysSQL, userID, query.StartAt, query.EndAt, query.Timezone)
	if err != nil {
		return nil, fmt.Errorf("query user billing charges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []UserBillingTimelineEntry
	for rows.Next() {
		var (
			occurredAt   time.Time
			amount       float64
			requestCount int64
		)
		if err := rows.Scan(&occurredAt, &amount, &requestCount); err != nil {
			return nil, fmt.Errorf("scan user billing charges: %w", err)
		}
		entries = append(entries, UserBillingTimelineEntry{
			Kind:           UserBillingKindCharge,
			Amount:         -amount,
			AffectsBalance: true,
			OccurredAt:     occurredAt,
			Reference:      occurredAt.UTC().Format("2006-01-02"),
			RequestCount:   requestCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query user billing charges: %w", err)
	}
	return entries, nil
}

// userBillingTimelineTotalsRow is one aggregated source row: the window sum of
// its credits and of its (absolute) debits, plus how many entries it covers.
type userBillingTimelineTotalsRow struct {
	Kind    string
	Credit  float64
	Debit   float64
	Entries int64
}

// accumulateUserBillingTimelineTotals folds the aggregated source rows into the
// window totals, separating real wallet movements from rebate accruals that sit
// in the affiliate quota until transferred.
func accumulateUserBillingTimelineTotals(rows []userBillingTimelineTotalsRow) (UserBillingTimelineTotals, int64, error) {
	var (
		totals          UserBillingTimelineTotals
		pagedEntryCount int64
	)
	for _, row := range rows {
		switch row.Kind {
		case UserBillingKindRecharge, UserBillingKindRedeem, UserBillingKindAdjustment:
			totals.Credits += row.Credit
			totals.Debits -= row.Debit
			pagedEntryCount += row.Entries
		case UserBillingKindRefund:
			totals.Debits -= row.Debit
			pagedEntryCount += row.Entries
		case "rebate_accrue":
			totals.AccruedRebate += row.Credit
			pagedEntryCount += row.Entries
		case "rebate_transfer":
			totals.Credits += row.Credit
			pagedEntryCount += row.Entries
		case UserBillingKindCharge:
			// The debit sum covers every recorded request; the entry count is the
			// number of loaded day buckets, added by the caller.
			totals.Debits -= row.Debit
			totals.ChargeRequestCount = row.Entries
		default:
			return UserBillingTimelineTotals{}, 0, fmt.Errorf("unknown user billing totals kind %q", row.Kind)
		}
	}
	totals.Net = totals.Credits + totals.Debits
	return totals, pagedEntryCount, nil
}

// loadUserBillingTimelineTotals sums every kind over the whole window (not just
// the current page) so a page can be reconciled against the recorded facts. The
// same statement also returns the entry counts used as the page total.
func (s *UsageService) loadUserBillingTimelineTotals(ctx context.Context, userID int64, query UserBillingTimelineQuery) (UserBillingTimelineTotals, int64, error) {
	rows, err := s.entClient.QueryContext(ctx, userBillingTimelineTotalsSQL, userID, query.StartAt, query.EndAt)
	if err != nil {
		return UserBillingTimelineTotals{}, 0, fmt.Errorf("query user billing totals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var aggregated []userBillingTimelineTotalsRow
	for rows.Next() {
		var row userBillingTimelineTotalsRow
		if err := rows.Scan(&row.Kind, &row.Credit, &row.Debit, &row.Entries); err != nil {
			return UserBillingTimelineTotals{}, 0, fmt.Errorf("scan user billing totals: %w", err)
		}
		aggregated = append(aggregated, row)
	}
	if err := rows.Err(); err != nil {
		return UserBillingTimelineTotals{}, 0, fmt.Errorf("query user billing totals: %w", err)
	}
	return accumulateUserBillingTimelineTotals(aggregated)
}

// Source statements. Every one keeps the same shape
// (kind, amount, affects_balance, occurred_at, reference, detail) and reads the
// amount column that was written when the event happened.
//
// Parameters: $1 user id, $2 window start (nullable), $3 window end (nullable),
// $4 row limit.
const (
	userBillingTimelineRechargesSQL = `
SELECT 'recharge' AS kind,
       po.amount::double precision AS amount,
       TRUE AS affects_balance,
       COALESCE(po.paid_at, po.completed_at, po.created_at) AS occurred_at,
       COALESCE(NULLIF(po.out_trade_no, ''), po.id::text) AS reference,
       COALESCE(NULLIF(po.payment_type, ''), '') AS detail
FROM payment_orders po
WHERE po.user_id = $1
  AND po.order_type = 'balance'
  AND po.status = 'COMPLETED'
  AND ($2::timestamptz IS NULL OR COALESCE(po.paid_at, po.completed_at, po.created_at) >= $2)
  AND ($3::timestamptz IS NULL OR COALESCE(po.paid_at, po.completed_at, po.created_at) < $3)
ORDER BY occurred_at DESC, reference DESC
LIMIT $4`

	userBillingTimelineRefundsSQL = `
SELECT 'refund' AS kind,
       (-po.refund_amount)::double precision AS amount,
       TRUE AS affects_balance,
       po.refund_at AS occurred_at,
       COALESCE(NULLIF(po.out_trade_no, ''), po.id::text) AS reference,
       COALESCE(NULLIF(po.refund_reason, ''), '') AS detail
FROM payment_orders po
WHERE po.user_id = $1
  AND po.refund_amount > 0
  AND po.refund_at IS NOT NULL
  AND ($2::timestamptz IS NULL OR po.refund_at >= $2)
  AND ($3::timestamptz IS NULL OR po.refund_at < $3)
ORDER BY occurred_at DESC, reference DESC
LIMIT $4`

	userBillingTimelineRebatesSQL = `
SELECT 'rebate' AS kind,
       l.amount::double precision AS amount,
       (l.action = 'transfer') AS affects_balance,
       l.created_at AS occurred_at,
       l.id::text AS reference,
       CASE WHEN l.action = 'transfer' THEN 'rebate_transfer' ELSE 'rebate_accrue' END AS detail
FROM user_affiliate_ledger l
WHERE l.user_id = $1
  AND l.action IN ('accrue', 'transfer')
  AND ($2::timestamptz IS NULL OR l.created_at >= $2)
  AND ($3::timestamptz IS NULL OR l.created_at < $3)
ORDER BY occurred_at DESC, reference DESC
LIMIT $4`

	userBillingTimelineRedeemCodesSQL = `
SELECT CASE WHEN rc.type = 'admin_balance' THEN 'adjustment' ELSE 'redeem' END AS kind,
       rc.value::double precision AS amount,
       TRUE AS affects_balance,
       COALESCE(rc.used_at, rc.created_at) AS occurred_at,
       rc.code AS reference,
       COALESCE(NULLIF(rc.notes, ''), '') AS detail
FROM redeem_codes rc
WHERE rc.used_by = $1
  AND rc.status = 'used'
  AND rc.type IN ('balance', 'invitation', 'affiliate_balance', 'admin_balance')
  AND ($2::timestamptz IS NULL OR COALESCE(rc.used_at, rc.created_at) >= $2)
  AND ($3::timestamptz IS NULL OR COALESCE(rc.used_at, rc.created_at) < $3)
ORDER BY occurred_at DESC, reference DESC
LIMIT $4`

	// Parameters: $1 user id, $2 window start, $3 window end, $4 IANA time zone.
	userBillingTimelineChargeDaysSQL = `
SELECT (date_trunc('day', ul.created_at AT TIME ZONE $4) AT TIME ZONE $4) AS occurred_at,
       COALESCE(SUM(ul.actual_cost), 0)::double precision AS amount,
       COUNT(*)::bigint AS request_count
FROM usage_logs ul
WHERE ul.user_id = $1
  AND ul.billing_type = 0
  AND ($2::timestamptz IS NULL OR ul.created_at >= $2)
  AND ($3::timestamptz IS NULL OR ul.created_at < $3)
GROUP BY 1
ORDER BY 1 DESC`

	// Parameters: $1 user id, $2 window start, $3 window end.
	// Each row is (kind, credit sum, absolute debit sum, entry count).
	userBillingTimelineTotalsSQL = `
SELECT 'recharge' AS kind,
       COALESCE(SUM(po.amount), 0)::double precision AS credit,
       0::double precision AS debit,
       COUNT(*)::bigint AS entries
FROM payment_orders po
WHERE po.user_id = $1 AND po.order_type = 'balance' AND po.status = 'COMPLETED'
  AND ($2::timestamptz IS NULL OR COALESCE(po.paid_at, po.completed_at, po.created_at) >= $2)
  AND ($3::timestamptz IS NULL OR COALESCE(po.paid_at, po.completed_at, po.created_at) < $3)
UNION ALL
SELECT 'refund', 0::double precision, COALESCE(SUM(po.refund_amount), 0)::double precision, COUNT(*)::bigint
FROM payment_orders po
WHERE po.user_id = $1 AND po.refund_amount > 0 AND po.refund_at IS NOT NULL
  AND ($2::timestamptz IS NULL OR po.refund_at >= $2)
  AND ($3::timestamptz IS NULL OR po.refund_at < $3)
UNION ALL
SELECT 'rebate_accrue', COALESCE(SUM(l.amount), 0)::double precision, 0::double precision, COUNT(*)::bigint
FROM user_affiliate_ledger l
WHERE l.user_id = $1 AND l.action = 'accrue'
  AND ($2::timestamptz IS NULL OR l.created_at >= $2)
  AND ($3::timestamptz IS NULL OR l.created_at < $3)
UNION ALL
SELECT 'rebate_transfer', COALESCE(SUM(l.amount), 0)::double precision, 0::double precision, COUNT(*)::bigint
FROM user_affiliate_ledger l
WHERE l.user_id = $1 AND l.action = 'transfer'
  AND ($2::timestamptz IS NULL OR l.created_at >= $2)
  AND ($3::timestamptz IS NULL OR l.created_at < $3)
UNION ALL
SELECT 'redeem',
       COALESCE(SUM(CASE WHEN rc.value > 0 THEN rc.value ELSE 0 END), 0)::double precision,
       COALESCE(SUM(CASE WHEN rc.value < 0 THEN -rc.value ELSE 0 END), 0)::double precision,
       COUNT(*)::bigint
FROM redeem_codes rc
WHERE rc.used_by = $1 AND rc.status = 'used'
  AND rc.type IN ('balance', 'invitation', 'affiliate_balance')
  AND ($2::timestamptz IS NULL OR COALESCE(rc.used_at, rc.created_at) >= $2)
  AND ($3::timestamptz IS NULL OR COALESCE(rc.used_at, rc.created_at) < $3)
UNION ALL
SELECT 'adjustment',
       COALESCE(SUM(CASE WHEN rc.value > 0 THEN rc.value ELSE 0 END), 0)::double precision,
       COALESCE(SUM(CASE WHEN rc.value < 0 THEN -rc.value ELSE 0 END), 0)::double precision,
       COUNT(*)::bigint
FROM redeem_codes rc
WHERE rc.used_by = $1 AND rc.status = 'used' AND rc.type = 'admin_balance'
  AND ($2::timestamptz IS NULL OR COALESCE(rc.used_at, rc.created_at) >= $2)
  AND ($3::timestamptz IS NULL OR COALESCE(rc.used_at, rc.created_at) < $3)
UNION ALL
SELECT 'charge', 0::double precision, COALESCE(SUM(ul.actual_cost), 0)::double precision, COUNT(*)::bigint
FROM usage_logs ul
WHERE ul.user_id = $1 AND ul.billing_type = 0
  AND ($2::timestamptz IS NULL OR ul.created_at >= $2)
  AND ($3::timestamptz IS NULL OR ul.created_at < $3)`
)
