package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPublicReasoningCallableContract(t *testing.T) {
	sixLevels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	for _, test := range []struct {
		name, profile string
		levels, want  []string
		unknown       bool
	}{
		{"codex six levels", "openai-codex", sixLevels, []string{"high", "low", "max", "medium", "xhigh"}, false},
		{"other provider", "openai", sixLevels, []string{"high", "low", "max", "medium", "ultra", "xhigh"}, false},
		{"codex ultra only", "openai-codex", []string{"ultra"}, nil, true},
		{"codex normalized ultra", "openai-codex", []string{" ULTRA ", "high"}, []string{"high"}, false},
		{"codex empty", "openai-codex", []string{}, nil, true},
		{"codex missing", "openai-codex", nil, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			yes := true
			metadata := publicMetadataWithEvidence(service.UpstreamModelMetadata{
				ID: "gpt-6-astra", Reasoning: &yes, SupportedReasoningLevels: test.levels,
				ReasoningControl: &service.ReasoningControl{Kind: "effort", EffortValues: test.levels},
			}, "upstream")
			metadata.ProviderProfile = test.profile
			for field, evidence := range metadata.FieldEvidence {
				evidence.ProviderProfile = test.profile
				metadata.FieldEvidence[field] = evidence
			}
			before, err := json.Marshal(metadata)
			require.NoError(t, err)
			observations := []service.UpstreamModelMetadata{metadata}
			facts := aggregatePublicMetadata(observations)
			standard := clientModelMetadata(observations)
			require.Equal(t, test.unknown, facts.ReasoningLevels.Unknown)
			require.NotNil(t, facts.Reasoning.Guaranteed)
			require.True(t, *facts.Reasoning.Guaranteed)
			require.True(t, *standard.SupportsReasoning)
			if test.unknown {
				require.Nil(t, facts.ReasoningLevels.Guaranteed)
				require.Empty(t, facts.ReasoningLevels.Available)
				require.Nil(t, standard.SupportedReasoningLevels)
			} else {
				require.Equal(t, test.want, *facts.ReasoningLevels.Guaranteed)
				require.Equal(t, test.want, facts.ReasoningLevels.Available)
				require.Equal(t, test.want, *standard.SupportedReasoningLevels)
				require.Equal(t, []string{"upstream"}, facts.ReasoningLevels.Sources)
			}
			after, err := json.Marshal(metadata)
			require.NoError(t, err)
			require.Equal(t, before, after, "public projection must not mutate discovery")

			account := publicMetadataAccount()
			if test.profile == "openai-codex" {
				account.Type = service.AccountTypeOAuth
			}
			account.Credentials["model_mapping"] = map[string]any{"astra-public": "gpt-6-astra"}
			account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{
				Complete: true, Revision: 1, DiscoveredModels: []string{"gpt-6-astra"},
				EligibleModels: []service.AccountCatalogModel{{ID: "gpt-6-astra", Endpoints: []string{"responses"}, EvidenceKind: "synthetic-contract"}},
			}
			account.Extra[service.UpstreamModelMetadataExtraKey] = service.UpstreamModelMetadataSnapshot{
				Models: map[string]service.UpstreamModelMetadata{"gpt-6-astra": metadata},
			}
			storedBefore, err := json.Marshal(account)
			require.NoError(t, err)
			gateway := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: []service.Account{account}})
			for _, catalog := range []bool{false, true} {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest("GET", "/v1/models", nil)
				ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 17, Group: &service.Group{ID: 31, Platform: service.PlatformOpenAI}})
				if catalog {
					gateway.ModelCatalog(ctx)
				} else {
					gateway.Models(ctx)
				}
				require.Equal(t, 200, recorder.Code, recorder.Body.String())
				models := gjson.Get(recorder.Body.String(), "data").Array()
				require.Len(t, models, 1, recorder.Body.String())
				require.Equal(t, "astra-public", models[0].Get("id").String())
				if catalog {
					require.Equal(t, test.unknown, models[0].Get("capabilities.supported_reasoning_levels.unknown").Bool())
					require.True(t, models[0].Get("capabilities.reasoning.guaranteed").Bool())
				} else {
					require.True(t, models[0].Get("supports_reasoning").Bool())
					require.Equal(t, !test.unknown, models[0].Get("supported_reasoning_levels").Exists())
				}
			}
			storedAfter, err := json.Marshal(account)
			require.NoError(t, err)
			require.Equal(t, storedBefore, storedAfter, "listing must preserve stored discovery and routing inputs")
		})
	}
}

func TestPublicReasoningProjectionPreservesNativeCodexManifest(t *testing.T) {
	before, err := service.BuildCodexModelsManifest([]string{"gpt-6-astra"})
	require.NoError(t, err)
	model := gjson.GetBytes(before, "models.0")
	require.Equal(t, "xhigh", model.Get("multi_agent_reasoning_effort").String())
	require.Equal(t, "ultra", model.Get(`supported_reasoning_levels.#(effort=="ultra").effort`).String())
	metadata := service.UpstreamModelMetadata{
		ID: "gpt-6-astra", ProviderProfile: "openai-codex",
		SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
	}
	_ = clientModelMetadata([]service.UpstreamModelMetadata{metadata})
	after, err := service.BuildCodexModelsManifest([]string{"gpt-6-astra"})
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}
