package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type dashboardAggregationRepository struct {
	sql   sqlExecutor
	clock func() time.Time
}

const usageLogsCleanupBatchSize = 10000
const usageBillingDedupCleanupBatchSize = 10000

// NewDashboardAggregationRepository 创建仪表盘预聚合仓储。
func NewDashboardAggregationRepository(sqlDB *sql.DB) service.DashboardAggregationRepository {
	if sqlDB == nil {
		return nil
	}
	if !isPostgresDriver(sqlDB) {
		log.Printf("[DashboardAggregation] 检测到非 PostgreSQL 驱动，已自动禁用预聚合")
		return nil
	}
	return newDashboardAggregationRepositoryWithSQL(sqlDB)
}

func newDashboardAggregationRepositoryWithSQL(sqlq sqlExecutor) *dashboardAggregationRepository {
	return &dashboardAggregationRepository{sql: sqlq, clock: time.Now}
}

func (r *dashboardAggregationRepository) now() time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now()
}

func isPostgresDriver(db *sql.DB) bool {
	if db == nil {
		return false
	}
	_, ok := db.Driver().(*pq.Driver)
	return ok
}

func (r *dashboardAggregationRepository) AggregateRange(ctx context.Context, start, end time.Time) error {
	if r != nil && r.sql != nil && end.After(start) {
		boundary := bucketFloor(start, "day").AddDate(0, 0, 1)
		if end.After(boundary) {
			if _, ok := r.sql.(*sql.DB); !ok {
				return fmt.Errorf("dashboard rebuild transaction exceeds one calendar day")
			}
			for cursor := start; cursor.Before(end); {
				next := bucketFloor(cursor, "day").AddDate(0, 0, 1)
				if next.After(end) {
					next = end
				}
				if err := r.AggregateRange(ctx, cursor, next); err != nil {
					return err
				}
				cursor = next
			}
			return nil
		}
	}

	if r == nil || r.sql == nil {
		return nil
	}
	loc := timezone.Location()
	startLocal := start.In(loc)
	endLocal := end.In(loc)
	if !endLocal.After(startLocal) {
		return nil
	}

	hourStart := bucketCeil(startLocal, "hour")
	hourEnd := endLocal.Truncate(time.Hour)

	dayStart := bucketCeil(startLocal, "day")
	dayEnd := truncateToDay(endLocal)

	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		txRepo := newDashboardAggregationRepositoryWithSQL(tx)
		if err := txRepo.aggregateRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return r.aggregateRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd)
}

func (r *dashboardAggregationRepository) aggregateRangeInTx(ctx context.Context, hourStart, hourEnd, dayStart, dayEnd time.Time) error {
	// Replace the projection, including empty buckets and distinct users. Upsert
	// alone leaves stale buckets behind after a correction or the last deletion.
	return r.recomputeRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd)
}

func (r *dashboardAggregationRepository) RecomputeRange(ctx context.Context, start, end time.Time) error {
	if r != nil && r.sql != nil && end.After(start) {
		boundary := bucketFloor(start, "day").AddDate(0, 0, 1)
		if end.After(boundary) {
			if _, ok := r.sql.(*sql.DB); !ok {
				return fmt.Errorf("dashboard rebuild transaction exceeds one calendar day")
			}
			for cursor := start; cursor.Before(end); {
				next := bucketFloor(cursor, "day").AddDate(0, 0, 1)
				if next.After(end) {
					next = end
				}
				if err := r.RecomputeRange(ctx, cursor, next); err != nil {
					return err
				}
				cursor = next
			}
			return nil
		}
	}

	if r == nil || r.sql == nil {
		return nil
	}
	loc := timezone.Location()
	startLocal := start.In(loc)
	endLocal := end.In(loc)
	if !endLocal.After(startLocal) {
		return nil
	}

	hourStart := bucketCeil(startLocal, "hour")
	hourEnd := endLocal.Truncate(time.Hour)

	dayStart := bucketCeil(startLocal, "day")
	dayEnd := truncateToDay(endLocal)

	// 尽量使用事务保证范围内的一致性（允许在非 *sql.DB 的情况下退化为非事务执行）。
	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := lockGroupUsageRollupState(ctx, tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := invalidateGroupUsageRollupsAt(ctx, tx, start); err != nil {
			_ = tx.Rollback()
			return err
		}
		txRepo := newDashboardAggregationRepositoryWithSQL(tx)
		if err := txRepo.recomputeRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return r.recomputeRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd)
}

