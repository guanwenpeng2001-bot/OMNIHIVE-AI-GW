package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPublicTrustedCreationAndAnthropicOrder(t *testing.T) {
	account := publicMetadataAccount()
	account.Credentials["model_mapping"] = map[string]any{"old": "native-old", "new": "native-new", "same": "native-same"}
	catalog := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1}
	facts := map[string]service.UpstreamModelMetadata{}
	for _, entry := range []struct{ id, date string }{{"native-old", "2024-01-02T00:00:00Z"}, {"native-new", "2026-06-02T00:00:00Z"}, {"native-same", "2026-06-02T00:00:00Z"}} {
		catalog.DiscoveredModels = append(catalog.DiscoveredModels, entry.id)
		catalog.EligibleModels = append(catalog.EligibleModels, service.AccountCatalogModel{ID: entry.id, Endpoints: []string{"responses"}, EvidenceKind: "synthetic-contract"})
		facts[entry.id] = service.UpstreamModelMetadata{ID: entry.id, ProviderProfile: "openai", CreatedAt: entry.date, FieldEvidence: map[string]service.ModelCapabilityEvidence{"created_at": {Source: "upstream"}}}
	}
	account.Extra[service.UpstreamModelCatalogExtraKey] = catalog
	account.Extra[service.UpstreamModelMetadataExtraKey] = service.UpstreamModelMetadataSnapshot{Models: facts}
	gateway := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: []service.Account{account}})
	request := func(query string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/v1/models"+query, nil)
		ctx.Request.Header.Set("anthropic-version", "2023-06-01")
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, Group: &service.Group{ID: 31, Platform: service.PlatformOpenAI}})
		gateway.Models(ctx)
		return recorder
	}
	response := request("")
	require.Equal(t, 200, response.Code)
	require.Equal(t, `["new","same","old"]`, gjson.GetBytes(response.Body.Bytes(), "data.#.id").Raw)
	require.Equal(t, "2026-06-02T00:00:00Z", gjson.GetBytes(response.Body.Bytes(), "data.0.created_at").String())
	page := request("?after_id=new&limit=1")
	require.Equal(t, "same", gjson.GetBytes(page.Body.Bytes(), "first_id").String())
	selected, source := publicModelCreatedAt([]service.UpstreamModelMetadata{{CreatedAt: "2020-01-01T00:00:00Z", CreatedAtStatus: "available", FieldEvidence: map[string]service.ModelCapabilityEvidence{"created_at": {Source: "platform-preset"}}}, {CreatedAt: "2025-01-01T00:00:00Z", CreatedAtStatus: "available", FieldEvidence: map[string]service.ModelCapabilityEvidence{"created_at": {Source: "upstream"}}}})
	require.Equal(t, 2025, selected.Year())
	require.Equal(t, "upstream", source)
}
func TestPublicGoogleMethodsAndVersionStayWithBindings(t *testing.T) {
	view := service.VisibleModelSet{Bindings: map[string][]service.AccountModelBinding{"public": {{Endpoint: "responses"}, {Endpoint: "gemini"}}}}
	metadata := map[string][]service.UpstreamModelMetadata{"public": {
		{SupportedMethods: []string{"embedContent"}, ModelVersion: "private-deployment", ModelVersionStatus: "available", ContextWindow: 9999, ContextLimitKind: "input"},
		{SupportedMethods: []string{"generateContent", "streamGenerateContent", "countTokens", "embedContent"}, ModelVersion: "001", ModelVersionStatus: "available", ContextWindow: 4096, ContextLimitKind: "input", MaxOutputTokens: 512},
	}}
	// Limits require native, model-bound facts; methods still come from bindings.
	for index := range metadata["public"] {
		metadata["public"][index] = publicMetadataWithEvidence(metadata["public"][index], "upstream")
	}
	entry := publicGoogleModelEntry(view, metadata, "public")
	require.Equal(t, "001", entry.Version)
	require.NotNil(t, entry.InputTokenLimit)
	require.Equal(t, int64(4096), *entry.InputTokenLimit)
	require.Equal(t, []string{"countTokens", "generateContent", "streamGenerateContent"}, entry.SupportedGenerationMethods)
	metadata["public"][1] = publicMetadataWithEvidence(metadata["public"][1], "models.dev")
	entry = publicGoogleModelEntry(view, metadata, "public")
	require.Equal(t, "001", entry.Version, "capability evidence does not change version serialization")
	require.Nil(t, entry.InputTokenLimit, "general specifications are not binding guarantees")
	require.Nil(t, entry.OutputTokenLimit)
	view.Bindings["public"] = view.Bindings["public"][:1]
	entry = publicGoogleModelEntry(view, metadata, "public")
	require.Empty(t, entry.Version)
	require.Nil(t, entry.InputTokenLimit)
	require.Empty(t, entry.SupportedGenerationMethods)
}

func TestPublicCreationRequiresIdentifiableCurrentSource(t *testing.T) {
	for _, evidence := range []service.ModelCapabilityEvidence{
		{}, {Source: "https://private.example/evidence"}, {Source: "upstream", Stale: true},
	} {
		created, source := publicModelCreatedAt([]service.UpstreamModelMetadata{{
			CreatedAt: "2026-01-01T00:00:00Z", CreatedAtStatus: "available",
			FieldEvidence: map[string]service.ModelCapabilityEvidence{"created_at": evidence},
		}})
		require.Nil(t, created)
		require.Equal(t, "unavailable", source)
	}
}

func TestPublicCreationRejectsContradictoryIdentity(t *testing.T) {
	for _, test := range []struct {
		name, source, model, profile, rule string
		want                               bool
	}{
		{"native match", "upstream", "native-model", "openai", "native_id", true},
		{"legacy native", "upstream", "", "", "", true},
		{"wrong native model", "upstream", "another-model", "openai", "native_id", false},
		{"native cannot claim registry alias", "upstream", "another-model", "openai", "reviewed_alias", false},
		{"wrong native profile", "upstream", "native-model", "anthropic", "native_id", false},
		{"registry match", "platform-preset", "native-model", "openai", "exact", true},
		{"wrong registry model", "platform-preset", "another-model", "openai", "exact", false},
		{"reviewed registry alias", "platform-preset", "canonical-model", "openai", "reviewed_alias", true},
		{"wrong alias profile", "platform-preset", "canonical-model", "anthropic", "reviewed_alias", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			observation := service.UpstreamModelMetadata{
				ID: "native-model", ProviderProfile: "openai", CreatedAt: "2026-01-01T00:00:00Z", CreatedAtStatus: "available",
				FieldEvidence: map[string]service.ModelCapabilityEvidence{"created_at": {
					Source: test.source, MatchedModelID: test.model, ProviderProfile: test.profile, MatchRule: test.rule,
				}},
			}
			created, source := publicModelCreatedAt([]service.UpstreamModelMetadata{observation})
			if test.want {
				require.NotNil(t, created)
				require.Equal(t, 2026, created.Year())
			} else {
				require.Nil(t, created)
				require.Equal(t, "unavailable", source)
			}
		})
	}
}
