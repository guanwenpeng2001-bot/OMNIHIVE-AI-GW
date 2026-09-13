package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Reject before admission: no upstream job may exist without durable ownership,
// terminal settlement and an independent 3D tariff. Never bill this as video.
func (h *OpenAIGatewayHandler) rejectQianwenNative3D(c *gin.Context) bool {
	if c.Request.URL.Path != service.QianwenNative3DPath {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "unsupported_operation_error", "code": "qianwen_3d_unsupported", "message": service.QianwenNative3DUnsupported}})
	return true
}
