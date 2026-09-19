package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

var ErrUsageHistoryUnavailable = infraerrors.New(409, "USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable")

const dashboardProjectionVersion = 2

type dashboardCoverage struct {
	raw, published       time.Time
	hourly, daily, dirty sql.NullTime
	zone                 string
	version              int
}

func readDashboardCoverage(ctx context.Context, q sqlExecutor, lock bool) (dashboardCoverage, error) {
	var c dashboardCoverage
	query := `SELECT raw_retained_from, hourly_retained_from, daily_retained_from,
 last_aggregated_at, dirty_from, timezone_name, projection_version
 FROM usage_dashboard_aggregation_watermark WHERE id=1`
	if lock {
		query += " FOR UPDATE"
	}
	err := scanSingleRow(ctx, q, query, nil, &c.raw, &c.hourly, &c.daily, &c.published, &c.dirty, &c.zone, &c.version)
	return c, err
}

func (c dashboardCoverage) watermark() time.Time {
	if c.zone != timezone.Name() || c.version != dashboardProjectionVersion {
		return time.Unix(0, 0).UTC()
	}
	if c.dirty.Valid && c.dirty.Time.Before(c.published) {
		return c.dirty.Time
	}
	return c.published
}

func (c dashboardCoverage) requireRaw(start, end time.Time) error {
	if end.After(start) && start.Before(c.raw) {
		return ErrUsageHistoryUnavailable.WithMetadata(map[string]string{"raw_retained_from": c.raw.UTC().Format(time.RFC3339Nano)})
	}
	return nil
}

func bucketFloor(t time.Time, granularity string) time.Time {
	if granularity == "hour" {
		return t.UTC().Truncate(time.Hour)
	}
	return truncateToDay(t.In(timezone.Location()))
}
func bucketCeil(t time.Time, granularity string) time.Time {
	floor := bucketFloor(t, granularity)
	if floor.Equal(t) {
		return floor
	}
	if granularity == "hour" {
		return floor.Add(time.Hour)
	}
	return floor.AddDate(0, 0, 1)
}

func (c dashboardCoverage) bounds(start, end time.Time, granularity string) (time.Time, time.Time) {
	first := bucketCeil(start, granularity)
	retained := c.daily
	if granularity == "hour" {
		retained = c.hourly
	}
	if !retained.Valid || c.zone != timezone.Name() || c.version != dashboardProjectionVersion {
		return end, end // all raw
	}
	if retained.Time.After(first) {
		first = retained.Time
	}
	closed := c.watermark()
	if end.Before(closed) {
		closed = end
	}
	closed = bucketFloor(closed, granularity)
	if !closed.After(first) {
		return end, end
	}
	return first, closed
}

func (c dashboardCoverage) requireTrend(start, end time.Time, granularity string) error {
	if !end.After(start) {
		return nil
	}
	first, closed := c.bounds(start, end, granularity)
	if err := c.requireRaw(start, first); err != nil {
		return err
	}
	return c.requireRaw(closed, end)
}

// Coverage and data must share a snapshot: otherwise a concurrent retention or
// correction can turn a successful preflight into a silently truncated result.
func usageReadSnapshot[T any](ctx context.Context, r *usageLogRepository, run func(*usageLogRepository, dashboardCoverage) (T, error)) (result T, err error) {
	db, ok := r.sql.(*sql.DB)
	if !ok || db == nil {
		return result, fmt.Errorf("usage snapshot requires a database connection pool")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	// Rollback is fallback cleanup; preserve the operation/commit error (or ErrTxDone after commit).
	defer func() { _ = tx.Rollback() }()
	// MVCC retains deleted rows, but cannot resurrect a dropped partition.
	// Lock the parent and its children before taking the coverage snapshot; this
	// allows normal writers while keeping physical retention out until commit.
	if _, err = tx.ExecContext(ctx, `LOCK TABLE usage_logs IN ACCESS SHARE MODE`); err != nil {
		return result, err
	}
	c, err := readDashboardCoverage(ctx, tx, false)
	if err != nil {
		return result, err
	}
	// Association hydration must use this connection too, including pools with
	// one available connection. Never escape the statistics snapshot via Ent.
	client := dbent.NewClient(dbent.Driver(entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})))
	snapshot := usageLogRepository{client: client, sql: tx, db: r.db, bestEffortRecent: r.bestEffortRecent}
	result, err = run(&snapshot, c)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func needsUsageSnapshot(r *usageLogRepository) bool {
	db, ok := r.sql.(*sql.DB)
	return ok && isPostgresDriver(db)
}

