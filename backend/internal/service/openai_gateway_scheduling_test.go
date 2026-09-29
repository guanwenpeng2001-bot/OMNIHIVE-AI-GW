package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// requestPlanGateAccount carries exactly the endpoint evidence a model declares
// so the pre-profit gate must consult that family instead of a fixed Responses
// endpoint.
func requestPlanGateAccount(platform, model string, endpoints ...string) Account {
	account := catalogEvidenceAccount(1, platform, model)
	snapshot, _ := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	snapshot.EligibleModels[0].Endpoints = endpoints
	return account
}

func requestPlanGateContext(spec GatewayRequestSpec) context.Context {
	return WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(spec))
}

func requestPlanGateReason(ctx context.Context, account *Account, platform, model string, capability OpenAIEndpointCapability) string {
	return openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, account, platform, model, false, capability)
}

// A media action carrying a request plan must be judged on the endpoint family
// that action actually executes on: before the fix every planned media request
// was probed for Responses evidence and vetoed with model_not_supported.
func TestRequestPlanMediaGateUsesRequestedEndpointFamily(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		spec       GatewayRequestSpec
		platform   string
		model      string
		endpoint   string
		capability OpenAIEndpointCapability
	}{
		{
			name:       "openai image generation",
			spec:       GatewayRequestSpec{Action: "image.generate", Protocol: "images", Route: "/v1/images/generations", Model: "gpt-image-1"},
			platform:   PlatformOpenAI,
			model:      "gpt-image-1",
			endpoint:   CompositeRouteEndpointImages,
			capability: OpenAIEndpointCapabilityChatCompletions,
		},
		{
			name:       "openai image edit",
			spec:       GatewayRequestSpec{Action: "image.edit", Protocol: "images", Route: "/v1/images/edits", Model: "gpt-image-1"},
			platform:   PlatformOpenAI,
			model:      "gpt-image-1",
			endpoint:   CompositeRouteEndpointImages,
			capability: OpenAIEndpointCapabilityChatCompletions,
		},
		{
			name:       "grok media image generation",
			spec:       GatewayRequestSpec{Action: "image.generate", Protocol: "media", Route: "/v1/images/generations", Model: "grok-imagine-image-quality"},
			platform:   PlatformGrok,
			model:      "grok-imagine-image-quality",
			endpoint:   string(OpenAIEndpointCapabilityGrokMediaGeneration),
			capability: OpenAIEndpointCapabilityGrokMediaGeneration,
		},
		{
			name:       "grok media video generation",
			spec:       GatewayRequestSpec{Action: "video.generate", Protocol: "media", Route: "/v1/videos", Model: "grok-imagine-video"},
			platform:   PlatformGrok,
			model:      "grok-imagine-video",
			endpoint:   string(OpenAIEndpointCapabilityGrokMediaGeneration),
			capability: OpenAIEndpointCapabilityGrokMediaGeneration,
		},
		{
			name: "zhipu media image generation",
			// The route deliberately avoids GatewayActionForRoute markers: the domain
			// action matrix for zhipu lands with the multimodal ingress wiring, and an
			// unresolved action would decide the verdict before the catalog family does
			// (same rationale as handler/zhipu_actions_flow_test.go).
			spec:       GatewayRequestSpec{Protocol: "media", Route: "/zhipu-adapter/img-gen", Model: "cogview-4"},
			platform:   PlatformZhipu,
			model:      "cogview-4",
			endpoint:   CompositeRouteEndpointImages,
			capability: OpenAIEndpointCapabilityChatCompletions,
		},
		{
			name:       "openai embeddings",
			spec:       GatewayRequestSpec{Action: "embeddings", Protocol: "embeddings", Route: "/v1/embeddings", Model: "text-embedding-3-large"},
			platform:   PlatformOpenAI,
			model:      "text-embedding-3-large",
			endpoint:   string(OpenAIEndpointCapabilityEmbeddings),
			capability: OpenAIEndpointCapabilityEmbeddings,
		},
		{
			name:       "openai alpha search",
			spec:       GatewayRequestSpec{Action: "alpha_search", Protocol: "search", Route: "/v1/alpha/search", Model: "gpt-5.1"},
			platform:   PlatformOpenAI,
			model:      "gpt-5.1",
			endpoint:   string(OpenAIEndpointCapabilityAlphaSearch),
			capability: OpenAIEndpointCapabilityAlphaSearch,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := requestPlanGateContext(scenario.spec)
			supported := requestPlanGateAccount(scenario.platform, scenario.model, scenario.endpoint)
			require.NotEqual(t, "model_not_supported",
				requestPlanGateReason(ctx, &supported, scenario.platform, scenario.model, scenario.capability),
				"the requested media family is declared by the account catalog and must not be vetoed")

			unsupported := requestPlanGateAccount(scenario.platform, scenario.model, "unrelated")
			require.Equal(t, "model_not_supported",
				requestPlanGateReason(ctx, &unsupported, scenario.platform, scenario.model, scenario.capability),
				"endpoint evidence outside the requested family still vetoes")
		})
	}
}

