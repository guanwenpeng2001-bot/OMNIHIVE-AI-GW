//go:build unit

package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeVideoExplicitUnsupportedBeforeAdmission(t *testing.T) {
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
			require.Equal(t, 404, w.Code)
			require.Contains(t, w.Body.String(), "unsupported_operation_error")
			require.Contains(t, w.Body.String(), "durable task ownership")
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
		require.False(t, h.rejectQianwenNativeVideo(c))
	}
}
