package status

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

//go:embed source.sql
var sourceSQL string

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const (
	collectorLock = 1889318018
	incidentLock  = 1889318019
	revisit       = 15 * time.Minute
)

type checkpoint struct {
	through, coveredFrom, updatedAt time.Time
}

func readCheckpoint(ctx context.Context, tx *sql.Tx) (checkpoint, error) {
	var c checkpoint
	err := tx.QueryRowContext(ctx, `SELECT observed_through, covered_from, updated_at FROM public_status_checkpoint WHERE singleton`).
		Scan(&c.through, &c.coveredFrom, &c.updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	c.through, c.coveredFrom = c.through.UTC(), c.coveredFrom.UTC()
	return c, err
}

func (s *Store) collect(ctx context.Context, fn func(*sql.Tx, checkpoint) (checkpoint, bool, error)) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var acquired bool
	err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, collectorLock).Scan(&acquired)
	if err != nil || !acquired {
		return false, err
	}
	c, err := readCheckpoint(ctx, tx)
	if err != nil {
		return false, err
	}
	c, more, err := fn(tx, c)
	if err != nil || c.through.IsZero() {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public_status_checkpoint (observed_through, covered_from)
	    VALUES ($1,$2) ON CONFLICT (singleton) DO UPDATE
	    SET observed_through = excluded.observed_through, covered_from = excluded.covered_from, updated_at = excluded.updated_at
	    WHERE (public_status_checkpoint.observed_through, public_status_checkpoint.covered_from)
	        IS DISTINCT FROM (excluded.observed_through, excluded.covered_from)`, c.through, c.coveredFrom)
	if err != nil {
		return false, err
	}
	return more, tx.Commit()
}

// Refresh advances live evidence without a long catch-up transaction. It can
// replace the recent bins on retry, including when the last attempt failed
// after the source transaction committed.
func (s *Store) Refresh(ctx context.Context, now time.Time) (needsCatchup bool, err error) {
	target := observationEnd(now)
	return s.collect(ctx, func(tx *sql.Tx, c checkpoint) (checkpoint, bool, error) {
		if !c.through.Before(target) {
			return c, false, nil
		}
		end := target
		start := target.Truncate(time.Hour).Add(-time.Hour)
		if c.through.Before(oldestDay(target)) {
			c.coveredFrom = start
		} else {
			end = minTime(target, dayStart(c.through).Add(24*time.Hour))
			start = maxTime(c.through.Add(-revisit), c.coveredFrom, oldestDay(end))
		}
		err := replaceRecent(ctx, tx, start, end, end)
		if err != nil {
			return c, false, err
		}
		c.through, c.coveredFrom = end, maxTime(c.coveredFrom, oldestDay(end))
		return c, end.Before(target), prune(ctx, tx, end)
	})
}

// Backfill covers one UTC day (or the uncovered part of the initial day)
// from verified sources. An empty covered day still reads as insufficient.
func (s *Store) Backfill(ctx context.Context) (more bool, err error) {
	return s.collect(ctx, func(tx *sql.Tx, c checkpoint) (checkpoint, bool, error) {
		if c.through.IsZero() || !c.coveredFrom.After(oldestDay(c.through)) {
			return c, false, nil
		}
		start := dayStart(c.coveredFrom.Add(-time.Minute))
		var err error
		if start.Before(recentFrom(c.through)) {
			err = replaceSource(ctx, tx, "day", "1 day", start, c.coveredFrom, c.through)
		} else {
			err = replaceRecent(ctx, tx, start, c.coveredFrom, c.through)
		}
		c.coveredFrom = start
		return c, start.After(oldestDay(c.through)), err
	})
}

// Run publishes closed minute bins. The owner cancels ctx on shutdown.
func (s *Store) Run(ctx context.Context, enabled func(context.Context) bool) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		delay := time.Until(ViewTime(time.Now()).Add(time.Minute))
		if enabled == nil || enabled(ctx) {
			work, cancel := context.WithTimeout(ctx, 20*time.Second)
			catchingUp, err := s.Refresh(work, time.Now())
			cancel()
			if err == nil && !catchingUp {
				work, cancel = context.WithTimeout(ctx, 20*time.Second)
				catchingUp, err = s.Backfill(work)
				cancel()
			}
			if err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "public status aggregation failed", "error", err)
			}
			if catchingUp {
				delay = time.Second
			}
		}
		timer.Reset(max(time.Millisecond, delay))
	}
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(at time.Time, other ...time.Time) time.Time {
	for _, b := range other {
		if b.After(at) {
			at = b
		}
	}
	return at
}

func recentFrom(end time.Time) time.Time { return dayStart(end).AddDate(0, 0, -30) }

func ceil(at time.Time, d time.Duration) time.Time { return at.Add(d - time.Nanosecond).Truncate(d) }

func replaceRecent(ctx context.Context, tx *sql.Tx, start, end, evidenceThrough time.Time) error {
	if err := replaceSource(ctx, tx, "minute", "1 minute", start, end, evidenceThrough); err != nil {
		return err
	}
	if err := mergeBuckets(ctx, tx, "minute", "hour", "1 hour", start.Truncate(time.Hour), ceil(end, time.Hour)); err != nil {
		return err
	}
	return mergeBuckets(ctx, tx, "hour", "day", "1 day", dayStart(start), ceil(end, 24*time.Hour))
}

const countColumns = `succeeded, gateway_errors, upstream_errors, upstream_rate_limits, stream_interruptions,
  unknown_outcomes, admission_denials, client_cancellations, attempts, first_byte_timeouts, stream_samples, stream_succeeded`

const metricColumns = countColumns + ", ttft_bins, last_observed, last_success"

const insertBuckets = `INSERT INTO public_status_buckets (grain,bucket_start,component_id,` + metricColumns + `) `
const epoch = "timestamptz '2000-01-01 00:00:00+00'"

func replaceSource(ctx context.Context, tx *sql.Tx, grain, interval string, start, end, evidenceThrough time.Time) error {
	if err := deleteRange(ctx, tx, grain, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, collectBinsSQL, start, end, evidenceThrough, interval, grain)
	return err
}

// Group once by public protocol and once across the allowlisted gateway
// protocols. Output timestamps are rounded before entering the public store.
var collectBinsSQL = sourceSQL + `
, fixed_dimensions AS (
  SELECT date_bin($4::interval, happened_at, ` + epoch + `) AS bucket_start,
    date_bin(interval '1 minute', happened_at, ` + epoch + `) AS observed_minute,
    component_id, outcome, stream, ttft_ms, weight
  FROM observations
  WHERE component_id IS NOT NULL AND weight > 0
)
` + insertBuckets + `
SELECT $5, bucket_start, coalesce(component_id, 'inference'),
    ` + sourceCounters() + `,
    ARRAY[` + sourceHistogram() + `],
    max(observed_minute) FILTER (WHERE outcome IN ('succeeded','gateway_error','upstream_error','upstream_rate_limit','stream_interruption','unknown')),
    max(observed_minute) FILTER (WHERE outcome = 'succeeded')
FROM fixed_dimensions
GROUP BY GROUPING SETS ((bucket_start, component_id), (bucket_start))
`

func sourceCounters() string {
	criteria := []string{
		"outcome = 'succeeded'",
		"outcome = 'gateway_error'",
		"outcome = 'upstream_error'",
		"outcome = 'upstream_rate_limit'",
		"outcome = 'stream_interruption'",
		"outcome = 'unknown'",
		"outcome = 'admission_denial'",
		"outcome = 'client_cancellation'",
		"outcome IN ('attempt', 'first_byte_timeout')",
		"outcome = 'first_byte_timeout'",
		"stream AND outcome IN ('succeeded','gateway_error','upstream_error','upstream_rate_limit','stream_interruption')",
		"stream AND outcome = 'succeeded'",
	}
	parts := make([]string, 0, len(criteria))
	for _, condition := range criteria {
		parts = append(parts, "coalesce(sum(weight) FILTER (WHERE "+condition+"), 0)::bigint")
	}
	return strings.Join(parts, ", ")
}

func sourceHistogram() string {
	parts := make([]string, 0, len(ttftBucketMax)+1)
	previous := -1
	for _, upper := range ttftBucketMax {
		parts = append(parts, fmt.Sprintf("count(*) FILTER (WHERE outcome = 'succeeded' AND ttft_ms > %d AND ttft_ms <= %d)", previous, upper))
		previous = upper
	}
	parts = append(parts, fmt.Sprintf("count(*) FILTER (WHERE outcome = 'succeeded' AND ttft_ms > %d)", previous))
	return strings.Join(parts, ", ")
}

var mergedMetrics = func() string {
	columns := strings.FieldsFunc(countColumns, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })
	sums := make([]string, 0, len(columns))
	for _, name := range columns {
		sums = append(sums, "sum("+name+")::bigint")
	}
	bins := make([]string, len(ttftBucketMax)+1)
	for i := range bins {
		bins[i] = "sum(ttft_bins[" + strconv.Itoa(i+1) + "])::bigint"
	}
	return strings.Join(sums, ",") + ", ARRAY[" + strings.Join(bins, ",") + "], max(last_observed), max(last_success)"
}()

func mergeBuckets(ctx context.Context, tx *sql.Tx, fromGrain, toGrain, interval string, start, end time.Time) error {
	if err := deleteRange(ctx, tx, toGrain, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, insertBuckets+`
    SELECT $2, date_bin($3::interval, bucket_start, `+epoch+`), component_id, `+mergedMetrics+`
    FROM public_status_buckets WHERE grain = $1 AND bucket_start >= $4 AND bucket_start < $5
    GROUP BY 2,3`, fromGrain, toGrain, interval, start, end)
	return err
}

func deleteRange(ctx context.Context, tx *sql.Tx, grain string, from, to time.Time) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM public_status_buckets WHERE grain = $1 AND bucket_start >= $2 AND bucket_start < $3`, grain, from, to)
	return err
}

