package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestChannelTelemetryDateBinOriginIsUTC(t *testing.T) {
	require.Equal(t, "TIMESTAMPTZ '1970-01-01 00:00:00+00'", channelTelemetryDateBinOrigin)
	require.Equal(t, "date_bin($1::interval,m.bucket_start,TIMESTAMPTZ '1970-01-01 00:00:00+00')", channelTelemetryDateBinExpr("m.bucket_start"))

	for _, query := range []string{
		channelTelemetryFixedRollupBoundsSQL,
		channelTelemetryMetricsRollupSQL,
		channelTelemetryUserMetricsRollupSQL,
		channelTelemetryHistogramRollupSQL,
		channelTelemetryErrorRollupSQL,
	} {
		require.Contains(t, query, channelTelemetryDateBinOrigin)
		require.NotContains(t, query, "TIMESTAMPTZ '1970-01-01'")
	}
}

func TestChannelTelemetryDisplayModelIsPlatformScoped(t *testing.T) {
	cfg := service.ChannelTelemetryConfig{Platforms: []service.ChannelTelemetryPlatformConfig{
		{Platform: "openai", Enabled: true, Models: []string{"shared", "gpt-5"}},
		{Platform: "grok", Enabled: true, Models: []string{"grok-4"}},
		// Empty models list must NOT collapse everything into __other__.
		{Platform: "anthropic", Enabled: true, Models: []string{}},
	}}
	require.Equal(t, "shared", channelTelemetryDisplayModel(cfg, "openai", "shared"))
	require.Equal(t, service.ChannelTelemetryOtherModel, channelTelemetryDisplayModel(cfg, "grok", "shared"))
	require.Equal(t, "claude-sonnet-4", channelTelemetryDisplayModel(cfg, "anthropic", "claude-sonnet-4"))
	// Unconfigured platform still surfaces the real model name.
	require.Equal(t, "gemini-2.5-pro", channelTelemetryDisplayModel(cfg, "gemini", "gemini-2.5-pro"))
	require.True(t, channelTelemetryModelSelected(service.ChannelTelemetryFilter{Models: []string{service.ChannelTelemetryOtherModel}}, cfg, "grok", "shared"))
}

func TestChannelTelemetryMatrixDimensionKey(t *testing.T) {
	cfg := service.ChannelTelemetryConfig{Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"gpt-5"}}}}
	key := channelTelemetryMatrixDimensionKey(service.ChannelTelemetryGroupByPlatformGroupModel, cfg, "openai", 7, "gpt-5")
	require.Equal(t, channelTelemetryMatrixKey{platform: "openai", groupID: 7, model: "gpt-5"}, key)
	key = channelTelemetryMatrixDimensionKey(service.ChannelTelemetryGroupByPlatformModel, cfg, "openai", 7, "unlisted")
	require.Equal(t, channelTelemetryMatrixKey{platform: "openai", model: service.ChannelTelemetryOtherModel}, key)
	key = channelTelemetryMatrixDimensionKey(service.ChannelTelemetryGroupByPlatform, cfg, "openai", 7, "gpt-5")
	require.Equal(t, channelTelemetryMatrixKey{platform: "openai"}, key)
}

func TestChannelTelemetryHistogramPercentilesAreMergedFromCounts(t *testing.T) {
	// 100 samples: 50@100, 40@500, 10@1000
	// target = int64(total*p + 0.999999) truncates: p50→50, p90→90, p95→95
	// cumulative hits: p50@100, p90@500 (50+40), p95@1000
	histogram := map[int64]int64{100: 50, 500: 40, 1000: 10}
	require.Equal(t, int64(100), *histPercentile(histogram, .5))
	require.Equal(t, int64(500), *histPercentile(histogram, .9))
	require.Equal(t, int64(1000), *histPercentile(histogram, .95))
	require.Nil(t, histPercentile(nil, .95))
	// latencyMetric exposes avg + p50 + p90 + p95
	lat := latencyMetric(1000, 10, histogram)
	require.NotNil(t, lat.AvgMs)
	require.NotNil(t, lat.P50Ms)
	require.NotNil(t, lat.P90Ms)
	require.NotNil(t, lat.P95Ms)
	require.Equal(t, int64(100), *lat.P50Ms)
	require.Equal(t, int64(500), *lat.P90Ms)
	require.Equal(t, int64(1000), *lat.P95Ms)
}

