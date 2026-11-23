package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// DefaultWorkspaceResolution attaches a lazy request-local resolver without applying a resource filter.
func DefaultWorkspaceResolution(workspaces *service.WorkspaceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(domain.WithDefaultWorkspaceResolver(c.Request.Context(), workspaces.DefaultWorkspaceID))
		c.Next()
	}
}

// DefaultWorkspaceScope keeps legacy resource URLs in the original domain.
func DefaultWorkspaceScope() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		subject, _ := GetAuthSubjectFromContext(ctx)
		id := domain.ResolvedDefaultWorkspaceID(ctx.Request.Context())
		if id <= 0 {
			response.ErrorFrom(ctx, service.ErrDefaultWorkspaceUnavailable)
			ctx.Abort()
			return
		}
		ctx.Request = ctx.Request.WithContext(domain.WithDefaultWorkspaceID(ctx.Request.Context(), id))
		installWorkspace(ctx, id, subject.UserID)
		ctx.Next()
	}
}

// WorkspaceScope authorizes every request before installing a resource boundary.
// It never changes the global role used by the existing admin route guard.
func WorkspaceScope(workspaces *service.WorkspaceService, requireAdmin bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		subject, authenticated := GetAuthSubjectFromContext(ctx)
		role, _ := GetUserRoleFromContext(ctx)
		workspaceID, err := strconv.ParseInt(ctx.Param("workspace_id"), 10, 64)
		if !authenticated || err != nil {
			response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
			ctx.Abort()
			return
		}
		if err := workspaces.Authorize(ctx.Request.Context(), workspaceID, subject.UserID, role == service.RoleAdmin, requireAdmin); err != nil {
			response.ErrorFrom(ctx, err)
			ctx.Abort()
			return
		}
		installWorkspace(ctx, workspaceID, subject.UserID)
		if requireAdmin {
			ctx.Request = ctx.Request.WithContext(domain.WithWorkspaceAdministration(ctx.Request.Context()))
		}
		ctx.Next()
	}
}

func installWorkspace(ctx *gin.Context, workspace, actor int64) {
	base := domain.WithWorkspaceScope(ctx.Request.Context(), workspace)
	ctx.Request = ctx.Request.WithContext(scope.With(base, scope.Authorized{WorkspaceID: workspace, ActorID: actor}))
}
