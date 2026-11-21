//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type effectiveRPMKeyRepoStub struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (s *effectiveRPMKeyRepoStub) GetByID(context.Context, int64) (*service.APIKey, error) {
	return s.key, nil
}

type effectiveRPMGroupRepoStub struct {
	service.GroupRepository
	override *int
}

func (s *effectiveRPMGroupRepoStub) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return &service.Group{ID: 5, Name: "team", RPMLimit: 100}, nil
}

func (s *effectiveRPMGroupRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return s.override, nil
}

type effectiveRPMRateRepoStub struct {
	service.UserGroupRateRepository
	override *int
}

func (s *effectiveRPMRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return s.override, nil
}

type effectiveRPMCacheStub struct {
	service.APIKeyCache
	resetAt time.Time
}

func (s *effectiveRPMCacheStub) RPMWindowResetAt(context.Context) (time.Time, error) {
	return s.resetAt, nil
}

func newKeyDetailRouter(key *service.APIKey, override *int, resetAt time.Time) *gin.Engine {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	rateRepo := &effectiveRPMRateRepoStub{override: override}
	groupRepo := &effectiveRPMGroupRepoStub{override: override}
	svc := service.NewAPIKeyService(
		&effectiveRPMKeyRepoStub{key: key}, nil, groupRepo, nil, rateRepo,
		&effectiveRPMCacheStub{resetAt: resetAt}, cfg,
	)
	h := NewAPIKeyHandler(svc)

	router := gin.New()
	router.GET("/api/v1/keys/:id", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 3})
		h.GetByID(c)
	})
	return router
}

func TestAPIKeyHandler_GetByIDExposesTheEffectiveRPM(t *testing.T) {
	resetAt := time.Now().Add(25 * time.Second).Truncate(time.Second)
	override := 30
	groupID := int64(5)
	key := &service.APIKey{
		ID: 7, UserID: 3, Name: "primary", Key: "sk-secret-value", GroupID: &groupID,
		User:  &service.User{ID: 3, RPMLimit: 240},
		Group: &service.Group{ID: groupID, Name: "team", RPMLimit: 100},
	}
	router := newKeyDetailRouter(key, &override, resetAt)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/7", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	envelope := map[string]any{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	data, ok := envelope["data"].(map[string]any)
	require.True(t, ok, rec.Body.String())

	// 既有字段保持原形：这是纯追加，不改变既有 consumers。
	require.Equal(t, "primary", data["name"])
	require.NotContains(t, rec.Body.String(), "sk-secret-value", "no read path may return the credential")

	effective, ok := data["effective_rpm"].(map[string]any)
	require.True(t, ok, "the user-visible effective RPM must be present: %s", rec.Body.String())
	require.Equal(t, service.EffectiveRPMPriority, effective["priority"])

	limits, ok := effective["limits"].([]any)
	require.True(t, ok)
	require.Len(t, limits, 2)
	first := limits[0].(map[string]any)
	require.Equal(t, string(service.AdmissionRateLimitSourceUserGroupOverride), first["source"])
	require.InDelta(t, 30, first["limit"], 1e-9)
	second := limits[1].(map[string]any)
	require.Equal(t, string(service.AdmissionRateLimitSourceUser), second["source"])
	require.InDelta(t, 240, second["limit"], 1e-9)

	resetsAt, ok := effective["window_resets_at"].(string)
	require.True(t, ok, "the real minute window end must be shown")
	parsed, err := time.Parse(time.RFC3339, resetsAt)
	require.NoError(t, err)
	require.WithinDuration(t, resetAt, parsed, time.Second)
}

func TestAPIKeyHandler_GetByIDDoesNotFabricateAResetWithoutEvidence(t *testing.T) {
	groupID := int64(5)
	key := &service.APIKey{
		ID: 7, UserID: 3, Name: "primary", GroupID: &groupID,
		User:  &service.User{ID: 3, RPMLimit: 240},
		Group: &service.Group{ID: groupID, RPMLimit: 100},
	}
	// 零值 resetAt 代表"没有服务端时间证据"。
	router := newKeyDetailRouter(key, nil, time.Time{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/7", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	envelope := map[string]any{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	data := envelope["data"].(map[string]any)
	effective := data["effective_rpm"].(map[string]any)
	require.NotContains(t, effective, "window_resets_at",
		"without a trustworthy window there is no reset time to show")
}
