package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// AggregateRangeAndPublish commits at most one statistics calendar day at a
// time. Each transaction rechecks invalidation under group -> dashboard locks.
// Cancellation leaves completed days durable and the remaining dirty suffix
// unavailable; a retry resumes from the database, not an in-memory job cursor.
func (r *dashboardAggregationRepository) AggregateRangeAndPublish(ctx context.Context, start, end time.Time) error {
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return errors.New("dashboard publication requires a database transaction")
	}
	closed := end.UTC().Truncate(time.Hour)
	if now := r.now().UTC().Truncate(time.Hour); closed.After(now) {
		closed = now
	}
	for {
		next, err := r.publishDashboardDay(ctx, db, start, closed)
		if err != nil {
			return err
		}
		if !next.Before(closed) {
			return nil
		}
		start = next
	}
}

func dashboardPublicationWindow(c dashboardCoverage, start, closed time.Time) (time.Time, time.Time) {
	if c.zone != timezone.Name() || c.version != dashboardProjectionVersion {
		start = bucketCeil(c.raw, "day")
	} else {
		if (c.hourly.Valid || c.daily.Valid) && start.After(c.published) {
			start = c.published
		}
		if c.dirty.Valid && c.dirty.Time.Before(start) {
			start = c.dirty.Time
		}
		if start.Before(c.raw) && (!c.dirty.Valid || !c.dirty.Time.Before(c.raw)) {
			start = bucketCeil(c.raw, "day")
		}
	}
	start = bucketFloor(start, "day")
	// A partial retained day is raw-only. An older dirty correction must fail
	// requireRaw instead of silently discarding its invalidation.
	if start.Before(c.raw) && (!c.dirty.Valid || !c.dirty.Time.Before(c.raw)) {
		start = bucketCeil(c.raw, "day")
	}
	end := start.AddDate(0, 0, 1)
	if closed.Before(end) {
		end = closed
	}
	return start, end
}

func (r *dashboardAggregationRepository) publishDashboardDay(ctx context.Context, db *sql.DB, requestedStart, closed time.Time) (time.Time, error) {
	began := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	if err = lockGroupUsageRollupState(ctx, tx); err != nil {
		return time.Time{}, err
	}
	groupLocked := time.Now()
	c, err := readDashboardCoverage(ctx, tx, true)
	if err != nil {
		return time.Time{}, err
	}
	start, end := dashboardPublicationWindow(c, requestedStart, closed)
	if !end.After(start) {
		return closed, tx.Commit()
	}
	if err = c.requireRaw(start, end); err != nil {
		return time.Time{}, err
	}
	if c.zone != timezone.Name() || c.version != dashboardProjectionVersion {
		// Invalidate the entire old identity before any new buckets become
		// readable. No old calendar buckets acquire the new identity's coverage.
		c.hourly, c.daily, c.dirty = sql.NullTime{}, sql.NullTime{}, sql.NullTime{}
		c.published = time.Unix(0, 0).UTC()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE usage_dashboard_aggregation_watermark SET timezone_name=$1,projection_version=$2 WHERE id=1`, timezone.Name(), dashboardProjectionVersion); err != nil {
		return time.Time{}, err
	}
	worker := newDashboardAggregationRepositoryWithSQL(tx)
	worker.clock = r.clock
	if err = worker.AggregateRange(ctx, start, end); err != nil {
		return time.Time{}, fmt.Errorf("rebuild dashboard day %s: %w", start.Format(time.RFC3339), err)
	}
	// Floors may extend only over adjacent rebuilt coverage, never over a gap.
	floor := c.hourly
	if !floor.Valid {
		floor = c.daily
	}
	connected := !floor.Valid || (!start.After(c.published) && !end.Before(floor.Time))
	if connected && (!floor.Valid || end.After(c.published)) {
		c.published = end
	}
	if start.Equal(bucketFloor(start, "hour")) && start.AddDate(0, 0, 1).Equal(bucketFloor(start.AddDate(0, 0, 1), "hour")) {
		if connected && (!c.hourly.Valid || start.Before(c.hourly.Time)) {
			c.hourly = sql.NullTime{Time: start, Valid: true}
		}
	} else {
		// A physical hour straddling a fractional-offset midnight cannot be
		// declared rebuilt by either day. Keep hourly queries raw rather than
		// claiming continuous hourly coverage across those seams.
		c.hourly = sql.NullTime{}
	}
	if connected && !bucketFloor(end, "day").Before(start.AddDate(0, 0, 1)) {
		if !c.daily.Valid {
			c.daily = sql.NullTime{Time: start, Valid: true}
		} else if !end.Before(c.daily.Time) && start.Before(c.daily.Time) {
			c.daily.Time = start
		}
	}
	if c.dirty.Valid && !start.After(c.dirty.Time) && end.After(c.dirty.Time) {
		if end.Before(c.published) {
			c.dirty.Time = end
		} else {
			c.dirty = sql.NullTime{}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE usage_dashboard_aggregation_watermark SET
 last_aggregated_at=$1,hourly_retained_from=$2,daily_retained_from=$3,
 dirty_from=$4,timezone_name=$5,projection_version=$6,updated_at=NOW() WHERE id=1`, c.published, c.hourly, c.daily, c.dirty, timezone.Name(), dashboardProjectionVersion)
	if err != nil {
		return time.Time{}, err
	}
	if err = tx.Commit(); err != nil {
		return time.Time{}, err
	}
	slog.Debug("dashboard day committed", "start", start, "end", end, "lock_wait", groupLocked.Sub(began), "lock_held", time.Since(groupLocked), "transaction_duration", time.Since(began))
	return end, nil
}
