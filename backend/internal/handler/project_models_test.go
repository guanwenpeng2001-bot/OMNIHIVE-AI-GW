package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type discoveryKey struct {
	service.APIKeyRepository
	identity.GatewayScopes
	policy identity.KeyPolicy
	key    service.APIKey
}

func (d *discoveryKey) GetByID(context.Context, int64) (*service.APIKey, error) { return &d.key, nil }
func (d *discoveryKey) ProjectCredential(context.Context, string) (*identity.KeyPolicy, error) {
	return &d.policy, nil
}
func (*discoveryKey) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type discoveryAccounts struct {
	publicMetadataRepository
}

func (d *discoveryAccounts) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return d.accounts, nil
}

func TestProjectModelsDiscovery(t *testing.T) {
	group := &service.Group{ID: 31, Platform: "openai", Status: "active"}
	repo := &discoveryKey{
		key:    service.APIKey{ID: 5, WorkspaceID: 2, UserID: 1, User: &service.User{ID: 1, Status: "active", Balance: 1}, Group: group, GroupID: &group.ID, Status: "active"},
		policy: identity.KeyPolicy{Scope: scope.Authorized{KeyID: 5, KeyOwnerID: 1, WorkspaceID: 2, ProjectID: 3}, Platform: "openai", GroupID: group.ID, Mode: "allow_list", Models: []string{catalog.StableID("openai", "gpt-alias")}},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	gateway := newGatewayModelsHandlerForTest(&discoveryAccounts{publicMetadataRepository{accounts: []service.Account{publicMetadataAccount()}}})
	engine := gin.New()
	engine.Use(middleware.InstallProjectScopes(repo, cfg), gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg)))
	engine.GET("/v1/models", service.GatewayModelsHandler(gateway.CodexModels, gateway.Models))
	engine.GET("/v1/models/:model", gateway.Models)
	engine.GET("/v1beta/models", gateway.GeminiV1BetaListModels)
	engine.GET("/antigravity/v1beta/models", gateway.GeminiV1BetaListModels)
	call := func(path string, status int) []byte {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer oh_p_fixture")
		engine.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, rec.Body.String())
		return rec.Body.Bytes()
	}
	require.Contains(t, string(call("/v1/models", 403)), "models.read")
	repo.policy.Actions = []string{"models.read"}
	require.Equal(t, `["gpt-alias"]`, gjson.GetBytes(call("/v1/models", 200), "data.#.id").Raw)
	require.Equal(t, `["gpt-alias"]`, gjson.GetBytes(call("/v1/models?client_version=1", 200), "models.#.slug").Raw)
	call("/v1beta/models", 200)
	call("/antigravity/v1beta/models", 200)
	call("/v1/models/denied", 404)
	call("/v1/models/gpt-alias", 200)
	repo.policy.Mode = "deny_all"
	require.Equal(t, "[]", gjson.GetBytes(call("/v1/models", 200), "data").Raw)
}
