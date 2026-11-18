package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// groupUsageTotalsRepoStub 只实现分组用量聚合。其余 UsageLogRepository 方法由嵌入接口占位，
// 被调用即 panic，避免测试悄悄依赖未桩方法。
type groupUsageTotalsRepoStub struct {
	service.UsageLogRepository
	totalsByGroup map[int64]*service.GroupUsageTotals
	calls         []int64
}

func (s *groupUsageTotalsRepoStub) GetGroupUsageTotals(_ context.Context, groupID int64) (*service.GroupUsageTotals, error) {
	s.calls = append(s.calls, groupID)
	if totals, ok := s.totalsByGroup[groupID]; ok {
		return totals, nil
	}
	return &service.GroupUsageTotals{
		WindowStart: time.Unix(0, 0).UTC(),
		WindowEnd:   time.Unix(0, 0).UTC(),
	}, nil
}

func newGroupStatsRouter(repo *groupUsageTotalsRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewGroupHandler(newStubAdminService(), service.NewDashboardService(repo, nil, nil, nil), nil)
	router.GET("/api/v1/admin/groups/:id/stats", handler.GetStats)
	return router
}

type groupStatsPayload struct {
	GroupID         int64   `json:"group_id"`
	TotalRequests   int64   `json:"total_requests"`
	TotalCost       float64 `json:"total_cost"`
	ActualCost      float64 `json:"actual_cost"`
	UsageWindowFrom string  `json:"usage_window_start"`
	UsageWindowEnd  string  `json:"usage_window_end"`
}

func fetchGroupStats(t *testing.T, router *gin.Engine, groupID string) groupStatsPayload {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/"+groupID+"/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var envelope struct {
		Data groupStatsPayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	return envelope.Data
}

// 两个分组的真实聚合必须按 :id 各自取数，不能串到同一个全局数字。
func TestGroupStatsAggregatesRequestedGroupOnly(t *testing.T) {
	windowStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	repo := &groupUsageTotalsRepoStub{totalsByGroup: map[int64]*service.GroupUsageTotals{
		2: {Requests: 11, TotalCost: 1.25, ActualCost: 1.0, WindowStart: windowStart, WindowEnd: windowEnd},
		3: {Requests: 99, TotalCost: 9.5, ActualCost: 9.0, WindowStart: windowStart, WindowEnd: windowEnd},
	}}
	router := newGroupStatsRouter(repo)

	secondGroup := fetchGroupStats(t, router, "2")
	require.Equal(t, int64(2), secondGroup.GroupID)
	require.Equal(t, int64(11), secondGroup.TotalRequests)
	require.InDelta(t, 1.25, secondGroup.TotalCost, 0.000001)
	require.InDelta(t, 1.0, secondGroup.ActualCost, 0.000001)
	require.Equal(t, windowStart.Format(time.RFC3339), secondGroup.UsageWindowFrom)

	thirdGroup := fetchGroupStats(t, router, "3")
	require.Equal(t, int64(3), thirdGroup.GroupID)
	require.Equal(t, int64(99), thirdGroup.TotalRequests)
	require.InDelta(t, 9.5, thirdGroup.TotalCost, 0.000001)

	require.Equal(t, []int64{2, 3}, repo.calls)
	require.Equal(t, windowEnd.Format(time.RFC3339), thirdGroup.UsageWindowEnd)
}

// 保留窗口起点必须回传给调用方，避免把「窗口内累计」误当成历史全量。
func TestGroupStatsReturnsUsageWindow(t *testing.T) {
	repo := &groupUsageTotalsRepoStub{totalsByGroup: map[int64]*service.GroupUsageTotals{
		7: {
			WindowStart: time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC),
			WindowEnd:   time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC),
		},
	}}
	payload := fetchGroupStats(t, newGroupStatsRouter(repo), "7")
	require.Equal(t, "2026-07-15T03:00:00Z", payload.UsageWindowFrom)
	require.Equal(t, "2026-09-21T03:00:00Z", payload.UsageWindowEnd)
}
