package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Regression coverage for all independently cached usage readers.
type coverageCacheRepo struct {
	service.UsageLogRepository
	revision     string
	failure      error
	rankingCalls int
	statsCalls   int
}

func (r *coverageCacheRepo) DashboardCoverageRevision(context.Context) (string, error) {
	return r.revision, nil
}
func (r *coverageCacheRepo) GetUserSpendingRanking(context.Context, time.Time, time.Time, int) (*usagestats.UserSpendingRankingResponse, error) {
	r.rankingCalls++
	return &usagestats.UserSpendingRankingResponse{TotalRequests: 777}, r.failure
}
func (r *coverageCacheRepo) GetStatsWithFilters(context.Context, usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.statsCalls++
	return &usagestats.UsageStats{TotalRequests: 777}, r.failure
}
func TestDashboardRankingCacheCoverageRevision(t *testing.T) {
	original := dashboardUsersRankingCache
	dashboardUsersRankingCache = newSnapshotCache(5 * time.Minute)
	t.Cleanup(func() { dashboardUsersRankingCache = original })
	repo := &coverageCacheRepo{revision: "before"}
	handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ranking", handler.GetUserSpendingRanking)
	request := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/ranking?start_date=2026-03-01&end_date=2026-03-02", nil))
		return rec
	}
	first := request()
	require.Equal(t, 200, first.Code)
	repo.revision = "after"
	repo.failure = infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
	uncached := request()
	require.Equal(t, 409, uncached.Code)
	require.Contains(t, uncached.Body.String(), "USAGE_HISTORY_INCOMPLETE")
	require.Equal(t, 2, repo.rankingCalls)
}
func TestUsageStatsCacheCoverageRevision(t *testing.T) {
	original := usageStatsCache
	usageStatsCache = newSnapshotCache(30 * time.Second)
	t.Cleanup(func() { usageStatsCache = original })
	repo := &coverageCacheRepo{revision: "before"}
	handler := &UsageHandler{usageService: service.NewUsageService(repo, nil, nil, nil)}
	start, end := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	filters := usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}
	_, hit, err := handler.getStatsCached(context.Background(), filters)
	require.NoError(t, err)
	require.False(t, hit)
	repo.revision = "after"
	repo.failure = infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable")
	_, hit, err = handler.getStatsCached(context.Background(), filters)
	require.False(t, hit)
	status, _ := infraerrors.ToHTTP(err)
	require.Equal(t, 409, status)
	require.Equal(t, 2, repo.statsCalls)
}

func (r *coverageCacheRepo) GetBatchUserUsageStats(context.Context, []int64, time.Time, time.Time) (map[int64]*usagestats.BatchUserUsageStats, error) {
	return map[int64]*usagestats.BatchUserUsageStats{}, r.failure
}
func (r *coverageCacheRepo) GetBatchAPIKeyUsageStats(context.Context, []int64, time.Time, time.Time) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	return map[int64]*usagestats.BatchAPIKeyUsageStats{}, r.failure
}
func TestDashboardBatchCacheCoverageRevision(t *testing.T) {
	for _, kind := range []string{"users", "keys"} {
		t.Run(kind, func(t *testing.T) {
			originalUsers, originalKeys := dashboardBatchUsersUsageCache, dashboardBatchAPIKeysUsageCache
			dashboardBatchUsersUsageCache, dashboardBatchAPIKeysUsageCache = newSnapshotCache(time.Minute), newSnapshotCache(time.Minute)
			t.Cleanup(func() { dashboardBatchUsersUsageCache, dashboardBatchAPIKeysUsageCache = originalUsers, originalKeys })
			repo := &coverageCacheRepo{revision: "before"}
			handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
			router := gin.New()
			body := `{"user_ids":[1]}`
			if kind == "users" {
				router.POST("/batch", handler.GetBatchUsersUsage)
			} else {
				router.POST("/batch", handler.GetBatchAPIKeysUsage)
				body = `{"api_key_ids":[1]}`
			}
			request := func() *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/batch", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(rec, req)
				return rec
			}
			require.Equal(t, 200, request().Code)
			require.Equal(t, "hit", request().Header().Get("X-Snapshot-Cache"))
			repo.revision, repo.failure = "after", infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable")
			require.Equal(t, 409, request().Code)
		})
	}
}
