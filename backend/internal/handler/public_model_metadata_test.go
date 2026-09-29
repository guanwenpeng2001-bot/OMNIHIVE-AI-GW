package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicMetadataRepository struct {
	service.AccountRepository
	accounts []service.Account
	err      error
	reads    int
}

func (repo *publicMetadataRepository) GetModelCatalogAccount(context.Context, int64) (*service.Account, error) {
	panic("public listing must reuse the account snapshot")
}

func (repo *publicMetadataRepository) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return repo.accounts, repo.err // Existing composite ownership lookup, not a second metadata read.
}

func (repo *publicMetadataRepository) ListModelCatalogAccounts(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	repo.reads++
	accounts := make([]service.Account, 0)
	for _, account := range repo.accounts {
		if len(platforms) == 0 || slices.Contains(platforms, account.Platform) {
			accounts = append(accounts, account)
		}
	}
	return accounts, repo.err
}

func publicMetadataAccount() service.Account {
	negative := false
	account := service.Account{ID: 91, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "private-key-never-public", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"gpt-alias": "gpt-native", "denied": "absent"}},
	}
	account.Extra = map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{
		Complete: true, Revision: 1, DiscoveredModels: []string{"gpt-native"},
		EligibleModels: []service.AccountCatalogModel{{ID: "gpt-native", Endpoints: []string{"responses", "chat_completions"}, EvidenceKind: "synthetic-contract"}},
	}}
	account.Extra[service.UpstreamModelMetadataExtraKey] = service.UpstreamModelMetadataSnapshot{
		Source: "https://private.example/evidence", SyncedAt: "2026-09-19T00:00:00Z",
		Models: map[string]service.UpstreamModelMetadata{"gpt-native": {
			ID: "gpt-native", ProviderProfile: "openai", ContextWindow: 128000, ContextLimitKind: "total", MaxOutputTokens: 4096,
			InputModalities: []string{}, OutputModalities: []string{"text"}, Reasoning: &negative,
			Description: "private-description", FieldEvidence: map[string]service.ModelCapabilityEvidence{"context_window": {Source: "private-source"}},
		}},
	}
	return account
}

func TestPublicModelMetadataProjection(t *testing.T) {
	negative, positive := false, true
	facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{
		{Reasoning: &negative, SupportsVision: &positive, ContextWindow: 1000, ContextLimitKind: "total", EmbeddingDimensions: 1536, InputModalities: []string{"TEXT", "image", "text"}},
		{Reasoning: &negative, SupportsVision: &negative, ContextWindow: 2000, ContextLimitKind: "total", EmbeddingDimensions: 3072, InputModalities: []string{"image", "text"}},
	})
	require.Equal(t, int64(1000), *facts.TotalContext.Guaranteed)
	require.Equal(t, []int64{1536, 3072}, facts.EmbeddingDimensions.Available)
	require.Nil(t, facts.EmbeddingDimensions.Guaranteed, "fixed dimensions must never be minimized")
	require.Equal(t, []string{"image", "text"}, *facts.InputModalities.Guaranteed)
	require.False(t, *facts.Reasoning.Guaranteed)
	require.False(t, *facts.SupportsVision.Guaranteed)
	require.Equal(t, []bool{false, true}, facts.SupportsVision.Available)
	unknown := aggregatePublicMetadata([]service.UpstreamModelMetadata{{ContextWindow: 4096}, {}})
	require.Nil(t, unknown.UnspecifiedContext.Guaranteed)
	require.True(t, unknown.UnspecifiedContext.Unknown)
	require.Equal(t, "unspecified", unknown.UnspecifiedContext.Kind)
	require.Equal(t, int64(4096), *unknown.UnspecifiedContext.ObservedMinimum)
	require.Nil(t, unknown.TotalContext.ObservedMinimum)
}

