package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// realtimeUsageRepoStub 只实现实时窗口查询；其余 UsageLogRepository 方法由嵌入接口占位。
type realtimeUsageRepoStub struct {
	service.UsageLogRepository
	stats *service.RealtimeWindowStats
	err   error
	calls int
}

func (r *realtimeUsageRepoStub) GetRealtimeWindowStats(_ context.Context, start, end time.Time) (*service.RealtimeWindowStats, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	stats := *r.stats
	stats.WindowStart = start
	stats.WindowEnd = end
	return &stats, nil
}

func newRealtimeMetricsRouter(repo *realtimeUsageRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/admin/dashboard/realtime", NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil).GetRealtimeMetrics)
	return router
}

type realtimeMetricsPayload struct {
	RequestsPerMinute   *int64   `json:"requests_per_minute"`
	AverageResponseTime *float64 `json:"average_response_time"`
	ActiveRequests      *int64   `json:"active_requests"`
	ErrorRate           *float64 `json:"error_rate"`
	CoverageComplete    bool     `json:"coverage_complete"`
	ObservedFrom        string   `json:"observed_from"`
	ObservedTo          string   `json:"observed_to"`
}

func fetchRealtimeMetrics(t *testing.T, router *gin.Engine) (int, realtimeMetricsPayload, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard/realtime", nil)
	router.ServeHTTP(rec, req)

	var envelope struct {
		Data realtimeMetricsPayload `json:"data"`
	}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	}
	return rec.Code, envelope.Data, rec.Body.String()
}

// 窗口内真实观测到的请求量与平均耗时必须原样回传。
func TestRealtimeMetricsReturnsObservedWindow(t *testing.T) {
	requests := int64(37)
	average := 412.5
	status, payload, body := fetchRealtimeMetrics(t, newRealtimeMetricsRouter(&realtimeUsageRepoStub{
		stats: &service.RealtimeWindowStats{
			Requests:              &requests,
			AverageResponseTimeMs: &average,
			CoverageComplete:      true,
		},
	}))

	require.Equal(t, http.StatusOK, status, body)
	require.NotNil(t, payload.RequestsPerMinute)
	require.Equal(t, int64(37), *payload.RequestsPerMinute)
	require.NotNil(t, payload.AverageResponseTime)
	require.InDelta(t, 412.5, *payload.AverageResponseTime, 0.001)
	require.True(t, payload.CoverageComplete)
	require.NotEmpty(t, payload.ObservedFrom)
	require.NotEmpty(t, payload.ObservedTo)
}

// 账本覆盖不住窗口（或窗口内没有耗时样本）时保持 null，不用 0 冒充观测。
func TestRealtimeMetricsKeepsUnknownAsNull(t *testing.T) {
	status, payload, body := fetchRealtimeMetrics(t, newRealtimeMetricsRouter(&realtimeUsageRepoStub{
		stats: &service.RealtimeWindowStats{CoverageComplete: false},
	}))

	require.Equal(t, http.StatusOK, status, body)
	require.Nil(t, payload.RequestsPerMinute)
	require.Nil(t, payload.AverageResponseTime)
	// 本地账本没有在途计数器与错误维度：显式未知而不是 0。
	require.Nil(t, payload.ActiveRequests)
	require.Nil(t, payload.ErrorRate)
	require.False(t, payload.CoverageComplete)
	require.NotContains(t, body, `"active_requests":0`)
	require.NotContains(t, body, `"error_rate":0`)
}

// 真实查询失败必须显式报错，不能退化成 200 全 0。
func TestRealtimeMetricsSurfacesQueryFailure(t *testing.T) {
	status, _, body := fetchRealtimeMetrics(t, newRealtimeMetricsRouter(&realtimeUsageRepoStub{err: errors.New("db down")}))
	require.Equal(t, http.StatusInternalServerError, status, body)
}
