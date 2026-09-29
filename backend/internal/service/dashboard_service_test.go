package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type usageRepoStub struct {
	UsageLogRepository
	stats      *usagestats.DashboardStats
	rangeStats *usagestats.DashboardStats
	err        error
	rangeErr   error
	calls      int32
	rangeCalls int32
	rangeStart time.Time
	rangeEnd   time.Time
	onCall     chan struct{}
}

func (s *usageRepoStub) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.onCall != nil {
		select {
		case s.onCall <- struct{}{}:
		default:
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.stats, nil
}

func (s *usageRepoStub) GetDashboardStatsWithRange(ctx context.Context, start, end time.Time) (*usagestats.DashboardStats, error) {
	atomic.AddInt32(&s.rangeCalls, 1)
	s.rangeStart = start
	s.rangeEnd = end
	if s.rangeErr != nil {
		return nil, s.rangeErr
	}
	if s.rangeStats != nil {
		return s.rangeStats, nil
	}
	return s.stats, nil
}

type dashboardCacheStub struct {
	get       func(ctx context.Context) (string, error)
	set       func(ctx context.Context, data string, ttl time.Duration) error
	del       func(ctx context.Context) error
	getCalls  int32
	setCalls  int32
	delCalls  int32
	lastSetMu sync.Mutex
	lastSet   string
}

func (c *dashboardCacheStub) GetDashboardStats(ctx context.Context) (string, error) {
	atomic.AddInt32(&c.getCalls, 1)
	if c.get != nil {
		return c.get(ctx)
	}
	return "", ErrDashboardStatsCacheMiss
}

func (c *dashboardCacheStub) SetDashboardStats(ctx context.Context, data string, ttl time.Duration) error {
	atomic.AddInt32(&c.setCalls, 1)
	c.lastSetMu.Lock()
	c.lastSet = data
	c.lastSetMu.Unlock()
	if c.set != nil {
		return c.set(ctx, data, ttl)
	}
	return nil
}

func (c *dashboardCacheStub) DeleteDashboardStats(ctx context.Context) error {
	atomic.AddInt32(&c.delCalls, 1)
	if c.del != nil {
		return c.del(ctx)
	}
	return nil
}

type dashboardAggregationRepoStub struct {
	watermark time.Time
	err       error
}

func (s *dashboardAggregationRepoStub) AggregateRange(ctx context.Context, start, end time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) RecomputeRange(ctx context.Context, start, end time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) GetAggregationWatermark(ctx context.Context) (time.Time, error) {
	if s.err != nil {
		return time.Time{}, s.err
	}
	return s.watermark, nil
}

func (s *dashboardAggregationRepoStub) UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) CleanupUsageLogs(ctx context.Context, cutoff time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error {
	return nil
}

func (c *dashboardCacheStub) readLastEntry(t *testing.T) dashboardStatsCacheEntry {
	t.Helper()
	c.lastSetMu.Lock()
	data := c.lastSet
	c.lastSetMu.Unlock()

	var entry dashboardStatsCacheEntry
	err := json.Unmarshal([]byte(data), &entry)
	require.NoError(t, err)
	return entry
}

func TestDashboardService_CacheHitFresh(t *testing.T) {
	stats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true},
		TotalUsers:     10,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	entry := dashboardStatsCacheEntry{
		Stats:     stats,
		UpdatedAt: time.Now().Unix(),
	}
	payload, err := json.Marshal(entry)
	require.NoError(t, err)

	cache := &dashboardCacheStub{
		get: func(ctx context.Context) (string, error) {
			return string(payload), nil
		},
	}
	repo := &usageRepoStub{
		stats: &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true}, TotalUsers: 99},
	}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(0), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalls))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalls))
}

func TestDashboardService_CacheMiss_StoresCache(t *testing.T) {
	stats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true},
		TotalUsers:     7,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	cache := &dashboardCacheStub{
		get: func(ctx context.Context) (string, error) {
			return "", ErrDashboardStatsCacheMiss
		},
	}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.setCalls))
	entry := cache.readLastEntry(t)
	require.Equal(t, stats, entry.Stats)
	require.WithinDuration(t, time.Now(), time.Unix(entry.UpdatedAt, 0), time.Second)
}

func TestDashboardService_CacheDisabled_SkipsCache(t *testing.T) {
	stats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true},
		TotalUsers:     3,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	cache := &dashboardCacheStub{
		get: func(ctx context.Context) (string, error) {
			return "", nil
		},
	}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: false},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.getCalls))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalls))
}

