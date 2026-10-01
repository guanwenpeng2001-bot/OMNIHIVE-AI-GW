//go:build unit

package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProductionRouterUnknownAPIRequestsNeverUseSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	settings := service.NewSettingService(newStubSettingRepo(), cfg)
	pass := func(c *gin.Context) { c.Next() }
	auth := func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "test authentication required"})
	}
	router := server.SetupRouter(gin.New(), &handler.Handlers{Admin: &handler.AdminHandlers{}},
		middleware.JWTAuthMiddleware(pass), middleware.OptionalJWTAuthMiddleware(pass),
		middleware.AdminAuthMiddleware(pass), middleware.APIKeyAuthMiddleware(auth),
		middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass),
		nil, nil, nil, settings, nil, cfg, nil)
	router.NoRoute(func(c *gin.Context) { c.Data(200, "text/html", []byte("<html>frontend</html>")) })
	for _, test := range []struct{ method, path string }{
		{"POST", "/v1"}, {"POST", "/v1/"}, {"POST", "/"},
		{"POST", "/v1/messages/v1/messages"}, {"GET", "/v1/unknown"},
		{"POST", "/audio/unknown"}, {"GET", "/v1beta/unknown"},
		{"POST", "/antigravity/v1"},
		{"GET", "/compatible-mode/v1/unknown"}, {"GET", "/api/unknown"},
		{"GET", "/tts/unknown"}, {"GET", "/alpha/search/unknown"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if test.path == "/v1/" {
				// Gin canonicalizes this path before executing any middleware.
				// Preserve the method through its 307 and check the final response.
				require.Equal(t, http.StatusTemporaryRedirect, response.Code)
				require.Equal(t, "/v1", response.Header().Get("Location"))
				response = httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(test.method, "/v1", nil))
			}
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Contains(t, response.Header().Get("Content-Type"), "application/json")
			require.NotContains(t, response.Body.String(), "<html>")
		})
	}
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, 200, page.Code)
	require.Contains(t, page.Header().Get("Content-Type"), "text/html")

	// Authentication aborts before any nil business handler. These aliases are
	// registered by SetupRouter, and must not be swallowed by the embedded SPA.
	for _, route := range []struct{ method, path string }{
		{"POST", "/messages"}, {"POST", "/chat/completions"}, {"POST", "/responses"},
		{"GET", "/models"}, {"POST", "/images/generations"}, {"POST", "/embeddings"},
		{"POST", "/audio/speech"}, {"POST", "/v1/messages"},
		{"GET", "/backend-api/codex/unknown"},
	} {
		t.Run("registered "+route.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
			require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
			require.Contains(t, response.Header().Get("Content-Type"), "application/json")
		})
	}
	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest("POST", "/v1/messages/", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusTemporaryRedirect, redirect.Code)
	require.Equal(t, "/v1/messages", redirect.Header().Get("Location"))

	// Exercise the real net/http redirect policy as an SDK would, including
	// replayable POST bodies and query preservation on both invalid and valid URLs.
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/v1/?check=1", http.StatusNotFound},
		{"/v1/messages/?check=1", http.StatusUnauthorized},
	} {
		request, err := http.NewRequest(http.MethodPost, httpServer.URL+test.path, strings.NewReader(`{}`))
		require.NoError(t, err)
		response, err := httpServer.Client().Do(request)
		require.NoError(t, err)
		_ = response.Body.Close()
		require.Equal(t, test.status, response.StatusCode)
		require.Equal(t, http.MethodPost, response.Request.Method)
		require.Equal(t, "check=1", response.Request.URL.RawQuery)
		require.Contains(t, response.Header.Get("Content-Type"), "application/json")
	}
}
