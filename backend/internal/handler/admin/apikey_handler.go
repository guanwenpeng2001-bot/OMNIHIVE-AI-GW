package admin

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService  service.AdminService
	apiKeyService *service.APIKeyService
}

func (h *AdminAPIKeyHandler) UpdateTPMLimit(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || keyID <= 0 {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	var request struct {
		TPMLimit service.OptionalTPMLimit `json:"tpm_limit,omitzero"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	key, err := h.adminService.AdminSetAPIKeyTPMLimit(c.Request.Context(), keyID, request.TPMLimit.Pointer())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.APIKeyFromService(key))
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService, apiKeyService *service.APIKeyService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService:  adminService,
		apiKeyService: apiKeyService,
	}
}

// CreateForUser mints a credential owned by the target user, using the same
// permissions and transactional digest storage as the user-facing create path.
// POST /api/v1/admin/users/:id/api-keys
func (h *AdminAPIKeyHandler) CreateForUser(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	// This provisioning contract deliberately excludes ownership, workspace,
	// custom secrets and quota/policy overrides. Reject unknown fields rather
	// than silently accepting options that would imply broader permissions.
	var req struct {
		Name    string `json:"name"`
		GroupID *int64 `json:"group_id"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		response.BadRequest(c, "Invalid request: expected name and group_id")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Invalid request: expected a single JSON object")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(c, "name is required")
		return
	}

	key, err := h.apiKeyService.Create(c.Request.Context(), userID, service.CreateAPIKeyRequest{
		Name: req.Name, GroupID: req.GroupID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Cumora expects data.id and data.key, not the masked management DTO or
	// APIKeyCredentialIssue. Never put this response in the idempotency store:
	// the secret is returned only by the request that created it.
	response.Success(c, struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}{ID: key.ID, Key: key.Key})
}

// AdminUpdateAPIKeyGroupRequest represents the request to update an API key.
type AdminUpdateAPIKeyGroupRequest struct {
	GroupID             *int64 `json:"group_id"`               // nil=不修改, 0=解绑, >0=绑定到目标分组
	ResetRateLimitUsage *bool  `json:"reset_rate_limit_usage"` // true=重置 5h/1d/7d 限速用量
}

// UpdateGroup handles updating an API key's admin-managed fields.
// PUT /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) UpdateGroup(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	var resetKey *service.APIKey
	if req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage {
		resetKey, err = h.adminService.AdminResetAPIKeyRateLimitUsage(c.Request.Context(), keyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	result, err := h.adminService.AdminUpdateAPIKeyGroupID(c.Request.Context(), keyID, req.GroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if resetKey != nil && req.GroupID == nil {
		result.APIKey = resetKey
	}

	resp := struct {
		APIKey                 *dto.APIKey `json:"api_key"`
		AutoGrantedGroupAccess bool        `json:"auto_granted_group_access"`
		GrantedGroupID         *int64      `json:"granted_group_id,omitempty"`
		GrantedGroupName       string      `json:"granted_group_name,omitempty"`
	}{
		APIKey:                 dto.APIKeyFromService(result.APIKey),
		AutoGrantedGroupAccess: result.AutoGrantedGroupAccess,
		GrantedGroupID:         result.GrantedGroupID,
		GrantedGroupName:       result.GrantedGroupName,
	}
	response.Success(c, resp)
}

// AdminSetAPIKeyStatusRequest enables or disables a user's key.
type AdminSetAPIKeyStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active disabled"`
}

// SetStatus enables or disables any user's API key.
// PUT /api/v1/admin/api-keys/:id/status
func (h *AdminAPIKeyHandler) SetStatus(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	var req AdminSetAPIKeyStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	key, err := h.adminService.AdminSetAPIKeyStatus(c.Request.Context(), keyID, req.Status)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.APIKeyFromService(key))
}

// Delete removes any user's API key and revokes its credentials.
// DELETE /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) Delete(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	if err := h.adminService.AdminDeleteAPIKey(c.Request.Context(), keyID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "API key deleted successfully"})
}

// AdminRotateAPIKeyCredentialRequest controls an admin credential rotation.
type AdminRotateAPIKeyCredentialRequest struct {
	GraceTTLHours  *int  `json:"grace_ttl_hours"`
	RevokePrevious *bool `json:"revoke_previous"`
}

// RotateCredential rotates any user's key secret in place, keeping its identity.
// POST /api/v1/admin/api-keys/:id/credentials/rotate
func (h *AdminAPIKeyHandler) RotateCredential(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	var req AdminRotateAPIKeyCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.GraceTTLHours != nil && *req.GraceTTLHours < 0 {
		response.BadRequest(c, "Invalid request: grace_ttl_hours must be non-negative")
		return
	}

	var graceTTL time.Duration
	if req.GraceTTLHours != nil {
		graceTTL = time.Duration(*req.GraceTTLHours) * time.Hour
	}
	revokePrevious := req.RevokePrevious != nil && *req.RevokePrevious

	issue, err := h.adminService.AdminRotateAPIKeyCredential(c.Request.Context(), keyID, graceTTL, revokePrevious)
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

// AdminUpdateAPIKeyBudgetRequest 是管理端预算写请求。
//
// 两个字段都可选，但至少要提供一个：
//   - Budget 整体替换周期预算策略，包含 budget_enforced 开关；
//   - Adjustment 记录一次带原因的已消费金额调整（唯一的调账方式）。
type AdminUpdateAPIKeyBudgetRequest struct {
	Budget     *service.APIKeyBudgetPolicy  `json:"budget"`
	Adjustment *AdminAPIKeyBudgetAdjustment `json:"adjustment"`
}

// AdminAPIKeyBudgetAdjustment 是一次管理端调账。
type AdminAPIKeyBudgetAdjustment struct {
	// DeltaUSD 为增量金额：正数增加已消费，负数退回已消费。不能为 0。
	DeltaUSD float64 `json:"delta_usd"`
	// Reason 必须非空，会随审计日志一起留痕。
	Reason string `json:"reason"`
}

// AdminUpdateAPIKeyBudget 写入周期预算策略与/或调账。
// PUT /api/v1/admin/api-keys/:id/budget
//
// 鉴权与审计与其它管理端写操作同源（admin 路由组上的 adminAuth + auditLog），
// 因此这里不再自建任何额外校验/审批环节。
func (h *AdminAPIKeyHandler) AdminUpdateAPIKeyBudget(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.Budget == nil && req.Adjustment == nil {
		response.BadRequest(c, "budget or adjustment is required")
		return
	}

	resp := struct {
		Policy *service.APIKeyBudgetPolicy `json:"policy,omitempty"`
		State  *service.APIKeyBudgetState  `json:"state,omitempty"`
	}{}

	if req.Budget != nil {
		saved, err := h.adminService.AdminSetAPIKeyBudgetPolicy(c.Request.Context(), keyID, *req.Budget)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		resp.Policy = &saved
	}

	if req.Adjustment != nil {
		actorID := int64(0)
		if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
			actorID = subject.UserID
		}
		state, err := h.adminService.AdminAdjustAPIKeyBudgetUsage(c.Request.Context(), &service.KeyBudgetAdjustmentCommand{
			APIKeyID:    keyID,
			ActorUserID: actorID,
			DeltaUSD:    req.Adjustment.DeltaUSD,
			Reason:      req.Adjustment.Reason,
		})
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		resp.State = state
	}

	response.Success(c, resp)
}
