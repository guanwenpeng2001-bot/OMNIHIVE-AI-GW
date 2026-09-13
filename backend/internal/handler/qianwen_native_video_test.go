//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoJobReadRepository struct {
	service.UpstreamJobRepository
	job   *service.UpstreamJob
	reads int
}

func (r *videoJobReadRepository) Get(_ context.Context, id string, user, key int64) (*service.UpstreamJob, error) {
	r.reads++
	if id != r.job.PublicRequestID || user != r.job.UserID || key != r.job.APIKeyID {
		return nil, service.ErrUpstreamJobNotFound
	}
	return r.job, nil
}

func TestQianwenNativeVideoMissingTariffNeverAdmits(t *testing.T) {
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(nil, nil, nil)}
	for range 2 {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", service.QianwenNativeVideoPath, strings.NewReader(`{"model":"wan2.6-t2v","parameters":{"resolution":"720P"}}`))
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 1, Group: &service.Group{Platform: service.PlatformQianwen, RateMultiplier: 1}})
		h.QianwenNative(c)
		require.Equal(t, 422, w.Code)
		require.Contains(t, w.Body.String(), "tariff")
	}
}

func TestQianwenNativeVideoReadOwnershipAndPublicResult(t *testing.T) {
	repo := &videoJobReadRepository{job: &service.UpstreamJob{PublicRequestID: "public-1", UpstreamTaskID: "secret-upstream", UserID: 1, APIKeyID: 2, Platform: service.PlatformQianwen, Family: "video", Status: "succeeded", ResultRef: "https://example.test/video.mp4"}}
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(repo, nil, nil)}
	for _, key := range []int64{2, 3} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/tasks/public-1", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: key, UserID: 1, Group: &service.Group{}})
		h.QianwenNative(c)
		require.NotContains(t, w.Body.String(), "secret-upstream")
		if key == 2 {
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), "video.mp4")
		} else {
			require.Equal(t, 404, w.Code)
			require.NotContains(t, w.Body.String(), "video.mp4")
		}
	}
	require.Equal(t, 2, repo.reads)
}

func TestQianwenNativeVideoUnavailableBeforeAdmission(t *testing.T) {
	// A handler without services proves no scheduler, network or billing access.
	h := &OpenAIGatewayHandler{}
	for _, method := range []string{"POST", "GET", "DELETE"} {
		for repeat := 0; repeat < 2; repeat++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, service.QianwenNativeVideoPath, strings.NewReader(`{"model":"wan2.6-t2v","input":{"prompt":"A cat"}}`))
			c.Request.Header.Set("X-DashScope-Async", "enable")
			c.Request.Header.Set("Idempotency-Key", "same-submit")
			h.QianwenNative(c)
			if method == "POST" {
				require.Equal(t, 503, w.Code)
			} else {
				require.Equal(t, 404, w.Code)
			}
			require.Contains(t, w.Body.String(), "unsupported_operation_error")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
		}
	}
}

func TestQianwenNativeVideoGateDoesNotClaimOtherFamilies(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	for _, path := range []string{service.QianwenNativeMultimodalPath, "/api/v1/tasks/job", "/api/v1/services/aigc/video-generation/3d-generation"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, nil)
		require.False(t, h.handleQianwenNativeVideo(c))
	}
}
