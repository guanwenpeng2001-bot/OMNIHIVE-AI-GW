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
	observations := []service.UpstreamModelMetadata{
		{Reasoning: &negative, SupportsVision: &positive, ContextWindow: 1000, ContextLimitKind: "total", EmbeddingDimensions: 1536, InputModalities: []string{"TEXT", "image", "text"}},
		{Reasoning: &negative, SupportsVision: &negative, ContextWindow: 2000, ContextLimitKind: "total", EmbeddingDimensions: 3072, InputModalities: []string{"image", "text"}},
	}
	for index := range observations {
		observations[index] = publicMetadataWithEvidence(observations[index], "upstream")
	}
	facts := aggregatePublicMetadata(observations)
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

// Mirror native/declaration stamps, including exact model/profile binding.
func publicMetadataWithEvidence(metadata service.UpstreamModelMetadata, source string) service.UpstreamModelMetadata {
	if metadata.ID == "" {
		metadata.ID = "model-native"
	}
	metadata.ProviderProfile = "openai"
	metadata.FieldEvidence = make(map[string]service.ModelCapabilityEvidence)
	rule := "native_id"
	if source == "account-declaration" {
		rule = "identity_bound_exact"
	}
	for _, field := range []string{"context_window", "context_limit_kind", "max_output_tokens", "embedding_dimensions", "reasoning", "supports_vision", "supports_function_calling", "supports_prompt_caching", "supports_pdf", "input_modalities", "output_modalities"} {
		metadata.FieldEvidence[field] = service.ModelCapabilityEvidence{Source: source, ProviderProfile: metadata.ProviderProfile, MatchedModelID: metadata.ID, MatchRule: rule}
	}
	return metadata
}

func TestAggregatePublicMetadataSources(t *testing.T) {
	positive := true
	template := service.UpstreamModelMetadata{ContextWindow: 128000, ContextLimitKind: "total", MaxOutputTokens: 4096, EmbeddingDimensions: 1536,
		Reasoning: &positive, SupportsVision: &positive, SupportsFunctionCalling: &positive, SupportsPromptCaching: &positive, SupportsPDF: &positive,
		InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}}
	for _, source := range []string{"upstream", "account-declaration", "models.dev", "litellm", "openrouter", "cursor-builtin", "heuristic", "derived", "gateway", ""} {
		t.Run(source, func(t *testing.T) {
			observation := publicMetadataWithEvidence(template, source)
			guaranteed := source == "upstream" || source == "account-declaration"
			for _, mixed := range []bool{false, true} {
				other := observation
				if mixed {
					other = publicMetadataWithEvidence(template, "upstream")
				}
				facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{other, observation})
				require.Equal(t, !guaranteed, facts.TotalContext.Unknown)
				require.Equal(t, []int64{128000}, facts.TotalContext.Available)
				require.Equal(t, int64(128000), *facts.TotalContext.ObservedMinimum)
				require.Equal(t, []int64{4096}, facts.MaxOutputTokens.Available)
				require.Equal(t, []int64{1536}, facts.EmbeddingDimensions.Available)
				for _, fact := range []publicFact[bool]{facts.Reasoning, facts.SupportsVision, facts.SupportsFunctionCalling, facts.SupportsPromptCaching, facts.SupportsPDF} {
					require.Equal(t, []bool{true}, fact.Available)
					require.Equal(t, !guaranteed, fact.Unknown)
					require.Equal(t, guaranteed, fact.Guaranteed != nil)
				}
				for _, fact := range []publicFact[int64]{facts.TotalContext.publicFact, facts.MaxOutputTokens.publicFact, facts.EmbeddingDimensions} {
					require.Equal(t, !guaranteed, fact.Unknown)
					require.Equal(t, guaranteed, fact.Guaranteed != nil)
				}
				for _, fact := range []publicModalities{facts.InputModalities, facts.OutputModalities} {
					require.NotEmpty(t, fact.Available)
					require.Equal(t, !guaranteed, fact.Unknown)
					require.Equal(t, guaranteed, fact.Guaranteed != nil)
				}
			}
		})
	}
}

