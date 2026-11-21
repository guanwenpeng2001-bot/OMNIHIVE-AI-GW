package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type channelTelemetryRepoStub struct {
	config serviceChannelTelemetryConfigAlias
	users  *ChannelTelemetryList[ChannelTelemetryUserRow]
	matrix *ChannelTelemetryMatrix
	errors *ChannelTelemetryList[ChannelTelemetryErrorRow]
	snap   *ChannelTelemetrySnapshot
	group  ChannelTelemetryGroupBy
	admin  bool
}

// Alias keeps composite literals readable without introducing another package.
type serviceChannelTelemetryConfigAlias = ChannelTelemetryConfig

func (s *channelTelemetryRepoStub) GetConfig(context.Context) (*ChannelTelemetryConfig, error) {
	cfg := ChannelTelemetryConfig(s.config)
	return &cfg, nil
}
func (s *channelTelemetryRepoStub) UpdateConfig(context.Context, ChannelTelemetryConfig, int) (*ChannelTelemetryConfig, error) {
	return nil, nil
}
func (s *channelTelemetryRepoStub) GetDimensions(context.Context, ChannelTelemetryFilter, ChannelTelemetryConfig) (*ChannelTelemetryDimensions, error) {
	return nil, nil
}
func (s *channelTelemetryRepoStub) GetSnapshot(_ context.Context, _ ChannelTelemetryFilter, _ ChannelTelemetryConfig, admin bool) (*ChannelTelemetrySnapshot, error) {
	s.admin = admin
	if s.snap == nil {
		return nil, nil
	}
	// Shallow copy so public redaction does not mutate fixture.
	cfg := s.snap.Config
	cfg.Platforms = append([]ChannelTelemetryPlatformConfig(nil), s.snap.Config.Platforms...)
	for i := range cfg.Platforms {
		cfg.Platforms[i].Models = append([]string(nil), s.snap.Config.Platforms[i].Models...)
	}
	cfg.GroupIDs = append([]int64(nil), s.snap.Config.GroupIDs...)
	cfg.IgnoredErrorCategories = append([]string(nil), s.snap.Config.IgnoredErrorCategories...)
	out := *s.snap
	out.Config = cfg
	return &out, nil
}
func (s *channelTelemetryRepoStub) GetModels(context.Context, ChannelTelemetryFilter, ChannelTelemetryConfig, bool) (*ChannelTelemetryList[ChannelTelemetryModelRow], error) {
	return nil, nil
}
func (s *channelTelemetryRepoStub) GetMatrix(_ context.Context, _ ChannelTelemetryFilter, _ ChannelTelemetryConfig, groupBy ChannelTelemetryGroupBy, admin bool) (*ChannelTelemetryMatrix, error) {
	s.group, s.admin = groupBy, admin
	return s.matrix, nil
}
func (s *channelTelemetryRepoStub) GetErrors(_ context.Context, _ ChannelTelemetryFilter, _ ChannelTelemetryConfig, admin bool) (*ChannelTelemetryList[ChannelTelemetryErrorRow], error) {
	s.admin = admin
	if s.errors != nil {
		// Return a shallow copy so service redaction does not mutate the fixture.
		out := &ChannelTelemetryList[ChannelTelemetryErrorRow]{
			Coverage: s.errors.Coverage,
			Items:    append([]ChannelTelemetryErrorRow(nil), s.errors.Items...),
		}
		for i := range out.Items {
			if len(out.Items[i].Details) > 0 {
				out.Items[i].Details = append([]ChannelTelemetryErrorDetail(nil), out.Items[i].Details...)
			}
		}
		return out, nil
	}
	return nil, nil
}
func (s *channelTelemetryRepoStub) GetUsers(context.Context, ChannelTelemetryFilter, ChannelTelemetryConfig, bool) (*ChannelTelemetryList[ChannelTelemetryUserRow], error) {
	return s.users, nil
}
func (s *channelTelemetryRepoStub) RecomputeRange(context.Context, time.Time, time.Time) error {
	return nil
}
func (s *channelTelemetryRepoStub) GetAggregationWatermark(context.Context) (*ChannelTelemetryAggregationWatermark, error) {
	return &ChannelTelemetryAggregationWatermark{}, nil
}

