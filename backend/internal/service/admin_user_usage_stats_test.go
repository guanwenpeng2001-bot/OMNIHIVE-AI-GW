package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type adminUserUsageStatsRepo struct {
	UsageLogRepository

	filters usagestats.UsageLogFilters
	stats   *usagestats.UsageStats
	err     error
	calls   int
}

func (r *adminUserUsageStatsRepo) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.calls++
	r.filters = filters
	if r.err != nil {
		return nil, r.err
	}
	return r.stats, nil
}

func TestGetUserUsageStatsAggregatesRealUsage(t *testing.T) {
	require.NoError(t, timezone.Init("UTC"))
	accountCost := 1.25
	repo := &adminUserUsageStatsRepo{stats: &usagestats.UsageStats{
		TotalRequests:        12,
		TotalInputTokens:     1000,
		TotalOutputTokens:    250,
		TotalCacheReadTokens: 30,
		TotalTokens:          1280,
		TotalCost:            3.5,
		TotalActualCost:      2.75,
		TotalAccountCost:     &accountCost,
		AverageDurationMs:    412.5,
	}}
	impl := &adminServiceImpl{usageLogRepo: repo}

	result, err := impl.GetUserUsageStats(context.Background(), 42, "month")
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, int64(42), repo.filters.UserID)
	require.NotNil(t, repo.filters.StartTime)
	require.NotNil(t, repo.filters.EndTime)
	require.True(t, repo.filters.EndTime.Sub(*repo.filters.StartTime) >= 27*24*time.Hour, "month resolves to a month long window")

	payload, ok := result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "month", payload["period"])
	require.Equal(t, true, payload["available"])
	require.Equal(t, int64(12), payload["total_requests"])
	require.Equal(t, int64(1280), payload["total_tokens"])
	require.Equal(t, int64(1000), payload["input_tokens"])
	require.Equal(t, int64(250), payload["output_tokens"])
	require.Equal(t, int64(30), payload["cache_read_tokens"])
	require.Equal(t, 3.5, payload["total_cost"])
	require.Equal(t, 2.75, payload["total_actual_cost"])
	require.Equal(t, 1.25, payload["total_account_cost"])
	require.Equal(t, 412.5, payload["avg_duration_ms"], "the average duration comes from the aggregation instead of a fake zero")
}

func TestGetUserUsageStatsReportsEmptyWindowWithoutInventingData(t *testing.T) {
	require.NoError(t, timezone.Init("UTC"))
	repo := &adminUserUsageStatsRepo{stats: &usagestats.UsageStats{}}
	impl := &adminServiceImpl{usageLogRepo: repo}

	result, err := impl.GetUserUsageStats(context.Background(), 7, "today")
	require.NoError(t, err)
	payload, _ := result.(map[string]any)
	require.Equal(t, true, payload["available"], "an empty window is still a measured result")
	require.Equal(t, int64(0), payload["total_requests"])
	require.Equal(t, 0.0, payload["total_account_cost"])

	start := repo.filters.StartTime
	end := repo.filters.EndTime
	require.NotNil(t, start)
	require.NotNil(t, end)
	require.Equal(t, 0, start.Hour())
	require.Equal(t, 0, start.Minute())
	require.Less(t, start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()+1, "today starts at the beginning of the configured day")
}

func TestGetUserUsageStatsPeriodWindowsFollowTheUsagePageConvention(t *testing.T) {
	require.NoError(t, timezone.Init("UTC"))
	repo := &adminUserUsageStatsRepo{stats: &usagestats.UsageStats{}}
	impl := &adminServiceImpl{usageLogRepo: repo}

	_, err := impl.GetUserUsageStats(context.Background(), 1, "week")
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().AddDate(0, 0, -7), *repo.filters.StartTime, 2*time.Second)

	_, err = impl.GetUserUsageStats(context.Background(), 1, "unknown-period")
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().AddDate(0, -1, 0), *repo.filters.StartTime, 2*time.Second, "an unknown period falls back to the monthly window")
}

func TestGetUserUsageStatsPropagatesAggregationFailure(t *testing.T) {
	require.NoError(t, timezone.Init("UTC"))
	repo := &adminUserUsageStatsRepo{err: context.DeadlineExceeded}
	impl := &adminServiceImpl{usageLogRepo: repo}

	_, err := impl.GetUserUsageStats(context.Background(), 1, "month")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
