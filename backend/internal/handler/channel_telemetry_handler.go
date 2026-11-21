package handler

import (
	"context"
	"errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type ChannelTelemetryHandler struct {
	service       *service.ChannelTelemetryService
	apiKeyService channelTelemetryGroupAuthorizer
}

type channelTelemetryGroupAuthorizer interface {
	GetAvailableGroups(ctx context.Context, userID int64) ([]service.Group, error)
}

func NewChannelTelemetryHandler(svc *service.ChannelTelemetryService, apiKeyService *service.APIKeyService) *ChannelTelemetryHandler {
	return &ChannelTelemetryHandler{service: svc, apiKeyService: apiKeyService}
}

// channelTelemetryIsAdmin is true when the request already passed admin auth
// (shared Dimensions/Errors handlers serve both user and admin route groups).
func channelTelemetryIsAdmin(c *gin.Context) bool {
	role, ok := middleware.GetUserRoleFromContext(c)
	return (ok && role == service.RoleAdmin) || domain.WorkspaceAdministration(c.Request.Context())
}

func (h *ChannelTelemetryHandler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *ChannelTelemetryHandler) UpdateConfig(c *gin.Context) {
	var input service.ChannelTelemetryConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "invalid channel monitor v2 config")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "user not found in context")
		return
	}
	updated, err := h.service.UpdateConfig(c.Request.Context(), input, input.Version, subject.UserID)
	if err != nil {
		if errors.Is(err, service.ErrChannelTelemetryConfigConflict) {
			response.Error(c, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, service.ErrChannelTelemetryInvalidConfig) {
			response.BadRequest(c, err.Error())
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}

func (h *ChannelTelemetryHandler) Dimensions(c *gin.Context) {
	filter, ok := h.parseFilter(c)
	if !ok {
		return
	}
	admin := channelTelemetryIsAdmin(c)
	if !h.scopeFilter(c, &filter, admin) {
		return
	}
	result, err := h.service.Dimensions(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// Admin and user share this handler; only non-admin responses strip volume.
	if !admin {
		service.RedactChannelTelemetryDimensions(result)
	}
	response.Success(c, result)
}

func (h *ChannelTelemetryHandler) Snapshot(c *gin.Context)      { h.snapshot(c, false) }
func (h *ChannelTelemetryHandler) AdminSnapshot(c *gin.Context) { h.snapshot(c, true) }
func (h *ChannelTelemetryHandler) Models(c *gin.Context)        { h.models(c, false) }
func (h *ChannelTelemetryHandler) AdminModels(c *gin.Context)   { h.models(c, true) }
func (h *ChannelTelemetryHandler) Matrix(c *gin.Context)        { h.matrix(c, false) }
func (h *ChannelTelemetryHandler) AdminMatrix(c *gin.Context)   { h.matrix(c, true) }
func (h *ChannelTelemetryHandler) Users(c *gin.Context)         { h.users(c, false) }
func (h *ChannelTelemetryHandler) AdminUsers(c *gin.Context)    { h.users(c, true) }

func (h *ChannelTelemetryHandler) snapshot(c *gin.Context, admin bool) {
	filter, ok := h.parseFilter(c)
	if !ok {
		return
	}
	if !h.scopeFilter(c, &filter, admin) {
		return
	}
	result, err := h.service.Snapshot(c.Request.Context(), filter, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ChannelTelemetryHandler) models(c *gin.Context, admin bool) {
	filter, ok := h.parseFilter(c)
	if !ok {
		return
	}
	if !h.scopeFilter(c, &filter, admin) {
		return
	}
	result, err := h.service.Models(c.Request.Context(), filter, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ChannelTelemetryHandler) matrix(c *gin.Context, admin bool) {
	filter, ok := h.parseFilter(c)
	if !ok {
		return
	}
	groupBy, err := service.ParseChannelTelemetryGroupBy(c.Query("group_by"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if !h.scopeFilter(c, &filter, admin) {
		return
	}
	result, err := h.service.Matrix(c.Request.Context(), filter, groupBy, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ChannelTelemetryHandler) Errors(c *gin.Context) {
	filter, ok := h.parseFilter(c)
	if !ok {
		return
	}
	admin := channelTelemetryIsAdmin(c)
	if !h.scopeFilter(c, &filter, admin) {
		return
	}
	result, err := h.service.ErrorsForViewer(c.Request.Context(), filter, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ChannelTelemetryHandler) users(c *gin.Context, admin bool) {
	filter, ok := h.parseFilter(c)
	if !ok {
		return
	}
	subject, exists := middleware.GetAuthSubjectFromContext(c)
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not found in context")
		return
	}
	if !h.scopeFilter(c, &filter, admin) {
		return
	}
	result, err := h.service.Users(c.Request.Context(), filter, subject.UserID, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ChannelTelemetryHandler) scopeFilter(c *gin.Context, filter *service.ChannelTelemetryFilter, admin bool) bool {
	if admin {
		return true
	}
	if h.apiKeyService == nil {
		response.Error(c, http.StatusInternalServerError, "channel monitor group authorization unavailable")
		return false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "user not found in context")
		return false
	}
	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	filter.RestrictGroups = true
	filter.AllowedGroupIDs = make([]int64, 0, len(groups))
	for i := range groups {
		filter.AllowedGroupIDs = append(filter.AllowedGroupIDs, groups[i].ID)
	}
	return true
}

func (h *ChannelTelemetryHandler) parseFilter(c *gin.Context) (service.ChannelTelemetryFilter, bool) {
	groups, err := parseChannelTelemetryGroupIDs(queryList(c, "group_id"))
	if err != nil {
		response.BadRequest(c, "invalid group_id")
		return service.ChannelTelemetryFilter{}, false
	}
	filter, err := h.service.ParseFilter(c.Query("range"), queryList(c, "platform"), queryList(c, "model"), groups)
	if err != nil {
		response.BadRequest(c, err.Error())
		return service.ChannelTelemetryFilter{}, false
	}
	filter.WorkspaceID, _ = domain.WorkspaceScope(c.Request.Context())
	return filter, true
}

func queryList(c *gin.Context, key string) []string {
	values := c.QueryArray(key)
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func parseChannelTelemetryGroupIDs(values []string) ([]int64, error) {
	result := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid group id")
		}
		result = append(result, id)
	}
	return result, nil
}
