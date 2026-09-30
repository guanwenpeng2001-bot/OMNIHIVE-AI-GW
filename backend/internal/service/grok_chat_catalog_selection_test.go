//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGrokChatCatalogSelectionUsesNativeEndpoint(test *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, model := range []string{"composer-2.5", "grok-composer-2.5-fast", "grok-4.5", "grok-4.6"} {
			test.Run(accountType+"/"+model, func(test *testing.T) {
				account := legacyCatalogSeedAccount(PlatformGrok)
				account.Type = accountType
				account.Credentials["model_mapping"] = map[string]any{model: model}
				seed := account.Extra[UpstreamModelCatalogExtraKey].(map[string]any)
				seed["eligible_models"] = []AccountCatalogModel{{ID: model, EvidenceKind: modelEvidenceLegacyMappingUpgrade, EvidenceObservedAt: seed["observed_at"].(string)}}
				catalog := account.EffectiveUpstreamModelCatalog()
				require.NotNil(test, catalog)
				require.Contains(test, catalog.EligibleModels[0].Endpoints, CompositeRouteEndpointChatCompletions)
				if accountType == AccountTypeOAuth {
					require.Contains(test, catalog.EligibleModels[0].Endpoints, CompositeRouteEndpointResponses)
				} else {
					require.NotContains(test, catalog.EligibleModels[0].Endpoints, CompositeRouteEndpointResponses)
				}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: model})
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				repository := &forwardCatalogRepo{current: &account}
				plan.catalogRepo = repository
				gateway := &OpenAIGatewayService{accountRepo: repository}
				require.True(test, gateway.supportsOpenAIAccountModel(ctx, &account, model, OpenAIEndpointCapabilityChatCompletions))
				require.Empty(test, requestPlanGateReason(ctx, &account, PlatformGrok, model, OpenAIEndpointCapabilityChatCompletions))
				require.NoError(test, validateAccountForwardModel(ctx, repository, &account, model, CompositeRouteEndpointChatCompletions))
				if accountType == AccountTypeOAuth {
					require.NoError(test, validateAccountForwardModel(ctx, repository, &account, model, CompositeRouteEndpointResponses))
				} else {
					require.ErrorIs(test, validateAccountForwardModel(ctx, repository, &account, model, CompositeRouteEndpointResponses), ErrAccountModelUnsupported)
				}
			})
		}
	}
}

func TestGrokComposerChatPlanReachesNativeUpstream(test *testing.T) {
	gin.SetMode(gin.TestMode)
	account := healthyGrokOAuthGatewayTestAccount(51, "access-token")
	seedAccount := legacyCatalogSeedAccount(PlatformGrok)
	account.Extra = seedAccount.Extra
	account.Credentials["model_mapping"] = map[string]any{"composer-2.5": "composer-2.5", "grok-composer-2.5-fast": "grok-composer-2.5-fast"}
	seed := account.Extra[UpstreamModelCatalogExtraKey].(map[string]any)
	seed["eligible_models"] = []AccountCatalogModel{
		{ID: "composer-2.5", EvidenceKind: modelEvidenceLegacyMappingUpgrade, EvidenceObservedAt: seed["observed_at"].(string)},
		{ID: "grok-composer-2.5-fast", EvidenceKind: modelEvidenceLegacyMappingUpgrade, EvidenceObservedAt: seed["observed_at"].(string)},
	}
	repository := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_composer","object":"chat.completion","model":"grok-4.5-build","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)),
	}}
	gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repository, grokTokenProvider: NewGrokTokenProvider(repository, nil)}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "composer-2.5"})
	installUnlimitedTPMFixture(plan)
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	body := []byte(`{"model":"composer-2.5","messages":[{"role":"user","content":"Reply OK."}],"max_tokens":16,"stream":false}`)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
	ginContext.Request.Header.Set("Content-Type", "application/json")
	require.True(test, gateway.supportsOpenAIAccountModel(ctx, account, "composer-2.5", OpenAIEndpointCapabilityChatCompletions))
	result, err := gateway.ForwardAsChatCompletions(ctx, ginContext, account, body, "", "")
	require.NoError(test, err)
	require.NotNil(test, result)
	require.Equal(test, xai.DefaultCLIBaseURL+"/chat/completions", upstream.lastReq.URL.String())
	require.Equal(test, "composer-2.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(test, "OK", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
}

func TestGrokChatCatalogSelectionPreservesEndpointDenials(test *testing.T) {
	for _, endpoints := range [][]string{{CompositeRouteEndpointResponses}, {"grok_media_generation"}} {
		account := requestPlanGateAccount(PlatformGrok, "grok-composer-2.5-fast", endpoints...)
		account.Type = AccountTypeOAuth
		ctx := requestPlanGateContext(GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "grok-composer-2.5-fast"})
		require.False(test, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "grok-composer-2.5-fast", OpenAIEndpointCapabilityChatCompletions))
		require.Equal(test, "model_not_supported", requestPlanGateReason(ctx, &account, PlatformGrok, "grok-composer-2.5-fast", OpenAIEndpointCapabilityChatCompletions))
	}
	account := requestPlanGateAccount(PlatformGrok, "grok-composer-2.5-fast", CompositeRouteEndpointChatCompletions)
	account.Type = AccountTypeOAuth
	for _, protocol := range []string{"responses", "anthropic"} {
		ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: protocol, Model: "grok-composer-2.5-fast"})
		require.Equal(test, CompositeRouteEndpointResponses, openAICompatiblePlanCatalogEndpoint(ctx, &account))
		require.False(test, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "grok-composer-2.5-fast", OpenAIEndpointCapabilityChatCompletions))
	}
}

func TestGrokChatCatalogSelectionWithoutEndpointMetadata(test *testing.T) {
	for _, model := range []string{"grok-composer-2.5-fast", "selected-private-model"} {
		test.Run(model, func(test *testing.T) {
			for _, endpoints := range [][]string{nil, {}} {
				account := requestPlanGateAccount(PlatformGrok, model, endpoints...)
				if model == "selected-private-model" {
					account.Credentials["model_mapping"] = map[string]string{model: model}
				}
				account.Type = AccountTypeOAuth
				ctx := requestPlanGateContext(GatewayRequestSpec{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: model})
				require.True(test, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, model, OpenAIEndpointCapabilityChatCompletions))
				require.Empty(test, requestPlanGateReason(ctx, &account, PlatformGrok, model, OpenAIEndpointCapabilityChatCompletions))
				repository := &forwardCatalogRepo{current: &account}
				require.NoError(test, validateAccountForwardModel(ctx, repository, &account, model, CompositeRouteEndpointChatCompletions))
				require.ErrorIs(test, validateAccountForwardModel(ctx, repository, &account, "unselected-model", CompositeRouteEndpointChatCompletions), ErrAccountModelUnsupported)
			}
		})
	}
}
