//go:build unit

package handler

import (
	"net/http/httptest"
	"testing"

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
