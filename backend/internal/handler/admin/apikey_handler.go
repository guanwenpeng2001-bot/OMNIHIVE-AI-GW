package admin

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService service.AdminService
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService: adminService,
	}
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
