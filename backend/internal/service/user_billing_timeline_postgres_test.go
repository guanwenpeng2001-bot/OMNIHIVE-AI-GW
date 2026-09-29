//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
)

// The billing timeline SQL lives in the service layer (its DI graph is frozen),
// so it is verified here against a disposable Postgres that already has the real
// migrations applied. Point USER_BILLING_TIMELINE_TEST_DSN at an isolated
// job_test database; the test skips when it is not set.
func openBillingTimelinePostgres(t *testing.T) (*sql.DB, *dbent.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := os.Getenv("USER_BILLING_TIMELINE_TEST_DSN")
	if dsn == "" {
		t.Skip("set USER_BILLING_TIMELINE_TEST_DSN to an isolated job_test database")
	}
	require.NoError(t, timezone.Init("UTC"))
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repository.ApplyMigrations(ctx, db))

	return db, dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
}

type billingTimelineFixture struct {
	userID      int64
	otherUserID int64
	rechargeRef string
	refundRef   string
	redeemCode  string
	adjustCode  string
}

func day(dayOfMonth, hour int) time.Time {
	return time.Date(2026, time.September, dayOfMonth, hour, 0, 0, 0, time.UTC)
}

// seedBillingTimelineFixture records one of every kind the timeline must merge:
// a 100 USD recharge, a 20 USD redeemed card, a 5 USD rebate accrual plus its
// 5 USD transfer, a -12.50 USD administrative adjustment, a 30 USD refund and
// 9.50 USD of consumption on a single day: a net movement of +73 USD.
func seedBillingTimelineFixture(t *testing.T, db *sql.DB, ctx context.Context) billingTimelineFixture {
	t.Helper()
	var fx billingTimelineFixture

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	mustScan := func(query string, args []any, dest ...any) {
		t.Helper()
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(dest...))
	}
	mustExec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}

	var apiKeyID, accountID, groupID int64
	fx.rechargeRef = "order-recharge-" + suffix
	fx.refundRef = "order-refund-" + suffix
	fx.redeemCode = "CARD" + suffix
	fx.adjustCode = "ADJ" + suffix

	mustScan(`INSERT INTO users (email, password_hash, role, status, balance, concurrency)
		VALUES ($1, 'hash', 'user', 'active', 73, 5) RETURNING id`,
		[]any{"billing-" + suffix + "@example.com"}, &fx.userID)
	mustScan(`INSERT INTO users (email, password_hash, role, status, balance, concurrency)
		VALUES ($1, 'hash', 'user', 'active', 0, 5) RETURNING id`,
		[]any{"billing-other-" + suffix + "@example.com"}, &fx.otherUserID)
	mustScan(`INSERT INTO groups (name, status) VALUES ($1, 'active') RETURNING id`,
		[]any{"billing-group-" + suffix}, &groupID)
	mustScan(`INSERT INTO api_keys (user_id, key, name, group_id, status)
		VALUES ($1, $2, 'billing-key', $3, 'active') RETURNING id`,
		[]any{fx.userID, "sk-billing-" + suffix, groupID}, &apiKeyID)
	mustScan(`INSERT INTO accounts (name, platform, type, status, credentials)
		VALUES ($1, 'anthropic', 'apikey', 'active', '{}'::jsonb) RETURNING id`,
		[]any{"billing-account-" + suffix}, &accountID)

	// Recharge: 100 USD credited on 2026-09-01; out_trade_no is the reference.
	mustExec(`INSERT INTO payment_orders (user_id, amount, pay_amount, order_type, status, out_trade_no, payment_type, paid_at, completed_at, created_at, expires_at)
		VALUES ($1, 100, 100, 'balance', 'COMPLETED', $2, 'alipay', $3, $3, $3, $4)`,
		fx.userID, fx.rechargeRef, day(1, 10), day(1, 11))

	// Refund: 30 USD taken back on 2026-09-07.
	mustExec(`INSERT INTO payment_orders (user_id, amount, pay_amount, order_type, status, out_trade_no, payment_type, refund_amount, refund_reason, refund_at, paid_at, completed_at, created_at, expires_at)
		VALUES ($1, 30, 30, 'balance', 'REFUNDED', $2, 'alipay', 30, 'user request', $3, $4, $4, $4, $5)`,
		fx.userID, fx.refundRef, day(7, 11), day(2, 10), day(2, 11))

	// Rebate ledger: 5 accrued on 2026-09-03 (frozen quota) and 5 transferred to
	// the wallet on 2026-09-04.
	mustExec(`INSERT INTO user_affiliate_ledger (user_id, action, amount, created_at, updated_at)
		VALUES ($1, 'accrue', 5, $2, $2), ($1, 'transfer', 5, $3, $3)`,
		fx.userID, day(3, 8), day(4, 8))

	// Redeemed card: 20 USD on 2026-09-02.
	mustExec(`INSERT INTO redeem_codes (code, type, value, status, used_by, used_at, created_at, validity_days)
		VALUES ($1, 'balance', 20, 'used', $2, $3, $3, 30)`,
		fx.redeemCode, fx.userID, day(2, 9))

	// Administrative adjustment: -12.50 USD on 2026-09-05. Adjustments are
	// recorded as used redeem codes of type admin_balance carrying the signed delta.
	mustExec(`INSERT INTO redeem_codes (code, type, value, status, used_by, used_at, created_at, notes, validity_days)
		VALUES ($1, 'admin_balance', -12.5, 'used', $2, $3, $3, 'manual correction', 30)`,
		fx.adjustCode, fx.userID, day(5, 12))

	// Consumption on 2026-09-06: 7.50 USD charged to the wallet (billing_type = 0)
	// over 3 requests, plus a subscription-billed row (billing_type = 1) that must
	// stay out of the wallet view. total_cost/rate_multiplier are deliberately
	// unrelated: the timeline must read the recorded actual_cost.
	mustExec(`INSERT INTO usage_logs (user_id, api_key_id, account_id, model, input_tokens, output_tokens, total_cost, actual_cost, rate_multiplier, billing_type, created_at)
		VALUES ($1, $2, $3, 'claude-sonnet-4', 1000, 100, 15, 3, 2.5, 0, $4),
		       ($1, $2, $3, 'claude-sonnet-4', 1000, 100, 15, 2.5, 2.5, 0, $4),
		       ($1, $2, $3, 'claude-sonnet-4', 1000, 100, 20, 4, 2.5, 0, $4),
		       ($1, $2, $3, 'claude-sonnet-4', 1000, 100, 15, 30, 2.5, 1, $4)`,
		fx.userID, apiKeyID, accountID, day(6, 23))

	// Another user's movements must never leak into this timeline.
	mustExec(`INSERT INTO payment_orders (user_id, amount, pay_amount, order_type, status, out_trade_no, payment_type, paid_at, completed_at, created_at, expires_at)
		VALUES ($1, 999, 999, 'balance', 'COMPLETED', $2, 'alipay', $3, $3, $3, $4)`,
		fx.otherUserID, "order-other-"+suffix, day(1, 12), day(1, 13))
	mustExec(`INSERT INTO usage_logs (user_id, api_key_id, account_id, model, total_cost, actual_cost, billing_type, created_at)
		VALUES ($1, $2, $3, 'claude-sonnet-4', 5, 5, 0, $4)`,
		fx.otherUserID, apiKeyID, accountID, day(6, 23))

	return fx
}

