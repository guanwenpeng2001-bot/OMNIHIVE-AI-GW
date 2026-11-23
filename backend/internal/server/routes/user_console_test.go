package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
)

func TestUserConsoleRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(domain.WithDefaultWorkspaceID(c.Request.Context(), 1))
	})
	v1 := router.Group("/api/v1")
	h := &handler.Handlers{}
	jwt := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer test-session" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), "user")
		c.Next()
	}
	RegisterUserRoutes(v1, h, jwt, func(c *gin.Context) { c.Next() }, nil, nil)
	registered := h.MCP
	require.NotNil(t, registered)
	RegisterModelPlazaRoutes(v1, h, middleware.NewOptionalJWTAuthMiddleware(nil, nil, nil, nil), nil, nil)
	require.Same(t, registered, h.MCP)

	request := func(method, path, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/user/onboarding", http.StatusOK},
		{http.MethodGet, "/user/audit-logs", http.StatusOK},
		{http.MethodGet, "/user/mcp/grants", http.StatusOK},
		{http.MethodGet, "/user/routing-policies", http.StatusOK},
		{http.MethodPost, "/user/mcp/authorize", http.StatusBadRequest},
		{http.MethodPost, "/user/mcp/grants", http.StatusBadRequest},
		{http.MethodPut, "/user/onboarding", http.StatusBadRequest},
		{http.MethodPut, "/user/routing-policies", http.StatusBadRequest},
		{http.MethodDelete, "/user/mcp/grants/missing", http.StatusNotFound},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			require.Equal(t, http.StatusUnauthorized, request(tc.method, tc.path, "").Code)
			w := request(tc.method, tc.path, "Bearer test-session")
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.method == http.MethodDelete {
				require.Contains(t, w.Body.String(), "Grant not found")
			}
		})
	}
	for _, path := range []string{"/public/rankings", "/model-plaza/rankings"} {
		t.Run(path, func(t *testing.T) {
			w := request(http.MethodGet, path, "")
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			require.Contains(t, w.Body.String(), "Public model rankings are unavailable")
			require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, path, "invalid").Code)
		})
	}
}
