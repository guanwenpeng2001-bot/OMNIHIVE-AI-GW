package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
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
