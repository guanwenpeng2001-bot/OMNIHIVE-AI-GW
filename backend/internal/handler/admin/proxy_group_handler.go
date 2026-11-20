package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type ProxyGroupHandler struct {
	groups       *service.ProxyGroupAdminService
	adminService service.AdminService
}

func NewProxyGroupHandler(groups *service.ProxyGroupAdminService, adminService service.AdminService) *ProxyGroupHandler {
	return &ProxyGroupHandler{groups: groups, adminService: adminService}
}

type ProxyGroupRequest struct {
	Name              *string                `json:"name"`
	Description       *string                `json:"description"`
	Region            *string                `json:"region"`
	TargetServices    *[]string              `json:"target_services"`
	Status            *string                `json:"status"`
	FallbackMode      *string                `json:"fallback_mode"`
	FallbackProxyID   dto.NullableInt64Field `json:"fallback_proxy_id"`
	BindingTTLSeconds *int                   `json:"binding_ttl_seconds"`
}

func (r ProxyGroupRequest) input() service.ProxyGroupAdminInput {
	return service.ProxyGroupAdminInput{Name: r.Name, Description: r.Description, Region: r.Region,
		TargetServices: r.TargetServices, Status: r.Status, FallbackMode: r.FallbackMode,
		FallbackProxyID: service.NullableInt64Update{Set: r.FallbackProxyID.Set, Value: r.FallbackProxyID.Value}, BindingTTLSeconds: r.BindingTTLSeconds}
}

func proxyGroupRequestID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid resource ID")
		return 0, false
	}
	return id, true
}

func (h *ProxyGroupHandler) List(c *gin.Context) {
	page, size := response.ParsePagination(c)
	views, result, err := h.groups.List(c.Request.Context(), pagination.PaginationParams{Page: page, PageSize: size}, c.Query("status"), c.Query("target_service"), c.Query("search"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]dto.ProxyGroup, 0, len(views))
	for i := range views {
		items = append(items, dto.ProxyGroupFromService(&views[i]))
	}
	response.Paginated(c, items, result.Total, page, size)
}

func (h *ProxyGroupHandler) GetByID(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	view, err := h.groups.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyGroupDetailFromService(view))
}

func (h *ProxyGroupHandler) Create(c *gin.Context) {
	var req ProxyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid proxy group request")
		return
	}
	view, err := h.groups.Create(c.Request.Context(), req.input())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyGroupDetailFromService(view))
}

func (h *ProxyGroupHandler) Update(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	var req ProxyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid proxy group request")
		return
	}
	view, err := h.groups.Update(c.Request.Context(), id, req.input())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyGroupDetailFromService(view))
}

func (h *ProxyGroupHandler) Delete(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	if err := h.groups.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy group deleted"})
}

func (h *ProxyGroupHandler) ReplaceMembers(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	var req struct {
		ProxyIDs *[]int64 `json:"proxy_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ProxyIDs == nil {
		response.BadRequest(c, "proxy_ids must be an array")
		return
	}
	view, err := h.groups.ReplaceMembers(c.Request.Context(), id, *req.ProxyIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyGroupDetailFromService(view))
}

func (h *ProxyGroupHandler) Bindings(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	items, err := h.groups.Bindings(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *ProxyGroupHandler) ResetBindings(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	cleared, err := h.groups.ResetBindings(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"cleared": cleared})
}

func (h *ProxyGroupHandler) ProbeNow(c *gin.Context) {
	id, ok := proxyGroupRequestID(c)
	if !ok {
		return
	}
	result, err := h.adminService.CheckProxyQuality(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result == nil {
		response.InternalError(c, "Proxy probe returned no result")
		return
	}
	reachable := false
	for _, item := range result.Items {
		// A real HTTP response also proves proxy reachability, even if the target
		// rejects this account/IP. A failed generic IP echo cannot override that.
		if (item.Target == "base_connectivity" && item.Status == "pass") || (item.HTTPStatus >= 100 && item.HTTPStatus <= 599) {
			reachable = true
		}
	}
	if reachable {
		if err := h.groups.ClearCooldown(c.Request.Context(), id); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	// Raw probe messages can contain proxy URLs. Return only safe observations.
	response.Success(c, gin.H{"proxy_id": id, "reachable": reachable, "cooldown_cleared": reachable,
		"score": result.Score, "grade": result.Grade, "checked_at": result.CheckedAt,
		"passed_count": result.PassedCount, "failed_count": result.FailedCount, "warn_count": result.WarnCount, "challenge_count": result.ChallengeCount})
}
