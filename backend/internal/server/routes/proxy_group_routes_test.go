package routes

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProxyGroupRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{ProxyGroup: admin.NewProxyGroupHandler(nil, nil), Proxy: admin.NewProxyHandler(nil)}}
	root := router.Group("/api/v1/admin")
	registerProxyGroupRoutes(root, h)
	registerProxyRoutes(root, h, func(c *gin.Context) { c.Next() })
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/admin/proxy-groups", "POST /api/v1/admin/proxy-groups",
		"GET /api/v1/admin/proxy-groups/:id", "PUT /api/v1/admin/proxy-groups/:id", "DELETE /api/v1/admin/proxy-groups/:id",
		"PUT /api/v1/admin/proxy-groups/:id/members", "GET /api/v1/admin/proxy-groups/:id/bindings", "DELETE /api/v1/admin/proxy-groups/:id/bindings",
		"POST /api/v1/admin/proxies/:id/probe-now", "POST /api/v1/admin/proxies/:id/quality-check",
	} {
		require.True(t, routes[route], route)
	}
}

func TestWorkspaceProxyGroupRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Workspace: &handler.WorkspaceHandler{}, Admin: &handler.AdminHandlers{ProxyGroup: admin.NewProxyGroupHandler(nil, nil)}}
	pass := func(c *gin.Context) { c.Next() }
	RegisterWorkspaceRoutes(router.Group("/api/v1"), h, middleware.JWTAuthMiddleware(pass), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), nil, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, suffix := range []string{"", "/:id", "/:id/members", "/:id/bindings"} {
		found := false
		for route := range routes {
			if strings.HasSuffix(route, " /api/v1/workspaces/:workspace_id/proxy-groups"+suffix) {
				found = true
			}
		}
		require.True(t, found, suffix)
	}
}
