package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

func registerModelAccessAdminRoutes(parent *gin.RouterGroup, handlers *handler.Handlers, omitList ...bool) {
	if handlers.ModelAccess == nil {
		return
	}
	access := parent.Group("/model-access-groups")
	if len(omitList) == 0 {
		access.GET("", handlers.ModelAccess.List)
	}
	access.POST("", handlers.ModelAccess.Save)
	access.PUT("/:id", handlers.ModelAccess.Save)
	access.DELETE("/:id", handlers.ModelAccess.Delete)
	parent.GET("/accounts/:id/model-access", handlers.ModelAccess.Rule("accounts", false, false))
	parent.POST("/accounts/:id/model-access/preview", handlers.ModelAccess.Preview("accounts", false))
	parent.PUT("/accounts/:id/model-access", handlers.ModelAccess.Rule("accounts", false, true))
	parent.GET("/api-keys/:id/model-access", handlers.ModelAccess.Rule("keys", false, false))
	parent.POST("/api-keys/:id/model-access/preview", handlers.ModelAccess.Preview("keys", false))
	parent.PUT("/api-keys/:id/model-access", handlers.ModelAccess.Rule("keys", false, true))
}
func registerModelAccessUserRoutes(parent *gin.RouterGroup, handlers *handler.Handlers) {
	if handlers.ModelAccess == nil {
		return
	}
	scoped := parent.Group("", middleware.DefaultWorkspaceScope())
	scoped.GET("/model-access-groups", handlers.ModelAccess.List)
	scoped.GET("/keys/:id/model-access", handlers.ModelAccess.Rule("keys", true, false))
	scoped.POST("/keys/:id/model-access/preview", handlers.ModelAccess.Preview("keys", true))
	scoped.PUT("/keys/:id/model-access", handlers.ModelAccess.Rule("keys", true, true))
}