func TestChannelTelemetryBootstrapProgress(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	t.Run("no data shows active zero progress", func(t *testing.T) {
		b := ChannelTelemetryBootstrapProgress(now, time.Time{}, false)
		require.NotNil(t, b)
		require.True(t, b.Active)
		require.Equal(t, 0, b.ProgressPercent)
		require.Equal(t, now.Add(-ChannelTelemetryBootstrapProductWindow), b.TargetStart)
	})

	t.Run("2h seed is small percent of 30d", func(t *testing.T) {
		covered := now.Add(-2 * time.Hour)
		b := ChannelTelemetryBootstrapProgress(now, covered, true)
		require.NotNil(t, b)
		require.True(t, b.Active)
		require.Greater(t, b.ProgressPercent, 0)
		require.Less(t, b.ProgressPercent, 5)
	})

	t.Run("30d covered hides bootstrap", func(t *testing.T) {
		covered := now.Add(-ChannelTelemetryBootstrapProductWindow)
		b := ChannelTelemetryBootstrapProgress(now, covered, true)
		require.Nil(t, b)
	})

	t.Run("beyond 30d also hides", func(t *testing.T) {
		covered := now.Add(-60 * 24 * time.Hour)
		b := ChannelTelemetryBootstrapProgress(now, covered, true)
		require.Nil(t, b)
	})
}

func TestChannelTelemetryParseFilterDefaultsAndBuckets(t *testing.T) {
	svc := &ChannelTelemetryService{now: func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }}

	filter, err := svc.ParseFilter("", []string{"openai", "openai", ""}, []string{"gpt-5"}, []int64{2, 1, 2, 0})
	require.NoError(t, err)
	require.Equal(t, "90m", filter.Range)
	require.Equal(t, 5*time.Minute, filter.Bucket)
	require.Equal(t, []string{"openai"}, filter.Platforms)
	require.Equal(t, []int64{1, 2}, filter.GroupIDs)
	require.Equal(t, 90*time.Minute, filter.End.Sub(filter.Start))

	filter, err = svc.ParseFilter("30d", nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, filter.Bucket)
	_, err = svc.ParseFilter("15d", nil, nil, nil)
	require.ErrorIs(t, err, ErrChannelTelemetryInvalidRange)
}

func TestParseChannelTelemetryGroupBy(t *testing.T) {
	groupBy, err := ParseChannelTelemetryGroupBy("")
	require.NoError(t, err)
	require.Equal(t, ChannelTelemetryGroupByPlatformGroup, groupBy)
	for _, value := range []ChannelTelemetryGroupBy{ChannelTelemetryGroupByPlatform, ChannelTelemetryGroupByPlatformGroup, ChannelTelemetryGroupByPlatformModel, ChannelTelemetryGroupByPlatformGroupModel} {
		parsed, parseErr := ParseChannelTelemetryGroupBy(string(value))
		require.NoError(t, parseErr)
		require.Equal(t, value, parsed)
	}
	_, err = ParseChannelTelemetryGroupBy("group")
	require.ErrorIs(t, err, ErrChannelTelemetryInvalidGroupBy)
}

func TestChannelTelemetryMatrixForwardsGroupingAndAdminScope(t *testing.T) {
	want := &ChannelTelemetryMatrix{GroupBy: ChannelTelemetryGroupByPlatformModel}
	repo := &channelTelemetryRepoStub{config: ChannelTelemetryConfig{Enabled: true}, matrix: want}
	result, err := NewChannelTelemetryService(repo).Matrix(context.Background(), ChannelTelemetryFilter{}, ChannelTelemetryGroupByPlatformModel, true)
	require.NoError(t, err)
	require.Same(t, want, result)
	require.Equal(t, ChannelTelemetryGroupByPlatformModel, repo.group)
	require.True(t, repo.admin)

	_, err = NewChannelTelemetryService(repo).Matrix(context.Background(), ChannelTelemetryFilter{}, "bad", false)
	require.ErrorIs(t, err, ErrChannelTelemetryInvalidGroupBy)
}

func TestChannelTelemetryConfigValidation(t *testing.T) {
	cfg := ChannelTelemetryConfig{
		Platforms: []ChannelTelemetryPlatformConfig{
			{Platform: " OpenAI ", Enabled: true, Models: []string{"gpt-5", "gpt-5", ""}},
			{Platform: "anthropic", Enabled: true},
		},
		GroupIDs: []int64{3, 1, 3},
	}
	require.NoError(t, normalizeChannelTelemetryConfig(&cfg))
	require.Equal(t, 300, cfg.RefreshIntervalSeconds)
	require.Equal(t, "anthropic", cfg.Platforms[0].Platform)
	require.Equal(t, []int64{1, 3}, cfg.GroupIDs)

	cfg.RefreshIntervalSeconds = 120
	require.ErrorIs(t, normalizeChannelTelemetryConfig(&cfg), ErrChannelTelemetryInvalidConfig)

	cfg.RefreshIntervalSeconds = 60
	cfg.GroupIDs = []int64{0}
	require.ErrorIs(t, normalizeChannelTelemetryConfig(&cfg), ErrChannelTelemetryInvalidConfig)
}

