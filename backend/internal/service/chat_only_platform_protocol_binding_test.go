package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Every text cell of the four chat-only platform entries must resolve through
// the real dispatcher: a cell the action matrix binds reaches exactly its own
// adapter, and every remaining cell - including the verified protocol absences -
// fails closed with the local action rejection instead of borrowing another
// platform's trunk.
func TestChatOnlyPlatformTextCellsReachOnlyBoundAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := GatewayActionHandlers{}
	called := ""
	record := func(adapter string) gin.HandlerFunc {
		return func(requestContext *gin.Context) {
			called = adapter
			requestContext.Status(http.StatusNoContent)
		}
	}
	for _, adapter := range []string{
		"openai.chat", "openai.messages", "openai.responses", "openai.count_tokens",
		"openai.embeddings", "openai.images", "openai.video.generate",
		"hunyuan.images", "hunyuan.image.edit", "hunyuan.video.generate", "hunyuan.video.status",
		"hunyuan.video.content", "hunyuan.audio.tts", "hunyuan.audio.stt", "hunyuan.voice",
		"hunyuan.embeddings",
		"volcengine.embeddings", "volcengine.images", "volcengine.video.generate",
		"volcengine.video.edit", "volcengine.video.extend", "volcengine.video.status",
		"volcengine.video.content",
		"qianfan.embeddings", "qianfan.rerank", "qianfan.images", "qianfan.image.edit",
		"qianfan.video.generate", "qianfan.video.status", "qianfan.video.content", "qianfan.video.cancel",
		"qianfan.audio.tts", "qianfan.voice", "qianfan.search",
		"mistral.embeddings", "mistral.audio.stt", "mistral.audio.tts", "mistral.voice",
		"grok.video.generate", "gateway.messages", "gateway.generate_content_compat",
	} {
		handlers[adapter] = record(adapter)
	}
	dispatch := NewGatewayActionDispatch(handlers)
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		router := gin.New()
		router.Use(func(requestContext *gin.Context) {
			requestContext.Set("api_key", &APIKey{Group: &Group{Platform: platform}})
		})
		for _, action := range domain.GatewayActions() {
			router.POST("/probe/"+action, dispatch.Handler(action))
		}
		for _, action := range domain.GatewayActions() {
			called = ""
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/probe/"+action, strings.NewReader(`{"model":"fixture"}`)))
			// The dispatcher resolves the platform ingress bound, so the test reads the
			// same identity the dispatch gate evaluates.
			cell := domain.PlatformAction(domain.ActionIdentity{Platform: platform}, action)
			if cell.Status == domain.ActionImplemented {
				require.Equal(t, cell.Adapter, called, "%s %s must reach its own adapter", platform, action)
				require.Equal(t, http.StatusNoContent, recorder.Code, "%s %s", platform, action)
				continue
			}
			require.Empty(t, called, "%s %s must not borrow another platform adapter", platform, action)
			require.Equal(t, http.StatusBadRequest, recorder.Code, "%s %s must fail closed", platform, action)
			require.Contains(t, recorder.Body.String(), "action_not_implemented", "%s %s", platform, action)
			if cell.Status == domain.ActionHardConflict {
				require.NotEmpty(t, cell.Evidence, "%s %s hard conflict must carry its evidence", platform, action)
			}
		}
	}
}

// A chat-only entry that publishes a native text protocol must declare that
// family for its text models. Otherwise the scheduler's model gate and the
// pre-profit gate read the conservative Responses family for every text request
// and veto the account with model_not_supported before one can be selected, so
// the bound cell would never execute. The family comes from the same platform
// descriptor facts the dispatch gate reads.
func TestChatOnlyPublishedTextProtocolsReachTheSchedulerModelGate(t *testing.T) {
	for _, scenario := range []struct {
		platform string
		families []string
	}{
		{PlatformHunyuan, []string{CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages, CompositeRouteEndpointResponses}},
		{PlatformVolcengine, []string{CompositeRouteEndpointChatCompletions, CompositeRouteEndpointResponses}},
		{PlatformQianfan, []string{CompositeRouteEndpointChatCompletions, CompositeRouteEndpointResponses}},
	} {
		probe := &Account{ID: 1, Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		declared := accountCatalogModelEndpoints(probe, "text-model", UpstreamModelMetadata{})
		require.Equal(t, scenario.families, declared, scenario.platform)

		account := requestPlanGateAccount(scenario.platform, "text-model", declared...)
		gateway := &OpenAIGatewayService{}
		for _, protocol := range []string{"chat_completions", "responses"} {
			ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: protocol, Route: "/fixture", Model: "text-model"})
			require.True(t, gateway.supportsOpenAIAccountModel(ctx, &account, "text-model", OpenAIEndpointCapabilityChatCompletions),
				"%s must stay selectable for a planned %s text request", scenario.platform, protocol)
		}
	}

	// Mistral publishes no native text protocol beyond Chat Completions, so its
	// catalog declares only that family and its entry is judged on the chat trunk
	// it actually serves (see
	// TestChatOnlyPlatformWithoutPublishedTextProtocolStaysSelectable). The
	// declaration itself stays untouched: what a chat-only catalog may claim and
	// which family the plan gate judges are separate rules.
	mistral := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	require.Equal(t, []string{CompositeRouteEndpointChatCompletions},
		accountCatalogModelEndpoints(mistral, "text-model", UpstreamModelMetadata{}))
}

