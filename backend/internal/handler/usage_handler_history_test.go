package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type userHistoryRepo struct{ service.UsageLogRepository }

func (*userHistoryRepo) GetUserDashboardStats(context.Context, int64) (*usagestats.UserDashboardStats, error) {
	return &usagestats.UserDashboardStats{Coverage: &usagestats.Coverage{Complete: false, Reason: "raw_history_unavailable", RawRetainedFrom: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}}, nil
}
func (*userHistoryRepo) GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return nil, infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
}
func (*userHistoryRepo) GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error) {
	return nil, infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
}
func (*userHistoryRepo) GetGroupStatsWithUsageFilters(context.Context, time.Time, time.Time, usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	return nil, infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
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
	router.GET("/snapshot", handler.DashboardSnapshot)
	for _, path := range []string{"/trend", "/models", "/snapshot", "/snapshot?include_trend=false", "/snapshot?include_trend=false&include_model_stats=false&include_group_stats=true"} {
		t.Run(path, func(t *testing.T) {
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
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/stats", nil))
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"complete":false`)
	require.Contains(t, response.Body.String(), `"raw_retained_from":"2026-03-05T00:00:00Z"`)
}
