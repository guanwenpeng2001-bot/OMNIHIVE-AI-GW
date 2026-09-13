package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// No upstream submission may happen while the durable video lifecycle and
// Qianwen-specific tariff resolver are unavailable. Keep authentication in the
// existing rootRoute middleware and fail before account/concurrency admission.
func (h *OpenAIGatewayHandler) rejectQianwenNativeVideo(c *gin.Context) bool {
	if c.Request.URL.Path != service.QianwenNativeVideoPath {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
	h.errorResponse(c, http.StatusNotFound, "unsupported_operation_error", service.QianwenNativeVideoUnsupported)
	return true
}