func prune(ctx context.Context, tx *sql.Tx, end time.Time) error {
	_, err := tx.ExecContext(ctx, `WITH expired AS (
	    DELETE FROM public_status_buckets WHERE grain = 'day' AND bucket_start < $1
	    RETURNING component_id, last_success
	  )
	  INSERT INTO public_status_last_success (component_id,last_success)
	  SELECT component_id, max(last_success) FROM expired WHERE last_success IS NOT NULL GROUP BY component_id
	  ON CONFLICT (component_id) DO UPDATE
	  SET last_success = greatest(public_status_last_success.last_success, excluded.last_success)`, oldestDay(end))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM public_status_buckets WHERE grain IN ('minute','hour') AND bucket_start < $1`, recentFrom(end))
	return err
}

func scanBucket(rows *sql.Rows, keys ...any) (bucket, error) {
	var b bucket
	var observed, success sql.NullTime
	err := rows.Scan(append(keys, &b.Succeeded, &b.GatewayErrors, &b.UpstreamErrors, &b.UpstreamRateLimits,
		&b.StreamInterruptions, &b.UnknownOutcomes, &b.AdmissionDenials, &b.ClientCancellations,
		&b.Attempts, &b.FirstByteTimeouts, &b.StreamSamples, &b.StreamSucceeded, pq.Array(&b.ttft),
		&observed, &success)...)
	b.lastObserved, b.lastSuccess = utcPointer(observed), utcPointer(success)
	return b, err
}

func utcPointer(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	at := t.Time.UTC()
	return &at
}
