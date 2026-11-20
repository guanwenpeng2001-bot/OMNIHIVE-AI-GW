package status

import (
	"context"
	"database/sql"
	"time"
)

const activeIncidentLimit = 25

func (s *Store) Summary(ctx context.Context, now time.Time, window time.Duration, monitoring bool) (Summary, time.Time, error) {
	var result Summary
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, time.Time{}, err
	}
	defer tx.Rollback()
	checkpoint, err := readCheckpoint(ctx, tx)
	if err != nil {
		return result, time.Time{}, err
	}
	view := ViewTime(now)
	end := checkpoint.through
	if end.IsZero() {
		end = observationEnd(now)
	} else {
		lag := max(0, int64(view.Sub(end)/time.Second))
		result.DataLagSeconds = &lag
	}
	start, firstDay := end.Add(-window), oldestDay(end)
	latest, err := currentBuckets(ctx, tx, start, end)
	if err != nil {
		return result, time.Time{}, err
	}
	daily, err := dailyBuckets(ctx, tx, firstDay, end)
	if err != nil {
		return result, time.Time{}, err
	}
	lastSuccess, err := archivedSuccesses(ctx, tx)
	if err != nil {
		return result, time.Time{}, err
	}
	active, impacts, count, err := activeIncidents(ctx, tx)
	if err != nil {
		return result, time.Time{}, err
	}
	updatedAt, err := latestPublication(ctx, tx)
	if err != nil {
		return result, time.Time{}, err
	}
	result.AsOf, result.WindowStart, result.WindowEnd = end, start, end
	result.Incidents, result.ActiveIncidentCount = active, count
	result.Components = make([]Component, 0, len(components))
	for _, spec := range components {
		b := latest[spec.id]
		c := Component{
			ID: spec.id, Name: spec.name, Source: "gateway_observations",
			LastSuccess: lastSuccess[spec.id], History: make([]Daily, 0, historyDays),
		}
		sourceReason := ""
		if !spec.measured {
			c.Source = "unavailable"
			sourceReason = "SOURCE_UNAVAILABLE"
		}
		observations := daily[spec.id]
		var mostRecent *time.Time
		for date := firstDay; !date.After(dayStart(end)); date = date.AddDate(0, 0, 1) {
			historical := observations[date]
			c.LastSuccess = latestTime(c.LastSuccess, historical.lastSuccess)
			mostRecent = latestTime(mostRecent, historical.lastObserved)
			reason := sourceReason
			if spec.measured && !checkpoint.through.IsZero() && date.Before(checkpoint.coveredFrom) {
				reason = "ROLLUP_PENDING"
			}
			m := measure(historical, reason)
			c.History = append(c.History, Daily{
				Date: date.Format(time.DateOnly), State: m.State, Reason: m.Reason, SampleCount: m.SampleCount, SuccessRate: m.SuccessRate,
			})
		}
		b.lastObserved = latestTime(mostRecent, c.LastSuccess)
		c.Freshness = Unknown
		if spec.measured {
			c.Freshness, c.StaleAfter = freshness(b, checkpoint.through, view)
		}
		reason := sourceReason
		if spec.measured {
			switch {
			case !monitoring:
				reason = "SOURCE_UNAVAILABLE"
				if !checkpoint.through.IsZero() {
					c.Freshness = "stale"
				}
			case c.Freshness == "stale":
				reason = "STALE_DATA"
			case !checkpoint.through.IsZero() && start.Before(checkpoint.coveredFrom):
				reason = "ROLLUP_PENDING"
			}
		}
		c.Measurement = measure(b, reason)
		c.ObservedState = c.State
		if severity, ok := impacts[c.ID]; ok {
			c.State = worse(c.State, severity)
		}
		result.Components = append(result.Components, c)
	}
	result.Overall = overallState(result.Components)
	result.Definitions = Definitions{
		SuccessRate:               "successful completed logical requests / admitted non-client-canceled logical requests with recorded final outcomes, as a percentage",
		StreamCompletionRate:      "successful observed streaming logical requests / observed streaming logical requests with a known outcome, as a percentage",
		TTFT:                      "first-token latency from successful gateway usage evidence, reported as the upper bound of the percentile's latency bucket",
		ExcludedCategories:        []string{"admission_denial", "client_canceled"},
		ReportedCategories:        []string{"gateway_error", "upstream_error", "upstream_rate_limit", "stream_interruption", "unknown"},
		AttemptsCountedSeparately: true, AggregationIntervalSeconds: 60, PublicationDelaySeconds: int(publicationLag / time.Second),
	}
	return result, maxTime(view, updatedAt, checkpoint.updatedAt), tx.Commit()
}

