package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// SetConfigResetHooks connects optional runtime reloaders without changing the
// settings constructor or assigning backup/image-secret ownership to BE-6.
func (h *SettingHandler) SetConfigResetHooks(refresh func(context.Context) error, imageReset func(context.Context) error) {
	h.configResetHooks = service.SettingResetHooks{Refresh: refresh, ImageReset: imageReset}
}

// GetEffectiveConfig handles GET /api/v1/admin/config/effective.
func (h *SettingHandler) GetEffectiveConfig(c *gin.Context) {
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			response.ErrorWithDetails(c, http.StatusBadRequest, "limit must be between 1 and 200", "CONFIG_LIMIT_INVALID", nil)
			return
		}
		limit = parsed
	}
	if source := c.Query("source"); source != "" && !validConfigSource(source) {
		response.ErrorWithDetails(c, http.StatusBadRequest, "unknown configuration source", "CONFIG_FILTER_INVALID", nil)
		return
	}
	if state := c.Query("state"); state != "" && !validConfigState(state) {
		response.ErrorWithDetails(c, http.StatusBadRequest, "unknown configuration state", "CONFIG_FILTER_INVALID", nil)
		return
	}
	page, err := h.settingService.EffectiveSettings(c.Request.Context(), service.EffectiveSettingsFilter{
		Group: c.Query("group"), Query: c.Query("q"), AfterCursor: c.Query("cursor"), Limit: limit,
		Source: c.Query("source"), State: service.EffectiveSettingState(c.Query("state")),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]dto.EffectiveConfigItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, effectiveConfigItemToDTO(item))
	}
	response.Success(c, dto.EffectiveConfigResponse{
		ConfigFile: page.ConfigFile, GeneratedAt: page.GeneratedAt.Format(time.RFC3339),
		Items: items, NextCursor: page.NextCursor,
	})
}

// ResetConfigOverrides handles POST /api/v1/admin/config/reset.
func (h *SettingHandler) ResetConfigOverrides(c *gin.Context) {
	var request dto.ResetConfigRequest
	if err := c.ShouldBindBodyWith(&request, binding.JSON); err != nil {
		response.ErrorWithDetails(c, http.StatusBadRequest, "keys must be a JSON array of registered configuration keys", "CONFIG_RESET_REQUEST_INVALID", nil)
		return
	}
	keys, err := h.settingService.ResetConfigOverrides(c.Request.Context(), request.Keys, h.configResetHooks)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.config.reset")
	keysJSON, _ := json.Marshal(keys)
	middleware.SetAuditExtra(c, map[string]any{"result": "success", "requested_count": len(keys), "config_reset_keys": string(keysJSON)})
	settings, err := h.SystemSettingsProjection(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items, err := h.settingService.EffectiveSettingByKeys(c.Request.Context(), keys)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	responseItems := make([]dto.EffectiveConfigItem, 0, len(items))
	for _, item := range items {
		responseItems = append(responseItems, effectiveConfigItemToDTO(item))
	}
	response.Success(c, dto.ResetConfigResponse{Settings: settings, Items: responseItems, GeneratedAt: time.Now().UTC().Format(time.RFC3339)})
}

func validConfigSource(source string) bool {
	switch source {
	case "database", "environment", "yaml", "default", "derived", "artifact":
		return true
	default:
		return false
	}
}

func validConfigState(state string) bool {
	switch service.EffectiveSettingState(state) {
	case service.EffectiveSettingStateEffective, service.EffectiveSettingStateInvalid, service.EffectiveSettingStateUnavailable, service.EffectiveSettingStateBlockedByHardGate:
		return true
	default:
		return false
	}
}

func effectiveConfigItemToDTO(item service.EffectiveSetting) dto.EffectiveConfigItem {
	result := dto.EffectiveConfigItem{
		Key: item.Key, Group: item.Group, ValueType: string(item.ValueType), Value: item.Value,
		Source: item.Source, Authority: string(item.Authority), FallbackChain: item.FallbackChain,
		RestartRequired: item.RestartRequired, Resettable: item.Resettable, Sensitive: item.Sensitive,
		State: string(item.State), Configured: item.Configured, BlockedBy: item.BlockedBy,
		InstallSeedSource: item.InstallSeedSource,
		ErrorCode:         item.ErrorCode,
	}
	if item.HasBaseline {
		result.Baseline = item.Baseline
	}
	return result
}