// A chat-only platform that publishes no native text protocol beyond the
// OpenAI-shaped chat trunk declares only that family for its text models, so a
// planned text request must be judged on the chat trunk. Measuring it against
// the conservative Responses default asks for evidence the platform never
// publishes and leaves the entry permanently unselectable.
func TestChatOnlyPlatformWithoutPublishedTextProtocolStaysSelectable(t *testing.T) {
	mistral := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	require.Empty(t, chatOnlyPublishedTextFamilies(mistral),
		"mistral publishes no Responses and no Messages protocol")
	declared := accountCatalogModelEndpoints(mistral, "text-model", UpstreamModelMetadata{})
	require.Equal(t, []string{CompositeRouteEndpointChatCompletions}, declared)

	account := requestPlanGateAccount(PlatformMistral, "text-model", declared...)
	ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "text-model"})
	require.Empty(t, requestPlanGateReason(ctx, &account, PlatformMistral, "text-model", OpenAIEndpointCapabilityChatCompletions),
		"the pre-profit gate must not veto the mistral entry")
	require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "text-model", OpenAIEndpointCapabilityChatCompletions),
		"the scheduler model gate must admit the mistral entry")
	require.Equal(t, CompositeRouteEndpointChatCompletions, openAICompatiblePlanCatalogEndpoint(ctx, &account),
		"a chat-only entry without a native text protocol is judged on its chat trunk")

	// Public Messages and Responses convert to the native Chat family; absent
	// vendor URLs must not reject these equivalent text requests.
	for _, spec := range []GatewayRequestSpec{
		{Action: "responses", Protocol: "responses", Route: "/v1/responses", Model: "text-model"},
		{Action: "messages", Protocol: "anthropic", Route: "/v1/messages", Model: "text-model"},
	} {
		absentCtx := requestPlanGateContext(spec)
		require.Empty(t, requestPlanGateReason(absentCtx, &account, PlatformMistral, "text-model", OpenAIEndpointCapabilityChatCompletions), "public %s converts through the existing chat family", spec.Protocol)
	}
}

// Empty optional metadata does not revoke a selected model. Explicit incompatible
// endpoint declarations and an unselected model remain separate negative cases.
func TestChatOnlyEmptyMetadataPreservesSelectedTextAdapter(t *testing.T) {
	for _, platform := range []string{PlatformMistral, PlatformHunyuan, PlatformVolcengine, PlatformQianfan} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey}
			require.Contains(t, accountCatalogModelEndpoints(account, "text-model", UpstreamModelMetadata{SupportedMethods: []string{}}), "chat_completions")
			for _, endpoints := range [][]string{nil, {}} {
				evidence := requestPlanGateAccount(platform, "text-model", endpoints...)
				ctx := requestPlanGateContext(GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "text-model"})
				require.Empty(t, requestPlanGateReason(ctx, &evidence, platform, "text-model", OpenAIEndpointCapabilityChatCompletions))
				require.Equal(t, "model_not_supported", requestPlanGateReason(ctx, &evidence, platform, "unselected-model", OpenAIEndpointCapabilityChatCompletions))
				incompatible := requestPlanGateAccount(platform, "text-model", "embeddings")
				require.Equal(t, "model_not_supported", requestPlanGateReason(ctx, &incompatible, platform, "text-model", OpenAIEndpointCapabilityChatCompletions))
			}
		})
	}
}

// The chat-trunk default is scoped to chat-only entries. Gemini publishes no
// Responses protocol either, but it is not a chat-only OpenAI-compatible entry,
// so it keeps the conservative Responses default; Antigravity bridges Responses
// and stays on the Responses family through its published protocol.
func TestChatTrunkDefaultStaysScopedToChatOnlyEntries(t *testing.T) {
	ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "text-model"})
	gemini := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey}
	require.False(t, IsChatOnlyPlatform(PlatformGemini))
	require.Empty(t, chatOnlyPublishedTextFamilies(gemini))
	require.Equal(t, CompositeRouteEndpointResponses, openAICompatiblePlanCatalogEndpoint(ctx, gemini),
		"a non chat-only platform keeps the conservative Responses default")
	antigravity := &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}
	require.False(t, IsChatOnlyPlatform(PlatformAntigravity))
	require.Equal(t, CompositeRouteEndpointResponses, openAICompatiblePlanCatalogEndpoint(ctx, antigravity),
		"antigravity bridges Responses and stays on the Responses family")
}

// The chat-trunk default is scoped to chat-only platforms that publish no
// native text protocol. Hunyuan, Volcengine and Qianfan publish a Responses
// protocol, so their planned Responses requests keep reading Responses evidence:
// a model whose only text evidence is the Responses family stays selectable,
// which a blanket chat-trunk default would break.
func TestPublishedResponsesChatOnlyPlatformsKeepTheirTextFamily(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan} {
		account := &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		require.Contains(t, chatOnlyPublishedTextFamilies(account), CompositeRouteEndpointResponses, platform)

		evidence := requestPlanGateAccount(platform, "text-model", CompositeRouteEndpointResponses)
		ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: "responses", Route: "/v1/responses", Model: "text-model"})
		require.Equal(t, CompositeRouteEndpointResponses, openAICompatiblePlanCatalogEndpoint(ctx, &evidence),
			"%s publishes the Responses protocol and keeps the Responses family", platform)
		require.Empty(t, requestPlanGateReason(ctx, &evidence, platform, "text-model", OpenAIEndpointCapabilityChatCompletions),
			"%s must stay selectable when its text evidence is the Responses family", platform)
		require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &evidence, "text-model", OpenAIEndpointCapabilityChatCompletions),
			"%s must stay selectable in the scheduler model gate", platform)
	}
}