func TestAggregatePublicMetadataFieldApplicability(t *testing.T) {
	positive := true
	native := publicMetadataWithEvidence(service.UpstreamModelMetadata{Reasoning: &positive, SupportsVision: &positive}, "upstream")
	cases := []struct {
		name   string
		change func(*service.UpstreamModelMetadata)
	}{
		{"missing field evidence", func(metadata *service.UpstreamModelMetadata) { delete(metadata.FieldEvidence, "reasoning") }},
		{"missing value", func(metadata *service.UpstreamModelMetadata) { metadata.Reasoning = nil }},
		{"missing profile", func(metadata *service.UpstreamModelMetadata) {
			evidence := metadata.FieldEvidence["reasoning"]
			evidence.ProviderProfile = ""
			metadata.FieldEvidence["reasoning"] = evidence
		}},
		{"different profile", func(metadata *service.UpstreamModelMetadata) {
			evidence := metadata.FieldEvidence["reasoning"]
			evidence.ProviderProfile = "anthropic"
			metadata.FieldEvidence["reasoning"] = evidence
		}},
		{"different model", func(metadata *service.UpstreamModelMetadata) {
			evidence := metadata.FieldEvidence["reasoning"]
			evidence.MatchedModelID = "other"
			metadata.FieldEvidence["reasoning"] = evidence
		}},
		{"stale", func(metadata *service.UpstreamModelMetadata) {
			evidence := metadata.FieldEvidence["reasoning"]
			evidence.Stale = true
			metadata.FieldEvidence["reasoning"] = evidence
		}},
		{"generic match", func(metadata *service.UpstreamModelMetadata) {
			evidence := metadata.FieldEvidence["reasoning"]
			evidence.MatchRule = "account_protocol"
			metadata.FieldEvidence["reasoning"] = evidence
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			observation := publicMetadataWithEvidence(native, "upstream")
			test.change(&observation)
			before, err := json.Marshal(observation)
			require.NoError(t, err)
			facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{native, observation})
			require.Nil(t, facts.Reasoning.Guaranteed)
			require.True(t, facts.Reasoning.Unknown)
			require.Equal(t, []bool{true}, facts.Reasoning.Available)
			require.NotNil(t, facts.SupportsVision.Guaranteed, "unrelated valid field remains authoritative")
			after, err := json.Marshal(observation)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
	facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{native, {}})
	require.True(t, facts.SupportsVision.Unknown)
	require.Nil(t, facts.SupportsVision.Guaranteed, "unknown account must retain its position")
}

func TestAggregatePublicMetadataConservativeValues(t *testing.T) {
	positive, negative := true, false
	first := publicMetadataWithEvidence(service.UpstreamModelMetadata{ContextWindow: 128000, ContextLimitKind: "input", MaxOutputTokens: 4096, EmbeddingDimensions: 1536, Reasoning: &positive, InputModalities: []string{"TEXT", "image", "text"}, OutputModalities: []string{"text"}}, "upstream")
	second := publicMetadataWithEvidence(service.UpstreamModelMetadata{ContextWindow: 200000, ContextLimitKind: "input", MaxOutputTokens: 8192, EmbeddingDimensions: 3072, Reasoning: &negative, InputModalities: []string{"text", "audio"}, OutputModalities: []string{}}, "account-declaration")
	facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{first, second})
	require.Equal(t, int64(128000), *facts.InputContext.Guaranteed)
	require.Equal(t, []int64{128000, 200000}, facts.InputContext.Available)
	require.Equal(t, int64(4096), *facts.MaxOutputTokens.Guaranteed)
	require.False(t, *facts.Reasoning.Guaranteed)
	require.False(t, facts.EmbeddingDimensions.Unknown)
	require.Nil(t, facts.EmbeddingDimensions.Guaranteed)
	require.Equal(t, []string{"audio", "image", "text"}, facts.InputModalities.Available)
	require.Equal(t, []string{"text"}, *facts.InputModalities.Guaranteed)
	require.Equal(t, []string{}, *facts.OutputModalities.Guaranteed)
	second.FieldEvidence["context_window"] = service.ModelCapabilityEvidence{Source: "models.dev"}
	facts = aggregatePublicMetadata([]service.UpstreamModelMetadata{first, second})
	require.Nil(t, facts.InputContext.Guaranteed)
	require.Equal(t, []int64{128000, 200000}, facts.InputContext.Available)
	require.Equal(t, int64(128000), *facts.InputContext.ObservedMinimum)
}

