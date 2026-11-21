package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChatOnlyPlatformProtocolGuard(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range service.ConcretePlatforms() {
		if !service.IsChatOnlyPlatform(platform) && platform != service.PlatformBedrock {
			continue
		}
		for _, endpoint := range []struct {
			method, path string
		}{
			{http.MethodPost, "/v1/chat/completions"},
			{http.MethodPost, "/chat/completions"},
			{http.MethodPost, "/v1/responses"},
			{http.MethodPost, "/v1/responses/compact"},
			{http.MethodPost, "/v1/messages"},
			{http.MethodPost, "/v1/images/generations"},
			{http.MethodPost, "/v1/embeddings"},
			{http.MethodPost, "/v1/rerank"},
			{http.MethodPost, "/v1/images/edits"},
			{http.MethodPost, "/v1/videos"},
			{http.MethodGet, "/v1/videos/:request_id"},
			{http.MethodGet, "/v1/videos/:request_id/content"},
			{http.MethodPost, "/v1/videos/:request_id/cancel"},
			{http.MethodPost, "/v1/audio/speech"},
			{http.MethodGet, "/models"},
			{http.MethodGet, "/models/:model"},
			{http.MethodGet, "/v1/models"},
			{http.MethodGet, "/v1/models/:model"},
			{http.MethodGet, "/v1beta/models"},
			{http.MethodGet, "/v1beta/models/:model"},
			{http.MethodGet, "/backend-api/codex/models"},
			{http.MethodGet, "/v1/usage"},
			{http.MethodGet, "/v1/images/batches/models"},
			{http.MethodGet, "/images/batches/models"},
			{http.MethodGet, "/v1/images/batches/models/:model"},
			{http.MethodGet, "/v1/other/models"},
			{http.MethodGet, "/v1/other/usage"},
			{http.MethodGet, "/v1/models/:model/extra"},
		} {
			test.Run(platform+"/"+endpoint.method+endpoint.path, func(test *testing.T) {
				calls := 0
				router := gin.New()
				router.Use(func(requestContext *gin.Context) {
					requestContext.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: platform}})
				}, service.GatewayActionRequirement)
				router.Handle(endpoint.method, endpoint.path, func(requestContext *gin.Context) { calls++; requestContext.Status(http.StatusNoContent) })
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, concreteMatrixPath(endpoint.path), nil))
				action := service.GatewayActionForRoute(endpoint.method, endpoint.path)
				allowed := action == "" || domain.PlatformAction(domain.ActionIdentity{Platform: platform}, action).Status == domain.ActionImplemented
				if allowed {
					require.Equal(test, 1, calls)
					require.Equal(test, http.StatusNoContent, recorder.Code)
				} else {
					require.Zero(test, calls, "unsupported protocols must not reach scheduling, transport or billing")
					require.Equal(test, http.StatusBadRequest, recorder.Code)
				}
			})
		}
	}
}

func TestQianfanPublicActionRoutes(t *testing.T) {
	for _, route := range []struct{ method, path, adapter string }{
		{http.MethodPost, "/v1/embeddings", "qianfan.embeddings"},
		{http.MethodPost, "/v1/rerank", "qianfan.rerank"},
		{http.MethodPost, "/v1/images/generations", "qianfan.images"},
		{http.MethodPost, "/v1/images/edits", "qianfan.image.edit"},
		{http.MethodPost, "/v1/videos", "qianfan.video.generate"},
		{http.MethodGet, "/v1/videos/:request_id", "qianfan.video.status"},
		{http.MethodGet, "/v1/videos/:request_id/content", "qianfan.video.content"},
		{http.MethodPost, "/v1/videos/:request_id/cancel", "qianfan.video.cancel"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			action := service.GatewayActionForRoute(route.method, route.path)
			cell := domain.PlatformAction(domain.ActionIdentity{Platform: service.PlatformQianfan, HostedAccountType: service.AccountTypeAPIKey}, action)
			require.Equal(t, domain.ActionImplemented, cell.Status)
			require.Equal(t, route.adapter, cell.Adapter)
			// Pin the expected adapter independently of the matrix used by the guard.
			calls := 0
			dispatch := service.NewGatewayActionDispatch(service.GatewayActionHandlers{route.adapter: func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) }})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: service.PlatformQianfan}})
			}, service.GatewayActionRequirement)
			router.Handle(route.method, route.path, dispatch.Handler(action))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(route.method, concreteMatrixPath(route.path), nil))
			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.Equal(t, 1, calls)
		})
	}
}
