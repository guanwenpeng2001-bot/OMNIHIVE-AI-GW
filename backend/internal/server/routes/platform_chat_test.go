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
		if !service.IsChatOnlyPlatform(platform) {
			continue
		}
		for _, path := range []string{"/v1/chat/completions", "/chat/completions", "/v1/responses", "/v1/responses/compact", "/v1/messages", "/v1/images/generations", "/v1/embeddings", "/v1/audio/speech"} {
			test.Run(platform+path, func(test *testing.T) {
				calls := 0
				router := gin.New()
				router.Use(func(requestContext *gin.Context) {
					requestContext.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: platform}})
				}, chatOnlyPlatformGuard)
				router.POST(path, func(requestContext *gin.Context) { calls++; requestContext.Status(http.StatusNoContent) })
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
				if path == "/v1/chat/completions" || path == "/chat/completions" {
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
