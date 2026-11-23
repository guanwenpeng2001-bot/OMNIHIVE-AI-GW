package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type w05PlazaSettings struct{ enabled, auth bool }

func (s *w05PlazaSettings) GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime {
	return service.ModelPlazaRuntime{Enabled: s.enabled, RequireAuth: s.auth}
}

type authCheckingCatalog struct {
	base     *fixtureCatalog
	settings *w05PlazaSettings
}

func (a authCheckingCatalog) Snapshot(ctx context.Context, p catalog.Perspective, at time.Time) (catalog.Snapshot, error) {
	if p.Public() && a.settings.auth && !catalog.PublicViewerAuthenticated(ctx) {
		return catalog.Snapshot{}, fault.Unauthenticated
	}
	return a.base.Snapshot(ctx, p, at)
}

func TestW05PublicCatalogSettingsGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &w05PlazaSettings{enabled: true}
	h := New(authCheckingCatalog{base: &fixtureCatalog{revision: "guard"}, settings: settings}, nil, &config.Config{})
	h.settings = settings
	r := gin.New()
	r.Use(fixtureDefault)
	strict := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer valid-session" {
			c.AbortWithStatus(401)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
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

type plazaSettingsRepository struct {
	service.SettingRepository
	values map[string]string
	err    error
}

func (r *plazaSettingsRepository) GetMultiple(context.Context, []string) (map[string]string, error) {
	return r.values, r.err
}

func TestW05PublicCatalogDatabaseSettingsDefaults(t *testing.T) {
	ctx := t.Context()
	repo := &plazaSettingsRepository{}
	settings := service.NewSettingService(repo, nil)
	require.Equal(t, service.ModelPlazaRuntime{}, settings.GetModelPlazaRuntime(ctx))
	repo.values = map[string]string{
		service.SettingKeyModelPlazaEnabled:     "false",
		service.SettingKeyModelPlazaRequireAuth: "true",
	}
	require.Equal(t, service.ModelPlazaRuntime{RequireAuth: true}, settings.GetModelPlazaRuntime(ctx))
	repo.values[service.SettingKeyModelPlazaEnabled] = "true"
	repo.values[service.SettingKeyModelPlazaRequireAuth] = "invalid"
	require.Equal(t, service.ModelPlazaRuntime{}, settings.GetModelPlazaRuntime(ctx))
	repo.values[service.SettingKeyModelPlazaRequireAuth] = "false"
	repo.err = errors.New("unavailable")
	require.False(t, settings.GetModelPlazaRuntime(ctx).Enabled)
}
