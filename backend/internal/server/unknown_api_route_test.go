//go:build unit

package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	paymentshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/payments"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/web"
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
	// Billable gateway routes fail closed without an execution journal store.
	// Production always wires a database; an expectation-free sqlmock keeps
	// the store present so authentication is what aborts these requests.
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	router := server.SetupRouter(gin.New(), &handler.Handlers{Admin: &handler.AdminHandlers{}},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, paymentshttp.Handler{}, nil, nil, nil, nil, nil, nil,
		middleware.JWTAuthMiddleware(auth), middleware.OptionalJWTAuthMiddleware(pass),
		middleware.AdminAuthMiddleware(pass), middleware.APIKeyAuthMiddleware(auth),
		middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass),
		nil, nil, nil, settings, nil, cfg, nil, db)
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, path := range []string{"/api/v2/public/models", "/api/v2/workspaces/:workspace_id/projects/:project_id/models"} {
		require.Contains(t, registered, "GET "+path)
		require.Contains(t, registered, "GET "+path+"/*id")
		require.Contains(t, registered, "POST "+path+"/compare")
	}
	const taskPath = "/api/v2/workspaces/:workspace_id/projects/:project_id/tasks"
	require.Contains(t, registered, "GET "+taskPath)
	require.Contains(t, registered, "POST "+taskPath)
	require.Contains(t, registered, "POST "+taskPath+"/:id/retry")
	const projectPath = "/api/v2/workspaces/:workspace_id/projects/:project_id"
	for _, route := range []string{
		"POST " + projectPath + "/quotes",
		"POST " + projectPath + "/playground/runs",
		"GET " + projectPath + "/playground/runs/by-idempotency-key",
		"GET " + projectPath + "/playground/runs/:id",
		"POST " + projectPath + "/playground/runs/:id/cancel",
		"POST " + projectPath + "/requests/:id/replay",
		"GET " + projectPath + "/billing/adjustments/:id/payment-evidence",
		"PUT " + projectPath + "/billing/adjustments/:id/payment-evidence/:event_id",
	} {
		require.Contains(t, registered, route)
	}
	require.Contains(t, registered, "GET /api/v2/admin/extensions")
	require.Contains(t, registered, "GET /api/v2/admin/catalog/models")
	require.Contains(t, registered, "GET /api/v2/admin/catalog/models/:id")
	require.Contains(t, registered, "GET /api/v1/admin/config/effective")
	require.Contains(t, registered, "POST /api/v1/admin/config/reset")
	extension := httptest.NewRecorder()
	router.ServeHTTP(extension, httptest.NewRequest(http.MethodGet, "/api/v2/admin/extensions", nil))
	require.Equal(t, http.StatusUnauthorized, extension.Code, extension.Body.String())
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
		{"GET", "/v1/usage"}, {"GET", "/antigravity/v1/usage"},
		{"POST", "/api/v2/workspaces/1/projects/1/playground/runs"},
		{"POST", "/api/v2/workspaces/1/projects/1/requests/request-id/replay"},
		{"GET", "/api/v2/workspaces/1/projects/1/billing/adjustments/00000000-0000-0000-0000-000000000001/payment-evidence"},
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
	if web.HasEmbeddedFrontend() {
		t.Run("embedded panel document reads", func(t *testing.T) {
			for _, path := range []string{"/usage", "/usage/", "/usage/?range=week", "/admin/usage", "/dashboard", "/workspaces", "/setup", "/model-plaza"} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					request, err := http.NewRequest(method, httpServer.URL+path, nil)
					require.NoError(t, err)
					request.Header.Set("Accept", "text/html")
					response, err := httpServer.Client().Do(request)
					require.NoError(t, err)
					_ = response.Body.Close()
					require.Equal(t, http.StatusOK, response.StatusCode, method+path)
					require.Contains(t, response.Header.Get("Content-Type"), "text/html", method+path)
				}
			}
		})
	}
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