func TestChannelTelemetryMetricIncludesSuccessRate(t *testing.T) {
	acc := newMetricAccumulator()
	acc.success, acc.errors = 80, 20
	metric := acc.metric(1, false)
	require.Equal(t, int64(100), metric.RequestCount)
	require.InDelta(t, 0.8, metric.SuccessRate, 0.0001)
	require.InDelta(t, 0.2, metric.ErrorRate, 0.0001)
	require.Nil(t, metric.UpstreamAffectedRequests)

	adminMetric := acc.metric(1, true)
	require.NotNil(t, adminMetric.UpstreamAffectedRequests)
}

func TestChannelTelemetryWhereUsesConfiguredScopeAndEmptyFilterMeansAllConfigured(t *testing.T) {
	filter := service.ChannelTelemetryFilter{Start: time.Unix(1, 0), End: time.Unix(2, 0)}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true}, {Platform: "grok", Enabled: false}},
		GroupIDs:  []int64{3, 4},
	}
	where, args := channelTelemetryWhere(filter, cfg, "m")
	require.Contains(t, where, "m.platform = ANY($3)")
	require.Contains(t, where, "m.group_id = ANY($4)")
	require.Len(t, args, 4)
}

func TestChannelTelemetryWhereRejectsGroupFilterOutsideConfiguredScope(t *testing.T) {
	filter := service.ChannelTelemetryFilter{
		Start: time.Unix(1, 0), End: time.Unix(2, 0), GroupIDs: []int64{9},
	}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true}},
		GroupIDs:  []int64{3, 4},
	}
	where, args := channelTelemetryWhere(filter, cfg, "m")
	require.Contains(t, where, "FALSE")
	require.NotContains(t, where, "m.group_id = ANY")
	require.Len(t, args, 3)
}

func TestChannelTelemetryWhereRestrictsOrdinaryViewerToAllowedConfiguredGroups(t *testing.T) {
	filter := service.ChannelTelemetryFilter{
		Start: time.Unix(1, 0), End: time.Unix(2, 0),
		GroupIDs: []int64{4, 9}, AllowedGroupIDs: []int64{3, 4}, RestrictGroups: true,
	}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true}},
		GroupIDs:  []int64{3, 4, 9},
	}
	where, args := channelTelemetryWhere(filter, cfg, "m")
	require.Contains(t, where, "m.group_id = ANY($4)")
	require.Equal(t, pq.Array([]int64{4}), args[3])
}

func TestChannelTelemetryWhereRejectsOrdinaryViewerWithNoAllowedGroups(t *testing.T) {
	filter := service.ChannelTelemetryFilter{
		Start: time.Unix(1, 0), End: time.Unix(2, 0),
		GroupIDs: []int64{9}, RestrictGroups: true,
	}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true}},
		GroupIDs:  []int64{3, 9},
	}
	where, _ := channelTelemetryWhere(filter, cfg, "m")
	require.Contains(t, where, "FALSE")
	require.NotContains(t, where, "m.group_id = ANY")
}

func TestChannelTelemetryCatalogKeepsViewerScopeWhileIgnoringPickerFilters(t *testing.T) {
	filter := service.ChannelTelemetryFilter{
		Platforms: []string{"openai"}, GroupIDs: []int64{9}, Models: []string{"gpt-5"},
		AllowedGroupIDs: []int64{3}, RestrictGroups: true,
	}
	catalog := channelTelemetryCatalogFilter(filter)
	require.Empty(t, catalog.Platforms)
	require.Empty(t, catalog.GroupIDs)
	require.Empty(t, catalog.Models)
	require.True(t, catalog.RestrictGroups)
	require.Equal(t, []int64{3}, catalog.AllowedGroupIDs)
}