func TestDashboardService_CacheHitStale_TriggersAsyncRefresh(t *testing.T) {
	staleStats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true},
		TotalUsers:     11,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	entry := dashboardStatsCacheEntry{
		Stats:     staleStats,
		UpdatedAt: time.Now().Add(-defaultDashboardStatsFreshTTL * 2).Unix(),
	}
	payload, err := json.Marshal(entry)
	require.NoError(t, err)

	cache := &dashboardCacheStub{
		get: func(ctx context.Context) (string, error) {
			return string(payload), nil
		},
	}
	refreshCh := make(chan struct{}, 1)
	repo := &usageRepoStub{
		stats:  &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true}, TotalUsers: 22},
		onCall: refreshCh,
	}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, staleStats, got)

	select {
	case <-refreshCh:
	case <-time.After(1 * time.Second):
		t.Fatal("等待异步刷新超时")
	}
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&cache.setCalls) >= 1
	}, 1*time.Second, 10*time.Millisecond)
}

func TestDashboardService_CacheParseError_EvictsAndRefetches(t *testing.T) {
	cache := &dashboardCacheStub{
		get: func(ctx context.Context) (string, error) {
			return "not-json", nil
		},
	}
	stats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true}, TotalUsers: 9}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.delCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
}

func TestDashboardService_CacheParseError_RepoFailure(t *testing.T) {
	cache := &dashboardCacheStub{
		get: func(ctx context.Context) (string, error) {
			return "not-json", nil
		},
	}
	repo := &usageRepoStub{err: errors.New("db down")}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	_, err := svc.GetDashboardStats(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.delCalls))
}

func TestDashboardService_StatsUpdatedAtEpochWhenMissing(t *testing.T) {
	stats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true}}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: false}}
	svc := NewDashboardService(repo, aggRepo, nil, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1970-01-01T00:00:00Z", got.StatsUpdatedAt)
	require.True(t, got.StatsStale)
}

func TestDashboardService_StatsStaleFalseWhenFresh(t *testing.T) {
	aggNow := time.Now().UTC().Truncate(time.Second)
	stats := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true}}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: aggNow}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: false},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled:         true,
			IntervalSeconds: 60,
			LookbackSeconds: 120,
		},
	}
	svc := NewDashboardService(repo, aggRepo, nil, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, aggNow.Format(time.RFC3339), got.StatsUpdatedAt)
	require.False(t, got.StatsStale)
}

func TestDashboardService_AggDisabled_UsesUsageLogsFallback(t *testing.T) {
	expected := &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: true}, TotalUsers: 42}
	repo := &usageRepoStub{
		rangeStats: expected,
		err:        errors.New("should not call aggregated stats"),
	}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: false},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: false,
			Retention: config.DashboardAggregationRetentionConfig{
				UsageLogsDays: 7,
			},
		},
	}
	svc := NewDashboardService(repo, nil, nil, cfg)

	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(42), got.TotalUsers)
	require.Equal(t, int32(0), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.rangeCalls))
	require.False(t, repo.rangeEnd.IsZero())
	require.Equal(t, truncateToDayUTC(repo.rangeEnd.AddDate(0, 0, -7)), repo.rangeStart)
}

func TestDashboardServiceIgnoresLegacyCacheWithoutCoverage(t *testing.T) {
	legacy := &usagestats.DashboardStats{TotalUsers: 99}
	payload, err := json.Marshal(dashboardStatsCacheEntry{Stats: legacy, UpdatedAt: time.Now().Unix()})
	require.NoError(t, err)
	fresh := &usagestats.DashboardStats{TotalUsers: 2, Coverage: &usagestats.Coverage{Complete: false, Reason: "history_before_coverage_floor_unverified"}}
	repo := &usageRepoStub{stats: fresh}
	cache := &dashboardCacheStub{get: func(context.Context) (string, error) { return string(payload), nil }}
	svc := NewDashboardService(repo, &dashboardAggregationRepoStub{}, cache, &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: true}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true}})
	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), got.TotalUsers)
	require.NotNil(t, got.Coverage)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
}

