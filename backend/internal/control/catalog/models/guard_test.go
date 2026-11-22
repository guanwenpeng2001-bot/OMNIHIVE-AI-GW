package models

import (
	"context"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type w05PlazaSettings struct{ enabled, auth bool }

func (s *w05PlazaSettings) GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime {
	return service.ModelPlazaRuntime{Enabled: s.enabled, RequireAuth: s.auth}
}
func TestW05PublicCatalogSettingsGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &w05PlazaSettings{enabled: true}
	h := New(&fixtureCatalog{revision: "guard"}, nil, &config.Config{})
	h.settings = settings
	r := gin.New()
	strict := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer valid-session" {
			c.AbortWithStatus(401)
			return
		}
		c.Next()
	}
	Register(r.Group("/api/v2"), h, nil, []gin.HandlerFunc{strict})
	for _, path := range []string{"", "/openai/sample-a", "/compare"} {
		method := "GET"
		if path == "/compare" {
			method = "POST"
		}
		request := func(token string) int {
			req := httptest.NewRequest(method, "/api/v2/public/models"+path, strings.NewReader(`{}`))
			if token != "" {
				req.Header.Set("Authorization", token)
			}
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			return res.Code
		}
		settings.enabled = false
		require.Equal(t, 404, request("Bearer valid-session"))
		settings.enabled = true
		settings.auth = true
		for _, token := range []string{"", "Bearer expired", "Basic valid-session"} {
			require.Equal(t, 401, request(token))
		}
		// The endpoint's own validation may reject the intentionally empty compare;
		// authentication must have passed before that validation runs.
		require.NotEqual(t, 401, request("Bearer valid-session"))
		settings.auth = false
		require.NotEqual(t, 401, request(""))
		h.settings = nil
		require.NotEqual(t, http.StatusUnauthorized, request(""))
		h.settings = settings
	}
}

func TestW05PublicCatalogDatabaseSettingsDefaults(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	settings := plazaDBSettings{db: db}
	mock.ExpectQuery("SELECT key,value FROM settings").WillReturnRows(sqlmock.NewRows([]string{"key", "value"}))
	require.Equal(t, service.ModelPlazaRuntime{Enabled: true}, settings.GetModelPlazaRuntime(context.Background()))
	mock.ExpectQuery("SELECT key,value FROM settings").WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).AddRow("model_plaza_enabled", "false").AddRow("model_plaza_require_auth", "true"))
	require.Equal(t, service.ModelPlazaRuntime{RequireAuth: true}, settings.GetModelPlazaRuntime(context.Background()))
	mock.ExpectQuery("SELECT key,value FROM settings").WillReturnError(fmt.Errorf("unavailable"))
	require.False(t, settings.GetModelPlazaRuntime(context.Background()).Enabled)
	require.NoError(t, mock.ExpectationsWereMet())
}
