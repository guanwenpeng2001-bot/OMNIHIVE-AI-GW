package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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
			allowed      bool
		}{
			{http.MethodPost, "/v1/chat/completions", true},
			{http.MethodPost, "/chat/completions", true},
			{http.MethodPost, "/v1/responses", platform == service.PlatformBedrock},
			{http.MethodPost, "/v1/responses/compact", false},
			{http.MethodPost, "/v1/messages", platform == service.PlatformBedrock},
			{http.MethodPost, "/v1/images/generations", false},
			{http.MethodPost, "/v1/embeddings", false},
			{http.MethodPost, "/v1/audio/speech", false},
			{http.MethodGet, "/models", true},
			{http.MethodGet, "/models/:model", true},
			{http.MethodGet, "/v1/models", true},
			{http.MethodGet, "/v1/models/:model", true},
			{http.MethodGet, "/v1beta/models", true},
			{http.MethodGet, "/v1beta/models/:model", true},
			{http.MethodGet, "/backend-api/codex/models", true},
			{http.MethodGet, "/v1/usage", true},
			{http.MethodGet, "/v1/images/batches/models", false},
			{http.MethodGet, "/images/batches/models", false},
			{http.MethodGet, "/v1/images/batches/models/:model", false},
			{http.MethodGet, "/v1/other/models", false},
			{http.MethodGet, "/v1/other/usage", false},
			{http.MethodGet, "/v1/models/:model/extra", false},
		} {
			test.Run(platform+"/"+endpoint.method+endpoint.path, func(test *testing.T) {
				calls := 0
				router := gin.New()
				router.Use(func(requestContext *gin.Context) {
					requestContext.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: platform}})
				}, chatOnlyPlatformGuard)
				router.Handle(endpoint.method, endpoint.path, func(requestContext *gin.Context) { calls++; requestContext.Status(http.StatusNoContent) })
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, concreteMatrixPath(endpoint.path), nil))
				if endpoint.allowed {
					require.Equal(test, 1, calls)
					require.Equal(test, http.StatusNoContent, recorder.Code)
				} else {
					require.Zero(test, calls, "unsupported protocols must not reach scheduling, transport or billing")
					require.Equal(test, http.StatusNotFound, recorder.Code)
				}
			})
		}
	}
}
