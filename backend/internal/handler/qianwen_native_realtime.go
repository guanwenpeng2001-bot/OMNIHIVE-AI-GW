package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// The native authenticated ingress handles the bounded TTS, Omni and ASR subsets and retains
// the structured gate for all other realtime families and transports.
func (h *OpenAIGatewayHandler) rejectQianwenNativeRealtime(c *gin.Context) bool {
	path := c.Request.URL.Path
	if path == "/api-ws/v1/realtime" && c.Request.Method == http.MethodGet {
		h.qianwenRealtime(c)
		return true
	}
	for _, prefix := range []string{"/api-ws/v1/realtime", "/api/v1/realtime", "/api/v1/services/aigc/realtime"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "unsupported_operation_error", "code": "qianwen_realtime_unsupported", "message": "Unsupported DashScope operation: realtime is not enabled; per-response text/audio usage billing and durable disconnect settlement are required. Setting audio_realtime_price_per_min cannot enable DashScope: Omni uses modality tokens and Qwen3-TTS uses characters, not elapsed session minutes. WebRTC/token endpoints are also unavailable"}})
			return true
		}
	}
	return false
}

// QianwenRealtimeCompat provides path compatibility for /v1/realtime.
// Clients still speak DashScope events; this is not full OpenAI realtime SDK compatibility.
func (h *OpenAIGatewayHandler) QianwenRealtimeCompat(c *gin.Context) { h.qianwenRealtime(c) }
