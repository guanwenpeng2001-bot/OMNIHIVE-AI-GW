//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compositeVideoOwnerCache struct {
	service.GatewayCache
	ownedHash string
}

func (cache *compositeVideoOwnerCache) GetSessionAccountID(_ context.Context, groupID int64, hash string) (int64, error) {
	if groupID == 13 && hash == cache.ownedHash {
		return 7, nil
	}
	return 0, service.ErrStickySessionNotFound
}

func TestCompositeVideoJobTargetRequiresPositiveGrokOwnership(test *testing.T) {
	cache := &compositeVideoOwnerCache{ownedHash: "openai:" + service.GrokMediaVideoRequestSessionHash("provider-video", 1, 2)}
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, cache, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	relay := &OpenAIGatewayHandler{gatewayService: gateway, upstreamJobs: service.NewUpstreamJobService(&gatewayResourceHandlerRepo{}, nil, nil)}
	for _, scenario := range []struct {
		name      string
		user, key int64
		status    int
	}{
		{"owner", 1, 2, 204}, {"wrong user", 99, 2, 404}, {"wrong key", 1, 99, 404},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			key := gatewayResourceKey(service.PlatformComposite)
			key.UserID = scenario.user
			key.ID = scenario.key
			router := gin.New()
			router.Use(func(request *gin.Context) { request.Set(string(middleware2.ContextKeyAPIKey), key); request.Next() })
			router.Use(relay.CompositeVideoJobTarget(func(request *gin.Context) { test.Fatal("lifecycle must use ownership, not model inference") }))
			router.GET("/v1/videos/:request_id", func(request *gin.Context) {
				platform, found := service.ResolvedTargetPlatformFromContext(request.Request.Context())
				require.True(test, found)
				require.Equal(test, service.PlatformGrok, platform)
				request.Status(204)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/videos/provider-video?model=qwen-video", nil))
			require.Equal(test, scenario.status, recorder.Code, recorder.Body.String())
		})
	}
}