func (r *dashboardAggregationRepository) recomputeRangeInTx(ctx context.Context, hourStart, hourEnd, dayStart, dayEnd time.Time) error {
	rebuildStart, rebuildEnd := hourStart, hourEnd
	if dayEnd.After(dayStart) {
		if dayStart.Before(rebuildStart) {
			rebuildStart = dayStart
		}
		if dayEnd.After(rebuildEnd) {
			rebuildEnd = dayEnd
		}
	}
	if err := prepareDashboardRebuild(ctx, r.sql, rebuildStart, rebuildEnd); err != nil {
		return err
	}
	// 先清空范围内桶，再重建（避免仅增量插入导致活跃用户等指标无法回退）。
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly WHERE bucket_start >= $1 AND bucket_start < $2", hourStart, hourEnd); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly_users WHERE bucket_start >= $1 AND bucket_start < $2", hourStart, hourEnd); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily WHERE bucket_date >= $1::date AND bucket_date < $2::date", dayStart, dayEnd); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily_users WHERE bucket_date >= $1::date AND bucket_date < $2::date", dayStart, dayEnd); err != nil {
		return err
	}

	if err := r.insertHourlyActiveUsers(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.insertDailyActiveUsers(ctx, dayStart, dayEnd); err != nil {
		return err
	}
	if err := r.upsertHourlyAggregates(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.upsertDailyAggregates(ctx, dayStart, dayEnd); err != nil {
		return err
	}
	return nil
}

func (r *dashboardAggregationRepository) GetAggregationWatermark(ctx context.Context) (time.Time, error) {
	var ts time.Time
	query := "SELECT CASE WHEN timezone_name=$1 AND projection_version=$2 THEN LEAST(last_aggregated_at, COALESCE(dirty_from,last_aggregated_at)) ELSE TIMESTAMPTZ '1970-01-01' END FROM usage_dashboard_aggregation_watermark WHERE id = 1"
	if err := scanSingleRow(ctx, r.sql, query, []any{timezone.Name(), dashboardProjectionVersion}, &ts); err != nil {
		if err == sql.ErrNoRows {
			return time.Unix(0, 0).UTC(), nil
		}
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

// A cursor can only advance with its data; legacy callers also use publication.
func (r *dashboardAggregationRepository) UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error {
	return r.AggregateRangeAndPublish(ctx, aggregatedAt, aggregatedAt)
}

func (r *dashboardAggregationRepository) CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error {
	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = lockGroupUsageRollupState(ctx, tx); err != nil {
			return err
		}
		if _, err = readDashboardCoverage(ctx, tx, true); err != nil {
			return err
		}
		worker := newDashboardAggregationRepositoryWithSQL(tx)
		if err = worker.CleanupAggregates(ctx, hourlyCutoff, dailyCutoff); err != nil {
			return err
		}
		return tx.Commit()
	}
	hourlyCutoffUTC := hourlyCutoff.UTC()
	dailyCutoffUTC := truncateToDay(dailyCutoff.In(timezone.Location()))
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly WHERE bucket_start < $1", hourlyCutoffUTC); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly_users WHERE bucket_start < $1", hourlyCutoffUTC); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily WHERE bucket_date < $1::date", dailyCutoffUTC); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily_users WHERE bucket_date < $1::date", dailyCutoffUTC); err != nil {
		return err
	}
	_, err := r.sql.ExecContext(ctx, `UPDATE usage_dashboard_aggregation_watermark SET
 hourly_retained_from=CASE WHEN hourly_retained_from IS NOT NULL THEN GREATEST(hourly_retained_from,$1) END,
 daily_retained_from=CASE WHEN daily_retained_from IS NOT NULL THEN GREATEST(daily_retained_from,$2) END, updated_at=NOW() WHERE id=1`, bucketCeil(hourlyCutoffUTC, "hour"), dailyCutoffUTC)
	return err
}

