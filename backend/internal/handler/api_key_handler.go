// Package handler provides HTTP request handlers for the application.
package handler

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyHandler handles API key-related requests
type APIKeyHandler struct {
	apiKeyService *service.APIKeyService
}

// NewAPIKeyHandler creates a new APIKeyHandler
func NewAPIKeyHandler(apiKeyService *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
	}
}

// CreateAPIKeyRequest represents the create API key request payload
type CreateAPIKeyRequest struct {
	Name          string   `json:"name" binding:"required"`
	GroupID       *int64   `json:"group_id"`        // nullable
	CustomKey     *string  `json:"custom_key"`      // 可选的自定义key
	IPWhitelist   []string `json:"ip_whitelist"`    // IP 白名单
	IPBlacklist   []string `json:"ip_blacklist"`    // IP 黑名单
	Quota         *float64 `json:"quota"`           // 配额限制 (USD)
	ExpiresInDays *int     `json:"expires_in_days"` // 过期天数

	// Rate limit fields (0 = unlimited)
	RateLimit5h *float64 `json:"rate_limit_5h"`
	RateLimit1d *float64 `json:"rate_limit_1d"`
	RateLimit7d *float64 `json:"rate_limit_7d"`

	// Period budget (USD): one explicit period, separate from RPM/TPM and from
	// the legacy USD windows above. LegacyBudgetAnchor keeps 5h/1d/7d compatible.
	LegacyBudgetAnchor    string  `json:"legacy_budget_anchor"`
	BudgetPeriodMode      string  `json:"budget_period_mode"`
	BudgetLimit           float64 `json:"budget_limit"`
	BudgetDurationSeconds int64   `json:"budget_duration_seconds"`
	BudgetAnchor          string  `json:"budget_anchor"`
	BudgetCalendarUnit    string  `json:"budget_calendar_unit"`
	BudgetTimezone        string  `json:"budget_timezone"`
}

// UpdateAPIKeyRequest represents the update API key request payload
type UpdateAPIKeyRequest struct {
	Name        string    `json:"name"`
	GroupID     *int64    `json:"group_id"`
	Status      string    `json:"status" binding:"omitempty,oneof=active inactive"`
	IPWhitelist *[]string `json:"ip_whitelist"` // IP 白名单（nil 不修改，空数组清空）
	IPBlacklist *[]string `json:"ip_blacklist"` // IP 黑名单（nil 不修改，空数组清空）
	Quota       *float64  `json:"quota"`        // 配额限制 (USD), 0=无限制
	ExpiresAt   *string   `json:"expires_at"`   // 过期时间 (ISO 8601)
	ResetQuota  *bool     `json:"reset_quota"`  // 重置已用配额

	// Rate limit fields (nil = no change, 0 = unlimited)
	RateLimit5h         *float64 `json:"rate_limit_5h"`
	RateLimit1d         *float64 `json:"rate_limit_1d"`
	RateLimit7d         *float64 `json:"rate_limit_7d"`
	ResetRateLimitUsage *bool    `json:"reset_rate_limit_usage"` // 面板自助重置被拒绝；管理员用审计化调整

	// Period budget (nil = no change). ClearBudget removes a self-set budget; an
	// enforced platform budget cannot be changed or cleared from the panel.
	LegacyBudgetAnchor    *string  `json:"legacy_budget_anchor"`
	BudgetPeriodMode      *string  `json:"budget_period_mode"`
	BudgetLimit           *float64 `json:"budget_limit"`
	BudgetDurationSeconds *int64   `json:"budget_duration_seconds"`
	BudgetAnchor          *string  `json:"budget_anchor"`
	BudgetCalendarUnit    *string  `json:"budget_calendar_unit"`
	BudgetTimezone        *string  `json:"budget_timezone"`
	ClearBudget           *bool    `json:"clear_budget"`
}

