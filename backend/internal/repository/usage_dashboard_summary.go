package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func (r *usageLogRepository) fillDashboardClosedAndRaw(ctx context.Context, stats *DashboardStats, today, now time.Time) error {
	if needsUsageSnapshot(r) {
		_, err := usageReadSnapshot(ctx, r, func(snapshot *usageLogRepository, c dashboardCoverage) (bool, error) {
			stats.Coverage = c.metadata()
			return true, snapshot.fillDashboardClosedAndRaw(ctx, stats, today, now)
		})
		return err
	}
	const query = `
 WITH bounds AS (
  SELECT raw_retained_from,COALESCE(daily_retained_from,$2::timestamptz) AS first_bucket,
  date_trunc('day',LEAST(CASE WHEN timezone_name=$3 AND projection_version=2 THEN
  LEAST(last_aggregated_at,COALESCE(dirty_from,last_aggregated_at)) ELSE TIMESTAMPTZ '1970-01-01' END,$2::timestamptz) AT TIME ZONE $3) AT TIME ZONE $3 AS closed_before
  FROM usage_dashboard_aggregation_watermark WHERE id=1
 ), combined AS (
  SELECT bucket_date::timestamp AT TIME ZONE $3 AS occurred_at,total_requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost,account_cost,total_duration_ms
  FROM usage_dashboard_daily,bounds WHERE bucket_date::timestamp AT TIME ZONE $3 >= first_bucket AND bucket_date::timestamp AT TIME ZONE $3 < closed_before
  UNION ALL
  SELECT created_at,1,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost,
    COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1),COALESCE(duration_ms,0)
  FROM usage_logs,bounds WHERE created_at >= raw_retained_from AND (created_at < first_bucket OR created_at >= closed_before) AND created_at < $2
 )
 SELECT COALESCE(SUM(total_requests),0),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),
  COALESCE(SUM(cache_creation_tokens),0),COALESCE(SUM(cache_read_tokens),0),COALESCE(SUM(total_cost),0),COALESCE(SUM(actual_cost),0),COALESCE(SUM(account_cost),0),COALESCE(SUM(total_duration_ms),0),
  COALESCE(SUM(total_requests) FILTER (WHERE occurred_at >= $1),0),COALESCE(SUM(input_tokens) FILTER (WHERE occurred_at >= $1),0),COALESCE(SUM(output_tokens) FILTER (WHERE occurred_at >= $1),0),
  COALESCE(SUM(cache_creation_tokens) FILTER (WHERE occurred_at >= $1),0),COALESCE(SUM(cache_read_tokens) FILTER (WHERE occurred_at >= $1),0),
  COALESCE(SUM(total_cost) FILTER (WHERE occurred_at >= $1),0),COALESCE(SUM(actual_cost) FILTER (WHERE occurred_at >= $1),0),COALESCE(SUM(account_cost) FILTER (WHERE occurred_at >= $1),0),
  (SELECT COUNT(DISTINCT user_id) FROM usage_logs WHERE created_at >= GREATEST($1::timestamptz,(SELECT raw_retained_from FROM bounds)) AND created_at < $2),
  (SELECT COUNT(DISTINCT user_id) FROM usage_logs WHERE created_at >= GREATEST($4::timestamptz,(SELECT raw_retained_from FROM bounds)) AND created_at < $2)
 FROM combined`
	var duration int64
	err := scanSingleRow(ctx, r.sql, query, []any{today, now, timezone.Name(), now.UTC().Truncate(time.Hour)},
		&stats.TotalRequests, &stats.TotalInputTokens, &stats.TotalOutputTokens, &stats.TotalCacheCreationTokens, &stats.TotalCacheReadTokens, &stats.TotalCost, &stats.TotalActualCost, &stats.TotalAccountCost, &duration,
		&stats.TodayRequests, &stats.TodayInputTokens, &stats.TodayOutputTokens, &stats.TodayCacheCreationTokens, &stats.TodayCacheReadTokens, &stats.TodayCost, &stats.TodayActualCost, &stats.TodayAccountCost, &stats.ActiveUsers, &stats.HourlyActiveUsers)
	if err != nil {
		return err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs = float64(duration) / float64(stats.TotalRequests)
	}
	return nil
}