func (r *dashboardAggregationRepository) CleanupUsageLogs(ctx context.Context, cutoff time.Time) error {
	isPartitioned, err := r.isUsageLogsPartitioned(ctx)
	if err != nil {
		return err
	}
	if isPartitioned {
		if err := r.dropUsageLogsPartitions(ctx, cutoff); err != nil {
			return err
		}
	} else if err := r.cleanupUsageLogsBatches(ctx, cutoff); err != nil {
		return err
	}
	return r.SyncGroupUsageRollups(ctx, service.GroupUsageTodayStart(r.now()))
}

func (r *dashboardAggregationRepository) cleanupUsageLogsBatches(ctx context.Context, cutoff time.Time) error {
	db, transactional := r.sql.(*sql.DB)
	for {
		if transactional {
			affected, err := cleanupUsageLogsBatchWithRollupInvalidation(ctx, db, cutoff)
			if err != nil {
				return err
			}
			if affected < usageLogsCleanupBatchSize {
				return nil
			}
			continue
		}

		res, err := r.sql.ExecContext(ctx, `
			WITH victims AS (
				SELECT ctid
				FROM usage_logs
				WHERE created_at < $1
				ORDER BY created_at ASC, id ASC
				LIMIT $2
			)
			DELETE FROM usage_logs
			WHERE ctid IN (SELECT ctid FROM victims)
		`, cutoff.UTC(), usageLogsCleanupBatchSize)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected < usageLogsCleanupBatchSize {
			return nil
		}
	}
}

