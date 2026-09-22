package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// dashboardBillingModeRepo captures the billing_mode filter every surface sends
// to the repository, so the table, the totals and every chart can be compared.
type dashboardBillingModeRepo struct {
	service.UsageLogRepository

	listFilters   usagestats.UsageLogFilters
	statsFilters  usagestats.UsageLogFilters
	trendFilters  usagestats.UsageLogFilters
	modelFilters  usagestats.UsageLogFilters
	groupFilters  usagestats.UsageLogFilters
	dashboardStat *usagestats.DashboardStats
}

func (s *dashboardBillingModeRepo) ListWithFilters(_ context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	s.listFilters = filters
	return nil, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize}, nil
}

func (s *dashboardBillingModeRepo) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	s.statsFilters = filters
	return &usagestats.UsageStats{}, nil
}

func (s *dashboardBillingModeRepo) GetUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, filters usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	s.trendFilters = filters
	return []usagestats.TrendDataPoint{}, nil
}

func (s *dashboardBillingModeRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, _, _ time.Time, filters usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	s.modelFilters = filters
	return []usagestats.ModelStat{}, nil
}

func (s *dashboardBillingModeRepo) GetGroupStatsWithUsageFilters(_ context.Context, _, _ time.Time, filters usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	s.groupFilters = filters
	return []usagestats.GroupStat{}, nil
}

func (s *dashboardBillingModeRepo) GetDashboardStats(_ context.Context) (*usagestats.DashboardStats, error) {
	if s.dashboardStat != nil {
		return s.dashboardStat, nil
	}
	return &usagestats.DashboardStats{}, nil
}

func newBillingModeConsistencyRouter(repo *dashboardBillingModeRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	dashboardService := service.NewDashboardService(repo, nil, nil, nil)
	usageService := service.NewUsageService(repo, nil, nil, nil)
	dashboardHandler := NewDashboardHandler(dashboardService, nil)
	usageHandler := NewUsageHandler(usageService, nil, nil, nil)
	router := gin.New()
	router.GET("/admin/dashboard/stats", dashboardHandler.GetStats)
	router.GET("/admin/dashboard/trend", dashboardHandler.GetUsageTrend)
	router.GET("/admin/dashboard/models", dashboardHandler.GetModelStats)
	router.GET("/admin/dashboard/groups", dashboardHandler.GetGroupStats)
	router.GET("/admin/dashboard/snapshot-v2", dashboardHandler.GetSnapshotV2)
	router.GET("/admin/usage", usageHandler.List)
	router.GET("/admin/usage/stats", usageHandler.Stats)
	return router
}

func TestAdminUsageTotalsAndChartsShareTheBillingModeFilter(t *testing.T) {
	repo := &dashboardBillingModeRepo{}
	router := newBillingModeConsistencyRouter(repo)

	for _, path := range []string{
		"/admin/usage?billing_mode=image&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC",
		"/admin/usage/stats?billing_mode=image&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC",
		"/admin/dashboard/trend?billing_mode=image&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC",
		"/admin/dashboard/models?billing_mode=image&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC",
		"/admin/dashboard/groups?billing_mode=image&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC",
		"/admin/dashboard/snapshot-v2?billing_mode=image&include_stats=false&include_trend=true&include_model_stats=true&include_group_stats=true&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, path)
	}

	require.Equal(t, "image", repo.listFilters.BillingMode, "the table filters by billing mode")
	require.Equal(t, "image", repo.statsFilters.BillingMode, "the totals filter by billing mode")
	require.Equal(t, "image", repo.trendFilters.BillingMode, "the trend chart filters by billing mode")
	require.Equal(t, "image", repo.modelFilters.BillingMode, "the model chart filters by billing mode")
	require.Equal(t, "image", repo.groupFilters.BillingMode, "the group chart filters by billing mode")
}

func TestDashboardSnapshotCacheSeparatesBillingModes(t *testing.T) {
	repo := &dashboardBillingModeRepo{}
	router := newBillingModeConsistencyRouter(repo)
	base := "/admin/dashboard/snapshot-v2?include_stats=false&include_trend=false&include_model_stats=false&include_group_stats=true&start_date=2026-08-01&end_date=2026-08-02&timezone=UTC"

	call := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec
	}

	call(base + "&billing_mode=image")
	require.Equal(t, "image", repo.groupFilters.BillingMode)

	call(base + "&billing_mode=token")
	require.Equal(t, "token", repo.groupFilters.BillingMode, "a different mode is not served from another mode's cache")

	hit := call(base + "&billing_mode=image")
	require.Equal(t, "hit", hit.Header().Get("X-Snapshot-Cache"), "the same mode reuses its own cached snapshot")
}

func TestDashboardSurfacesRejectUnknownBillingMode(t *testing.T) {
	repo := &dashboardBillingModeRepo{}
	router := newBillingModeConsistencyRouter(repo)

	for _, path := range []string{
		"/admin/dashboard/trend?billing_mode=nope",
		"/admin/dashboard/models?billing_mode=nope",
		"/admin/dashboard/groups?billing_mode=nope",
		"/admin/dashboard/snapshot-v2?billing_mode=nope",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, path)
		require.Contains(t, rec.Body.String(), "billing_mode", path)
	}
}

func TestDashboardStatsReturnsRealAccountCost(t *testing.T) {
	repo := &dashboardBillingModeRepo{dashboardStat: &usagestats.DashboardStats{
		TotalCost:        10,
		TotalActualCost:  8,
		TotalAccountCost: 4.5,
		TodayCost:        1,
		TodayActualCost:  0.8,
		TodayAccountCost: 0.45,
	}}
	router := newBillingModeConsistencyRouter(repo)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "the dashboard overview is wrapped in the standard envelope")
	require.Equal(t, 4.5, data["total_account_cost"], "the overview fallback carries the real account cost")
	require.Equal(t, 0.45, data["today_account_cost"])
	require.Equal(t, 10.0, data["total_cost"])
	require.Equal(t, 1.0, data["today_cost"])
}
