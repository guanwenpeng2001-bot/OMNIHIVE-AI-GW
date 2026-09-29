//go:build unit

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageDashboardKnownOriginPostgres(test *testing.T) {
	dsn := os.Getenv("USAGE_STATS_TEST_DSN")
	if dsn == "" {
		test.Skip("USAGE_STATS_TEST_DSN must name disposable stats_test")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(test, err)
	defer database.Close()
	var databaseName string
	require.NoError(test, database.QueryRow(`SELECT current_database()`).Scan(&databaseName))
	require.Equal(test, "stats_test", databaseName)
	var connections int
	require.NoError(test, database.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname IN ('stats_test','job_test') AND pid<>pg_backend_pid() AND backend_type='client backend'`).Scan(&connections))
	require.Zero(test, connections)
	ctx := context.Background()
	require.NoError(test, ApplyMigrations(ctx, database))
	for _, calendar := range []struct{ name, zone, day string }{
		{"UTC", "UTC", "2026-03-08"},
		{"Asia/Shanghai", "Asia/Shanghai", "2026-03-08"},
		{"Asia/Kolkata", "Asia/Kolkata", "2026-03-08"},
		{"Asia/Kathmandu", "Asia/Kathmandu", "2026-03-08"},
		{"DST_forward", "America/New_York", "2026-03-08"},
		{"DST_fold", "America/New_York", "2025-11-02"},
	} {
		test.Run(calendar.name, func(test *testing.T) {
			previous := timezone.Name()
			require.NoError(test, timezone.Init(calendar.zone))
			test.Cleanup(func() { require.NoError(test, timezone.Init(previous)) })
			execute := func(statement string, arguments ...any) {
				test.Helper()
				_, err := database.Exec(statement, arguments...)
				require.NoError(test, err)
			}
			execute(`TRUNCATE usage_settlement_receipts,users,accounts,api_keys,usage_logs,usage_dashboard_hourly,usage_dashboard_daily,usage_dashboard_hourly_users,usage_dashboard_daily_users RESTART IDENTITY CASCADE`)
			day, err := time.ParseInLocation("2006-01-02", calendar.day, timezone.Location())
			require.NoError(test, err)
			origin := day.Add(10*time.Hour + 15*time.Minute)
			end := day.AddDate(0, 0, 1)
			execute(`UPDATE usage_dashboard_aggregation_watermark SET last_aggregated_at='1970-01-01',raw_retained_from=$1,hourly_retained_from=NULL,daily_retained_from=NULL,dirty_from=NULL,timezone_name='',history_known_empty_before=NULL`, origin)
			repository := &usageLogRepository{sql: database}
			unknownRevision, err := repository.DashboardCoverageRevision(ctx)
			require.NoError(test, err)
			execute(`UPDATE usage_dashboard_aggregation_watermark SET history_known_empty_before=$1`, origin)
			declaredRevision, err := repository.DashboardCoverageRevision(ctx)
			require.NoError(test, err)
			require.NotEqual(test, unknownRevision, declaredRevision)
			execute(`INSERT INTO users(id,email,password_hash) VALUES(1,'origin@example.test','synthetic')`)
			execute(`INSERT INTO api_keys(id,user_id,key,name) VALUES(1,1,'synthetic-origin','origin')`)
			execute(`INSERT INTO accounts(id,name,platform,type) VALUES(1,'origin','openai','apikey')`)
			execute(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model,input_tokens,total_cost,actual_cost,created_at) VALUES(1,1,1,'origin',10,2,1,$1),(1,1,1,'origin',10,2,1,$2)`, origin, origin.Add(time.Minute))
			for _, scenario := range []struct {
				name  string
				end   time.Time
				count int64
			}{{"empty_prefix", origin, 0}, {"cross_origin", end, 2}} {
				test.Run(scenario.name, func(test *testing.T) {
					filters := UsageLogFilters{StartTime: &day, EndTime: &scenario.end}
					stats, err := repository.GetStatsWithFilters(ctx, filters)
					require.NoError(test, err)
					require.Equal(test, scenario.count, stats.TotalRequests)
					listed, _, err := repository.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, filters)
					require.NoError(test, err)
					require.Len(test, listed, int(scenario.count))
					exported := 0
					require.NoError(test, repository.StreamUsageLogsWithFilters(ctx, filters, nil, 1, func(rows []service.UsageLog) error { exported += len(rows); return nil }))
					require.EqualValues(test, scenario.count, exported)
					ranged, err := repository.GetDashboardStatsWithRange(ctx, day, scenario.end)
					require.NoError(test, err)
					require.Equal(test, scenario.count, ranged.TotalRequests)
					require.True(test, ranged.Coverage.Complete)
				})
			}
			userStats, err := repository.GetUserDashboardStats(ctx, 1)
			require.NoError(test, err)
			require.True(test, userStats.Coverage.Complete)
			keyStats, err := repository.GetAPIKeyDashboardStats(ctx, 1)
			require.NoError(test, err)
			require.True(test, keyStats.Coverage.Complete)
			aggregator := newDashboardAggregationRepositoryWithSQL(database)
			publicationEnd := bucketCeil(end, "hour")
			aggregator.clock = func() time.Time { return publicationEnd }
			require.NoError(test, aggregator.AggregateRangeAndPublish(ctx, time.Unix(0, 0), publicationEnd))
			coverage, err := readDashboardCoverage(ctx, database, false)
			require.NoError(test, err)
			require.True(test, coverage.daily.Time.Equal(day))
			require.NoError(test, aggregator.cleanupUsageLogsBatches(ctx, end))
			for _, granularity := range []string{"day", "hour"} {
				if granularity == "hour" && !coverage.hourly.Valid {
					_, err := repository.getUsageTrendFromAggregates(ctx, origin, end, granularity)
					require.ErrorIs(test, err, ErrUsageHistoryUnavailable, "fractional midnight seams do not invent hourly coverage")
					continue
				}
				for _, start := range []time.Time{day, origin} {
					points, err := repository.getUsageTrendFromAggregates(ctx, start, end, granularity)
					require.NoError(test, err)
					var count int64
					for _, point := range points {
						count += point.Requests
					}
					require.EqualValues(test, 2, count)
				}
				_, err := repository.getUsageTrendFromAggregates(ctx, origin.Add(time.Second), end, granularity)
				require.ErrorIs(test, err, ErrUsageHistoryUnavailable)
				_, err = repository.getUsageTrendFromAggregates(ctx, origin, origin.Add(time.Second), granularity)
				require.ErrorIs(test, err, ErrUsageHistoryUnavailable)
			}
			stats, err := repository.GetDashboardStats(ctx)
			require.NoError(test, err)
			require.True(test, stats.Coverage.Complete)
			require.EqualValues(test, 2, stats.TotalRequests)
			userStats, err = repository.GetUserDashboardStats(ctx, 1)
			require.NoError(test, err)
			require.False(test, userStats.Coverage.Complete)
			keyStats, err = repository.GetAPIKeyDashboardStats(ctx, 1)
			require.NoError(test, err)
			require.False(test, keyStats.Coverage.Complete)
			empty, err := repository.GetStatsWithFilters(ctx, UsageLogFilters{StartTime: &day, EndTime: &origin})
			require.NoError(test, err)
			require.Zero(test, empty.TotalRequests)
			_, err = repository.GetStatsWithFilters(ctx, UsageLogFilters{StartTime: &day, EndTime: &end})
			require.ErrorIs(test, err, ErrUsageHistoryUnavailable)
			beforeRevocation, err := repository.DashboardCoverageRevision(ctx)
			require.NoError(test, err)
			execute(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model,created_at) VALUES(1,1,1,'counterevidence',$1)`, day)
			afterRevocation, err := repository.DashboardCoverageRevision(ctx)
			require.NoError(test, err)
			require.NotEqual(test, beforeRevocation, afterRevocation)
			coverage, err = readDashboardCoverage(ctx, database, false)
			require.NoError(test, err)
			require.False(test, coverage.historyKnownEmptyBefore.Valid)
			_, err = repository.GetStatsWithFilters(ctx, UsageLogFilters{StartTime: &day, EndTime: &origin})
			require.ErrorIs(test, err, ErrUsageHistoryUnavailable)

		})
	}
}
