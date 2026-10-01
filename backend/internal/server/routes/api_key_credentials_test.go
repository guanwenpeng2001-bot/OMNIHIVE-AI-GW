package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the production registration functions: manually registering handlers
// here would hide missing routes in the running application.
func TestAPIKeyCredentialProductionRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	deny := func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }
	RegisterUserRoutes(router.Group("/api/v1"), &handler.Handlers{},
		middleware.JWTAuthMiddleware(deny), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	admin := router.Group("/api/v1/admin", deny)
	registerAdminAPIKeyRoutes(admin, &handler.Handlers{Admin: &handler.AdminHandlers{}})
	RegisterWorkspaceRoutes(router.Group("/api/v1"), &handler.Handlers{
		Workspace: &handler.WorkspaceHandler{}, Admin: &handler.AdminHandlers{},
	}, middleware.JWTAuthMiddleware(deny), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.StepUpAuthMiddleware(deny), nil, nil)

	for _, path := range []string{
		"/api/v1/keys/123/credentials/rotate",
		"/api/v1/keys/123/credentials/revoke",
		"/api/v1/admin/api-keys/123/credentials/rotate",
		"/api/v1/workspaces/1/keys/123/credentials/rotate",
		"/api/v1/workspaces/1/keys/123/credentials/revoke",
		"/api/v1/workspaces/1/api-keys/123/credentials/rotate",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
			require.Equal(t, http.StatusUnauthorized, response.Code)
		})
	}
}
