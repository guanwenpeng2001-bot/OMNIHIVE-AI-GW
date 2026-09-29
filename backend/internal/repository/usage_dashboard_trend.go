package repository

import (
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/lib/pq"
)

// Hour keys identify real instants (including a DST fold); coarser keys use the
// configured business calendar. Never rely on a pooled connection's timezone.
func usageTrendDateSQL(column, granularity string) string {
	zone, format := timezone.Name(), safeDateFormat(granularity)
	if granularity == "hour" {
		zone, format = "UTC", `YYYY-MM-DD HH24:00"Z"`
	}
	return fmt.Sprintf("TO_CHAR(%s AT TIME ZONE %s, %s)", column, pq.QuoteLiteral(zone), pq.QuoteLiteral(format))
}

// A single statement pins the watermark, projection and raw tail to the same
// MVCC snapshot. Partial boundary buckets always come from raw data.
func dashboardTrendQuery(granularity string) string {
	table, bucket, unit := "usage_dashboard_daily", "bucket_date::timestamp AT TIME ZONE $3", "day"
	if granularity == "hour" {
		table, bucket, unit = "usage_dashboard_hourly", "bucket_start", "hour"
	}
	boundaryZone := "$3"
	if granularity == "hour" {
		boundaryZone = "'UTC'"
	}
	return fmt.Sprintf(`
 WITH state AS (
   SELECT CASE WHEN timezone_name=$3 AND projection_version=2
     THEN LEAST(last_aggregated_at,COALESCE(dirty_from,last_aggregated_at))
     ELSE TIMESTAMPTZ '1970-01-01' END AS watermark,
     COALESCE(%[6]s_retained_from,$2::timestamptz) AS retained_from, history_known_empty_before
   FROM usage_dashboard_aggregation_watermark WHERE id=1
 ), bounds AS (
   SELECT GREATEST(retained_from,CASE WHEN $1::timestamptz <= history_known_empty_before
     THEN date_trunc('%[1]s',history_known_empty_before AT TIME ZONE %[5]s) AT TIME ZONE %[5]s
     WHEN $1::timestamptz = date_trunc('%[1]s',$1::timestamptz AT TIME ZONE %[5]s) AT TIME ZONE %[5]s
     THEN $1::timestamptz
     ELSE (date_trunc('%[1]s',$1::timestamptz AT TIME ZONE %[5]s) + INTERVAL '1 %[1]s') AT TIME ZONE %[5]s END) AS first_bucket,
     date_trunc('%[1]s',LEAST(watermark,$2::timestamptz) AT TIME ZONE %[5]s) AT TIME ZONE %[5]s AS closed_before
   FROM state
 ), combined AS (
   SELECT %[2]s AS occurred_at, total_requests AS requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost
   FROM %[3]s, bounds WHERE %[2]s >= first_bucket AND %[2]s < closed_before
   UNION ALL
   SELECT created_at,1,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost
   FROM usage_logs, bounds
   WHERE created_at >= $1 AND created_at < $2 AND (created_at < first_bucket OR created_at >= closed_before)
 )
 SELECT %[4]s AS date,
   SUM(requests),SUM(input_tokens),SUM(output_tokens),SUM(cache_creation_tokens),SUM(cache_read_tokens),
   SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),SUM(total_cost),SUM(actual_cost)
 FROM combined GROUP BY 1 ORDER BY 1`, unit, bucket, table, usageTrendDateSQL("occurred_at", granularity), boundaryZone, map[string]string{"hour": "hourly", "day": "daily"}[unit])
}
