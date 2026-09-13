//go:build unit

package handler

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type qianwenResourceUnreadBody struct{ t *testing.T }

func (b qianwenResourceUnreadBody) Read([]byte) (int, error) {
	b.t.Fatal("unsupported resource request must not consume uploads or start inference")
	return 0, io.EOF
}
func (qianwenResourceUnreadBody) Close() error { return nil }

func TestQianwenNativeResourcesRejectedBeforeAdmission(t *testing.T) {
	for _, path := range []string{"/api/v1/indices/knowledge/search", "/api/v1/apps/foreign/completion", "/api/v1/files", "/compatible-mode/v1/files/foreign/content", "/compatible-mode/v1/batches", "/compatible-mode/v1/batches/foreign/cancel"} {
		for _, method := range []string{"POST", "GET", "DELETE", "PUT", "PATCH"} {
			t.Run(method+path, func(t *testing.T) {
				// A nil handler has no scheduler, transport, billing service or repo.
				// Repeated submission/polling must continue to be a pure rejection.
				var h *OpenAIGatewayHandler
				for repeat := 0; repeat < 2; repeat++ {
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest(method, path, nil)
					c.Request.Body = qianwenResourceUnreadBody{t}
					c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=never-read")
					c.Request.Header.Set("Idempotency-Key", "same-key")
					h.QianwenNative(c)
					require.Equal(t, 404, w.Code)
					require.Contains(t, w.Body.String(), "unsupported_operation_error")
					require.True(t, c.IsAborted())
					_, selected := c.Get(opsAccountIDKey)
					require.False(t, selected)
				}
			})
		}
	}
}
