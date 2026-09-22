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
		"qianfan.video.generate", "qianfan.video.status", "qianfan.video.content",
		"qianfan.audio.tts", "qianfan.voice", "qianfan.search",
		"mistral.embeddings", "mistral.audio.stt", "mistral.audio.tts", "mistral.voice",
		"grok.video.generate", "gateway.messages",
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
	// catalog declares only that family. Its planned text requests are still judged
	// on the conservative Responses family, which this catalog cannot declare, so
	// the entry stays unselectable for text today. That pre-existing gap (the chat
	// trunk never becomes the evidence family for a chat-only platform without a
	// Responses protocol) is reported with this task instead of being changed here,
	// because the fix alters the shared Responses-bridge rule for a platform whose
	// protocol cells this task must not extend.
	mistral := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	require.Equal(t, []string{CompositeRouteEndpointChatCompletions},
		accountCatalogModelEndpoints(mistral, "text-model", UpstreamModelMetadata{}))
}
