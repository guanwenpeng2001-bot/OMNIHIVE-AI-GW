package routes

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeResourceLifecycleRoutes(t *testing.T) {
	r := newGatewayRoutesTestRouter(service.PlatformQianwen)
	for _, tc := range []struct{ path, family string }{
		{"/api/v1/apps/app/completion", "rag"}, {"/api/v1/indices/knowledge/search", "rag"},
		{"/api/v1/files", "files"}, {"/api/v1/batches", "batch"},
		{"/compatible-mode/v1/files", "files"}, {"/compatible-mode/v1/files/foreign", "files"},
		{"/compatible-mode/v1/files/foreign/content", "files"},
		{"/compatible-mode/v1/batches", "batch"}, {"/compatible-mode/v1/batches/foreign", "batch"},
		{"/compatible-mode/v1/batches/foreign/cancel", "batch"},
	} {
		for _, method := range []string{"POST", "GET", "DELETE", "PUT", "PATCH"} {
			t.Run(method+tc.path, func(t *testing.T) {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(method, tc.path, strings.NewReader(`{"model":"qwen-plus"}`)))
				require.Equal(t, 404, w.Code)
				var body struct {
					Error struct{ Type, Code, Family, Message string }
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				require.Equal(t, "unsupported_operation_error", body.Error.Type)
				require.Equal(t, "qianwen_"+tc.family+"_unsupported", body.Error.Code)
				require.Equal(t, tc.family, body.Error.Family)
				require.Contains(t, body.Error.Message, "Unsupported DashScope operation")
			})
		}
	}
}
