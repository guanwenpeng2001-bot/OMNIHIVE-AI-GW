package routes

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// RegisterWorkspaceRoutes provides a separate delegation boundary. Existing
// /admin routes retain their original global administrator middleware.
func RegisterWorkspaceRoutes(parent *gin.RouterGroup, handlers *handler.Handlers, jwt middleware.JWTAuthMiddleware,
	audit middleware.AuditLogMiddleware, stepUp middleware.StepUpAuthMiddleware, settings *service.SettingService,
	limiter *middleware.PanelRateLimiter) {
	if handlers.Workspace == nil {
		return
	}
	workspaces := parent.Group("/workspaces", gin.HandlerFunc(jwt), limiter.Global(), gin.HandlerFunc(audit))
	workspaces.GET("", handlers.Workspace.List)
	workspaces.POST("", handlers.Workspace.Create)
	members := workspaces.Group("/:workspace_id", middleware.WorkspaceScope(handlers.Workspace.Service, true))
	registerModelAccessAdminRoutes(members, handlers, true)
	members.GET("/members", handlers.Workspace.Members)
	members.PUT("/members/:user_id", handlers.Workspace.SetMember)
	members.DELETE("/members/:user_id", handlers.Workspace.RemoveMember)
	resources := members.Group("", middleware.WorkspaceResources(handlers.Workspace.Service), workspaceResourceOnly())
	registerAccountRoutes(resources, handlers, stepUp)
	registerGroupRoutes(resources, handlers)
	registerProxyRoutes(resources, handlers, stepUp)
	registerChannelRoutes(resources, handlers)
	registerChannelMonitorRoutes(resources, handlers, settings)
	monitor := resources.Group("/channel-monitor-v2", channelMonitorModeV2Guard(settings))
	monitor.GET("/dimensions", handlers.ChannelMonitorV2.Dimensions)
	monitor.GET("/snapshot", handlers.ChannelMonitorV2.AdminSnapshot)
	monitor.GET("/models", handlers.ChannelMonitorV2.AdminModels)
	monitor.GET("/matrix", handlers.ChannelMonitorV2.AdminMatrix)
	monitor.GET("/errors", handlers.ChannelMonitorV2.Errors)
	monitor.GET("/users", handlers.ChannelMonitorV2.AdminUsers)
	registerScheduledTestRoutes(resources, handlers)
	registerAdminAPIKeyRoutes(resources, handlers)
	registerOpenAIOAuthRoutes(resources, handlers)
	registerGeminiOAuthRoutes(resources, handlers)
	registerAntigravityOAuthRoutes(resources, handlers)
	registerGrokOAuthRoutes(resources, handlers)
	registerCNProviderRoutes(resources, handlers)
	keys := workspaces.Group("/:workspace_id/keys", middleware.WorkspaceScope(handlers.Workspace.Service, false), middleware.BackendModeUserGuard(settings), middleware.WorkspaceResources(handlers.Workspace.Service))
	keys.GET("", handlers.APIKey.List)
	keys.POST("", handlers.APIKey.Create)
	keys.GET("/:id", handlers.APIKey.GetByID)
	keys.PUT("/:id", handlers.APIKey.Update)
	keys.DELETE("/:id", handlers.APIKey.Delete)
	keys.POST("/:id/credentials/rotate", handlers.APIKey.RotateCredential)
	keys.POST("/:id/credentials/revoke", handlers.APIKey.RevokeCredential)
	access := workspaces.Group("/:workspace_id", middleware.WorkspaceScope(handlers.Workspace.Service, false))
	if handlers.ModelAccess != nil {
		access.GET("/model-access-groups", handlers.ModelAccess.List)
		keys.GET("/:id/model-access", handlers.ModelAccess.Rule("keys", true, false))
		keys.POST("/:id/model-access/preview", handlers.ModelAccess.Preview("keys", true))
		keys.PUT("/:id/model-access", handlers.ModelAccess.Rule("keys", true, true))
	}
	available := workspaces.Group("/:workspace_id/groups", middleware.WorkspaceScope(handlers.Workspace.Service, false))
	available.GET("/available", handlers.APIKey.GetAvailableGroups)
	available.GET("/rates", handlers.APIKey.GetUserGroupRates)
}

func workspaceResourceOnly() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		path := ctx.FullPath()
		// These settings configure the entire deployment rather than a resource.
		if strings.HasSuffix(path, "/model-registry/reload") || strings.Contains(path, "/upstream-billing-probe/settings") ||
			strings.Contains(path, "/ollama-cloud-usage/settings") {
			response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}
