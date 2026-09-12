//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCursorModelFallbackAndEndpointOwnership(t *testing.T) {
	models := defaultModelIDsForPlatform(service.PlatformCursor)
	require.Contains(t, models, "default")
	require.Contains(t, models, "claude-sonnet-4-6")
	for _, inbound := range []string{EndpointChatCompletions, EndpointMessages, EndpointResponses} {
		require.Equal(t, "/agent.v1.AgentService/Run", DeriveUpstreamEndpoint(inbound, inbound, service.PlatformCursor))
	}
	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	setActualUpstreamEndpoint(requestContext, EndpointChatCompletions)
	require.Equal(t, EndpointChatCompletions, GetUpstreamEndpoint(requestContext, service.PlatformCursor))
}

func TestCursorPublicModelsFallsBackWithoutMapping(t *testing.T) {
	groupID := int64(81)
	handler := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {{ID: 1, Platform: service.PlatformCursor}}}})
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	requestContext.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: groupID, Platform: service.PlatformCursor}})
	handler.Models(requestContext)
	require.Equal(t, http.StatusOK, writer.Code)
	var response gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(writer.Body.Bytes(), &response))
	require.Equal(t, service.DefaultCursorModelIDs(), modelIDsForTest(response.Data))
	for _, model := range response.Data {
		require.Equal(t, "cursor", model.OwnedBy)
	}
}