func TestPublicModelCatalogSharesMinimalMembership(t *testing.T) {
	account := publicMetadataAccount()
	repo := &publicMetadataRepository{accounts: []service.Account{account}}
	handler := newGatewayModelsHandlerForTest(repo)
	group := &service.Group{ID: 31, Platform: service.PlatformOpenAI}
	before, err := json.Marshal(account)
	require.NoError(t, err)
	request := func(path, model string, catalog bool) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", path, nil)
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 17, GroupID: &group.ID, Group: group})
		if model != "" {
			ctx.Params = gin.Params{{Key: "model", Value: model}}
		}
		if catalog {
			handler.ModelCatalog(ctx)
		} else {
			handler.Models(ctx)
		}
		return recorder
	}
	minimal := request("/v1/models", "", false)
	require.Equal(t, 200, minimal.Code)
	require.NotContains(t, minimal.Body.String(), "capabilities")
	require.NotContains(t, minimal.Body.String(), "created")
	require.Contains(t, minimal.Body.String(), `"object":"model"`)
	rich := request("/v1/models/catalog", "", true)
	require.Equal(t, 200, rich.Code)
	require.Contains(t, rich.Body.String(), `"observed_minimum":128000`)
	require.Contains(t, rich.Body.String(), `"created_at_source":"unavailable"`)
	require.Equal(t, minimal.Header().Get("X-Model-Catalog-Revision"), rich.Header().Get("X-Model-Catalog-Revision"))
	require.Equal(t, "private, no-store", rich.Header().Get("Cache-Control"))
	require.NotContains(t, rich.Body.String(), "private")
	require.NotContains(t, rich.Body.String(), "gpt-native")
	single := request("/v1/models/gpt-alias", "gpt-alias", false)
	require.Equal(t, 200, single.Code)
	missing := request("/v1/models/gpt-native", "gpt-native", false)
	require.Equal(t, 404, missing.Code)
	after, err := json.Marshal(account)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	repo.err = errors.New("private database error")
	failure := request("/v1/models/catalog", "", true)
	require.Equal(t, 503, failure.Code)
	require.NotContains(t, failure.Body.String(), "private")
}

func TestPublicModelCursorRevocation(t *testing.T) {
	account := publicMetadataAccount()
	account.Credentials["model_mapping"] = map[string]any{"alpha": "gpt-native", "beta": "gpt-native"}
	repo := &publicMetadataRepository{accounts: []service.Account{account}}
	handler := newGatewayModelsHandlerForTest(repo)
	group := &service.Group{ID: 31, Platform: service.PlatformOpenAI}
	request := func(path string, policy []string, keyID int64) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", path, nil)
		ctx.Request = ctx.Request.WithContext(service.WithVisibleModelKeyPolicy(ctx.Request.Context(), policy))
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: keyID, GroupID: &group.ID, Group: group})
		handler.ModelCatalog(ctx)
		return recorder
	}
	first := request("/v1/models/catalog?limit=1", nil, 7)
	require.Equal(t, 200, first.Code)
	var page struct {
		Next string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &page))
	require.NotEmpty(t, page.Next)
	nextPath := "/v1/models/catalog?limit=1&cursor=" + page.Next
	require.Equal(t, 200, request(nextPath, nil, 7).Code)
	require.Equal(t, 400, request(nextPath, []string{"alpha"}, 7).Code)
	require.Equal(t, 400, request(nextPath, nil, 8).Code)
	group.ID++
	require.Equal(t, 400, request(nextPath, nil, 7).Code)
}

func TestPublicListingDoesNotReapplyStaleAuthenticatedGroupPolicy(t *testing.T) {
	repo := &publicMetadataRepository{accounts: []service.Account{publicMetadataAccount()}}
	current := &service.Group{ID: 31, Platform: service.PlatformOpenAI, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-alias"}}}
	handler := newGatewayModelsHandlerForTest(repo, &publicListingGroupRepository{current: current})
	stale := *current
	stale.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"old-model"}}
	response := requestModelForTest(handler, &stale, "", "")
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), "gpt-alias")
}