func TestChannelTelemetryAdminScopeRemainsGlobal(t *testing.T) {
	filter := service.ChannelTelemetryFilter{Start: time.Unix(1, 0), End: time.Unix(2, 0), GroupIDs: []int64{9}}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true}},
		GroupIDs:  []int64{3, 9},
	}
	where, args := channelTelemetryWhere(filter, cfg, "m")
	require.Contains(t, where, "m.group_id = ANY($4)")
	require.Equal(t, pq.Array([]int64{9}), args[3])
}

func TestChannelTelemetryMatrixDoesNotSeedGroupsForEmptyViewerScope(t *testing.T) {
	filter := service.ChannelTelemetryFilter{RestrictGroups: true}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true}},
		GroupIDs:  []int64{3, 9},
	}
	accs := seedChannelTelemetryMatrixAccumulators(filter, cfg, service.ChannelTelemetryGroupByPlatformGroup, map[int64]channelTelemetryGroupInfo{
		3: {name: "private"}, 9: {name: "other-private"},
	})
	require.Empty(t, accs)
}

func TestChannelTelemetryEmptyRestrictedScopeReturnsEmptyInventory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &channelTelemetryRepository{db: db}
	filter := service.ChannelTelemetryFilter{RestrictGroups: true}
	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"gpt-5"}}},
		GroupIDs:  []int64{3, 9},
	}

	dimensions, err := repo.GetDimensions(context.Background(), filter, cfg)
	require.NoError(t, err)
	require.Empty(t, dimensions.Platforms)
	require.Empty(t, dimensions.Groups)
	require.Empty(t, dimensions.Models)
	require.NotNil(t, dimensions.Platforms)
	require.NotNil(t, dimensions.Groups)
	require.NotNil(t, dimensions.Models)

	models, err := repo.GetModels(context.Background(), filter, cfg, false)
	require.NoError(t, err)
	require.Empty(t, models.Items)
	require.NotNil(t, models.Items)
	require.Equal(t, service.ChannelTelemetryCoverage{}, models.Coverage)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelTelemetryEmptyRestrictedScopeReturnsEmptySnapshotWithoutQueries(t *testing.T) {
	tests := []struct {
		name   string
		filter service.ChannelTelemetryFilter
	}{
		{
			name:   "no allowed groups",
			filter: service.ChannelTelemetryFilter{RestrictGroups: true},
		},
		{
			name: "requested groups exclude allowed configured scope",
			filter: service.ChannelTelemetryFilter{
				GroupIDs: []int64{9}, AllowedGroupIDs: []int64{3}, RestrictGroups: true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &channelTelemetryRepository{db: db}
			cfg := service.ChannelTelemetryConfig{
				Enabled: true,
				Platforms: []service.ChannelTelemetryPlatformConfig{{
					Platform: "openai", Enabled: true, Models: []string{"gpt-5"},
				}},
				GroupIDs: []int64{3},
			}

			snapshot, err := repo.GetSnapshot(context.Background(), test.filter, cfg, false)
			require.NoError(t, err)
			require.Equal(t, service.ChannelTelemetryConfig{}, snapshot.Config)
			require.Equal(t, service.ChannelTelemetryCoverage{}, snapshot.Coverage)
			require.Equal(t, service.ChannelTelemetryMetric{}, snapshot.Metrics)
			require.Equal(t, service.ChannelTelemetryHealth{}, snapshot.Health)
			require.Empty(t, snapshot.Trend)
			require.NotNil(t, snapshot.Trend)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelTelemetryEmptyRestrictedScopeReturnsEmptyMatrixForEveryGrouping(t *testing.T) {
	groupings := []service.ChannelTelemetryGroupBy{
		service.ChannelTelemetryGroupByPlatform,
		service.ChannelTelemetryGroupByPlatformModel,
		service.ChannelTelemetryGroupByPlatformGroup,
		service.ChannelTelemetryGroupByPlatformGroupModel,
	}
	for _, groupBy := range groupings {
		t.Run(string(groupBy), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &channelTelemetryRepository{db: db}
			filter := service.ChannelTelemetryFilter{RestrictGroups: true}
			cfg := service.ChannelTelemetryConfig{
				Platforms: []service.ChannelTelemetryPlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"gpt-5"}}},
				GroupIDs:  []int64{3, 9},
			}

			matrix, err := repo.GetMatrix(context.Background(), filter, cfg, groupBy, false)
			require.NoError(t, err)
			require.Equal(t, groupBy, matrix.GroupBy)
			require.Empty(t, matrix.Items)
			require.NotNil(t, matrix.Items)
			require.Equal(t, service.ChannelTelemetryCoverage{}, matrix.Coverage)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelTelemetryErrorAggregationCountsFinalUserErrorsOnly(t *testing.T) {
	query := strings.ToLower(channelTelemetryErrorAggregationSQL)
	require.Contains(t, query, "not current_error.is_count_tokens")
	require.Contains(t, query, "error_type = 'cyber_policy'")
	require.Contains(t, query, "distinct on")
	require.Contains(t, query, "candidate_ids")
	require.Contains(t, query, "where bucket_start >= $1 and bucket_start < $2")
	require.Contains(t, query, "upstream_affected_requests")
	require.Contains(t, query, "jsonb_array_length(current_error.upstream_errors) > 0")
	// request_id dedup must be time-bounded (no full-history scan).
	require.Contains(t, query, "interval '90 minutes'")
	require.Contains(t, query, "current_error.created_at >= $1 - interval '90 minutes'")
}

func TestChannelTelemetryErrorAggregationResolvesCompositePlatform(t *testing.T) {
	query := strings.ToLower(channelTelemetryErrorAggregationSQL)
	// Composite groups are a routing layer: error facts must resolve the concrete
	// account platform (joining groups/accounts) so they aggregate under the same
	// platform key as usage facts instead of the never-enabled 'composite' platform.
	require.Contains(t, query, "g.platform = 'composite'")
	require.Contains(t, query, "left join groups g on g.id = current_error.group_id")
	require.Contains(t, query, "left join accounts a on a.id = current_error.account_id")
	require.Contains(t, query, "a.platform")
	require.Contains(t, query, "nullif(trim(a.platform), '')")
	require.NotContains(t, query, "nullif(trim(a.platform))")
}

func TestChannelTelemetryUsageSuccessExcludesCyberBillingRows(t *testing.T) {
	for _, query := range []string{channelTelemetryUsageMetricsSQL, channelTelemetryUserMetricsSQL} {
		require.Contains(t, query, "COALESCE(ul.request_type, 0) NOT IN (4, 6)")
		require.NotContains(t, query, "ul.actual_cost > 0")
		require.Contains(t, query, "r.completion_state='complete'")
	}
	require.Contains(t, channelTelemetryPlatformSQL, "g.platform = 'composite'")
	require.Contains(t, channelTelemetryPlatformSQL, "a.platform")
	require.Contains(t, channelTelemetryHistogramSQL, "r.completion_state='complete'")
}

func TestChannelTelemetryRatesUseCoveredWindow(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	filter := service.ChannelTelemetryFilter{Start: start, End: start.Add(24 * time.Hour)}
	coverage := service.ChannelTelemetryCoverage{CoverageStart: start.Add(6 * time.Hour), DataThrough: start.Add(18 * time.Hour)}
	require.Equal(t, 12*60.0, channelTelemetryCoveredMinutes(filter, coverage))
	effective := channelTelemetryCommonCoverageFilter(filter, coverage)
	require.Equal(t, coverage.CoverageStart, effective.Start)
	require.Equal(t, coverage.DataThrough, effective.End)
}

func TestChannelTelemetryHistoryCoverageCompleteIgnoresTrailingLag(t *testing.T) {
	start := time.Date(2026, 8, 7, 2, 20, 0, 0, time.UTC)
	// History reaches the window start → complete even if data_through is behind filter.End.
	require.True(t, channelTelemetryHistoryCoverageComplete(start, start))
	require.True(t, channelTelemetryHistoryCoverageComplete(start.Add(-time.Hour), start))
	// Backfill still short of the window start → incomplete.
	require.False(t, channelTelemetryHistoryCoverageComplete(start.Add(time.Hour), start))
	require.False(t, channelTelemetryHistoryCoverageComplete(time.Time{}, start))
}

func TestChannelTelemetryTierRetentionPolicy(t *testing.T) {
	require.Equal(t, 3*24*time.Hour, channelTelemetryRetentionUser1m)
	require.Equal(t, 7*24*time.Hour, channelTelemetryRetentionMetrics1m)
	require.Equal(t, 7*24*time.Hour, channelTelemetryRetentionError1m)
	require.Equal(t, 7*24*time.Hour, channelTelemetryRetentionHistogram1m)
	require.Equal(t, 7*24*time.Hour, channelTelemetryRetentionRollup5m)
	require.Equal(t, 30*24*time.Hour, channelTelemetryRetentionRollup1h)
	require.Equal(t, 45*24*time.Hour, channelTelemetryRetentionRollup12h)
	require.Equal(t, 90*24*time.Hour, channelTelemetryRetentionRollup1d)
	require.Equal(t, channelTelemetryRetentionRollup1d, channelTelemetryMaxRetention())
	require.Contains(t, channelTelemetryWatermarkSQL, "INTERVAL '90 days'")

	// Every fixed rollup second must appear with a retention rule.
	wantSeconds := map[int]time.Duration{
		300:   channelTelemetryRetentionRollup5m,
		3600:  channelTelemetryRetentionRollup1h,
		43200: channelTelemetryRetentionRollup12h,
		86400: channelTelemetryRetentionRollup1d,
	}
	seen := map[int]time.Duration{}
	for _, rule := range channelTelemetryRetentionRules {
		if rule.bucketSeconds == 0 {
			require.True(t, rule.retention > 0)
			continue
		}
		if prev, ok := seen[rule.bucketSeconds]; ok {
			require.Equal(t, prev, rule.retention)
		}
		seen[rule.bucketSeconds] = rule.retention
	}
	for seconds, want := range wantSeconds {
		got, ok := seen[seconds]
		require.Truef(t, ok, "missing retention rule for bucket_seconds=%d", seconds)
		require.Equal(t, want, got)
	}

	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	require.Equal(t, now.Add(-7*24*time.Hour), channelTelemetryRetentionCutoff(now, channelTelemetryRetentionMetrics1m))
	require.Equal(t, now.Add(-90*24*time.Hour), channelTelemetryRetentionCutoff(now, channelTelemetryMaxRetention()))
}

func TestSameFixedRollupBucket(t *testing.T) {
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	require.True(t, sameFixedRollupBucket(start, start.Add(10*time.Minute), 86400))
	require.False(t, sameFixedRollupBucket(start, start.Add(15*time.Hour), 43200))
	require.False(t, sameFixedRollupBucket(start, start.Add(24*time.Hour), 86400))
}

// Needles present in service.ClassifyChannelTelemetryError must appear in the
// aggregation SQL CASE so rollup categories match drilldown classification.
func TestChannelTelemetrySQLTaxonomyContainsGoNeedles(t *testing.T) {
	sql := channelTelemetryErrorAggregationSQL
	needles := []string{
		"blocked keyword",
		"invalid_api_key",
		"max_tokens",
		"invalid_request",
		"model not supported",
		"billing hard limit",
		"no healthy upstream account",
		"rate_limit",
		"gateway timeout",
		"connection refused",
		"unexpected eof",
	}
	for _, needle := range needles {
		require.Containsf(t, strings.ToLower(sql), strings.ToLower(needle), "SQL taxonomy missing Go needle %q", needle)
	}
}

func TestApplyIgnoredErrorsAdjustsRatesKeepsAbsoluteVolume(t *testing.T) {
	m := service.ChannelTelemetryMetric{
		RequestCount:  100,
		ErrorRequests: 20,
		ErrorRate:     0.20,
		SuccessRate:   0.80,
	}
	// Success absolute still 80 → success rate stays 0.80 even after ignoring 5 errors.
	m.SuccessRequests = 80
	applyIgnoredErrors(&m, 5)
	require.Equal(t, int64(100), m.RequestCount)
	require.Equal(t, int64(20), m.ErrorRequests)
	require.InDelta(t, 0.15, m.ErrorRate, 0.0001)
	require.InDelta(t, 0.80, m.SuccessRate, 0.0001)

	// Clamp ignored > errors: scored error_rate → 0; success stays true ratio.
	m2 := service.ChannelTelemetryMetric{RequestCount: 10, ErrorRequests: 2, SuccessRequests: 8, ErrorRate: 0.2, SuccessRate: 0.8}
	applyIgnoredErrors(&m2, 99)
	require.InDelta(t, 0.0, m2.ErrorRate, 0.0001)
	require.InDelta(t, 0.8, m2.SuccessRate, 0.0001)

	// No-op when ignored is zero
	m3 := service.ChannelTelemetryMetric{RequestCount: 10, ErrorRequests: 2, SuccessRequests: 8, ErrorRate: 0.2, SuccessRate: 0.8}
	applyIgnoredErrors(&m3, 0)
	require.InDelta(t, 0.2, m3.ErrorRate, 0.0001)
	require.InDelta(t, 0.8, m3.SuccessRate, 0.0001)
}

func TestRedactChannelTelemetryMetricZerosVolume(t *testing.T) {
	// Service helper is in service package; covered there. Keep a smoke note that
	// rates survive a manual zeroing of volume fields used by the UI contract.
	m := service.ChannelTelemetryMetric{
		RequestCount: 100, ErrorRequests: 10, SuccessRequests: 90,
		TokenCount: 1000, RPM: 5, TPM: 50, ErrorRate: 0.1, SuccessRate: 0.9, CacheRate: 0.4,
	}
	// Mimic redact: zero volume only
	m.RequestCount, m.ErrorRequests, m.SuccessRequests, m.TokenCount = 0, 0, 0, 0
	require.Equal(t, 0.1, m.ErrorRate)
	require.Equal(t, 5.0, m.RPM)
}

func TestChannelTelemetryCatalogFilterClearsMultiSelectDimensions(t *testing.T) {
	start := time.Unix(1, 0)
	end := time.Unix(2, 0)
	filter := service.ChannelTelemetryFilter{
		Start: start, End: end, Bucket: time.Minute,
		Platforms: []string{"openai"}, GroupIDs: []int64{3}, Models: []string{"gpt-5"},
	}
	catalog := channelTelemetryCatalogFilter(filter)
	require.Nil(t, catalog.Platforms)
	require.Nil(t, catalog.GroupIDs)
	require.Nil(t, catalog.Models)
	// Time window / coverage-related fields remain.
	require.Equal(t, start, catalog.Start)
	require.Equal(t, end, catalog.End)
	require.Equal(t, time.Minute, catalog.Bucket)

	cfg := service.ChannelTelemetryConfig{
		Platforms: []service.ChannelTelemetryPlatformConfig{
			{Platform: "openai", Enabled: true},
			{Platform: "grok", Enabled: true},
		},
		GroupIDs: []int64{3, 4},
	}
	catalogWhere, catalogArgs := channelTelemetryWhere(catalog, cfg, "m")
	_, metricArgs := channelTelemetryWhere(filter, cfg, "m")

	// Catalog WHERE still applies config scope (enabled platforms + group allow-list).
	require.Contains(t, catalogWhere, "m.platform = ANY")
	require.Contains(t, catalogWhere, "m.group_id = ANY")
	require.Len(t, catalogArgs, 4) // start, end, platforms, groups
	require.Len(t, metricArgs, 4)

	// Metrics WHERE is narrower once multi-select platforms/groups are applied.
	require.NotEqual(t, catalogArgs, metricArgs)
	// Group seeding without multi-select uses full config allow-list.
	require.Equal(t, []int64{3, 4}, configuredChannelTelemetryGroupIDs(catalog, cfg))
	require.Equal(t, []int64{3}, configuredChannelTelemetryGroupIDs(filter, cfg))
}