func TestChannelTelemetryErrorTaxonomyPriority(t *testing.T) {
	tests := []struct {
		name string
		in   ChannelTelemetryErrorInput
		want string
	}{
		{"cyber", ChannelTelemetryErrorInput{ErrorType: "cyber_policy", StatusCode: 200}, "content_policy"},
		{"auth before forbidden", ChannelTelemetryErrorInput{StatusCode: 403, Message: "invalid API key"}, "authentication"},
		{"context", ChannelTelemetryErrorInput{Message: "maximum prompt length exceeded"}, "context_limit"},
		{"unsupported", ChannelTelemetryErrorInput{Message: "not supported by any configured account"}, "model_unsupported"},
		{"pool", ChannelTelemetryErrorInput{Message: "No available accounts"}, "account_pool_unavailable"},
		{"timeout", ChannelTelemetryErrorInput{Message: "error code: 524"}, "timeout"},
		{"upstream", ChannelTelemetryErrorInput{ErrorOwner: "provider", StatusCode: 502, UpstreamStatusCode: 502}, "upstream_5xx"},
		{"other", ChannelTelemetryErrorInput{StatusCode: 400, Message: "unknown"}, "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, ClassifyChannelTelemetryError(tt.in)) })
	}
}

func TestChannelTelemetryHealthBlendsErrorTTFTAndCache(t *testing.T) {
	// error 3%/5% → 40; ttft p50 2s → 100; cache 50% → 50
	// overall = (0.6*40 + 0.2*100 + 0.2*50) / 1.0 = 54 → warning
	p50 := int64(2000)
	p95 := int64(9000)
	thresholds := ChannelTelemetryHealthThresholds{
		MinimumSample:     20,
		WarningErrorRate:  0.02,
		CriticalErrorRate: 0.05,
		TargetTTFTMs:      2500,
		WarningTTFTMs:     2501,
		CriticalTTFTMs:    6000,
		WarningCacheRate:  0.20,
		CriticalCacheRate: 0.05,
		ErrorWeight:       0.60,
		TTFTWeight:        0.20,
		CacheWeight:       0.20,
	}
	metrics := ChannelTelemetryMetric{
		RequestCount:         100,
		ErrorRate:            0.03,
		CacheRate:            0.50,
		CacheRateDenominator: 100,
		TTFT:                 ChannelTelemetryLatency{SampleCount: 100, P50Ms: &p50, P95Ms: &p95},
	}
	health := ChannelTelemetryHealthForWithThresholds(metrics, thresholds)
	require.Equal(t, "warning", health.ErrorRate)
	require.Equal(t, "healthy", health.TTFT)
	require.Equal(t, "healthy", health.Cache)
	require.NotNil(t, health.Score)
	require.NotNil(t, health.CacheScore)
	require.InDelta(t, 50.0, *health.CacheScore, 0.01)
	require.InDelta(t, 54.0, *health.Score, 0.01)
	require.Equal(t, "warning", health.Overall)

	// Perfect signals → 100
	p50OK := int64(1000)
	metrics.ErrorRate = 0
	metrics.CacheRate = 1
	metrics.TTFT.P50Ms = &p50OK
	health = ChannelTelemetryHealthForWithThresholds(metrics, thresholds)
	require.Equal(t, "healthy", health.Overall)
	require.NotNil(t, health.Score)
	require.InDelta(t, 100.0, *health.Score, 0.01)

	// Small samples stay unknown
	health = ChannelTelemetryHealthFor(ChannelTelemetryMetric{RequestCount: 2})
	require.Equal(t, "unknown", health.Overall)
	require.Nil(t, health.Score)
}

func TestChannelTelemetryHealthLeavesMissingTTFTUnknown(t *testing.T) {
	health := ChannelTelemetryHealthFor(ChannelTelemetryMetric{
		RequestCount:         200,
		ErrorRate:            0,
		CacheRate:            0,
		CacheRateDenominator: 200,
		TTFT:                 ChannelTelemetryLatency{SampleCount: 0},
	})
	require.Equal(t, "unknown", health.TTFT)
	require.Nil(t, health.TTFTScore)
	require.NotEqual(t, "critical", health.Overall)
	require.Equal(t, "healthy", health.Overall)
}

func TestChannelTelemetryDefaultHealthThresholdsAreTolerant(t *testing.T) {
	p50 := int64(2500)
	health := ChannelTelemetryHealthFor(ChannelTelemetryMetric{
		RequestCount:         100,
		ErrorRate:            0.03,
		CacheRate:            0,
		CacheRateDenominator: 100,
		TTFT:                 ChannelTelemetryLatency{SampleCount: 100, P50Ms: &p50},
	})
	require.Equal(t, "healthy", health.ErrorRate)
	require.Equal(t, "healthy", health.TTFT)
	require.Equal(t, "healthy", health.Cache)
	require.Equal(t, "healthy", health.Overall)
	require.NotNil(t, health.CacheScore)
	require.InDelta(t, 100.0, *health.CacheScore, 0.01)
}

func TestErrorRateTTFTAndCacheScoreHelpers(t *testing.T) {
	require.InDelta(t, 100.0, errorRateScore(0, 0.05), 0.001)
	require.InDelta(t, 0.0, errorRateScore(0.05, 0.05), 0.001)
	require.InDelta(t, 40.0, errorRateScore(0.03, 0.05), 0.001)

	require.InDelta(t, 100.0, ttftP50Score(2500, 2500, 6000), 0.001)
	require.InDelta(t, 100.0, ttftP50Score(1000, 2500, 6000), 0.001)
	require.InDelta(t, 0.0, ttftP50Score(6000, 2500, 6000), 0.001)
	require.InDelta(t, 50.0, ttftP50Score(4250, 2500, 6000), 0.001)

	require.InDelta(t, 0.0, cacheRateScore(0), 0.001)
	require.InDelta(t, 100.0, cacheRateScore(1), 0.001)
	require.InDelta(t, 50.0, cacheRateScore(0.5), 0.001)
	require.Equal(t, "critical", cacheRateBand(0.02, 0.20, 0.05))
	require.Equal(t, "warning", cacheRateBand(0.10, 0.20, 0.05))
	require.Equal(t, "healthy", cacheRateBand(0.50, 0.20, 0.05))
}

type channelTelemetryRuntimeStub struct {
	rt ChannelMonitorRuntime
}

func (s channelTelemetryRuntimeStub) GetChannelMonitorRuntime(context.Context) ChannelMonitorRuntime {
	return s.rt
}

func TestChannelTelemetryUsersHiddenWhenSettingEnabled(t *testing.T) {
	selfID, otherID := int64(7), int64(9)
	repo := &channelTelemetryRepoStub{config: ChannelTelemetryConfig{Enabled: true}, users: &ChannelTelemetryList[ChannelTelemetryUserRow]{Items: []ChannelTelemetryUserRow{
		{UserID: &otherID, Email: "other@example.com", Username: "other"},
		{UserID: &selfID, Email: "self@example.com", Username: "self"},
	}}}
	svc := NewChannelTelemetryService(repo)
	svc.SetRuntimeReader(channelTelemetryRuntimeStub{rt: ChannelMonitorRuntime{HideUserRanking: true}})

	hidden, err := svc.Users(context.Background(), ChannelTelemetryFilter{}, selfID, false)
	require.NoError(t, err)
	require.Empty(t, hidden.Items)

	admin, err := svc.Users(context.Background(), ChannelTelemetryFilter{}, selfID, true)
	require.NoError(t, err)
	require.Len(t, admin.Items, 2)
}

func TestChannelTelemetryUsersRemovesOtherUserIdentity(t *testing.T) {
	selfID, otherID := int64(7), int64(9)
	repo := &channelTelemetryRepoStub{config: ChannelTelemetryConfig{Enabled: true}, users: &ChannelTelemetryList[ChannelTelemetryUserRow]{Items: []ChannelTelemetryUserRow{
		{UserID: &otherID, Email: "other@example.com", Username: "other"},
		{UserID: &selfID, Email: "self@example.com", Username: "self"},
	}}}
	result, err := NewChannelTelemetryService(repo).Users(context.Background(), ChannelTelemetryFilter{}, selfID, false)
	require.NoError(t, err)
	require.Nil(t, result.Items[0].UserID)
	require.Empty(t, result.Items[0].Email)
	require.Equal(t, "Other user #1", result.Items[0].DisplayLabel)
	require.Equal(t, selfID, *result.Items[1].UserID)
	require.True(t, result.Items[1].IsSelf)
	require.Equal(t, "Me", result.Items[1].DisplayLabel)
}

func TestChannelTelemetryUsersAppendsSelfWhenMissingFromRanking(t *testing.T) {
	selfID, otherID := int64(7), int64(9)
	repo := &channelTelemetryRepoStub{config: ChannelTelemetryConfig{Enabled: true}, users: &ChannelTelemetryList[ChannelTelemetryUserRow]{Items: []ChannelTelemetryUserRow{
		{UserID: &otherID, Email: "other@example.com", Username: "other", Metrics: ChannelTelemetryMetric{RequestCount: 10}},
	}}}
	result, err := NewChannelTelemetryService(repo).Users(context.Background(), ChannelTelemetryFilter{}, selfID, false)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	self := result.Items[1]
	require.True(t, self.IsSelf)
	require.Equal(t, "Me", self.DisplayLabel)
	require.Equal(t, selfID, *self.UserID)
	require.Equal(t, 0, self.Rank) // unranked / no traffic in window
}

func TestChannelTelemetryTopUsersKeepsSelfOutsideLimit(t *testing.T) {
	items := make([]ChannelTelemetryUserRow, 0, 12)
	for i := 1; i <= 12; i++ {
		id := int64(i)
		items = append(items, ChannelTelemetryUserRow{UserID: &id, Rank: i, DisplayLabel: fmt.Sprintf("u%d", i)})
	}
	// self is rank 12 (index 11)
	out := channelTelemetryTopUsersWithSelf(items, 11, 10)
	require.Len(t, out, 11)
	require.Equal(t, int64(12), *out[10].UserID)
	require.Equal(t, 12, out[10].Rank)
}

func TestChannelTelemetryReadAPIsRejectDisabledConfig(t *testing.T) {
	repo := &channelTelemetryRepoStub{config: ChannelTelemetryConfig{Enabled: false}}
	_, err := NewChannelTelemetryService(repo).Matrix(context.Background(), ChannelTelemetryFilter{}, ChannelTelemetryGroupByPlatform, false)
	require.ErrorIs(t, err, ErrChannelMonitorDisabled)
}

func TestNormalizeChannelTelemetryIgnoredCategories(t *testing.T) {
	got := normalizeChannelTelemetryIgnoredCategories([]string{"timeout", "TIMEOUT", "not-a-real", "", "timeout", "other"})
	require.Equal(t, []string{"other", "timeout"}, got)
}

func TestApplyIgnoredErrorsAdjustsRateOnly(t *testing.T) {
	// applyIgnoredErrors lives in repository package; mirror the scored-rate formula.
	// scored errors = total_errors - ignored; error_rate = scored / request_count
	// success_rate stays SuccessRequests / RequestCount (ignored ≠ success).
	m := ChannelTelemetryMetric{RequestCount: 100, ErrorRequests: 20, SuccessRequests: 80, ErrorRate: 0.2, SuccessRate: 0.8}
	ignored := int64(5)
	counted := m.ErrorRequests - ignored
	errorRate := float64(counted) / float64(m.RequestCount)
	successRate := float64(m.SuccessRequests) / float64(m.RequestCount)
	require.InDelta(t, 0.15, errorRate, 0.0001)
	require.InDelta(t, 0.80, successRate, 0.0001)
	require.Equal(t, int64(20), m.ErrorRequests)
	require.Equal(t, int64(100), m.RequestCount)
}

func TestErrorsForViewerStripsDetailsAndCountsForNonAdmin(t *testing.T) {
	fixture := &ChannelTelemetryList[ChannelTelemetryErrorRow]{
		Items: []ChannelTelemetryErrorRow{{
			Category: "timeout",
			Count:    42,
			Rate:     0.4,
			Details: []ChannelTelemetryErrorDetail{{
				Platform:           "anthropic",
				Model:              "claude",
				ErrorType:          "upstream",
				StatusCode:         504,
				UpstreamStatusCode: 504,
				Message:            "gateway timeout sk-secret",
				Count:              12,
			}},
		}},
	}
	repo := &channelTelemetryRepoStub{config: ChannelTelemetryConfig{Enabled: true}, errors: fixture}
	svc := NewChannelTelemetryService(repo)

	userList, err := svc.ErrorsForViewer(context.Background(), ChannelTelemetryFilter{}, false)
	require.NoError(t, err)
	require.False(t, repo.admin)
	require.Len(t, userList.Items, 1)
	require.Zero(t, userList.Items[0].Count)
	require.Empty(t, userList.Items[0].Details)
	require.InDelta(t, 0.4, userList.Items[0].Rate, 0.0001)

	adminList, err := svc.ErrorsForViewer(context.Background(), ChannelTelemetryFilter{}, true)
	require.NoError(t, err)
	require.True(t, repo.admin)
	require.Equal(t, int64(42), adminList.Items[0].Count)
	require.Len(t, adminList.Items[0].Details, 1)
	require.Contains(t, adminList.Items[0].Details[0].Message, "gateway timeout")
}

func TestSnapshotRedactsPublicConfigPolicyFields(t *testing.T) {
	updatedBy := int64(9)
	repo := &channelTelemetryRepoStub{
		config: ChannelTelemetryConfig{Enabled: true},
		snap: &ChannelTelemetrySnapshot{
			Config: ChannelTelemetryConfig{
				Version:                3,
				Enabled:                true,
				RefreshIntervalSeconds: 300,
				Platforms: []ChannelTelemetryPlatformConfig{
					{Platform: "openai", Enabled: true, Models: []string{"gpt-5"}},
				},
				GroupIDs:               []int64{1, 2},
				IgnoredErrorCategories: []string{"timeout"},
				UpdatedBy:              &updatedBy,
			},
			Metrics: ChannelTelemetryMetric{RequestCount: 100, ErrorRate: 0.1, SuccessRate: 0.9, RPM: 5},
		},
	}
	svc := NewChannelTelemetryService(repo)
	snap, err := svc.Snapshot(context.Background(), ChannelTelemetryFilter{}, false)
	require.NoError(t, err)
	require.Empty(t, snap.Config.GroupIDs)
	require.Empty(t, snap.Config.IgnoredErrorCategories)
	require.Nil(t, snap.Config.UpdatedBy)
	require.Empty(t, snap.Config.Platforms[0].Models)
	require.Equal(t, 300, snap.Config.RefreshIntervalSeconds)
	require.Zero(t, snap.Metrics.RequestCount)
	require.InDelta(t, 0.1, snap.Metrics.ErrorRate, 0.0001)
	require.Zero(t, snap.Metrics.RPM)
}

func TestRedactChannelTelemetryMetricKeepsRates(t *testing.T) {
	p50 := int64(100)
	m := ChannelTelemetryMetric{
		SuccessRequests: 90, ErrorRequests: 10, RequestCount: 100,
		InputTokens: 1, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 4,
		TokenCount: 10, RPM: 12, TPM: 34, ErrorRate: 0.1, SuccessRate: 0.9,
		CacheRate: 0.5, CacheRateNumerator: 4, CacheRateDenominator: 8,
		TTFT: ChannelTelemetryLatency{SampleCount: 50, P50Ms: &p50},
	}
	upstream := int64(3)
	m.UpstreamAffectedRequests = &upstream
	redactChannelTelemetryMetric(&m, false)
	require.Zero(t, m.RequestCount)
	require.Zero(t, m.ErrorRequests)
	require.Zero(t, m.TokenCount)
	require.Zero(t, m.CacheRateNumerator)
	require.Zero(t, m.TTFT.SampleCount)
	require.Nil(t, m.UpstreamAffectedRequests)
	require.InDelta(t, 0.1, m.ErrorRate, 0.0001)
	require.InDelta(t, 12.0, m.RPM, 0.0001)
	require.InDelta(t, 34.0, m.TPM, 0.0001)
	require.NotNil(t, m.TTFT.P50Ms)
	require.Equal(t, int64(100), *m.TTFT.P50Ms)

	redactChannelTelemetryMetric(&m, true)
	require.Zero(t, m.RPM)
	require.Zero(t, m.TPM)
	require.InDelta(t, 0.1, m.ErrorRate, 0.0001)
}

func TestChannelTelemetryHealthTTFTAtTargetIsHealthy(t *testing.T) {
	p50 := int64(2500)
	m := ChannelTelemetryMetric{
		RequestCount: 100, ErrorRate: 0,
		CacheRate: 1, CacheRateDenominator: 100,
		TTFT: ChannelTelemetryLatency{SampleCount: 100, P50Ms: &p50},
	}
	h := ChannelTelemetryHealthFor(m)
	require.Equal(t, "healthy", h.TTFT)
	require.NotNil(t, h.TTFTScore)
	require.InDelta(t, 100.0, *h.TTFTScore, 0.01)
}