func TestDashboardServiceClosedHourIsNotStaleDuringRawTail(t *testing.T) {
	svc := &DashboardService{aggEnabled: true, aggInterval: time.Minute, aggLookback: 2 * time.Minute}
	cursor := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	require.False(t, svc.isAggregationStale(cursor, cursor.Add(59*time.Minute)), "a current closed cursor is fresh throughout its raw open hour")
	require.False(t, svc.isAggregationStale(cursor, cursor.Add(63*time.Minute)), "keep the configured publication grace period")
	require.True(t, svc.isAggregationStale(cursor, cursor.Add(64*time.Minute)), "a missed closed hour beyond grace is stale")
}

func TestDashboardServiceDirtyCoverageIsStaleEvenInsideGrace(t *testing.T) {
	svc := &DashboardService{aggEnabled: true, aggInterval: time.Minute, aggLookback: 2 * time.Minute}
	dirty := time.Now().UTC().Add(-time.Minute)
	stats := &usagestats.DashboardStats{StatsUpdatedAt: dirty.Format(time.RFC3339), Coverage: &usagestats.Coverage{DirtyFrom: &dirty}}
	svc.refreshAggregationStaleness(stats)
	require.True(t, stats.StatsStale)
}

type coverageRevisionRepoStub struct {
	*usageRepoStub
	revision          string
	changeDuringFetch bool
}

func (s *coverageRevisionRepoStub) DashboardCoverageRevision(context.Context) (string, error) {
	return s.revision, nil
}

func (s *coverageRevisionRepoStub) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	stats, err := s.usageRepoStub.GetDashboardStats(ctx)
	if s.changeDuringFetch {
		s.revision = "corrected"
	}
	return stats, err
}

func TestDashboardServiceCoverageRevisionInvalidatesFreshCache(t *testing.T) {
	old := &usagestats.DashboardStats{TotalRequests: 99, Coverage: &usagestats.Coverage{Complete: true}}
	payload, err := json.Marshal(dashboardStatsCacheEntry{Stats: old, UpdatedAt: time.Now().Unix(), CoverageRevision: "before-retention"})
	require.NoError(t, err)
	repo := &coverageRevisionRepoStub{usageRepoStub: &usageRepoStub{stats: &usagestats.DashboardStats{TotalRequests: 2, Coverage: &usagestats.Coverage{Complete: false}}}, revision: "after-retention"}
	cache := &dashboardCacheStub{get: func(context.Context) (string, error) { return string(payload), nil }}
	svc := NewDashboardService(repo, &dashboardAggregationRepoStub{}, cache, &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: true}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true}})
	got, err := svc.GetDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), got.TotalRequests)
	require.Equal(t, "after-retention", cache.readLastEntry(t).CoverageRevision)
	// The fetch must never stamp pre-correction data with a post-correction revision.
	repo.changeDuringFetch = true
	_, err = svc.refreshDashboardStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, "after-retention", cache.readLastEntry(t).CoverageRevision)
	require.Equal(t, "corrected", repo.revision)
}

func TestDashboardServiceOriginCoverageCache(test *testing.T) {
	origin := time.Date(2026, 3, 1, 10, 15, 0, 0, time.UTC)
	for _, transition := range []struct {
		name              string
		previous, current *usagestats.Coverage
	}{
		{"declared", &usagestats.Coverage{Complete: false}, &usagestats.Coverage{Complete: true, HistoryKnownEmptyBefore: &origin}},
		{"revoked", &usagestats.Coverage{Complete: true, HistoryKnownEmptyBefore: &origin}, &usagestats.Coverage{Complete: false}},
	} {
		test.Run(transition.name, func(test *testing.T) {
			payload, err := json.Marshal(dashboardStatsCacheEntry{Stats: &usagestats.DashboardStats{Coverage: transition.previous}, UpdatedAt: time.Now().Unix(), CoverageRevision: "old-origin"})
			require.NoError(test, err)
			repository := &coverageRevisionRepoStub{usageRepoStub: &usageRepoStub{stats: &usagestats.DashboardStats{Coverage: transition.current}}, revision: "new-origin"}
			cache := &dashboardCacheStub{get: func(context.Context) (string, error) { return string(payload), nil }}
			dashboard := NewDashboardService(repository, &dashboardAggregationRepoStub{}, cache, &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: true}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true}})
			stats, err := dashboard.GetDashboardStats(context.Background())
			require.NoError(test, err)
			require.Equal(test, transition.current, stats.Coverage)
			require.Equal(test, "new-origin", cache.readLastEntry(test).CoverageRevision)
		})
	}
}
