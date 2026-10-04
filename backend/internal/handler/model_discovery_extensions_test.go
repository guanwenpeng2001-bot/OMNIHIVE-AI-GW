package handler

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testassert"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClientModelMetadataPublishesKnownCapabilitiesOnly(t *testing.T) {
	yes := true
	first := publicMetadataWithEvidence(service.UpstreamModelMetadata{
		ContextWindow: 1000000, ContextLimitKind: "total", MaxOutputTokens: 32768,
		Reasoning: &yes, SupportedReasoningLevels: []string{"low", "medium", "high"},
	}, "upstream")
	second := publicMetadataWithEvidence(service.UpstreamModelMetadata{
		ContextWindow: 200000, ContextLimitKind: "total", MaxOutputTokens: 8192,
		Reasoning: &yes, SupportedReasoningLevels: []string{"medium", "high"},
	}, "upstream")
	fields := clientModelMetadata([]service.UpstreamModelMetadata{first, second})
	require.EqualValues(t, 200000, *fields.ContextWindow)
	require.Equal(t, fields.ContextWindow, fields.ContextLength)
	require.EqualValues(t, 8192, *fields.MaxOutputTokens)
	require.True(t, *fields.SupportsReasoning)
	require.ElementsMatch(t, []string{"medium", "high"}, *fields.SupportedReasoningLevels)
	require.Nil(t, fields.MaxInputTokens, "total context must not be relabeled input-only")

	unknown := clientModelMetadata([]service.UpstreamModelMetadata{first, {}})
	require.Nil(t, unknown.ContextWindow)
	require.Nil(t, unknown.MaxOutputTokens)
	require.Nil(t, unknown.SupportsReasoning, "unknown is not false")
	encoded, err := json.Marshal(unknown)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "context_window")
	require.NotContains(t, string(encoded), "supports_reasoning")

	reference := first
	reference = publicMetadataWithEvidence(reference, "models.dev")
	require.Nil(t, clientModelMetadata([]service.UpstreamModelMetadata{reference}).ContextWindow,
		"reference information stays in the rich catalog; it is not a guaranteed routing limit")
	no := false
	negative := publicMetadataWithEvidence(service.UpstreamModelMetadata{Reasoning: &no}, "upstream")
	encoded, err = json.Marshal(clientModelMetadata([]service.UpstreamModelMetadata{negative}))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"supports_reasoning":false`)
	input := publicMetadataWithEvidence(service.UpstreamModelMetadata{ContextWindow: 64000, ContextLimitKind: "input"}, "upstream")
	inputFields := clientModelMetadata([]service.UpstreamModelMetadata{input})
	require.EqualValues(t, 64000, *inputFields.MaxInputTokens)
	require.Nil(t, inputFields.ContextWindow, "input-only capacity must not be relabeled total context")
	require.Nil(t, inputFields.ContextLength)
}

func TestPublicReasoningSetsPreserveUncertaintyAndSources(t *testing.T) {
	for _, test := range []struct {
		name    string
		levels  []string
		source  string
		unknown bool
		want    []string
	}{
		{"normalize", []string{" HIGH ", "high", "off", "extra-high"}, "upstream", false, []string{"high", "none", "xhigh"}},
		{"explicit empty", []string{}, "upstream", false, []string{}},
		{"missing", nil, "upstream", true, []string{}},
		{"invalid", []string{"text"}, "upstream", true, []string{}},
		{"reference", []string{"low", "high"}, "models.dev", true, []string{"high", "low"}},
		{"private source", []string{"high"}, "https://private.example/evidence", true, []string{"high"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := publicMetadataWithEvidence(service.UpstreamModelMetadata{SupportedReasoningLevels: test.levels}, test.source)
			fact := aggregatePublicMetadata([]service.UpstreamModelMetadata{metadata}).ReasoningLevels
			require.Equal(t, test.unknown, fact.Unknown)
			require.Equal(t, test.want, fact.Available)
			if test.unknown {
				require.Nil(t, fact.Guaranteed)
			} else {
				require.Equal(t, test.want, *fact.Guaranteed)
			}
			encoded, err := json.Marshal(fact)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private.example")
			if test.name == "reference" {
				require.Equal(t, []string{"models.dev"}, fact.Sources)
			}
		})
	}
	metadata := publicMetadataWithEvidence(service.UpstreamModelMetadata{SupportedReasoningLevels: []string{"high"}}, "upstream")
	evidence := metadata.FieldEvidence["supported_reasoning_levels"]
	evidence.Stale = true
	metadata.FieldEvidence["supported_reasoning_levels"] = evidence
	require.Nil(t, clientModelMetadata([]service.UpstreamModelMetadata{metadata}).SupportedReasoningLevels)
}

func TestModelDiscoveryExtensionsThroughHandlers(t *testing.T) {
	account := publicMetadataAccount()
	yes := true
	snapshot := testassert.Type[service.UpstreamModelMetadataSnapshot](t, account.Extra[service.UpstreamModelMetadataExtraKey])
	metadata := snapshot.Models["gpt-native"]
	metadata.Reasoning = &yes
	metadata.SupportedReasoningLevels = []string{"low", "high"}
	snapshot.Models["gpt-native"] = publicMetadataWithEvidence(metadata, "upstream")
	account.Extra[service.UpstreamModelMetadataExtraKey] = snapshot
	gateway := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: []service.Account{account}})
	for _, protocol := range []string{"openai", "anthropic", "catalog"} {
		t.Run(protocol, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/v1/models", nil)
			if protocol == "anthropic" {
				ctx.Request.Header.Set("anthropic-version", "2023-06-01")
			}
			ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 17, Group: &service.Group{ID: 31, Platform: service.PlatformOpenAI}})
			if protocol == "catalog" {
				gateway.ModelCatalog(ctx)
			} else {
				gateway.Models(ctx)
			}
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			body := recorder.Body.Bytes()
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			require.NotEmpty(t, recorder.Header().Get("X-Model-Catalog-Revision"))
			for _, private := range []string{"gpt-native", "private-key", "private.example", "private-description"} {
				require.NotContains(t, string(body), private)
			}
			require.Equal(t, int64(2), gjson.GetBytes(body, "data.#").Int(), "unknown metadata must retain the selected model")
			known := gjson.GetBytes(body, `data.#(id=="gpt-alias")`)
			unknown := gjson.GetBytes(body, `data.#(id=="denied")`)
			if protocol == "catalog" {
				require.Equal(t, int64(128000), known.Get("capabilities.total_context.guaranteed").Int())
				require.Equal(t, `["upstream"]`, known.Get("capabilities.supported_reasoning_levels.sources").Raw)
				require.True(t, unknown.Get("capabilities.reasoning.unknown").Bool())
			} else {
				require.Equal(t, int64(128000), known.Get("context_window").Int())
				require.Equal(t, int64(128000), known.Get("context_length").Int())
				require.Equal(t, int64(4096), known.Get("max_output_tokens").Int())
				require.True(t, known.Get("supports_reasoning").Bool())
				require.Equal(t, `["high","low"]`, known.Get("supported_reasoning_levels").Raw)
				require.False(t, known.Get("max_input_tokens").Exists())
				require.False(t, unknown.Get("supports_reasoning").Exists())
				require.False(t, unknown.Get("context_window").Exists())
				require.False(t, known.Get("created").Exists())
				require.False(t, known.Get("created_at").Exists())
			}
		})
	}
}