func cleanupUsageLogsBatchWithRollupInvalidation(ctx context.Context, db *sql.DB, cutoff time.Time) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	rollback := func(err error) (int64, error) {
		_ = tx.Rollback()
		return 0, err
	}

	if err := lockGroupUsageRollupState(ctx, tx); err != nil {
		return rollback(err)
	}
	if err := prepareDashboardRawRetention(ctx, tx, cutoff); err != nil {
		return rollback(err)
	}
	rows, err := tx.QueryContext(ctx, `
		WITH victims AS (
			SELECT ctid
			FROM usage_logs
			WHERE created_at < $1
			ORDER BY created_at ASC, id ASC
			LIMIT $2
		)
		DELETE FROM usage_logs
		WHERE ctid IN (SELECT ctid FROM victims)
		RETURNING created_at
	`, cutoff.UTC(), usageLogsCleanupBatchSize)
	if err != nil {
		return rollback(err)
	}

	var affected int64
	var earliestDeletedAt time.Time
	for rows.Next() {
		var deletedAt time.Time
		if err := rows.Scan(&deletedAt); err != nil {
			_ = rows.Close()
			return rollback(err)
		}
		affected++
		if earliestDeletedAt.IsZero() || deletedAt.Before(earliestDeletedAt) {
			earliestDeletedAt = deletedAt
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return rollback(err)
	}
	if err := rows.Close(); err != nil {
		return rollback(err)
	}
	if affected > 0 {
		if err := invalidateGroupUsageRollupsAt(ctx, tx, earliestDeletedAt); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func (r *dashboardAggregationRepository) CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error {
	for {
		res, err := r.sql.ExecContext(ctx, `
			WITH victims AS (
				SELECT ctid, request_id, api_key_id, request_fingerprint, created_at
				FROM usage_billing_dedup
				WHERE created_at < $1
				LIMIT $2
			), archived AS (
				INSERT INTO usage_billing_dedup_archive (request_id, api_key_id, request_fingerprint, created_at)
				SELECT request_id, api_key_id, request_fingerprint, created_at
				FROM victims
				ON CONFLICT (request_id, api_key_id) DO NOTHING
			)
			DELETE FROM usage_billing_dedup
			WHERE ctid IN (SELECT ctid FROM victims)
		`, cutoff.UTC(), usageBillingDedupCleanupBatchSize)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected < usageBillingDedupCleanupBatchSize {
			return nil
		}
	}
}

func (r *dashboardAggregationRepository) EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error {
	isPartitioned, err := r.isUsageLogsPartitioned(ctx)
	if err != nil || !isPartitioned {
		return err
	}
	monthStart := truncateToMonthUTC(now)
	prevMonth := monthStart.AddDate(0, -1, 0)
	nextMonth := monthStart.AddDate(0, 1, 0)

	for _, m := range []time.Time{prevMonth, monthStart, nextMonth} {
		if err := r.createUsageLogsPartition(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func (r *dashboardAggregationRepository) insertHourlyActiveUsers(ctx context.Context, start, end time.Time) error {
	query := `
		INSERT INTO usage_dashboard_hourly_users (bucket_start, user_id)
		SELECT DISTINCT
			date_trunc('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
			user_id
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		ON CONFLICT DO NOTHING
	`
	_, err := r.sql.ExecContext(ctx, query, start, end)
	return err
}

func (r *dashboardAggregationRepository) insertDailyActiveUsers(ctx context.Context, start, end time.Time) error {
	tzName := timezone.Name()
	query := `
		INSERT INTO usage_dashboard_daily_users (bucket_date, user_id)
		SELECT DISTINCT
			(created_at AT TIME ZONE $3)::date AS bucket_date,
			user_id
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		ON CONFLICT DO NOTHING
	`
	_, err := r.sql.ExecContext(ctx, query, start, end, tzName)
	return err
}

func (r *dashboardAggregationRepository) upsertHourlyAggregates(ctx context.Context, start, end time.Time) error {
	query := `
		WITH hourly AS (
			SELECT
				date_trunc('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
				COUNT(*) AS total_requests,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS account_cost,
				COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS total_duration_ms
			FROM usage_logs
			WHERE created_at >= $1 AND created_at < $2
			GROUP BY 1
		),
		user_counts AS (
			SELECT bucket_start, COUNT(*) AS active_users
			FROM usage_dashboard_hourly_users
			WHERE bucket_start >= $1 AND bucket_start < $2
			GROUP BY bucket_start
		)
		INSERT INTO usage_dashboard_hourly (
			bucket_start,
			total_requests,
			input_tokens,
			output_tokens,
			cache_creation_tokens,
			cache_read_tokens,
			total_cost,
			actual_cost,
			account_cost,
			total_duration_ms,
			active_users,
			computed_at
		)
		SELECT
			hourly.bucket_start,
			hourly.total_requests,
			hourly.input_tokens,
			hourly.output_tokens,
			hourly.cache_creation_tokens,
			hourly.cache_read_tokens,
			hourly.total_cost,
			hourly.actual_cost,
			hourly.account_cost,
			hourly.total_duration_ms,
			COALESCE(user_counts.active_users, 0) AS active_users,
			NOW()
		FROM hourly
		LEFT JOIN user_counts ON user_counts.bucket_start = hourly.bucket_start
		ON CONFLICT (bucket_start)
		DO UPDATE SET
			total_requests = EXCLUDED.total_requests,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cache_creation_tokens = EXCLUDED.cache_creation_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			total_cost = EXCLUDED.total_cost,
			actual_cost = EXCLUDED.actual_cost,
			account_cost = EXCLUDED.account_cost,
			total_duration_ms = EXCLUDED.total_duration_ms,
			active_users = EXCLUDED.active_users,
			computed_at = EXCLUDED.computed_at
	`
	_, err := r.sql.ExecContext(ctx, query, start, end)
	return err
}

func (r *dashboardAggregationRepository) upsertDailyAggregates(ctx context.Context, start, end time.Time) error {
	tzName := timezone.Name()
	query := `
		WITH daily AS (
			SELECT
				(created_at AT TIME ZONE $5)::date AS bucket_date,
				COUNT(*) AS total_requests,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS account_cost,
				COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS total_duration_ms
			FROM usage_logs
			WHERE created_at >= $1 AND created_at < $2
			GROUP BY (created_at AT TIME ZONE $5)::date
		),
		user_counts AS (
			SELECT bucket_date, COUNT(*) AS active_users
			FROM usage_dashboard_daily_users
			WHERE bucket_date >= $3::date AND bucket_date < $4::date
			GROUP BY bucket_date
		)
		INSERT INTO usage_dashboard_daily (
			bucket_date,
			total_requests,
			input_tokens,
			output_tokens,
			cache_creation_tokens,
			cache_read_tokens,
			total_cost,
			actual_cost,
			account_cost,
			total_duration_ms,
			active_users,
			computed_at
		)
		SELECT
			daily.bucket_date,
			daily.total_requests,
			daily.input_tokens,
			daily.output_tokens,
			daily.cache_creation_tokens,
			daily.cache_read_tokens,
			daily.total_cost,
			daily.actual_cost,
			daily.account_cost,
			daily.total_duration_ms,
			COALESCE(user_counts.active_users, 0) AS active_users,
			NOW()
		FROM daily
		LEFT JOIN user_counts ON user_counts.bucket_date = daily.bucket_date
		ON CONFLICT (bucket_date)
		DO UPDATE SET
			total_requests = EXCLUDED.total_requests,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cache_creation_tokens = EXCLUDED.cache_creation_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			total_cost = EXCLUDED.total_cost,
			actual_cost = EXCLUDED.actual_cost,
			account_cost = EXCLUDED.account_cost,
			total_duration_ms = EXCLUDED.total_duration_ms,
			active_users = EXCLUDED.active_users,
			computed_at = EXCLUDED.computed_at
	`
	_, err := r.sql.ExecContext(ctx, query, start, end, start, end, tzName)
	return err
}

func (r *dashboardAggregationRepository) isUsageLogsPartitioned(ctx context.Context) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1
			FROM pg_partitioned_table pt
			JOIN pg_class c ON c.oid = pt.partrelid
			WHERE c.relname = 'usage_logs'
		)
	`
	var partitioned bool
	if err := scanSingleRow(ctx, r.sql, query, nil, &partitioned); err != nil {
		return false, err
	}
	return partitioned, nil
}

func (r *dashboardAggregationRepository) dropUsageLogsPartitions(ctx context.Context, cutoff time.Time) error {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_inherits
		JOIN pg_class c ON c.oid = pg_inherits.inhrelid
		JOIN pg_class p ON p.oid = pg_inherits.inhparent
		WHERE p.relname = 'usage_logs'
	`)
	if err != nil {
		return err
	}
	cutoffMonth := truncateToMonthUTC(cutoff)
	type usageLogsPartition struct {
		name  string
		month time.Time
	}
	partitions := make([]usageLogsPartition, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		if !strings.HasPrefix(name, "usage_logs_") {
			continue
		}
		suffix := strings.TrimPrefix(name, "usage_logs_")
		month, err := time.Parse("200601", suffix)
		if err != nil {
			continue
		}
		month = month.UTC()
		if month.Before(cutoffMonth) {
			partitions = append(partitions, usageLogsPartition{name: name, month: month})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].month.Before(partitions[j].month)
	})
	if db, ok := r.sql.(*sql.DB); ok {
		for _, partition := range partitions {
			if err := dropUsageLogsPartitionWithRollupInvalidation(ctx, db, partition.name, partition.month); err != nil {
				return err
			}
		}
		return nil
	}
	for _, partition := range partitions {
		if _, err := r.sql.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pq.QuoteIdentifier(partition.name))); err != nil {
			return err
		}
	}
	return nil
}

