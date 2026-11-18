package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type dashboardHistoryRepo struct {
	service.UsageLogRepository
	failure  error
	revision string
}

func (repo *dashboardHistoryRepo) DashboardCoverageRevision(context.Context) (string, error) {
	return repo.revision, nil
}
func (repo *dashboardHistoryRepo) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return &usagestats.DashboardStats{Coverage: &usagestats.Coverage{Complete: false, Reason: "raw_history_unavailable", RawRetainedFrom: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}}, repo.failure
}
func (repo *dashboardHistoryRepo) GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return []usagestats.TrendDataPoint{}, repo.failure
}
func (repo *dashboardHistoryRepo) GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error) {
	return []usagestats.ModelStat{}, repo.failure
}
func (repo *dashboardHistoryRepo) GetGroupStatsWithUsageFilters(context.Context, time.Time, time.Time, usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	return []usagestats.GroupStat{}, repo.failure
}
func (repo *dashboardHistoryRepo) GetUserUsageTrend(context.Context, time.Time, time.Time, string, int) ([]usagestats.UserUsageTrendPoint, error) {
	return []usagestats.UserUsageTrendPoint{}, repo.failure
}
func (repo *dashboardHistoryRepo) GetAPIKeyUsageTrend(context.Context, time.Time, time.Time, string, int) ([]usagestats.APIKeyUsageTrendPoint, error) {
	return []usagestats.APIKeyUsageTrendPoint{}, repo.failure
}

func dashboardHistoryRouter(repo *dashboardHistoryRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/snapshot", handler.GetSnapshotV2)
	router.GET("/stats", handler.GetStats)
	router.GET("/trend", handler.GetUsageTrend)
	router.GET("/models", handler.GetModelStats)
	router.GET("/groups", handler.GetGroupStats)
	router.GET("/users", handler.GetUserUsageTrend)
	router.GET("/keys", handler.GetAPIKeyUsageTrend)
	return router
}
func TestDashboardHistoryErrors(t *testing.T) {
	paths := []string{"/stats", "/trend", "/models", "/groups", "/users", "/keys"}
	parts := []string{"stats", "trend", "model_stats", "group_stats", "users_trend"}
	for _, selected := range parts {
		query := url.Values{}
		for _, part := range parts {
			query.Set("include_"+part, strconv.FormatBool(part == selected))
		}
		paths = append(paths, "/snapshot?"+query.Encode())
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			t.Cleanup(resetDashboardReadCachesForTest)
			failure := fmt.Errorf("repository context: %w", infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"}))
			router := dashboardHistoryRouter(&dashboardHistoryRepo{failure: failure})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			require.Equal(t, 409, response.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, "USAGE_HISTORY_INCOMPLETE", body["reason"])
			metadata, ok := body["metadata"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "2026-03-05T00:00:00Z", metadata["raw_retained_from"])
			require.NotContains(t, body, "data")
		})
	}
}
func TestDashboardHistoryCoverage(t *testing.T) {
	resetDashboardReadCachesForTest()
	t.Cleanup(resetDashboardReadCachesForTest)
	router := dashboardHistoryRouter(&dashboardHistoryRepo{})
	for _, path := range []string{"/stats", "/snapshot?include_trend=false&include_model_stats=false"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, response.Code)
		require.Contains(t, response.Body.String(), `"complete":false`)
		require.Contains(t, response.Body.String(), `"raw_retained_from":"2026-03-05T00:00:00Z"`)
	}
}
func TestDashboardSnapshotInternalErrorIsPrivate(t *testing.T) {
	resetDashboardReadCachesForTest()
	t.Cleanup(resetDashboardReadCachesForTest)
	router := dashboardHistoryRouter(&dashboardHistoryRepo{failure: errors.New("secret SQL details")})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/snapshot", nil))
	require.Equal(t, 500, response.Code)
	require.NotContains(t, response.Body.String(), "secret")
}
func TestDashboardCoverageRevisionInvalidatesCaches(t *testing.T) {
	for _, path := range []string{"/trend", "/models", "/groups", "/users", "/keys", "/snapshot?include_stats=false", "/snapshot?include_stats=true"} {
		t.Run(path, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			t.Cleanup(resetDashboardReadCachesForTest)
			repo := &dashboardHistoryRepo{revision: "before"}
			router := dashboardHistoryRouter(repo)
			first := httptest.NewRecorder()
			router.ServeHTTP(first, httptest.NewRequest("GET", path, nil))
			require.Equal(t, 200, first.Code)
			repo.failure = infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
			cached := httptest.NewRecorder()
			router.ServeHTTP(cached, httptest.NewRequest("GET", path, nil))
			require.Equal(t, 200, cached.Code)
			repo.revision = "after"
			request := httptest.NewRequest("GET", path, nil)
			request.Header.Set("If-None-Match", first.Header().Get("ETag"))
			refreshed := httptest.NewRecorder()
			router.ServeHTTP(refreshed, request)
			require.Equal(t, 409, refreshed.Code)
		})
	}
}
