//go:build embed

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedFrontendPreservesRegisteredRoutes(t *testing.T) {
	server, err := NewFrontendServer(&mockSettingsProvider{settings: map[string]any{}})
	require.NoError(t, err)
	legacy, err := ServeEmbeddedFrontend()
	require.NoError(t, err)
	for name, frontend := range map[string]gin.HandlerFunc{"settings": server.Middleware(), "legacy": legacy} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.Use(frontend)
			for _, path := range []string{"/usage", "/usage/", "/v1/usage", "/antigravity/v1/usage"} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
					router.Handle(method, path, func(ctx *gin.Context) {
						ctx.JSON(http.StatusUnauthorized, gin.H{"error": "API authentication required"})
					})
				}
			}
			for _, path := range []string{"/usage", "/usage/", "/usage/?range=week"} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
					require.Equal(t, http.StatusOK, response.Code, method+path)
					require.Contains(t, response.Header().Get("Content-Type"), "text/html")
				}
			}
			for _, route := range []struct{ method, path string }{
				{http.MethodPost, "/usage"}, {http.MethodGet, "/v1/usage"},
				{http.MethodGet, "/antigravity/v1/usage"}, {http.MethodHead, "/v1/usage"},
			} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
				require.Equal(t, http.StatusUnauthorized, response.Code)
				require.Contains(t, response.Header().Get("Content-Type"), "application/json")
			}
			for _, path := range []string{"/messages", "/chat/completions", "/audio/speech", "/embeddings", "/custom-api"} {
				router.POST(path, func(ctx *gin.Context) { ctx.JSON(http.StatusOK, gin.H{"registered": true}) })
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
				require.Equal(t, http.StatusOK, response.Code)
				require.JSONEq(t, `{"registered":true}`, response.Body.String())
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/login", nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Contains(t, response.Header().Get("Content-Type"), "text/html")
		})
	}
}
