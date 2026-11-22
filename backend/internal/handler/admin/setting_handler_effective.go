package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// GetEffectiveSettings serves a redacted snapshot through the admin route group.
func (h *SettingHandler) GetEffectiveSettings(c *gin.Context) {
	settings, err := h.settingService.GetEffectiveSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// ResetSettingOverrides removes explicit overrides. OAuth credentials are reset
// by whole provider group, with an optimistic version checked atomically in DB.
func (h *SettingHandler) ResetSettingOverrides(c *gin.Context) {
	var req service.ResetSettingOverridesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid settings reset request")
		return
	}
	settings, err := h.settingService.ResetSettingOverrides(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
