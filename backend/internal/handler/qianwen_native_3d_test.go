//go:build unit

package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNative3DOfficialModelsRejectedBeforeAdmission(t *testing.T) {
	for _, model := range []string{"Tripo/Tripo-H3.1", "Tripo/Tripo-P1.0"} {
		for _, path := range []string{service.QianwenNativeTextPath, service.QianwenNativeMultimodalPath} {
			h := newOpenAIImageChatRejectionHandler(t)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", path, strings.NewReader(`{"model":"`+model+`","input":{"prompt":"a cat"}}`))
			setImageChatTestAuth(c)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group = &service.Group{Platform: service.PlatformQianwen}
			h.QianwenNative(c)
			require.Equal(t, 400, w.Code)
			require.Contains(t, w.Body.String(), "unsupported DashScope model family")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
		}
	}
}

func TestQianwenNative3DRejectsSubmissionAndRetriesBeforeAdmission(t *testing.T) {
	// Nil dependencies prove the feature gate never enters scheduling or billing.
	h := &OpenAIGatewayHandler{}
	for _, method := range []string{"POST", "GET", "DELETE"} {
		for attempt := 0; attempt < 2; attempt++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, service.QianwenNative3DPath, strings.NewReader(`{"model":"Tripo/Tripo-P1.0","input":{"prompt":"a cat"}}`))
			c.Request.Header.Set("X-DashScope-Async", "enable")
			c.Request.Header.Set("Idempotency-Key", "same-submission")
			h.QianwenNative(c)
			require.Equal(t, 404, w.Code)
			require.Contains(t, w.Body.String(), "unsupported_operation_error")
			require.Contains(t, w.Body.String(), "qianwen_3d_unsupported")
			require.Contains(t, w.Body.String(), "specification-based 3D billing")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
		}
	}
}
