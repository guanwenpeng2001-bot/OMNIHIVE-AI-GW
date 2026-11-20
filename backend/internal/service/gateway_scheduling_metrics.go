package service

import (
	"context"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

const publicRankingMinimumSamples = 10

type PublicModelRanking struct {
	Model          string   `json:"model"`
	Provider       string   `json:"provider"`
	Requests       int64    `json:"request_count"`
	SuccessRate    *float64 `json:"success_rate"`
	AverageLatency *float64 `json:"avg_latency_ms"`
	LatencySamples int64    `json:"latency_samples"`
	SampleState    string   `json:"sample_state"`
}

// PublicModelRankings aggregates the same observed success and deduplicated
// failure counts used by channel telemetry. Only public model names may leave
// this boundary. The success rate is a fraction of observed requests.
func (s *UsageService) PublicModelRankings(ctx context.Context, start, end time.Time, limit int) ([]PublicModelRanking, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("usage telemetry unavailable")
	}
	var rows entsql.Rows
	err := s.entClient.Driver().Query(ctx, `WITH publication AS (
		SELECT payload FROM catalog_publications WHERE published_at <= $2 AND expires_at > $2
		ORDER BY published_at DESC,version LIMIT 1
	), public_models AS (
		SELECT DISTINCT entry->'model'->>'model' AS model,entry->'model'->>'provider' AS platform
		FROM publication CROSS JOIN LATERAL jsonb_array_elements(payload->'models') AS entry
	), totals AS (
		SELECT model,platform,sum(success_requests) AS successes,sum(success_requests+error_requests) AS requests,
			sum(duration_sum_ms) AS latency_sum,sum(duration_count) AS latency_samples
		FROM channel_monitor_v2_metrics_1m WHERE bucket_start >= $1 AND bucket_start < $2
		GROUP BY model,platform HAVING sum(success_requests+error_requests)>0
	)
	SELECT totals.model,totals.platform,requests,successes::double precision/requests,
		latency_samples,latency_sum::double precision/nullif(latency_samples,0),
		public_models.model IS NOT NULL
	FROM totals LEFT JOIN public_models USING (model,platform)
	ORDER BY requests DESC,totals.platform,totals.model`, []any{start.UTC(), end.UTC()}, &rows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PublicModelRanking{}
	for rows.Next() {
		var item PublicModelRanking
		var published bool
		if err := rows.Scan(&item.Model, &item.Provider, &item.Requests, &item.SuccessRate, &item.LatencySamples, &item.AverageLatency, &published); err != nil {
			return nil, err
		}
		if !published && len(catalog.BuiltinMethods(item.Provider, item.Model)) == 0 {
			continue
		}
		item.SampleState = "observed"
		if item.Requests < publicRankingMinimumSamples {
			item.SuccessRate = nil
			item.SampleState = "insufficient_samples"
		}
		if item.LatencySamples < publicRankingMinimumSamples {
			item.AverageLatency = nil
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	return items, rows.Err()
}
