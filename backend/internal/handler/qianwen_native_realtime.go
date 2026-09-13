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
// The bounded implementation and release criteria are specified in
// docs/DASHSCOPE_REALTIME_MVP.md. A per-minute group price cannot enable this API.
func (h *OpenAIGatewayHandler) rejectQianwenNativeRealtime(c *gin.Context) bool {
	path := c.Request.URL.Path
	for _, prefix := range []string{"/api-ws/v1/realtime", "/api/v1/realtime", "/api/v1/services/aigc/realtime"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "unsupported_operation_error", "code": "qianwen_realtime_unsupported", "message": "Unsupported DashScope operation: realtime is not enabled; per-response text/audio usage billing and durable disconnect settlement are required. Setting audio_realtime_price_per_min cannot enable DashScope: Omni uses modality tokens and Qwen3-TTS uses characters, not elapsed session minutes. WebRTC/token endpoints are also unavailable"}})
			return true
		}
	}
	return false
}