func rawUsageReadSnapshot[T any](ctx context.Context, r *usageLogRepository, start, end time.Time, run func(*usageLogRepository) (T, error)) (T, error) {
	return usageReadSnapshot(ctx, r, func(snapshot *usageLogRepository, c dashboardCoverage) (result T, err error) {
		if err = c.requireRaw(start, end); err != nil {
			return result, err
		}
		return run(snapshot)
	})
}

func (c dashboardCoverage) metadata() *usagestats.Coverage {
	result := &usagestats.Coverage{Complete: false, RawRetainedFrom: c.raw, Timezone: c.zone, ProjectionVersion: c.version, Reason: "history_before_coverage_floor_unverified"}
	if c.hourly.Valid && c.zone == timezone.Name() && c.version == dashboardProjectionVersion {
		t := c.hourly.Time
		result.HourlyRetainedFrom = &t
	}
	if c.daily.Valid && c.zone == timezone.Name() && c.version == dashboardProjectionVersion {
		t := c.daily.Time
		result.DailyRetainedFrom = &t
	}
	if c.dirty.Valid {
		t := c.dirty.Time
		result.DirtyFrom = &t
	}
	result.PublishedBefore = c.watermark()
	return result
}

// All callers hold the group state before locking the dashboard state.
func prepareDashboardRebuild(ctx context.Context, q sqlExecutor, start, end time.Time) error {
	var id int
	if err := scanSingleRow(ctx, q, `SELECT id FROM usage_group_rollup_state WHERE id=1 FOR UPDATE`, nil, &id); err != nil {
		return err
	}
	c, err := readDashboardCoverage(ctx, q, true)
	if err != nil {
		return err
	}
	if err = c.requireRaw(start, end); err != nil {
		return err
	}
	if c.zone != "" && (c.zone != timezone.Name() || c.version != dashboardProjectionVersion) {
		return fmt.Errorf("%w: projection identity changed; rebuild the retained range through publication", ErrUsageHistoryUnavailable)
	}
	return nil
}

func prepareDashboardRawRetention(ctx context.Context, q sqlExecutor, cutoff time.Time) error {
	c, err := readDashboardCoverage(ctx, q, true)
	if err != nil {
		return err
	}
	// A day straddling the raw cutoff cannot be rebuilt. Only discard raw data
	// once the full containing calendar day is durably published.
	if cutoff.After(c.raw) && bucketCeil(cutoff, "day").After(bucketFloor(c.watermark(), "day")) {
		return fmt.Errorf("%w: retention would outrun verified daily publication", ErrUsageHistoryUnavailable)
	}
	_, err = q.ExecContext(ctx, `UPDATE usage_dashboard_aggregation_watermark SET raw_retained_from=GREATEST(raw_retained_from,$1),updated_at=NOW() WHERE id=1`, cutoff)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `SELECT set_config('sub2api.usage_retention','on',true)`)
	return err
}

// DashboardCoverageRevision invalidates cached summaries after calibration,
// retention, publication or a correction, even if the effective cursor is equal.
func (r *usageLogRepository) DashboardCoverageRevision(ctx context.Context) (string, error) {
	var revision string
	err := scanSingleRow(ctx, r.sql, `SELECT md5(row_to_json(state)::text) FROM usage_dashboard_aggregation_watermark state WHERE id=1`, nil, &revision)
	return revision, err
}
