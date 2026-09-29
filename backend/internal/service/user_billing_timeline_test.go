package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 统一时间线：六类条目（充值 / 扣费 / 退款 / 返利 / 兑换 / 调账）在同一条线上，
// 顺序与金额取自各自发生时的记录；汇总能与源事实对账。
func TestUserBillingTimeline_MergesSixKindsInOrder(t *testing.T) {
	at := func(day int, hour int) time.Time {
		return time.Date(2026, time.September, day, hour, 0, 0, 0, time.UTC)
	}

	entries := []UserBillingTimelineEntry{
		{Kind: UserBillingKindRecharge, Amount: 100, AffectsBalance: true, OccurredAt: at(1, 10), Reference: "order-1"},
		{Kind: UserBillingKindRedeem, Amount: 20, AffectsBalance: true, OccurredAt: at(2, 9), Reference: "card-1"},
		{Kind: UserBillingKindRebate, Amount: 5, AffectsBalance: false, OccurredAt: at(3, 8), Reference: "7", Detail: "rebate_accrue"},
		{Kind: UserBillingKindRebate, Amount: 5, AffectsBalance: true, OccurredAt: at(4, 8), Reference: "8", Detail: "rebate_transfer"},
		{Kind: UserBillingKindAdjustment, Amount: -12.5, AffectsBalance: true, OccurredAt: at(5, 12), Reference: "adj-1"},
		{Kind: UserBillingKindCharge, Amount: -7.5, AffectsBalance: true, OccurredAt: at(6, 23), Reference: "2026-09-06", RequestCount: 3},
		{Kind: UserBillingKindRefund, Amount: -30, AffectsBalance: true, OccurredAt: at(7, 11), Reference: "order-1"},
		{Kind: UserBillingKindCharge, Amount: -2.5, AffectsBalance: true, OccurredAt: at(7, 23), Reference: "2026-09-07", RequestCount: 1},
	}

	sortUserBillingTimelineEntries(entries)

	require.Equal(t, []string{
		UserBillingKindCharge,
		UserBillingKindRefund,
		UserBillingKindCharge,
		UserBillingKindAdjustment,
		UserBillingKindRebate,
		UserBillingKindRebate,
		UserBillingKindRedeem,
		UserBillingKindRecharge,
	}, kindsOf(entries), "newest first, six kinds on one axis")
	require.Equal(t, -2.5, entries[0].Amount)
	require.Equal(t, -30.0, entries[1].Amount)
	require.Equal(t, -12.5, entries[3].Amount)
	require.False(t, entries[5].AffectsBalance, "rebate accrual stays out of the wallet")
	require.True(t, entries[4].AffectsBalance, "transferred rebate is a wallet credit")
	require.Equal(t, 100.0, entries[len(entries)-1].Amount)

	// Same-second entries stay deterministically ordered for stable paging.
	tie := []UserBillingTimelineEntry{
		{Kind: UserBillingKindRedeem, OccurredAt: at(9, 9), Reference: "b"},
		{Kind: UserBillingKindRedeem, OccurredAt: at(9, 9), Reference: "a"},
		{Kind: UserBillingKindAdjustment, OccurredAt: at(9, 9), Reference: "z"},
	}
	sortUserBillingTimelineEntries(tie)
	require.Equal(t, []string{"adjustment", "redeem", "redeem"}, kindsOf(tie))
	require.Equal(t, "a", tie[1].Reference)
	require.Equal(t, "b", tie[2].Reference)
}

func TestUserBillingTimeline_PagesTheMergedTimeline(t *testing.T) {
	entries := make([]UserBillingTimelineEntry, 0, 10)
	base := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	for i := range 10 {
		entries = append(entries, UserBillingTimelineEntry{
			Kind:       UserBillingKindCharge,
			Amount:     -float64(i + 1),
			OccurredAt: base.AddDate(0, 0, i),
			Reference:  base.AddDate(0, 0, i).Format("2006-01-02"),
		})
	}
	sortUserBillingTimelineEntries(entries)

	first := paginateUserBillingTimeline(entries, UserBillingTimelineQuery{Page: 1, PageSize: 4})
	require.Len(t, first, 4)
	require.Equal(t, -10.0, first[0].Amount)
	require.Equal(t, -7.0, first[3].Amount)

	last := paginateUserBillingTimeline(entries, UserBillingTimelineQuery{Page: 3, PageSize: 4})
	require.Len(t, last, 2)
	require.Equal(t, -2.0, last[0].Amount)
	require.Equal(t, -1.0, last[1].Amount)

	beyond := paginateUserBillingTimeline(entries, UserBillingTimelineQuery{Page: 4, PageSize: 4})
	require.Empty(t, beyond)
	require.NotNil(t, beyond, "an empty page serializes as []")
}

