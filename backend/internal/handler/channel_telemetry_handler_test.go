package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type channelTelemetryGroupAuthorizerStub struct {
	groups []service.Group
	err    error
	calls  []int64
}

func (s *channelTelemetryGroupAuthorizerStub) GetAvailableGroups(_ context.Context, userID int64) ([]service.Group, error) {
	s.calls = append(s.calls, userID)
	return s.groups, s.err
}

func TestChannelTelemetryQueryListSupportsRepeatedAndCommaValues(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/?platform=openai,grok&platform=anthropic", nil)
	require.Equal(t, []string{"openai", "grok", "anthropic"}, queryList(c, "platform"))
}

func TestChannelTelemetryGroupByQueryDefaultsAndRejectsInvalid(t *testing.T) {
	groupBy, err := service.ParseChannelTelemetryGroupBy("")
	require.NoError(t, err)
	require.Equal(t, service.ChannelTelemetryGroupByPlatformGroup, groupBy)
	_, err = service.ParseChannelTelemetryGroupBy("invalid")
	require.Error(t, err)
}

func TestChannelTelemetryMatrixHandlerRejectsInvalidGroupBy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/matrix?group_by=invalid", nil)
	h := NewChannelTelemetryHandler(service.NewChannelTelemetryService(nil), nil)
	h.Matrix(c)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestChannelTelemetryScopeFilterUsesAvailableGroupsForOrdinaryUser(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/snapshot", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	authorizer := &channelTelemetryGroupAuthorizerStub{groups: []service.Group{{ID: 3}, {ID: 7}}}
	h := &ChannelTelemetryHandler{apiKeyService: authorizer}
	filter := service.ChannelTelemetryFilter{GroupIDs: []int64{7, 9}}

	require.True(t, h.scopeFilter(c, &filter, false))
	require.True(t, filter.RestrictGroups)
	require.Equal(t, []int64{3, 7}, filter.AllowedGroupIDs)
	require.Equal(t, []int64{42}, authorizer.calls)
}

func TestChannelTelemetryScopeFilterPreservesEmptyOrdinaryUserScope(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/snapshot", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	h := &ChannelTelemetryHandler{apiKeyService: &channelTelemetryGroupAuthorizerStub{}}
	filter := service.ChannelTelemetryFilter{}

	require.True(t, h.scopeFilter(c, &filter, false))
	require.True(t, filter.RestrictGroups)
	require.Empty(t, filter.AllowedGroupIDs)
}

func TestChannelTelemetryScopeFilterLeavesAdminUnrestricted(t *testing.T) {
	authorizer := &channelTelemetryGroupAuthorizerStub{}
	h := &ChannelTelemetryHandler{apiKeyService: authorizer}
	filter := service.ChannelTelemetryFilter{GroupIDs: []int64{9}}

	require.True(t, h.scopeFilter(nil, &filter, true))
	require.False(t, filter.RestrictGroups)
	require.Nil(t, filter.AllowedGroupIDs)
	require.Empty(t, authorizer.calls)
}
