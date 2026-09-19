package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func TestPublicModelMetadataProjection(test *testing.T) {
	for _, scenario := range []struct {
		name, platform, forced string
		allowlist, failure     bool
	}{
		{name: "ordinary", platform: service.PlatformOpenAI},
		{name: "forced-platform", platform: service.PlatformAnthropic, forced: service.PlatformOpenAI},
		{name: "composite", platform: service.PlatformComposite},
		{name: "allowlist", platform: service.PlatformOpenAI, allowlist: true},
		{name: "catalog-failure", platform: service.PlatformOpenAI, failure: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := publicMetadataAccount()
			repo := &publicMetadataRepository{accounts: []service.Account{account}}
			if scenario.failure {
				repo.err = errors.New("private database failure")
			}
			handler := newGatewayModelsHandlerForTest(repo)
			group := &service.Group{ID: 31, Platform: scenario.platform}
			if scenario.allowlist {
				group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-alias"}}
			}
			request := func() *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
				requestContext.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
				if scenario.forced != "" {
					middleware.ForcePlatform(scenario.forced)(requestContext)
				}
				handler.Models(requestContext)
				return recorder
			}
			before, err := json.Marshal(account)
			require.NoError(test, err)
			got := request()
			require.Equal(test, 1, repo.reads, "one request captures one account snapshot")
			after, err := json.Marshal(account)
			require.NoError(test, err)
			require.Equal(test, string(before), string(after), "projection cannot mutate catalog, credentials, or prices")
			require.NotContains(test, got.Body.String(), "private")
			if scenario.failure {
				require.Equal(test, http.StatusServiceUnavailable, got.Code)
				require.JSONEq(test, `{"type":"error","error":{"type":"api_error","message":"Model catalog unavailable"}}`, got.Body.String())
				return
			}
			require.Equal(test, http.StatusOK, got.Code, got.Body.String())
			var enriched map[string]json.RawMessage
			require.NoError(test, json.Unmarshal(got.Body.Bytes(), &enriched))
			var entries []map[string]json.RawMessage
			require.NoError(test, json.Unmarshal(enriched["data"], &entries))
			found := false
			for _, entry := range entries {
				if string(entry["id"]) == `"gpt-alias"` {
					found = true
					require.JSONEq(test, `{"context_window":128000,"context_limit_kind":"total","max_output_tokens":4096,"input_modalities":[],"output_modalities":["text"],"reasoning":false}`, string(entry["capabilities"]))
				}
				delete(entry, "capabilities")
			}
			require.True(test, found)
			require.Len(test, entries, 1, "metadata and unresolved mappings cannot create extra members")
			stripped, err := json.Marshal(gin.H{"object": "list", "data": entries})
			require.NoError(test, err)
			// Frozen pre-projection fields from writeModelsList at baseline 2279965a.
			golden := `{"object":"list","data":[{"id":"gpt-alias","object":"model","created":1704067200,"owned_by":"openai","type":"model","display_name":"gpt-alias"}]}`
			if scenario.platform == service.PlatformComposite {
				golden = `{"object":"list","data":[{"id":"gpt-alias","type":"model","display_name":"gpt-alias","created_at":"2024-01-01T00:00:00Z"}]}`
			}
			require.JSONEq(test, golden, string(stripped))
			// Same membership with no specifications is the pre-addition wire form.
			delete(repo.accounts[0].Extra, service.UpstreamModelMetadataExtraKey)
			legacy := request()
			require.JSONEq(test, legacy.Body.String(), string(stripped))
		})
	}
	test.Run("tri-state-and-conflicts", func(test *testing.T) {
		negative, positive := false, true
		models := []gin.H{{"id": "model", "created": 9007199254740993}}
		metadata := map[string][]service.UpstreamModelMetadata{"model": {
			{Reasoning: &negative, SupportsVision: &positive, InputModalities: []string{}, ContextWindow: 1000, ContextLimitKind: "total"},
			{Reasoning: &negative, SupportsVision: &negative, InputModalities: []string{}, ContextWindow: 2000, ContextLimitKind: "total"},
		}}
		projected, err := projectPublicModelMetadata(models, metadata)
		require.NoError(test, err)
		body, err := json.Marshal(projected)
		require.NoError(test, err)
		require.JSONEq(test, `[{"id":"model","created":9007199254740993,"capabilities":{"reasoning":false,"input_modalities":[]}}]`, string(body))
	})
	test.Run("single-snapshot-many-models", func(test *testing.T) {
		account := publicMetadataAccount()
		mapping := map[string]any{}
		for index := range 100 {
			mapping[fmt.Sprintf("gpt-public-%03d", index)] = "gpt-native"
		}
		account.Credentials["model_mapping"] = mapping
		capture := &metadataSnapshotCapture{snapshot: account.Extra[service.UpstreamModelMetadataExtraKey]}
		account.Extra[service.UpstreamModelMetadataExtraKey] = capture
		repo := &publicMetadataRepository{accounts: []service.Account{account}}
		handler := newGatewayModelsHandlerForTest(repo)
		group := &service.Group{ID: 31, Platform: service.PlatformOpenAI}
		response := requestModelForTest(handler, group, "", "")
		require.Equal(test, http.StatusOK, response.Code)
		require.Equal(test, 1, capture.reads, "100 models must share one parsed metadata snapshot")
		require.Equal(test, 1, repo.reads)
		var catalog struct {
			Data []struct {
				Capabilities map[string]any `json:"capabilities"`
			} `json:"data"`
		}
		require.NoError(test, json.Unmarshal(response.Body.Bytes(), &catalog))
		require.Len(test, catalog.Data, 100)
		for _, entry := range catalog.Data {
			require.EqualValues(test, 128000, entry.Capabilities["context_window"])
		}
		// A later publication is visible only on the next request.
		capture.snapshot = service.UpstreamModelMetadataSnapshot{Models: map[string]service.UpstreamModelMetadata{"gpt-native": {ID: "gpt-native", ProviderProfile: "openai", ContextWindow: 64000, ContextLimitKind: "total"}}}
		response = requestModelForTest(handler, group, "", "")
		require.Equal(test, 2, capture.reads)
		require.NoError(test, json.Unmarshal(response.Body.Bytes(), &catalog))
		for _, entry := range catalog.Data {
			require.EqualValues(test, 64000, entry.Capabilities["context_window"])
		}
	})
	test.Run("private-text-and-unknown-exit", func(test *testing.T) {
		models := []gin.H{{"id": "model"}}
		for _, observations := range [][]service.UpstreamModelMetadata{
			{{InputModalities: []string{"https://private.example/evidence"}, ContextWindow: 123, ContextLimitKind: "private-limit", Description: "private-trace"}},
			{{ContextWindow: 123, ContextLimitKind: "total"}, {}},
		} {
			projected, err := projectPublicModelMetadata(models, map[string][]service.UpstreamModelMetadata{"model": observations})
			require.NoError(test, err)
			body, err := json.Marshal(projected)
			require.NoError(test, err)
			require.JSONEq(test, `[{"id":"model"}]`, string(body))
		}
	})
	test.Run("pinned-manifest", TestRetrievePinnedModelPreservesMetadataFilteringAndErrors)
	test.Run("client-version", TestGatewayCodexModels_NonOpenAIGroupsUseMappedModels)
	test.Run("legacy-fallback", TestRetrieveModelMatchesVisibleCatalogue)
}

type metadataSnapshotCapture struct {
	snapshot any
	reads    int
}

func (capture *metadataSnapshotCapture) MarshalJSON() ([]byte, error) {
	capture.reads++
	return json.Marshal(capture.snapshot)
}
