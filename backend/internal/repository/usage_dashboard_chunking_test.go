//go:build unit

package repository

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestUsageDashboardChunkingCalendarWindow(t *testing.T) {
	previous := timezone.Name()
	t.Cleanup(func() { require.NoError(t, timezone.Init(previous)) })
	for _, date := range []string{"2026-03-08", "2026-11-01"} {
		require.NoError(t, timezone.Init("America/New_York"))
		start, err := time.ParseInLocation("2006-01-02", date, timezone.Location())
		require.NoError(t, err)
		c := dashboardCoverage{raw: start, published: start.AddDate(0, 1, 0), hourly: sql.NullTime{Time: start, Valid: true}, dirty: sql.NullTime{Time: start.Add(time.Hour), Valid: true}, zone: timezone.Name(), version: dashboardProjectionVersion}
		first, end := dashboardPublicationWindow(c, start.AddDate(0, 0, 20), start.AddDate(0, 0, 30))
		require.True(t, first.Equal(start))
		require.True(t, end.Equal(start.AddDate(0, 0, 1)))
		require.Contains(t, []time.Duration{23 * time.Hour, 25 * time.Hour}, end.Sub(first))
	}
}

func TestUsageDashboardChunkingPostgres(t *testing.T) {
	dsn := os.Getenv("USAGE_STATS_TEST_DSN")
	if dsn == "" {
		t.Skip("set USAGE_STATS_TEST_DSN to a disposable statistics database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	var name string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&name))
	require.Equal(t, "stats_test", name)
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, db))
	previous := timezone.Name()
	t.Cleanup(func() { require.NoError(t, timezone.Init(previous)) })
	require.NoError(t, timezone.Init("UTC"))
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	exec(`TRUNCATE usage_settlement_receipts, users, accounts, api_keys, usage_logs, usage_dashboard_hourly, usage_dashboard_daily, usage_dashboard_hourly_users, usage_dashboard_daily_users RESTART IDENTITY CASCADE`)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 3)
	exec(`UPDATE usage_dashboard_aggregation_watermark SET history_known_empty_before=NULL,last_aggregated_at='1970-01-01',raw_retained_from=$1,hourly_retained_from=NULL,daily_retained_from=NULL,dirty_from=NULL,timezone_name='UTC',projection_version=2`, start)
	var user, key, account int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES('chunks@example.test','test',100) RETURNING id`).Scan(&user))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,'chunks','chunks') RETURNING id`, user).Scan(&key))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('chunks','openai','apikey') RETURNING id`).Scan(&account))
	for day := 0; day < 3; day++ {
		exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost,account_stats_cost,account_rate_multiplier,created_at)
 VALUES($1,$2,$3,'chunks',11,7,3,5,0.123456,0.012345,0.234567,0.5,$4)`, user, key, account, start.AddDate(0, 0, day).Add(time.Hour))
	}
	exec(`CREATE TABLE stats_chunk_audit(transaction_id bigint, bucket_date date)`)
	exec(`CREATE FUNCTION stats_chunk_audit_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO stats_chunk_audit VALUES(txid_current(),NEW.bucket_date); RETURN NEW; END $$`)
	exec(`CREATE TRIGGER stats_chunk_audit_write AFTER INSERT ON usage_dashboard_daily FOR EACH ROW EXECUTE FUNCTION stats_chunk_audit_write()`)
	t.Cleanup(func() {
		db.Exec(`DROP TRIGGER IF EXISTS stats_chunk_audit_write ON usage_dashboard_daily`)
		db.Exec(`DROP FUNCTION IF EXISTS stats_chunk_audit_write()`)
		db.Exec(`DROP TABLE IF EXISTS stats_chunk_audit`)
	})
	agg := newDashboardAggregationRepositoryWithSQL(db)
	agg.clock = func() time.Time { return end.AddDate(0, 0, 20) }
	read := func() dashboardCoverage {
		c, err := readDashboardCoverage(ctx, db, false)
		require.NoError(t, err)
		return c
	}

	t.Run("failure_commits_only_completed_days", func(t *testing.T) {
		exec(`CREATE FUNCTION stats_chunk_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.bucket_date='2026-03-02'::date THEN RAISE EXCEPTION 'second day failed'; END IF; RETURN NEW; END $$`)
		exec(`CREATE TRIGGER stats_chunk_fail BEFORE INSERT ON usage_dashboard_daily FOR EACH ROW EXECUTE FUNCTION stats_chunk_fail()`)
		err := agg.AggregateRangeAndPublish(ctx, start, end)
		exec(`DROP TRIGGER stats_chunk_fail ON usage_dashboard_daily`)
		exec(`DROP FUNCTION stats_chunk_fail()`)
		require.ErrorContains(t, err, "second day failed")
		require.True(t, read().published.Equal(start.AddDate(0, 0, 1)))
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_dashboard_daily`).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("mid_chunk_cancellation_preserves_previous_commit", func(t *testing.T) {
		exec(`CREATE FUNCTION stats_chunk_cancel() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.bucket_date='2026-03-02'::date THEN PERFORM pg_sleep(1); END IF; RETURN NEW; END $$`)
		exec(`CREATE TRIGGER stats_chunk_cancel BEFORE INSERT ON usage_dashboard_daily FOR EACH ROW EXECUTE FUNCTION stats_chunk_cancel()`)
		deadline, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		err := agg.AggregateRangeAndPublish(deadline, start.AddDate(0, 0, 1), end)
		cancel()
		exec(`DROP TRIGGER stats_chunk_cancel ON usage_dashboard_daily`)
		exec(`DROP FUNCTION stats_chunk_cancel()`)
		require.Error(t, err)
		require.ErrorIs(t, deadline.Err(), context.DeadlineExceeded)
		require.True(t, read().published.Equal(start.AddDate(0, 0, 1)))
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_dashboard_hourly WHERE bucket_start >= $1`, start.AddDate(0, 0, 1)).Scan(&count))
		require.Zero(t, count, "the cancelled day's hourly replacement must also roll back")
	})
	t.Run("resume_rechecks_database_and_separates_transactions", func(t *testing.T) {
		// Ask for only the last day: the database cursor closes the missing day
		// first, without putting both days in a single transaction.
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, start.AddDate(0, 0, 2), end))
		require.True(t, read().published.Equal(end))
		var transactions, largest int
		require.NoError(t, db.QueryRow(`SELECT COUNT(DISTINCT transaction_id) FROM stats_chunk_audit`).Scan(&transactions))
		require.Equal(t, 3, transactions)
		require.NoError(t, db.QueryRow(`SELECT MAX(days) FROM (SELECT COUNT(DISTINCT bucket_date) days FROM stats_chunk_audit GROUP BY transaction_id) counted`).Scan(&largest))
		require.Equal(t, 1, largest)
		var equal bool
		require.NoError(t, db.QueryRow(`SELECT (SELECT SUM(actual_cost) FROM usage_dashboard_daily)=(SELECT SUM(actual_cost) FROM usage_logs)
 AND (SELECT SUM(total_cost) FROM usage_dashboard_daily)=(SELECT SUM(total_cost) FROM usage_logs)
 AND (SELECT SUM(account_cost) FROM usage_dashboard_daily)=(SELECT SUM(account_stats_cost*account_rate_multiplier) FROM usage_logs)
 AND (SELECT SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens) FROM usage_dashboard_daily)=(SELECT SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens) FROM usage_logs)`).Scan(&equal))
		require.True(t, equal, "compare PostgreSQL numeric values without UI rounding")
	})
	t.Run("dirty_suffix_survives_old_caller_and_late_writer", func(t *testing.T) {
		exec(`UPDATE usage_logs SET input_tokens=input_tokens+1`)
		next, err := agg.publishDashboardDay(ctx, db, end.AddDate(0, 0, 10), end)
		require.NoError(t, err)
		require.True(t, next.Equal(start.AddDate(0, 0, 1)))
		c := read()
		require.True(t, c.published.Equal(end))
		require.True(t, c.dirty.Valid)
		require.True(t, c.dirty.Time.Equal(next))
		// An old caller ending at the already repaired boundary cannot erase
		// the remainder, and a later correction moves dirty back again.
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, next))
		require.True(t, read().dirty.Time.Equal(next))
		exec(`UPDATE usage_logs SET output_tokens=output_tokens+1 WHERE created_at<$1`, next)
		require.True(t, read().dirty.Time.Before(next))
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, next, end))
		require.False(t, read().dirty.Valid)
	})
	t.Run("cancellation_and_replay", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		require.ErrorIs(t, agg.AggregateRangeAndPublish(cancelled, start, end), context.Canceled)
		require.True(t, read().published.Equal(end))
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, end))
		var count int64
		require.NoError(t, db.QueryRow(`SELECT SUM(total_requests) FROM usage_dashboard_daily`).Scan(&count))
		require.Equal(t, int64(3), count)
	})
	t.Run("historical_recompute_cannot_publish_across_gap", func(t *testing.T) {
		require.NoError(t, agg.RecomputeRange(ctx, end.AddDate(0, 0, 5), end.AddDate(0, 0, 8)))
		require.True(t, read().published.Equal(end))
	})
	t.Run("old_identity_cannot_relabel_unrebuilt_buckets", func(t *testing.T) {
		exec(`UPDATE usage_dashboard_aggregation_watermark SET projection_version=1`)
		next, err := agg.publishDashboardDay(ctx, db, end, end.AddDate(0, 0, 20))
		require.NoError(t, err)
		require.True(t, next.Equal(start.AddDate(0, 0, 1)))
		c := read()
		require.True(t, c.published.Equal(next))
		require.Equal(t, dashboardProjectionVersion, c.version)
		require.NoError(t, c.requireTrend(start, end, "day")) // remaining days are raw
	})
	t.Run("partial_raw_floor_and_summary_filter", func(t *testing.T) {
		floor := start.Add(12 * time.Hour)
		exec(`UPDATE usage_dashboard_aggregation_watermark SET raw_retained_from=$1,last_aggregated_at='1970-01-01',hourly_retained_from=NULL,daily_retained_from=NULL,timezone_name='',dirty_from=NULL`, floor)
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, start, end))
		c := read()
		require.True(t, c.daily.Time.Equal(start.AddDate(0, 0, 1)))
		repo := &usageLogRepository{sql: db}
		stats, err := repo.GetUserDashboardStats(ctx, user)
		require.NoError(t, err)
		require.Equal(t, int64(2), stats.TotalRequests)
		require.False(t, stats.Coverage.Complete)
		keyStats, err := repo.GetAPIKeyDashboardStats(ctx, key)
		require.NoError(t, err)
		require.Equal(t, int64(2), keyStats.TotalRequests)
	})
	t.Run("writer_lock_pressure_observation", func(t *testing.T) {
		exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model,input_tokens,total_cost,actual_cost,created_at)
 SELECT $1,$2,$3,'load',10,0.001,0.0005,$4::timestamptz+(sequence%172800)*INTERVAL '1 second' FROM generate_series(1,40000) sequence`, user, key, account, start.AddDate(0, 0, 1))
		var before int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_logs`).Scan(&before))
		writeSamples := func() ([]time.Duration, error) {
			samples := make([]time.Duration, 0, 100)
			for attempt := 0; attempt < 100; attempt++ {
				began := time.Now()
				_, err := db.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model,created_at) VALUES($1,$2,$3,'writer',NOW())`, user, key, account)
				if err != nil {
					return nil, err
				}
				samples = append(samples, time.Since(began))
			}
			return samples, nil
		}
		baseline, err := writeSamples()
		require.NoError(t, err)
		rebuilding := make(chan error, 1)
		began := time.Now()
		go func() { rebuilding <- agg.AggregateRangeAndPublish(ctx, start.AddDate(0, 0, 1), end) }()
		loaded, err := writeSamples()
		require.NoError(t, err)
		require.NoError(t, <-rebuilding)
		rebuildDuration := time.Since(began)
		var after int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_logs`).Scan(&after))
		require.Equal(t, before+200, after, "no writes lost while publication holds the consistency locks")
		for label, samples := range map[string][]time.Duration{"without_rebuild": baseline, "with_rebuild": loaded} {
			sort.Slice(samples, func(left, right int) bool { return samples[left] < samples[right] })
			t.Logf("%s: writes=%d p50=%s p95=%s p99=%s", label, len(samples), samples[49], samples[94], samples[98])
		}
		t.Logf("concurrent rebuild and writer elapsed=%s; 40000 historical rows, two calendar-day transactions; this fixture is not the release SLO benchmark", rebuildDuration)
	})

	t.Run("fractional_midnight_keeps_unbuilt_hour_seams_raw", func(t *testing.T) {
		require.NoError(t, timezone.Init("Asia/Kolkata"))
		day := time.Date(2026, 3, 1, 0, 0, 0, 0, timezone.Location())
		finish := day.AddDate(0, 0, 4)
		exec(`UPDATE usage_dashboard_aggregation_watermark SET raw_retained_from=$1,last_aggregated_at='1970-01-01',hourly_retained_from=NULL,daily_retained_from=NULL,timezone_name='',dirty_from=NULL`, day)
		require.NoError(t, agg.AggregateRangeAndPublish(ctx, day, finish.Add(time.Hour)))
		c := read()
		require.True(t, c.daily.Valid)
		require.False(t, c.hourly.Valid, "hour seams are not continuously rebuilt by calendar-day chunks")
		repo := &usageLogRepository{sql: db}
		for _, granularity := range []string{"hour", "day"} {
			points, err := repo.getUsageTrendFromAggregates(ctx, day, finish, granularity)
			require.NoError(t, err)
			var want, got int64
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE created_at>=$1 AND created_at<$2`, day, finish).Scan(&want))
			for _, point := range points {
				got += point.Requests
			}
			require.Equal(t, want, got)
		}
		require.NoError(t, timezone.Init("UTC"))
	})

}