// Inference aggregates all measured gateway requests. An unmeasured
// component retains its own unknown state. An incident can still elevate
// overall severity even when that component has no observation source.
func overallState(components []Component) string {
	result := Unknown
	for _, c := range components {
		if result == Unknown && c.ID == "inference" && c.State == Operational {
			result = Operational
		}
		if c.State != Unknown {
			result = worse(result, c.State)
		}
	}
	return result
}

func latestTime(a, b *time.Time) *time.Time {
	if a == nil || (b != nil && b.After(*a)) {
		return b
	}
	return a
}

func currentBuckets(ctx context.Context, tx *sql.Tx, from, to time.Time) (map[string]bucket, error) {
	firstHour, lastHour := ceil(from, time.Hour), to.Truncate(time.Hour)
	rows, err := tx.QueryContext(ctx, `WITH intervals AS (
	    SELECT component_id,`+metricColumns+` FROM public_status_buckets
	    WHERE grain = 'hour' AND bucket_start >= $1 AND bucket_start < $2
	    UNION ALL
	    SELECT component_id,`+metricColumns+` FROM public_status_buckets
	    WHERE grain = 'minute' AND bucket_start >= $3 AND bucket_start < least($1,$4)
	    UNION ALL
	    SELECT component_id,`+metricColumns+` FROM public_status_buckets
	    WHERE grain = 'minute' AND bucket_start >= greatest($1,$2) AND bucket_start < $4
	  )
	  SELECT component_id,`+mergedMetrics+` FROM intervals GROUP BY component_id`, firstHour, lastHour, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bucket)
	for rows.Next() {
		var id string
		b, err := scanBucket(rows, &id)
		if err != nil {
			return nil, err
		}
		out[id] = b
	}
	return out, rows.Err()
}

func dailyBuckets(ctx context.Context, tx *sql.Tx, from, to time.Time) (map[string]map[time.Time]bucket, error) {
	rows, err := tx.QueryContext(ctx, `SELECT component_id,bucket_start,`+metricColumns+`
	    FROM public_status_buckets WHERE grain = 'day' AND bucket_start BETWEEN $1 AND $2`, from, dayStart(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]map[time.Time]bucket)
	for rows.Next() {
		var id string
		var at time.Time
		b, err := scanBucket(rows, &id, &at)
		if err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = make(map[time.Time]bucket)
		}
		out[id][at.UTC()] = b
	}
	return out, rows.Err()
}

func archivedSuccesses(ctx context.Context, tx *sql.Tx) (map[string]*time.Time, error) {
	rows, err := tx.QueryContext(ctx, `SELECT component_id,last_success FROM public_status_last_success`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]*time.Time)
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		at = at.UTC()
		out[id] = &at
	}
	return out, rows.Err()
}

func activeIncidents(ctx context.Context, tx *sql.Tx) ([]IncidentSummary, map[string]string, int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+incidentColumns+` FROM public_status_incidents i
	    WHERE i.resolved_at IS NULL ORDER BY i.started_at DESC,i.id DESC LIMIT $1`, activeIncidentLimit)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	out := make([]IncidentSummary, 0, activeIncidentLimit)
	for rows.Next() {
		i, err := scanIncident(rows.Scan)
		if err != nil {
			return nil, nil, 0, err
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	var count int64
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM public_status_incidents WHERE resolved_at IS NULL`).Scan(&count)
	if err != nil {
		return nil, nil, 0, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT unnest(component_ids) AS component_id,severity
	    FROM public_status_incidents WHERE resolved_at IS NULL`)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	impacts := make(map[string]string)
	for rows.Next() {
		var id, severity string
		if err := rows.Scan(&id, &severity); err != nil {
			return nil, nil, 0, err
		}
		impacts[id] = worse(impacts[id], severity)
	}
	return out, impacts, count, rows.Err()
}
