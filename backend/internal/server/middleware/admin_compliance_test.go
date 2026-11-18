package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type complianceGuardRepoStub struct {
	values map[string]string
}

func (r *complianceGuardRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	if value, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (r *complianceGuardRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *complianceGuardRepoStub) Set(ctx context.Context, key, value string) error { return nil }
func (r *complianceGuardRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *complianceGuardRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	return nil
}
func (r *complianceGuardRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *complianceGuardRepoStub) Delete(ctx context.Context, key string) error { return nil }

func TestAdminComplianceGuardBlocksAdminRouteWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewSettingService(&complianceGuardRepoStub{}, &config.Config{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 1})
		c.Next()
	})
	router.Use(AdminComplianceGuard(svc))
	router.GET("/api/v1/admin/users", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusLocked, w.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, float64(http.StatusLocked), envelope["code"])
	require.Equal(t, "ADMIN_COMPLIANCE_ACK_REQUIRED", envelope["reason"])
	metadata, _ := envelope["metadata"].(map[string]any)
	require.Equal(t, service.AdminComplianceVersion, metadata["version"])
}

func TestAdminComplianceGuardBypassesComplianceEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewSettingService(&complianceGuardRepoStub{}, &config.Config{})
	router := gin.New()
	router.Use(AdminComplianceGuard(svc))
	router.GET("/api/v1/admin/compliance", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/compliance", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
}
