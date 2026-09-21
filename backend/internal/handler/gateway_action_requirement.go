package handler

import "github.com/gin-gonic/gin"

// ActionRequirement delegates admission to the same service resolver used by
// both gateway trunks; routes do not inspect model facts or platform policy.
func (h *GatewayHandler) ActionRequirement(requestContext *gin.Context) {
	h.gatewayService.ActionRequirement(requestContext)
}
