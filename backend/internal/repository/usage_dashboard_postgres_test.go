//go:build unit

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

// The dashboard reads globally from the dedicated stats_test database. Reset
// only dashboard inputs and projections. Financial evidence retains user IDs.
func newDashboardUsageFixture(t *testing.T, db *sql.DB) (user, key, account int64) {
	t.Helper()
	_, err := db.Exec(`DELETE FROM usage_logs;
		TRUNCATE usage_dashboard_hourly, usage_dashboard_daily,
		usage_dashboard_hourly_users, usage_dashboard_daily_users`)
	require.NoError(t, err)
	name := uniqueName("dashboard")
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM usage_settlement_receipts WHERE api_key_id=$1`, key)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM users WHERE id=$1`, user)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM accounts WHERE id=$1`, account)
		require.NoError(t, err)
	})
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, name+"@example.test").Scan(&user))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,$2) RETURNING id`, user, name).Scan(&key))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES($1,'openai','apikey') RETURNING id`, name).Scan(&account))
	return user, key, account
}

func TestUsageDashboardPostgres(t *testing.T) {
	dsn := os.Getenv("USAGE_STATS_TEST_DSN")
	if dsn == "" {
		t.Skip("set USAGE_STATS_TEST_DSN to a disposable stats_test database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var name string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&name))
	require.Equal(t, "stats_test", name, "only a dedicated disposable statistics database is allowed")
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, db))
	previousTimezone := timezone.Name()
	t.Cleanup(func() { require.NoError(t, timezone.Init(previousTimezone)) })
	require.NoError(t, timezone.Init("UTC"))
	exec := func(query string, args ...any) {
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	user, key, account := newDashboardUsageFixture(t, db)
	exec(`UPDATE usage_dashboard_aggregation_watermark SET history_known_empty_before=NULL,last_aggregated_at='1970-01-01',raw_retained_from='1970-01-01',hourly_retained_from=NULL,daily_retained_from=NULL,dirty_from=NULL,timezone_name=$1`, timezone.Name())
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	insert := func(at time.Time) {
		exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model,input_tokens,output_tokens,total_cost,actual_cost,account_stats_cost,account_rate_multiplier,created_at) VALUES($1,$2,$3,'stats',10,5,1,0.125,2,0.5,$4)`, user, key, account, at)
	}
	insert(start.Add(15 * time.Minute))
	insert(start.Add(time.Hour))
	insert(start.Add(90 * time.Minute))
	agg := newDashboardAggregationRepositoryWithSQL(db)
	agg.clock = func() time.Time { return start.Add(95 * time.Minute) }
	require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, start.Add(95*time.Minute)))
	var watermark time.Time
	require.NoError(t, db.QueryRow(`SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark`).Scan(&watermark))
	require.True(t, watermark.Equal(start.Add(time.Hour)))
	repo := &usageLogRepository{sql: db}
	assertTrend := func(from, to time.Time, granularity string, want int64) {
		t.Helper()
		points, err := repo.getUsageTrendFromAggregates(ctx, from, to, granularity)
		require.NoError(t, err)
		var count int64
		var amount float64
		for _, point := range points {
			count += point.Requests
			amount += point.ActualCost
		}
		require.Equal(t, want, count)
		require.Equal(t, float64(want)*0.125, amount)
	}
	assertTrend(start, start.Add(95*time.Minute), "hour", 3)
	assertTrend(start.Add(20*time.Minute), start.Add(80*time.Minute), "hour", 1)
	assertTrend(start, start.Add(95*time.Minute), "day", 3)
	require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, start.Add(95*time.Minute)))
	assertTrend(start, start.Add(95*time.Minute), "hour", 3)
	// Exercise real list/CSV rows and association hydration on a one-connection
	// pool: the read transaction must not acquire another connection via Ent.
	db.SetMaxOpenConns(1)
	listEnd := start.Add(95 * time.Minute)
	listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
	listed, _, listErr := repo.ListWithFilters(listCtx, pagination.PaginationParams{Page: 1, PageSize: 10}, UsageLogFilters{StartTime: &start, EndTime: &listEnd})
	listCancel()
	require.NoError(t, listErr)
	require.Len(t, listed, 3)
	db.SetMaxOpenConns(8)
	// Full-day recomputation remains distinct from settlement/funds.
	agg.clock = func() time.Time { return start.AddDate(0, 0, 1) }
	require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, start.AddDate(0, 0, 1)))
	assertTrend(start, start.AddDate(0, 0, 1), "day", 3)
	var balance float64
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, user).Scan(&balance))
	require.Equal(t, 100.0, balance)

	stats := &DashboardStats{}
	require.NoError(t, repo.fillDashboardClosedAndRaw(ctx, stats, start, start.AddDate(0, 0, 1)))
	require.Equal(t, int64(3), stats.TotalRequests)
	require.Equal(t, 0.375, stats.TotalActualCost)
	require.Equal(t, 3.0, stats.TotalAccountCost)
	ranged, err := repo.GetDashboardStatsWithRange(ctx, start, start.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.NotNil(t, ranged.Coverage, "raw summary fallback must also expose coverage")
	require.Equal(t, int64(3), ranged.TotalRequests)

	// A settled late receipt inserts canonical usage at occurrence time, after
	// the day was published. Reads must repair through raw data immediately.
	late := start.Add(2 * time.Hour)
	settled := start.AddDate(0, 0, 1).Add(time.Hour)
	var logID int64
	require.NoError(t, db.QueryRow(`INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,input_tokens,output_tokens,total_cost,actual_cost,created_at) VALUES($1,$2,$3,'late-receipt','stats',10,5,1,0.125,$4) RETURNING id`, user, key, account, late).Scan(&logID))
	exec(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,usage_log_id,settled_at) VALUES('late-receipt',$1,$2,$3,'late','{}','{}','priced','{}','resolved','complete','settled',$4,$5)`, key, user, account, logID, settled)
	assertTrend(start, start.AddDate(0, 0, 1), "hour", 4)
	assertTrend(start, start.AddDate(0, 0, 1), "day", 4)
	var dirty time.Time
	require.NoError(t, db.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
	require.True(t, late.Equal(dirty))
	logs, err := repo.queryUsageLogs(ctx, "SELECT "+usageLogSelectColumns+" FROM usage_logs WHERE id=$1", logID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	financialState := func() string {
		var state string
		require.NoError(t, db.QueryRow(`SELECT json_build_object('balance',(SELECT balance FROM users WHERE id=$1),'quota',(SELECT quota_used FROM api_keys WHERE id=$2),'receipt',(SELECT row_to_json(r) FROM usage_settlement_receipts r WHERE request_id='late-receipt'))::text`, user, key).Scan(&state))
		return state
	}
	before := financialState()
	require.NoError(t, agg.AggregateRangeAndPublish(ctx, start.Add(20*time.Hour), start.AddDate(0, 0, 1)))
	assertTrend(start, start.AddDate(0, 0, 1), "day", 4)
	require.Equal(t, before, financialState(), "stats replay must never settle funds or mutate receipts/quota")
	// A rolled-back correction cannot invalidate a committed projection.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.Exec(`UPDATE usage_logs SET actual_cost=9 WHERE id=$1`, logID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	var nullDirty sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&nullDirty))
	require.False(t, nullDirty.Valid)
	assertTrend(start, start.AddDate(0, 0, 1), "hour", 4)
	// A publisher waits for a writer holding the shared lock order, then sees
	// the committed correction rather than clearing its invalidation unseen.
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.Exec(`UPDATE usage_logs SET input_tokens=99 WHERE id=$1`, logID)
	require.NoError(t, err)
	published := make(chan error, 1)
	go func() {
		published <- agg.AggregateRangeAndPublish(ctx, start.Add(20*time.Hour), start.AddDate(0, 0, 1))
	}()
	select {
	case err := <-published:
		t.Fatalf("publication passed uncommitted writer: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	select {
	case err := <-published:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not finish")
	}
	var inputs int64
	require.NoError(t, db.QueryRow(`SELECT SUM(input_tokens) FROM usage_dashboard_daily`).Scan(&inputs))
	require.Equal(t, int64(129), inputs)
	require.Equal(t, before, financialState())

	// Force a real mid-publication failure after hourly replacement. PostgreSQL
	// must restore both projection tables and retain the dirty repair boundary.
	exec(`UPDATE usage_logs SET input_tokens=100 WHERE id=$1`, logID)
	exec(`CREATE OR REPLACE FUNCTION stats_test_fail_daily() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'publication failure'; END $$`)
	exec(`CREATE TRIGGER stats_test_fail_daily BEFORE INSERT ON usage_dashboard_daily FOR EACH ROW EXECUTE FUNCTION stats_test_fail_daily()`)
	err = agg.AggregateRangeAndPublish(ctx, start, start.AddDate(0, 0, 1))
	exec(`DROP TRIGGER stats_test_fail_daily ON usage_dashboard_daily`)
	exec(`DROP FUNCTION stats_test_fail_daily()`)
	require.ErrorContains(t, err, "publication failure")
	require.NoError(t, db.QueryRow(`SELECT SUM(input_tokens) FROM usage_dashboard_hourly`).Scan(&inputs))
	require.Equal(t, int64(129), inputs)
	require.NoError(t, db.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&nullDirty))
	require.True(t, nullDirty.Valid)
	exec(`UPDATE usage_logs SET input_tokens=99 WHERE id=$1`, logID)
	require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, start.AddDate(0, 0, 1)))
	// A retention transaction that aborts cannot publish a floor or lose rows.
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, lockGroupUsageRollupState(ctx, tx))
	require.NoError(t, prepareDashboardRawRetention(ctx, tx, start.AddDate(0, 0, 1)))
	_, err = tx.Exec(`DELETE FROM usage_logs WHERE created_at<$1`, start.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	var retained time.Time
	require.NoError(t, db.QueryRow(`SELECT raw_retained_from FROM usage_dashboard_aggregation_watermark`).Scan(&retained))
	require.True(t, retained.Equal(time.Unix(0, 0)))
	assertTrend(start, start.AddDate(0, 0, 1), "day", 4)
	// Retention preserves verified aggregate history. Partial/raw-only reports
	// and backfill fail when the original data/archive is no longer available.
	cutoff := start.AddDate(0, 0, 1)
	require.NoError(t, agg.cleanupUsageLogsBatches(ctx, cutoff))
	assertTrend(start, cutoff, "day", 4)
	assertTrend(start, cutoff, "hour", 4)
	_, err = repo.getUsageTrendFromAggregates(ctx, start.Add(time.Minute), cutoff, "hour")
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	_, err = repo.GetStatsWithFilters(ctx, UsageLogFilters{StartTime: &start, EndTime: &cutoff})
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	_, _, err = repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, UsageLogFilters{StartTime: &start, EndTime: &cutoff})
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	_, _, err = repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, UsageLogFilters{})
	require.NoError(t, err, "omitting StartTime keeps the existing retained-window list contract")
	require.ErrorIs(t, agg.AggregateRange(ctx, start, cutoff), ErrUsageHistoryUnavailable)
	assertTrend(start, cutoff, "day", 4)
	require.NoError(t, repo.fillDashboardClosedAndRaw(ctx, stats, cutoff, cutoff.Add(time.Hour)))
	require.Equal(t, int64(4), stats.TotalRequests)
	require.False(t, stats.Coverage.Complete)
	require.NoError(t, agg.CleanupAggregates(ctx, cutoff, start))
	_, err = repo.getUsageTrendFromAggregates(ctx, start, cutoff, "hour")
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	assertTrend(start, cutoff, "day", 4)
	insert(late) // too old to repair from the surviving raw slice
	_, err = repo.getUsageTrendFromAggregates(ctx, start, cutoff, "day")
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	require.ErrorIs(t, agg.AggregateRangeAndPublish(ctx, start, cutoff), ErrUsageHistoryUnavailable)
	require.Equal(t, before, financialState())
	require.NoError(t, timezone.Init("America/New_York"))
	_, err = repo.getUsageTrendFromAggregates(ctx, start, cutoff, "day")
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	require.NoError(t, timezone.Init("UTC"))
	// Repeated and skipped wall-clock hours remain distinct physical buckets.
	for _, date := range []string{"2026-03-08", "2026-11-01"} {
		location, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		day, err := time.ParseInLocation("2006-01-02", date, location)
		require.NoError(t, err)
		require.NoError(t, timezone.Init("America/New_York"))
		next := day.AddDate(0, 0, 1)
		hours := int64(next.Sub(day) / time.Hour)
		require.Contains(t, []int64{23, 25}, hours)
		exec(`TRUNCATE usage_logs, usage_dashboard_hourly, usage_dashboard_daily, usage_dashboard_hourly_users, usage_dashboard_daily_users CASCADE`)
		exec(`UPDATE usage_dashboard_aggregation_watermark SET history_known_empty_before=NULL,last_aggregated_at='1970-01-01',raw_retained_from='1970-01-01',hourly_retained_from=NULL,daily_retained_from=NULL,dirty_from=NULL,timezone_name=$1`, timezone.Name())
		for at := day; at.Before(next); at = at.Add(time.Hour) {
			insert(at.Add(10 * time.Minute))
		}
		agg.clock = func() time.Time { return next }
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, day, next))
		points, err := repo.getUsageTrendFromAggregates(ctx, day, next, "hour")
		require.NoError(t, err)
		require.Len(t, points, int(hours))
		assertTrend(day, next, "hour", hours)
		assertTrend(day, next, "day", hours)
		// Repair after the final event of a bucket disappears, then replay repair.
		exec(`DELETE FROM usage_logs WHERE created_at >= $1 AND created_at < $2`, day, day.Add(time.Hour))
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, day, next))
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, day, next))
		assertTrend(day, next, "hour", hours-1)
		assertTrend(day, next, "day", hours-1)
	}
}
