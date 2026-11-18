package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// redeemStatsRepoStub 只实现统计聚合；其余 RedeemCodeRepository 方法由嵌入接口占位。
type redeemStatsRepoStub struct {
	service.RedeemCodeRepository
	stats *service.RedeemCodeStats
	err   error
}

func (s *redeemStatsRepoStub) GetStats(context.Context) (*service.RedeemCodeStats, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.stats, nil
}

func newRedeemStatsRouter(repo *redeemStatsRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	redeemService := service.NewRedeemService(repo, nil, nil, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/redeem-codes/stats", NewRedeemHandler(newStubAdminService(), redeemService).GetStats)
	return router
}

// 统计端点必须回传服务层聚合出的真实分布，而不是写死的零。
func TestRedeemStatsReturnsAggregatedCounts(t *testing.T) {
	repo := &redeemStatsRepoStub{stats: &service.RedeemCodeStats{
		TotalCodes:            7,
		ActiveCodes:           3,
		UsedCodes:             2,
		ExpiredCodes:          2,
		TotalValueDistributed: 42.5,
		ByType:                map[string]int64{"balance": 5, "concurrency": 2},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/stats", nil)
	newRedeemStatsRouter(repo).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var envelope struct {
		Data service.RedeemCodeStats `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, int64(7), envelope.Data.TotalCodes)
	require.Equal(t, int64(3), envelope.Data.ActiveCodes)
	require.Equal(t, int64(2), envelope.Data.UsedCodes)
	require.Equal(t, int64(2), envelope.Data.ExpiredCodes)
	require.InDelta(t, 42.5, envelope.Data.TotalValueDistributed, 0.000001)
	require.Equal(t, map[string]int64{"balance": 5, "concurrency": 2}, envelope.Data.ByType)
}

// 聚合失败必须显式报错，不能退化成「全 0 的成功响应」。
func TestRedeemStatsSurfacesAggregationFailure(t *testing.T) {
	repo := &redeemStatsRepoStub{err: errors.New("db down")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/stats", nil)
	newRedeemStatsRouter(repo).ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"total_codes":0`)
}