func TestUserBillingTimeline_TotalsReconcileWithRecordedFacts(t *testing.T) {
	rows := []userBillingTimelineTotalsRow{
		{Kind: UserBillingKindRecharge, Credit: 100, Entries: 1},
		{Kind: UserBillingKindRefund, Debit: 30, Entries: 1},
		{Kind: "rebate_accrue", Credit: 5, Entries: 1},
		{Kind: "rebate_transfer", Credit: 5, Entries: 1},
		{Kind: UserBillingKindRedeem, Credit: 20, Debit: 3, Entries: 2},
		{Kind: UserBillingKindAdjustment, Credit: 4, Debit: 12.5, Entries: 2},
		{Kind: UserBillingKindCharge, Debit: 10, Entries: 7},
	}

	totals, counted, err := accumulateUserBillingTimelineTotals(rows)
	require.NoError(t, err)

	// Credits: recharge 100 + transferred rebate 5 + redeem 20 + adjustment 4.
	require.Equal(t, 129.0, totals.Credits)
	// Debits: refund 30 + redeem reversals 3 + adjustment 12.5 + charges 10.
	require.Equal(t, -55.5, totals.Debits)
	require.Equal(t, 73.5, totals.Net, "net is the wallet movement of the window")
	require.Equal(t, 5.0, totals.AccruedRebate, "accruals are reported but not counted as wallet credit")
	require.Equal(t, int64(7), totals.ChargeRequestCount)

	// The page total counts the entries of every kind except the daily charge
	// buckets, which the caller adds from the loaded rows.
	require.Equal(t, int64(8), counted)

	// Reconciliation: the same window expressed as entries must produce the same
	// net movement as the aggregated source rows.
	entries := []UserBillingTimelineEntry{
		{Kind: UserBillingKindRecharge, Amount: 100, AffectsBalance: true},
		{Kind: UserBillingKindRefund, Amount: -30, AffectsBalance: true},
		{Kind: UserBillingKindRebate, Amount: 5, AffectsBalance: false},
		{Kind: UserBillingKindRebate, Amount: 5, AffectsBalance: true},
		{Kind: UserBillingKindRedeem, Amount: 20, AffectsBalance: true},
		{Kind: UserBillingKindRedeem, Amount: -3, AffectsBalance: true},
		{Kind: UserBillingKindAdjustment, Amount: 4, AffectsBalance: true},
		{Kind: UserBillingKindAdjustment, Amount: -12.5, AffectsBalance: true},
		{Kind: UserBillingKindCharge, Amount: -6, AffectsBalance: true},
		{Kind: UserBillingKindCharge, Amount: -4, AffectsBalance: true},
	}
	require.Equal(t, totals.Net, sumBalanceAffectingAmounts(entries))
	require.Equal(t, 73.5, sumBalanceAffectingAmounts(entries))
}

func TestUserBillingTimeline_TotalsRejectUnknownKind(t *testing.T) {
	_, _, err := accumulateUserBillingTimelineTotals([]userBillingTimelineTotalsRow{{Kind: "mystery"}})
	require.Error(t, err)
}

func TestNormalizeUserBillingTimelineQuery(t *testing.T) {
	normalized := normalizeUserBillingTimelineQuery(UserBillingTimelineQuery{})
	require.Equal(t, 1, normalized.Page)
	require.Equal(t, defaultUserBillingTimelinePageSize, normalized.PageSize)
	require.NotEmpty(t, normalized.Timezone)

	capped := normalizeUserBillingTimelineQuery(UserBillingTimelineQuery{Page: -3, PageSize: 100000})
	require.Equal(t, 1, capped.Page)
	require.Equal(t, maxUserBillingTimelinePageSize, capped.PageSize)

	// Unknown time zones fall back to the configured server zone instead of
	// failing the query in Postgres.
	require.Equal(t, resolveUserBillingTimelineTimezone(""), resolveUserBillingTimelineTimezone("Not/AZone"))
	require.Equal(t, "Asia/Shanghai", resolveUserBillingTimelineTimezone("Asia/Shanghai"))
}

func kindsOf(entries []UserBillingTimelineEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Kind)
	}
	return out
}

func sumBalanceAffectingAmounts(entries []UserBillingTimelineEntry) float64 {
	var total float64
	for _, entry := range entries {
		if entry.AffectsBalance {
			total += entry.Amount
		}
	}
	return total
}