func TestAggregatePublicMetadataContextKindsAndMissingModalities(t *testing.T) {
	for _, kind := range []string{"input", "total", "unspecified"} {
		t.Run(kind, func(t *testing.T) {
			observation := publicMetadataWithEvidence(service.UpstreamModelMetadata{ContextWindow: 1024, ContextLimitKind: kind}, "upstream")
			facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{observation})
			limits := map[string]publicLimit{"input": facts.InputContext, "total": facts.TotalContext, "unspecified": facts.UnspecifiedContext}
			require.Equal(t, int64(1024), *limits[kind].Guaranteed)
			if kind != "unspecified" {
				delete(observation.FieldEvidence, "context_limit_kind")
				facts = aggregatePublicMetadata([]service.UpstreamModelMetadata{observation})
				limits = map[string]publicLimit{"input": facts.InputContext, "total": facts.TotalContext}
				require.Nil(t, limits[kind].Guaranteed)
				require.Equal(t, int64(1024), *limits[kind].ObservedMinimum)
			}
		})
	}
	for _, modalities := range [][]string{nil, {"unsupported"}} {
		observation := publicMetadataWithEvidence(service.UpstreamModelMetadata{InputModalities: modalities, ContextWindow: -1, MaxOutputTokens: 0}, "upstream")
		facts := aggregatePublicMetadata([]service.UpstreamModelMetadata{observation})
		require.Nil(t, facts.InputModalities.Guaranteed)
		require.True(t, facts.InputModalities.Unknown)
		require.Empty(t, facts.InputModalities.Available)
		require.Nil(t, facts.UnspecifiedContext.ObservedMinimum)
		require.True(t, facts.MaxOutputTokens.Unknown)
	}
	empty := aggregatePublicMetadata(nil)
	require.True(t, empty.Reasoning.Unknown)
	require.True(t, empty.InputModalities.Unknown)
	require.Equal(t, []bool{}, empty.Reasoning.Available)
}

func TestPublicModelMetadataCatalogMixedAccountSources(t *testing.T) {
	for _, sources := range [][2]string{{"upstream", "upstream"}, {"upstream", "models.dev"}, {"models.dev", "models.dev"}, {"upstream", ""}} {
		t.Run(sources[0]+"_"+sources[1], func(t *testing.T) {
			accounts := []service.Account{publicMetadataAccount(), publicMetadataAccount()}
			for index := range accounts {
				accounts[index].ID += int64(index)
				snapshot := accounts[index].Extra[service.UpstreamModelMetadataExtraKey].(service.UpstreamModelMetadataSnapshot)
				metadata := snapshot.Models["gpt-native"]
				metadata.ContextWindow += int64(index) * 72000
				metadata = publicMetadataWithEvidence(metadata, sources[index])
				snapshot.Models["gpt-native"] = metadata
				accounts[index].Extra[service.UpstreamModelMetadataExtraKey] = snapshot
			}
			handler := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: accounts})
			group := &service.Group{ID: 31, Platform: service.PlatformOpenAI}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/v1/models/catalog", nil)
			ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 17, GroupID: &group.ID, Group: group})
			handler.ModelCatalog(ctx)
			require.Equal(t, 200, recorder.Code)
			var response struct {
				Data []struct {
					ID           string              `json:"id"`
					Capabilities publicModelMetadata `json:"capabilities"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Len(t, response.Data, 1)
			require.Equal(t, "gpt-alias", response.Data[0].ID)
			context := response.Data[0].Capabilities.TotalContext
			require.Equal(t, []int64{128000, 200000}, context.Available)
			require.Equal(t, int64(128000), *context.ObservedMinimum)
			if sources[0] == "upstream" && sources[1] == "upstream" {
				require.Equal(t, int64(128000), *context.Guaranteed)
				require.False(t, context.Unknown)
			} else {
				require.Nil(t, context.Guaranteed)
				require.True(t, context.Unknown)
			}
			require.NotContains(t, recorder.Body.String(), "field_evidence")
			require.NotContains(t, recorder.Body.String(), "gpt-native")
			require.NotContains(t, recorder.Body.String(), "private")
		})
	}
}
