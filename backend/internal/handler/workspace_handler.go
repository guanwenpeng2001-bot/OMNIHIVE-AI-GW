package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type WorkspaceHandler struct{ Service *service.WorkspaceService }

func NewWorkspaceHandler(workspaces *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{Service: workspaces}
}

func (h *WorkspaceHandler) List(ctx *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(ctx)
	role, _ := middleware.GetUserRoleFromContext(ctx)
	workspaces, err := h.Service.List(ctx.Request.Context(), subject.UserID, role == service.RoleAdmin)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, workspaces)
}

func (h *WorkspaceHandler) Create(ctx *gin.Context) {
	role, _ := middleware.GetUserRoleFromContext(ctx)
	if role != service.RoleAdmin {
		response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
		return
	}
	var input struct {
		Name string `json:"name" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		response.BadRequest(ctx, "Invalid workspace")
		return
	}
	workspace, err := h.Service.Create(ctx.Request.Context(), input.Name, role == service.RoleAdmin)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, workspace)
}

func (h *WorkspaceHandler) Members(ctx *gin.Context) {
	workspaceID, _ := strconv.ParseInt(ctx.Param("workspace_id"), 10, 64)
	members, err := h.Service.Members(ctx.Request.Context(), workspaceID)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, members)
}

func (h *WorkspaceHandler) SetMember(ctx *gin.Context) {
	workspaceID, _ := strconv.ParseInt(ctx.Param("workspace_id"), 10, 64)
	userID, err := strconv.ParseInt(ctx.Param("user_id"), 10, 64)
	if err != nil {
		response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
		return
	}
	var input struct {
		Role string `json:"role" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		response.BadRequest(ctx, "Invalid membership")
		return
	}
	if err := h.Service.SetMember(ctx.Request.Context(), workspaceID, userID, input.Role); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"user_id": userID, "role": input.Role})
}

func (h *WorkspaceHandler) RemoveMember(ctx *gin.Context) {
	workspaceID, _ := strconv.ParseInt(ctx.Param("workspace_id"), 10, 64)
	userID, err := strconv.ParseInt(ctx.Param("user_id"), 10, 64)
	if err != nil {
		response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
		return
	}
	if err := h.Service.RemoveMember(ctx.Request.Context(), workspaceID, userID); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"deleted": true})
}