func TestUserBillingTimelinePostgres_MergesRecordedFacts(t *testing.T) {
	db, client := openBillingTimelinePostgres(t)
	ctx := context.Background()
	fx := seedBillingTimelineFixture(t, db, ctx)

	usageService := service.NewUsageService(nil, nil, client, nil)
	page, err := usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{Page: 1, PageSize: 20})
	require.NoError(t, err)

	require.Equal(t, int64(7), page.Total, "recharge, refund, 2 rebates, redeem, adjustment, 1 charge day")
	require.Len(t, page.Entries, 7)

	kinds := make([]string, 0, len(page.Entries))
	for _, entry := range page.Entries {
		kinds = append(kinds, entry.Kind)
	}
	require.Equal(t, []string{
		service.UserBillingKindRefund,
		service.UserBillingKindCharge,
		service.UserBillingKindAdjustment,
		service.UserBillingKindRebate,
		service.UserBillingKindRebate,
		service.UserBillingKindRedeem,
		service.UserBillingKindRecharge,
	}, kinds, "newest first across all six kinds")

	byKind := map[string][]service.UserBillingTimelineEntry{}
	for _, entry := range page.Entries {
		byKind[entry.Kind] = append(byKind[entry.Kind], entry)
	}

	recharge := byKind[service.UserBillingKindRecharge][0]
	require.Equal(t, 100.0, recharge.Amount)
	require.Equal(t, fx.rechargeRef, recharge.Reference)
	require.Equal(t, day(1, 10), recharge.OccurredAt.UTC())
	require.True(t, recharge.AffectsBalance)

	refund := byKind[service.UserBillingKindRefund][0]
	require.Equal(t, -30.0, refund.Amount)
	require.Equal(t, fx.refundRef, refund.Reference)
	require.Equal(t, "user request", refund.Detail)

	require.Equal(t, 20.0, byKind[service.UserBillingKindRedeem][0].Amount)
	require.Equal(t, fx.redeemCode, byKind[service.UserBillingKindRedeem][0].Reference)

	adjustment := byKind[service.UserBillingKindAdjustment][0]
	require.Equal(t, -12.5, adjustment.Amount)
	require.Equal(t, "manual correction", adjustment.Detail)

	// The recorded actual_cost is reported (3 + 2.5 + 4 = 9.5), not total_cost and
	// certainly not total_cost * rate_multiplier; the subscription row is excluded.
	charge := byKind[service.UserBillingKindCharge][0]
	require.Equal(t, -9.5, charge.Amount)
	require.Equal(t, int64(3), charge.RequestCount)
	require.Equal(t, "2026-09-06", charge.Reference)
	require.True(t, charge.AffectsBalance)

	transfers, accruals := 0, 0
	for _, entry := range byKind[service.UserBillingKindRebate] {
		if entry.AffectsBalance {
			transfers++
			require.Equal(t, 5.0, entry.Amount)
			continue
		}
		accruals++
		require.Equal(t, 5.0, entry.Amount)
	}
	require.Equal(t, 1, transfers, "the transferred rebate moved the wallet")
	require.Equal(t, 1, accruals, "the accrual stays informational")

	// Reconciliation: credits - debits is the wallet movement of the window, and
	// the accrual is reported separately because it never reached the wallet.
	require.Equal(t, 125.0, page.Totals.Credits, "100 recharge + 20 redeem + 5 transfer")
	require.Equal(t, -52.0, page.Totals.Debits, "-12.5 adjustment - 30 refund - 9.5 charges")
	require.Equal(t, 73.0, page.Totals.Net)
	require.Equal(t, 5.0, page.Totals.AccruedRebate)
	require.Equal(t, int64(3), page.Totals.ChargeRequestCount)

	// The wallet snapshot recorded after those same movements must equal the net
	// movement of the timeline.
	var balance float64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT balance::double precision FROM users WHERE id = $1`, fx.userID).Scan(&balance))
	require.Equal(t, balance, page.Totals.Net)
}

func TestUserBillingTimelinePostgres_WindowAndPaging(t *testing.T) {
	db, client := openBillingTimelinePostgres(t)
	ctx := context.Background()
	fx := seedBillingTimelineFixture(t, db, ctx)

	usageService := service.NewUsageService(nil, nil, client, nil)

	start := day(2, 0)
	end := day(6, 0)
	windowed, err := usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{
		Page: 1, PageSize: 20, StartAt: &start, EndAt: &end,
	})
	require.NoError(t, err)
	require.Equal(t, int64(4), windowed.Total, "redeem, accrue, transfer, adjustment inside 2026-09-02..09-05")
	require.Equal(t, 25.0, windowed.Totals.Credits)
	require.Equal(t, -12.5, windowed.Totals.Debits)
	require.Equal(t, 12.5, windowed.Totals.Net)

	firstPage, err := usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{Page: 1, PageSize: 3})
	require.NoError(t, err)
	require.Len(t, firstPage.Entries, 3)
	require.Equal(t, int64(7), firstPage.Total)

	secondPage, err := usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{Page: 2, PageSize: 3})
	require.NoError(t, err)
	require.Len(t, secondPage.Entries, 3)
	require.Equal(t, firstPage.Totals, secondPage.Totals, "totals cover the window, not the page")

	thirdPage, err := usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{Page: 3, PageSize: 3})
	require.NoError(t, err)
	require.Len(t, thirdPage.Entries, 1)

	seen := map[string]bool{}
	for _, page := range []*service.UserBillingTimelinePage{firstPage, secondPage, thirdPage} {
		for _, entry := range page.Entries {
			key := fmt.Sprintf("%s|%s|%s", entry.Kind, entry.OccurredAt.UTC().Format(time.RFC3339), entry.Reference)
			require.False(t, seen[key], "entries must not repeat across pages: %s", key)
			seen[key] = true
		}
	}
	require.Len(t, seen, 7)

	beyond, err := usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{Page: 4, PageSize: 3})
	require.NoError(t, err)
	require.Empty(t, beyond.Entries)
	require.Equal(t, int64(7), beyond.Total)

	_, err = usageService.UserBillingTimeline(ctx, fx.userID, service.UserBillingTimelineQuery{Page: 5000, PageSize: 100})
	require.ErrorIs(t, err, service.ErrUserBillingTimelinePageOutOfRange)
}
