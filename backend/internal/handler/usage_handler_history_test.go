package handler

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

type userHistoryRepo struct{ service.UsageLogRepository }

func (*userHistoryRepo) GetUserDashboardStats(context.Context, int64) (*usagestats.UserDashboardStats, error) {
	return &usagestats.UserDashboardStats{Coverage: &usagestats.Coverage{Complete: false, Reason: "raw_history_unavailable", RawRetainedFrom: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}}, nil
}
func (*userHistoryRepo) GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return nil, repository.ErrUsageHistoryUnavailable.WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
}
func (*userHistoryRepo) GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error) {
	return nil, repository.ErrUsageHistoryUnavailable.WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
}
func (*userHistoryRepo) GetGroupStatsWithUsageFilters(context.Context, time.Time, time.Time, usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	return nil, repository.ErrUsageHistoryUnavailable.WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
}
func TestUsageDashboardHistoryContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUsageHandler(service.NewUsageService(&userHistoryRepo{}, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/stats", handler.DashboardStats)
	router.GET("/trend", handler.DashboardTrend)
	router.GET("/models", handler.DashboardModels)
	router.GET("/snapshot", handler.DashboardSnapshotV2)
	for _, path := range []string{"/trend", "/models", "/snapshot", "/snapshot?include_trend=false", "/snapshot?include_trend=false&include_model_stats=false&include_group_stats=true"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			require.Equal(t, 409, response.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, "USAGE_HISTORY_INCOMPLETE", body["reason"])
			require.Equal(t, "2026-03-05T00:00:00Z", body["metadata"].(map[string]any)["raw_retained_from"])
			require.NotContains(t, body, "data")
		})
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/stats", nil))
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"complete":false`)
	require.Contains(t, response.Body.String(), `"raw_retained_from":"2026-03-05T00:00:00Z"`)
}