// The endpoint family is resolved from the plan's own protocol through the
// single rule the OpenAI scheduler's model gate reads. Media protocols read the
// family their account records (Grok declares its imagine family, other
// platforms declare image endpoints); text protocols keep the Responses bridge
// evidence, so an OpenAI OAuth account (Responses-only text catalog) and a
// bridged API key account (Chat Completions-only catalog) stay admissible. An
// absent plan keeps the legacy Responses evidence.
func TestRequestPlanCatalogEndpointFamilies(t *testing.T) {
	plan := func(protocol string) context.Context {
		return requestPlanGateContext(GatewayRequestSpec{Protocol: protocol, Route: "/fixture", Model: "fixture"})
	}
	for _, scenario := range []struct {
		name     string
		protocol string
		platform string
		want     string
	}{
		{"responses", "responses", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"chat completions", "chat_completions", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"anthropic messages", "anthropic", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"gemini", "gemini", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"audio", "audio", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"rerank", "rerank", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"unknown protocol", "unregistered", PlatformOpenAI, CompositeRouteEndpointResponses},
		{"images", "images", PlatformOpenAI, CompositeRouteEndpointImages},
		{"media on openai", "media", PlatformOpenAI, CompositeRouteEndpointImages},
		{"media on zhipu", "media", PlatformZhipu, CompositeRouteEndpointImages},
		{"media on grok", "media", PlatformGrok, string(OpenAIEndpointCapabilityGrokMediaGeneration)},
		{"embeddings", "embeddings", PlatformOpenAI, string(OpenAIEndpointCapabilityEmbeddings)},
		{"search", "search", PlatformOpenAI, string(OpenAIEndpointCapabilityAlphaSearch)},
		{"live", "live", PlatformOpenAI, string(OpenAIEndpointCapabilityLive)},
		{"realtime", "realtime", PlatformOpenAI, string(OpenAIEndpointCapabilityLive)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := requestPlanGateAccount(scenario.platform, "fixture", CompositeRouteEndpointAny)
			require.Equal(t, scenario.want, requestPlanCatalogEndpoint(plan(scenario.protocol), &account))
		})
	}
	account := requestPlanGateAccount(PlatformOpenAI, "fixture", CompositeRouteEndpointAny)
	require.Equal(t, CompositeRouteEndpointResponses, requestPlanCatalogEndpoint(context.Background(), &account),
		"no plan keeps the legacy Responses evidence")
	require.Equal(t, CompositeRouteEndpointImages, requestPlanCatalogEndpoint(plan("media"), nil),
		"a nil account must not change the media family")
}

// Text, audio and bridge actions keep their previous verdict for the same
// catalog: the shared rule hands them the Responses evidence, whose bridge
// accepts native Chat Completions evidence but never a media family.
func TestRequestPlanTextGateKeepsResponsesEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		spec     GatewayRequestSpec
		platform string
		model    string
		kept     []string
	}{
		{
			name:     "responses",
			spec:     GatewayRequestSpec{Action: "responses", Protocol: "responses", Route: "/v1/responses", Model: "gpt-5.1"},
			platform: PlatformOpenAI,
			model:    "gpt-5.1",
			kept:     []string{CompositeRouteEndpointResponses},
		},
		{
			name:     "chat completions bridge",
			spec:     GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "gpt-5.1"},
			platform: PlatformOpenAI,
			model:    "gpt-5.1",
			kept:     []string{CompositeRouteEndpointResponses},
		},
		{
			name:     "anthropic messages bridge",
			spec:     GatewayRequestSpec{Action: "messages", Protocol: "anthropic", Route: "/v1/messages", Model: "gpt-5.1"},
			platform: PlatformOpenAI,
			model:    "gpt-5.1",
			kept:     []string{CompositeRouteEndpointResponses},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := requestPlanGateContext(scenario.spec)
			account := requestPlanGateAccount(scenario.platform, scenario.model, scenario.kept...)
			require.Empty(t, requestPlanGateReason(ctx, &account, scenario.platform, scenario.model, OpenAIEndpointCapabilityChatCompletions))

			bridged := requestPlanGateAccount(scenario.platform, scenario.model, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointResponses)
			require.Empty(t, requestPlanGateReason(ctx, &bridged, scenario.platform, scenario.model, OpenAIEndpointCapabilityChatCompletions),
				"native Chat Completions evidence supports the Responses bridge")

			revoked := requestPlanGateAccount(scenario.platform, scenario.model, CompositeRouteEndpointImages)
			require.Equal(t, "model_not_supported",
				requestPlanGateReason(ctx, &revoked, scenario.platform, scenario.model, OpenAIEndpointCapabilityChatCompletions),
				"a text request without text-family evidence is still refused")
		})
	}
}