func validAPIKeyLimit(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

func validateAPIKeyCreateRequest(req CreateAPIKeyRequest) error {
	if req.Quota != nil && !validAPIKeyLimit(*req.Quota) {
		return errors.New("invalid quota")
	}
	if req.RateLimit5h != nil && !validAPIKeyLimit(*req.RateLimit5h) {
		return errors.New("invalid rate_limit_5h")
	}
	if req.RateLimit1d != nil && !validAPIKeyLimit(*req.RateLimit1d) {
		return errors.New("invalid rate_limit_1d")
	}
	if req.RateLimit7d != nil && !validAPIKeyLimit(*req.RateLimit7d) {
		return errors.New("invalid rate_limit_7d")
	}
	if req.ExpiresInDays != nil && *req.ExpiresInDays <= 0 {
		return errors.New("invalid expires_in_days")
	}
	if !validAPIKeyLimit(req.BudgetLimit) {
		return errors.New("invalid budget_limit")
	}
	return nil
}

func validateAPIKeyUpdateRequest(req UpdateAPIKeyRequest) error {
	if req.Quota != nil && !validAPIKeyLimit(*req.Quota) {
		return errors.New("invalid quota")
	}
	if req.RateLimit5h != nil && !validAPIKeyLimit(*req.RateLimit5h) {
		return errors.New("invalid rate_limit_5h")
	}
	if req.RateLimit1d != nil && !validAPIKeyLimit(*req.RateLimit1d) {
		return errors.New("invalid rate_limit_1d")
	}
	if req.RateLimit7d != nil && !validAPIKeyLimit(*req.RateLimit7d) {
		return errors.New("invalid rate_limit_7d")
	}
	if req.BudgetLimit != nil && !validAPIKeyLimit(*req.BudgetLimit) {
		return errors.New("invalid budget_limit")
	}
	return nil
}

// List handles listing user's API keys with pagination
// GET /api/v1/api-keys
func (h *APIKeyHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	// Parse filter parameters
	var filters service.APIKeyListFilters
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		if len(search) > 100 {
			search = search[:100]
		}
		filters.Search = search
	}
	filters.Status = c.Query("status")
	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		gid, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err == nil {
			filters.GroupID = &gid
		}
	}

	keys, result, err := h.apiKeyService.List(c.Request.Context(), subject.UserID, params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 列表同样给出有效 RPM：按分组去重解析，避免逐 Key 重复查组与覆盖。
	out := make([]apiKeyListEntry, 0, len(keys))
	effectiveByGroup := make(map[int64]*service.APIKeyEffectiveRPM, len(keys))
	for i := range keys {
		entry := apiKeyListEntry{APIKey: *dto.APIKeyFromService(&keys[i])}
		groupID := int64(0)
		if keys[i].GroupID != nil {
			groupID = *keys[i].GroupID
		}
		effective, cached := effectiveByGroup[groupID]
		if !cached {
			effective = h.apiKeyService.EffectiveRPM(c.Request.Context(), &keys[i])
			effectiveByGroup[groupID] = effective
		}
		entry.EffectiveRPM = effective
		out = append(out, entry)
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

// apiKeyListEntry / apiKeyDetailResponse 保持既有字段原形，仅追加有效 RPM 说明。
// 用户可见的限额只来自用户/组维度，绝不包含上游账号额度。
type apiKeyListEntry struct {
	dto.APIKey
	EffectiveRPM *service.APIKeyEffectiveRPM `json:"effective_rpm,omitempty"`
}

type apiKeyDetailResponse struct {
	dto.APIKey
	EffectiveRPM *service.APIKeyEffectiveRPM `json:"effective_rpm,omitempty"`
}

// GetByID handles getting a single API key
// GET /api/v1/api-keys/:id
func (h *APIKeyHandler) GetByID(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 验证所有权
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}

	// 有效 RPM 与 Key 详情同源返回：用户可见的来源/限值/重置时间只由
	// 用户与组维度决定，不含任何上游账号额度。
	response.Success(c, apiKeyDetailResponse{
		APIKey:       *dto.APIKeyFromService(key),
		EffectiveRPM: h.apiKeyService.EffectiveRPM(c.Request.Context(), key),
	})
}

// Create handles creating a new API key
// POST /api/v1/api-keys
func (h *APIKeyHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := validateAPIKeyCreateRequest(req); err != nil {
		response.BadRequest(c, "Invalid request: numeric limits must be finite and non-negative, and expires_in_days must be greater than zero")
		return
	}

	svcReq := service.CreateAPIKeyRequest{
		Name:          req.Name,
		GroupID:       req.GroupID,
		CustomKey:     req.CustomKey,
		IPWhitelist:   req.IPWhitelist,
		IPBlacklist:   req.IPBlacklist,
		ExpiresInDays: req.ExpiresInDays,
	}
	if req.Quota != nil {
		svcReq.Quota = *req.Quota
	}
	if req.RateLimit5h != nil {
		svcReq.RateLimit5h = *req.RateLimit5h
	}
	if req.RateLimit1d != nil {
		svcReq.RateLimit1d = *req.RateLimit1d
	}
	if req.RateLimit7d != nil {
		svcReq.RateLimit7d = *req.RateLimit7d
	}
	svcReq.LegacyBudgetAnchor = req.LegacyBudgetAnchor
	svcReq.BudgetPeriodMode = req.BudgetPeriodMode
	svcReq.BudgetLimit = req.BudgetLimit
	svcReq.BudgetDurationSeconds = req.BudgetDurationSeconds
	svcReq.BudgetAnchor = req.BudgetAnchor
	svcReq.BudgetCalendarUnit = req.BudgetCalendarUnit
	svcReq.BudgetTimezone = req.BudgetTimezone

	executeUserIdempotentJSON(c, "user.api_keys.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		key, err := h.apiKeyService.Create(ctx, subject.UserID, svcReq)
		if err != nil {
			return nil, err
		}
		return dto.APIKeyCredentialIssue{APIKey: dto.APIKeyFromService(key), Secret: key.Key}, nil
	})
}

