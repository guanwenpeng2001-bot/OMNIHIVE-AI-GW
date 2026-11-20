package postgres

import (
	"context"
	"database/sql"
	"slices"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/lib/pq"
)

type MetricKey struct {
	Group int64
	Model string
}

const metricQuery = `WITH selected AS (
    SELECT * FROM unnest($5::bigint[],$6::text[],$7::text[]) AS m(group_id,model,model_id)
), keys AS (
    SELECT k.id FROM api_keys k
    JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
    LEFT JOIN project_key_policies kp ON kp.api_key_id=k.id AND kp.workspace_id=k.workspace_id
    LEFT JOIN project_resources r ON r.workspace_id=k.workspace_id AND r.kind='api_keys' AND r.resource_id=k.id
    WHERE k.user_id=$1 AND k.workspace_id=$2 AND coalesce(kp.project_id,r.project_id,d.id)=$8
      AND NOT EXISTS (SELECT FROM identity_scope_quarantine q WHERE q.kind='api_keys' AND q.resource_id=k.id)
), samples AS (
    SELECT m.model_id,u.api_key_id,u.request_id,u.created_at,true AS success,
        CASE WHEN u.first_token_ms>=0 THEN u.first_token_ms END AS ttft,
        CASE WHEN u.first_token_ms>=0 AND u.output_tokens>0 AND u.duration_ms>u.first_token_ms
             THEN 1000.0*u.output_tokens/(u.duration_ms-u.first_token_ms) END AS speed
    FROM usage_logs u JOIN keys k ON k.id=u.api_key_id
    JOIN selected m ON m.group_id=u.group_id AND m.model=coalesce(nullif(u.requested_model,''),u.model)
    WHERE u.user_id=$1 AND u.created_at >= $3 AND u.created_at < $4 AND nullif(u.request_id,'') IS NOT NULL
    UNION ALL
    SELECT m.model_id,e.api_key_id,e.request_id,e.created_at,false,NULL,NULL
    FROM ops_error_logs e JOIN keys k ON k.id=e.api_key_id
    JOIN selected m ON m.group_id=e.group_id AND m.model=coalesce(nullif(e.requested_model,''),e.model)
    WHERE $9 AND e.user_id=$1 AND e.created_at >= $3 AND e.created_at < $4 AND nullif(e.request_id,'') IS NOT NULL
      AND e.status_code>=400 AND NOT e.is_count_tokens AND NOT e.is_business_limited
), logical AS (
    SELECT DISTINCT ON(api_key_id,request_id) model_id,success,ttft,speed FROM samples
    ORDER BY api_key_id,request_id,success DESC,created_at DESC,model_id,ttft NULLS LAST,speed NULLS LAST
)
SELECT model_id,count(*),count(*) FILTER(WHERE success),count(ttft),count(speed),
       percentile_disc(0.5) WITHIN GROUP (ORDER BY ttft),avg(speed)::float8
FROM logical GROUP BY model_id`

func (s *Store) Metrics(ctx context.Context, p catalog.Perspective, w catalog.Window, selected map[MetricKey]string, opsEnabled bool) (map[string]catalog.Metrics, error) {
	groups, names, ids := make([]int64, 0, len(selected)), make([]string, 0, len(selected)), make([]string, 0, len(selected))
	out := make(map[string]catalog.Metrics, len(selected))
	for key, id := range selected {
		if id == "" {
			continue
		}
		groups, names, ids = append(groups, key.Group), append(names, key.Model), append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	queries := slices.Clone(ids)
	slices.Sort(queries)
	for _, id := range slices.Compact(queries) {
		out[id] = observedMetrics(w, 0, 0, 0, 0, opsEnabled, sql.NullInt64{}, sql.NullFloat64{})
	}
	rows, err := s.db.QueryContext(ctx, metricQuery, p.Actor, p.Workspace, w.Start, w.End, pq.Array(groups), pq.Array(names), pq.Array(ids), p.Project, opsEnabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var requests, successes, timings, speeds int64
		var ttft sql.NullInt64
		var speed sql.NullFloat64
		if err := rows.Scan(&id, &requests, &successes, &timings, &speeds, &ttft, &speed); err != nil {
			return nil, err
		}
		out[id] = observedMetrics(w, requests, successes, timings, speeds, opsEnabled, ttft, speed)
	}
	return out, rows.Err()
}

func observedMetrics(w catalog.Window, requests, successes, timings, speeds int64, opsEnabled bool, ttft sql.NullInt64, speed sql.NullFloat64) catalog.Metrics {
	const minSamples = 5
	v := catalog.Metrics{Start: w.Start, End: w.End, Source: "gateway_observations", Requests: &requests, TTFTSamples: &timings, SpeedSamples: &speeds, State: "insufficient_samples"}
	if requests >= minSamples && opsEnabled {
		v.State = "partial"
		ratio := float64(successes) / float64(requests)
		v.SuccessRate = &ratio
	}
	if timings >= minSamples && ttft.Valid {
		v.State, v.TTFT = "partial", &ttft.Int64
	}
	if speeds >= minSamples && speed.Valid {
		v.State, v.Speed = "partial", &speed.Float64
	}
	if v.SuccessRate != nil && v.TTFT != nil && v.Speed != nil {
		v.State = "available"
	}
	return v
}
