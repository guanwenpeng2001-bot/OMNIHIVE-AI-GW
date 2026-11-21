package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func newProxyStatsRouter(svc *stubAdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/admin/proxies/:id/stats", NewProxyHandler(svc).GetStats)
	return router
}

type proxyStatsPayload struct {
	ProxyID          int64    `json:"proxy_id"`
	TotalAccounts    int64    `json:"total_accounts"`
	ActiveAccounts   int64    `json:"active_accounts"`
	TotalRequests    int64    `json:"total_requests"`
	LastRequestAt    *string  `json:"last_request_at"`
	UsageWindowStart string   `json:"usage_window_start"`
	AverageLatency   *int64   `json:"average_latency"`
	MeasuredAt       *string  `json:"measured_at"`
	ProbeSuccess     *bool    `json:"probe_success"`
	QualityStatus    string   `json:"quality_status"`
	QualityScore     *int     `json:"quality_score"`
	SuccessRate      *float64 `json:"success_rate"`
}

func fetchProxyStats(t *testing.T, router *gin.Engine, proxyID string) (int, proxyStatsPayload, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/"+proxyID+"/stats", nil)
	router.ServeHTTP(rec, req)

	var envelope struct {
		Data proxyStatsPayload `json:"data"`
	}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	}
	return rec.Code, envelope.Data, rec.Body.String()
}

// 两个代理各自取自己的真实计数与账本请求量，不能串到同一个全局数字。
func TestProxyStatsReturnsPerProxyAggregation(t *testing.T) {
	windowStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	lastRequest := windowEnd.Add(-time.Minute)
	measuredAt := windowEnd.Add(-30 * time.Minute)
	latency := int64(142)
	probeSuccess := true
	score := 90

	svc := newStubAdminService()
	svc.proxyStats = map[int64]*service.ProxyStats{
		2: {
			ProxyID:        2,
			TotalAccounts:  4,
			ActiveAccounts: 3,
			Usage: &service.ProxyUsageTotals{
				Requests:      11,
				LastRequestAt: &lastRequest,
				WindowStart:   windowStart,
				WindowEnd:     windowEnd,
			},
			AverageLatencyMs: &latency,
			MeasuredAt:       &measuredAt,
			ProbeSuccess:     &probeSuccess,
			QualityStatus:    "healthy",
			QualityScore:     &score,
		},
		3: {
			ProxyID:        3,
			TotalAccounts:  1,
			ActiveAccounts: 1,
			Usage: &service.ProxyUsageTotals{
				Requests:    99,
				WindowStart: windowStart,
				WindowEnd:   windowEnd,
			},
		},
	}
	router := newProxyStatsRouter(svc)

	status, second, _ := fetchProxyStats(t, router, "2")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, int64(2), second.ProxyID)
	require.Equal(t, int64(4), second.TotalAccounts)
	require.Equal(t, int64(3), second.ActiveAccounts)
	require.Equal(t, int64(11), second.TotalRequests)
	require.Equal(t, windowStart.Format(time.RFC3339), second.UsageWindowStart)
	require.NotNil(t, second.AverageLatency)
	require.Equal(t, int64(142), *second.AverageLatency)
	require.NotNil(t, second.ProbeSuccess)
	require.True(t, *second.ProbeSuccess)
	require.Equal(t, "healthy", second.QualityStatus)

	status, third, _ := fetchProxyStats(t, router, "3")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, int64(3), third.ProxyID)
	require.Equal(t, int64(99), third.TotalRequests)
	require.Equal(t, int64(1), third.TotalAccounts)
	require.Equal(t, []int64{2, 3}, svc.proxyStatsCalls)
}

// 从未探测过的代理不能假填 100% 成功率 / 0 延迟：相关字段必须是 null。
func TestProxyStatsDoesNotFabricateUnmeasuredObservations(t *testing.T) {
	svc := newStubAdminService()
	svc.proxyStats = map[int64]*service.ProxyStats{
		7: {ProxyID: 7, TotalAccounts: 2, ActiveAccounts: 2},
	}
	status, payload, body := fetchProxyStats(t, newProxyStatsRouter(svc), "7")

	require.Equal(t, http.StatusOK, status)
	require.Nil(t, payload.AverageLatency)
	require.Nil(t, payload.MeasuredAt)
	require.Nil(t, payload.ProbeSuccess)
	require.Nil(t, payload.QualityScore)
	require.Nil(t, payload.SuccessRate)
	require.NotContains(t, body, `"success_rate"`)
	require.NotContains(t, body, `"average_latency":0`)
}

// 代理不存在/加载失败必须显式报错，不能退化成 200 空统计。
func TestProxyStatsSurfacesLookupFailure(t *testing.T) {
	svc := newStubAdminService()
	svc.proxyStatsErr = infraerrors.NotFound("PROXY_NOT_FOUND", "proxy not found")
	status, _, body := fetchProxyStats(t, newProxyStatsRouter(svc), "404")
	require.Equal(t, http.StatusNotFound, status, body)
	require.NotContains(t, body, `"total_accounts":0`)
}

func TestProxyStatsRejectsInvalidID(t *testing.T) {
	svc := newStubAdminService()
	svc.proxyStatsErr = errors.New("should not be called")
	status, _, body := fetchProxyStats(t, newProxyStatsRouter(svc), "abc")
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Empty(t, svc.proxyStatsCalls)
}