// Update handles updating an API key
// PUT /api/v1/api-keys/:id
func (h *APIKeyHandler) Update(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	var req UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := validateAPIKeyUpdateRequest(req); err != nil {
		response.BadRequest(c, "Invalid request: numeric limits must be finite and non-negative")
		return
	}

	svcReq := service.UpdateAPIKeyRequest{
		IPWhitelist:           req.IPWhitelist,
		IPBlacklist:           req.IPBlacklist,
		Quota:                 req.Quota,
		ResetQuota:            req.ResetQuota,
		RateLimit5h:           req.RateLimit5h,
		RateLimit1d:           req.RateLimit1d,
		RateLimit7d:           req.RateLimit7d,
		ResetRateLimitUsage:   req.ResetRateLimitUsage,
		LegacyBudgetAnchor:    req.LegacyBudgetAnchor,
		BudgetPeriodMode:      req.BudgetPeriodMode,
		BudgetLimit:           req.BudgetLimit,
		BudgetDurationSeconds: req.BudgetDurationSeconds,
		BudgetAnchor:          req.BudgetAnchor,
		BudgetCalendarUnit:    req.BudgetCalendarUnit,
		BudgetTimezone:        req.BudgetTimezone,
		ClearBudget:           req.ClearBudget,
	}
	if req.Name != "" {
		svcReq.Name = &req.Name
	}
	svcReq.GroupID = req.GroupID
	if req.Status != "" {
		svcReq.Status = &req.Status
	}
	// Parse expires_at if provided
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			// Empty string means clear expiration
			svcReq.ExpiresAt = nil
			svcReq.ClearExpiration = true
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				response.BadRequest(c, "Invalid expires_at format: "+err.Error())
				return
			}
			svcReq.ExpiresAt = &t
		}
	}

	key, err := h.apiKeyService.Update(c.Request.Context(), keyID, subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Delete handles deleting an API key
// DELETE /api/v1/api-keys/:id
func (h *APIKeyHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	err = h.apiKeyService.Delete(c.Request.Context(), keyID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "API key deleted successfully"})
}

// GetAvailableGroups 获取用户可以绑定的分组列表
// GET /api/v1/groups/available
func (h *APIKeyHandler) GetAvailableGroups(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.Group, 0, len(groups))
	for i := range groups {
		out = append(out, *dto.GroupFromService(&groups[i]))
	}
	response.Success(c, out)
}

// GetUserGroupRates 获取当前用户的专属分组倍率配置
// GET /api/v1/groups/rates
func (h *APIKeyHandler) GetUserGroupRates(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	rates, err := h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, rates)
}

// RotateAPIKeyCredentialRequest controls a credential rotation.
type RotateAPIKeyCredentialRequest struct {
	// GraceTTLHours overrides the dual-read window for the previous secret. Zero
	// uses the configured default (24h).
	GraceTTLHours *int `json:"grace_ttl_hours"`
	// RevokePrevious makes the previous secret stop working immediately instead of
	// staying valid through the grace window.
	RevokePrevious *bool `json:"revoke_previous"`
}

// RotateCredential rotates an existing key's secret in place, keeping its identity.
// POST /api/v1/api-keys/:id/credentials/rotate
func (h *APIKeyHandler) RotateCredential(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	var req RotateAPIKeyCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.GraceTTLHours != nil && *req.GraceTTLHours < 0 {
		response.BadRequest(c, "Invalid request: grace_ttl_hours must be non-negative")
		return
	}

	key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}

	var graceTTL time.Duration
	if req.GraceTTLHours != nil {
		graceTTL = time.Duration(*req.GraceTTLHours) * time.Hour
	}
	revokePrevious := req.RevokePrevious != nil && *req.RevokePrevious

	issue, err := h.apiKeyService.RotateAPIKey(c.Request.Context(), keyID, graceTTL, revokePrevious, service.ConfigurationActor(c.Request.Context()))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.APIKeyCredentialIssue{
		APIKey:     dto.APIKeyFromService(issue.APIKey),
		Secret:     issue.Secret,
		GraceUntil: issue.GraceUntil,
	})
}

// RevokeCredential immediately revokes a key's graceful (previous) credentials.
// POST /api/v1/api-keys/:id/credentials/revoke
func (h *APIKeyHandler) RevokeCredential(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}

	revoked, err := h.apiKeyService.RevokeAPIKeyCredentials(c.Request.Context(), keyID, service.ConfigurationActor(c.Request.Context()))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"revoked_credentials": revoked})
}
