package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// rejectQianwenNativeRealtime runs inside the existing authenticated native
// ingress, before admission or an upstream handshake. HTTP/SSE accounting cannot
// settle response.done audio/text usage, deduplicate turns, or settle a dropped
// session. Do not upgrade until those capabilities share the gateway lifecycle.
func (h *OpenAIGatewayHandler) rejectQianwenNativeRealtime(c *gin.Context) bool {
	path := c.Request.URL.Path
	for _, prefix := range []string{"/api-ws/v1/realtime", "/api/v1/realtime", "/api/v1/services/aigc/realtime"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			h.errorResponse(c, http.StatusNotFound, "unsupported_operation_error", "Unsupported DashScope realtime operation: bidirectional WebSocket sessions and WebRTC/token endpoints are not enabled; session admission, bounded bidirectional streaming, disconnect settlement and per-response text/audio usage billing are required")
			return true
		}
	}
	return false
}
