package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// channelMonitorUserSettingRepo 只实现 GetMultiple：GetChannelMonitorRuntime 读开关只走这一条。
type channelMonitorUserSettingRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *channelMonitorUserSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

// channelMonitorUserRepoStub 只实现 ListEnabled；其它仓储方法由嵌入接口占位，
// 被调用即 panic，保证「功能关闭/模式不符」时确实没有查询监控数据。
type channelMonitorUserRepoStub struct {
	service.ChannelMonitorRepository
	monitors  []*service.ChannelMonitor
	listCalls int
}

func (r *channelMonitorUserRepoStub) ListEnabled(context.Context) ([]*service.ChannelMonitor, error) {
	r.listCalls++
	return r.monitors, nil
}

func newChannelMonitorUserRouter(t *testing.T, enabled, mode string, repo *channelMonitorUserRepoStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	settingService := service.NewSettingService(&channelMonitorUserSettingRepo{values: map[string]string{
		service.SettingKeyChannelMonitorEnabled: enabled,
		service.SettingKeyChannelMonitorMode:    mode,
	}}, nil)
	handler := NewChannelMonitorUserHandler(service.NewChannelMonitorService(repo, nil), settingService)
	router := gin.New()
	router.GET("/api/v1/channel-monitors", handler.List)
	router.GET("/api/v1/channel-monitors/:id/status", handler.GetStatus)
	return router
}

func performChannelMonitorUserRequest(router *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(rec, req)
	return rec
}

// 功能关闭时用户 V1 列表必须给规范机器码（403 CHANNEL_MONITOR_DISABLED），
// 而不是旧的「200 空列表」假空。
func TestChannelMonitorUserListReturnsDisabledReason(t *testing.T) {
	repo := &channelMonitorUserRepoStub{}
	rec := performChannelMonitorUserRequest(newChannelMonitorUserRouter(t, "false", service.ChannelMonitorModeV1, repo), "/api/v1/channel-monitors")

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	var envelope struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, "CHANNEL_MONITOR_DISABLED", envelope.Reason)
	require.Zero(t, repo.listCalls, "功能关闭时不应查询监控数据")
}

// 功能开着但模式是 v2 时，V1 用户视图返回 CHANNEL_MONITOR_MODE_MISMATCH，
// 不能退化成 200 空列表。
func TestChannelMonitorUserListReturnsModeMismatchReason(t *testing.T) {
	repo := &channelMonitorUserRepoStub{}
	rec := performChannelMonitorUserRequest(newChannelMonitorUserRouter(t, "true", service.ChannelMonitorModeV2, repo), "/api/v1/channel-monitors")

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	var envelope struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, "CHANNEL_MONITOR_MODE_MISMATCH", envelope.Reason)
	require.Zero(t, repo.listCalls)
}

// 单条状态查询同样返回规范机器码：关闭≠「监控不存在」(404)。
func TestChannelMonitorUserStatusReturnsCanonicalReason(t *testing.T) {
	cases := []struct {
		name           string
		enabled        string
		mode           string
		expectedStatus int
		expectedReason string
	}{
		{name: "disabled", enabled: "false", mode: service.ChannelMonitorModeV1, expectedStatus: http.StatusForbidden, expectedReason: "CHANNEL_MONITOR_DISABLED"},
		{name: "mode mismatch", enabled: "true", mode: service.ChannelMonitorModeV2, expectedStatus: http.StatusForbidden, expectedReason: "CHANNEL_MONITOR_MODE_MISMATCH"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rec := performChannelMonitorUserRequest(
				newChannelMonitorUserRouter(t, testCase.enabled, testCase.mode, &channelMonitorUserRepoStub{}),
				"/api/v1/channel-monitors/1/status",
			)
			require.Equal(t, testCase.expectedStatus, rec.Code, rec.Body.String())
			var envelope struct {
				Reason string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
			require.Equal(t, testCase.expectedReason, envelope.Reason)
		})
	}
}

// 启用且 v1 时列表照常返回（真实为空就是空列表，不是被开关短路）。
func TestChannelMonitorUserListServesEnabledV1View(t *testing.T) {
	repo := &channelMonitorUserRepoStub{}
	rec := performChannelMonitorUserRequest(newChannelMonitorUserRouter(t, "true", service.ChannelMonitorModeV1, repo), "/api/v1/channel-monitors")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, repo.listCalls)
	var envelope struct {
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Empty(t, envelope.Data.Items)
}
