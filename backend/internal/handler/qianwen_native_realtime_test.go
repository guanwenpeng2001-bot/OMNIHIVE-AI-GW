//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeRealtimeRejectsBeforeHandshakeAndAdmission(t *testing.T) {
	for _, path := range []string{"/api-ws/v1/realtime?model=qwen-omni-turbo-realtime", "/api-ws/v1/realtime/", "/api/v1/realtime", "/api/v1/realtime/tokens", "/api/v1/realtime/calls", "/api/v1/services/aigc/realtime/generation"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			t.Run(method+path, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(method, path, strings.NewReader("not JSON"))
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
				// No service dependencies: rejection cannot dial, acquire slots or bill.
				h := &OpenAIGatewayHandler{}
				h.QianwenNative(c)
				require.Equal(t, http.StatusNotFound, w.Code)
				require.Empty(t, w.Header().Get("Upgrade"))
				require.Contains(t, w.Body.String(), "qianwen_realtime_unsupported")
				var response struct {
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
				require.Equal(t, "unsupported_operation_error", response.Error.Type)
				require.Contains(t, response.Error.Message, "per-response text/audio usage billing")
				_, selected := c.Get(opsAccountIDKey)
				require.False(t, selected)
			})
		}
	}
}

func TestQianwenNativeRealtimeGateDoesNotClaimOtherFamilies(t *testing.T) {
	for _, path := range []string{"/api-ws/v1/inference", "/api-ws/v1/realtime-other", "/api/v1/realtime-other", "/api/v1/services/aigc/multimodal-generation/generation", "/v1/realtime"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		require.False(t, (&OpenAIGatewayHandler{}).rejectQianwenNativeRealtime(c), path)
	}
}
