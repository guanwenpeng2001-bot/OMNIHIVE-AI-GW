//go:build unit

package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQianwenAsyncResultsAreOwnerScoped(t *testing.T) {
	for _, family := range []string{"3d", "asr"} {
		result := `[{"pbr_model_url":"https://example.com/model.glb"}]`
		if family == "asr" {
			result = `[{"channel_id":0,"text":"private transcript"}]`
		}
		repo := &videoJobReadRepository{job: &service.UpstreamJob{PublicRequestID: "public-async", UpstreamTaskID: "private-upstream-id", UserID: 1, APIKeyID: 2, Platform: service.PlatformQianwen, Family: family, Status: "succeeded", ResultRef: result}}
		h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(repo, nil, nil)}
		for _, keyID := range []int64{2, 3} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api/v1/tasks/public-async", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: keyID, UserID: 1, Group: &service.Group{}})
			h.QianwenNative(c)
			require.NotContains(t, w.Body.String(), "private-upstream-id")
			if keyID == 2 {
				require.Equal(t, 200, w.Code)
				require.Contains(t, w.Body.String(), `"results":[`)
				require.NotContains(t, w.Body.String(), "video_url")
			} else {
				require.Equal(t, 404, w.Code)
				require.NotContains(t, w.Body.String(), "private transcript")
			}
		}
	}
}

func TestQianwenAsyncRejectsUnknownTariffsBeforeScheduling(t *testing.T) {
	for _, path := range []string{service.QianwenNative3DPath, service.QianwenNativeASRPath, service.QianwenNativeText2ImagePath} {
		h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(nil, nil, nil)}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", path, strings.NewReader(`{"model":"unpriced-model"}`))
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, Group: &service.Group{Platform: service.PlatformQianwen, RateMultiplier: 1}})
		h.QianwenNative(c)
		require.Equal(t, 422, w.Code)
		require.Contains(t, w.Body.String(), "unsupported")
	}
}
