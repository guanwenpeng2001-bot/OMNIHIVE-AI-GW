package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// rejectQianwenNativeResource runs after the existing route authentication chain
// and before parsing bodies, acquiring slots, selecting accounts or billing.
func (h *OpenAIGatewayHandler) rejectQianwenNativeResource(c *gin.Context) bool {
	gate, ok := service.QianwenNativeResourceGateForPath(c.Request.URL.Path)
	if !ok {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{
		"type":    "unsupported_operation_error",
		"code":    gate.Code,
		"family":  gate.Family,
		"message": "Unsupported DashScope operation: " + gate.Reason,
	}})
	return true
}