func dropUsageLogsPartitionWithRollupInvalidation(ctx context.Context, db *sql.DB, name string, monthStart time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	if err := lockGroupUsageRollupState(ctx, tx); err != nil {
		return rollback(err)
	}
	if err := invalidateGroupUsageRollupsAt(ctx, tx, monthStart); err != nil {
		return rollback(err)
	}
	if err := prepareDashboardRawRetention(ctx, tx, monthStart.AddDate(0, 1, 0)); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pq.QuoteIdentifier(name))); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func (r *dashboardAggregationRepository) createUsageLogsPartition(ctx context.Context, month time.Time) error {
	monthStart := truncateToMonthUTC(month)
	nextMonth := monthStart.AddDate(0, 1, 0)
	name := fmt.Sprintf("usage_logs_%s", monthStart.Format("200601"))
	query := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s PARTITION OF usage_logs FOR VALUES FROM (%s) TO (%s)",
		pq.QuoteIdentifier(name),
		pq.QuoteLiteral(monthStart.Format("2006-01-02")),
		pq.QuoteLiteral(nextMonth.Format("2006-01-02")),
	)
	_, err := r.sql.ExecContext(ctx, query)
	return err
}

func truncateToDay(t time.Time) time.Time {
	return timezone.StartOfDay(t)
}

func truncateToMonthUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