// A planned request whose endpoint family cannot be resolved must not widen the
// gate into a pass-all: the legacy conservative Responses evidence stays the
// basis for the veto.
func TestRequestPlanUnresolvedFamilyStaysConservative(t *testing.T) {
	ctx := requestPlanGateContext(GatewayRequestSpec{Model: "gpt-5.1"})
	for _, endpoints := range [][]string{{"unrelated"}, {CompositeRouteEndpointImages}} {
		account := requestPlanGateAccount(PlatformOpenAI, "gpt-5.1", endpoints...)
		require.Equal(t, "model_not_supported",
			requestPlanGateReason(ctx, &account, PlatformOpenAI, "gpt-5.1", OpenAIEndpointCapabilityChatCompletions),
			"an unresolved family must not admit every account")
	}
}

// Without a plan the gate keeps its historical meaning: the catalog pre-check
// is not applied, only the account-level model list decides.
func TestRequestPlanGateRequiresPlan(t *testing.T) {
	account := requestPlanGateAccount(PlatformOpenAI, "gpt-5.1", "unrelated")
	require.Empty(t, requestPlanGateReason(context.Background(), &account, PlatformOpenAI, "gpt-5.1", OpenAIEndpointCapabilityChatCompletions))
}

// The pre-profit gate must never be stricter than the scheduler's model gate it
// precedes, otherwise accounts that selection would accept are vetoed earlier.
// Both gates read the same plan-to-family rule, so their verdicts must agree for
// every protocol family an OpenAI-compatible catalog can declare.
func TestRequestPlanGateAgreesWithSchedulerModelGate(t *testing.T) {
	families := []string{
		CompositeRouteEndpointResponses,
		CompositeRouteEndpointChatCompletions,
		CompositeRouteEndpointMessages,
		CompositeRouteEndpointImages,
		CompositeRouteEndpointEmbeddings,
		CompositeRouteEndpointGemini,
		"generate_content",
		string(OpenAIEndpointCapabilityLive),
		string(OpenAIEndpointCapabilityAlphaSearch),
		string(OpenAIEndpointCapabilityGrokMediaGeneration),
		"videos",
		"audio_speech",
		"rerank",
		"unrelated",
	}
	for _, scenario := range []struct {
		name     string
		spec     GatewayRequestSpec
		platform string
		model    string
	}{
		{"openai responses", GatewayRequestSpec{Action: "responses", Protocol: "responses", Route: "/v1/responses", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"openai chat completions", GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"openai anthropic bridge", GatewayRequestSpec{Action: "messages", Protocol: "anthropic", Route: "/v1/messages", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"openai gemini bridge", GatewayRequestSpec{Protocol: "gemini", Route: "/v1beta/models/fixture:generateContent", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"openai images", GatewayRequestSpec{Action: "image.generate", Protocol: "images", Route: "/v1/images/generations", Model: "gpt-image-1"}, PlatformOpenAI, "gpt-image-1"},
		{"openai embeddings", GatewayRequestSpec{Action: "embeddings", Protocol: "embeddings", Route: "/v1/embeddings", Model: "text-embedding-3-large"}, PlatformOpenAI, "text-embedding-3-large"},
		{"openai alpha search", GatewayRequestSpec{Action: "alpha_search", Protocol: "search", Route: "/v1/alpha/search", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"openai live", GatewayRequestSpec{Action: "live", Protocol: "live", Route: "/v1/live", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"openai audio", GatewayRequestSpec{Action: "audio.tts", Protocol: "audio", Route: "/v1/audio/speech", Model: "gpt-5.1"}, PlatformOpenAI, "gpt-5.1"},
		{"grok media", GatewayRequestSpec{Action: "image.generate", Protocol: "media", Route: "/v1/images/generations", Model: "grok-imagine-image-quality"}, PlatformGrok, "grok-imagine-image-quality"},
		{"grok chat completions", GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "grok-4.5"}, PlatformGrok, "grok-4.5"},
		{"qianwen images", GatewayRequestSpec{Action: "image.generate", Protocol: "images", Route: "/v1/images/generations", Model: "wanx2.1-t2i-turbo"}, PlatformQianwen, "wanx2.1-t2i-turbo"},
		{"zhipu media", GatewayRequestSpec{Action: "image.generate", Protocol: "media", Route: "/v1/images/generations", Model: "cogview-4"}, PlatformZhipu, "cogview-4"},
		{"minimax media", GatewayRequestSpec{Action: "image.generate", Protocol: "media", Route: "/v1/images/generations", Model: "image-01"}, PlatformMiniMax, "image-01"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := requestPlanGateContext(scenario.spec)
			schedulerGate := &OpenAIGatewayService{}
			for _, family := range families {
				account := requestPlanGateAccount(scenario.platform, scenario.model, family)
				admitted := schedulerGate.supportsOpenAIAccountModel(ctx, &account, scenario.model, OpenAIEndpointCapabilityChatCompletions)
				vetoed := requestPlanGateReason(ctx, &account, scenario.platform, scenario.model, OpenAIEndpointCapabilityChatCompletions) == "model_not_supported"
				require.Equal(t, !admitted, vetoed,
					"pre-profit veto must match the scheduler model gate for catalog family %q", family)
			}
		})
	}
}
