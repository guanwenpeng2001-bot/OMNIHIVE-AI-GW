package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayActionDispatchPreservesTargetsAndRejectsMissingAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformCursor, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformOpenAI, PlatformMistral} {
		require.Equal(t, platform, NormalizeOpenAICompatiblePlatform(platform))
		router := gin.New()
		calls := 0
		router.Use(func(requestContext *gin.Context) {
			requestContext.Set("api_key", &APIKey{Group: &Group{Platform: platform}})
		}, GatewayActionRequirement)
		callback := func(requestContext *gin.Context) {
			calls++
			require.Equal(t, platform, GatewayTargetPlatform(requestContext))
			requestContext.Status(http.StatusNoContent)
		}
		dispatch := NewGatewayActionDispatch(GatewayActionHandlers{"gateway.chat": callback, "openai.chat": callback})
		router.POST("/chat/completions", dispatch.Handler("chat"))
		router.POST("/images/generations", dispatch.Handler("image.generate"))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"unknown-private-model"}`)))
		require.Equal(t, http.StatusNoContent, recorder.Code)
		require.Equal(t, 1, calls)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/images/generations", strings.NewReader(`{"model":"unknown-private-model"}`)))
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Equal(t, 1, calls)
	}
}

func TestGatewayActionEvidenceRecheckedAtSelectionAndSend(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "private-model")
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Action: "chat", Route: "/v1/chat/completions", Protocol: "chat_completions", Model: "private-model"})
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	openAI := &OpenAIGatewayService{}
	require.True(t, openAI.supportsOpenAIAccountModel(ctx, &account, "private-model", OpenAIEndpointCapabilityChatCompletions))
	require.True(t, requestAccountCatalogSupports(ctx, &account, "private-model", "chat_completions"))
	repo := &forwardCatalogRepo{current: &account}
	require.NoError(t, validateAccountForwardModel(ctx, repo, &account, "private-model", "chat_completions"))
	snapshot, _ := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	snapshot.EligibleModels[0].Endpoints = []string{"embeddings"}
	require.False(t, openAI.supportsOpenAIAccountModel(ctx, &account, "private-model", OpenAIEndpointCapabilityChatCompletions))
	require.False(t, requestAccountCatalogSupports(ctx, &account, "private-model", "chat_completions"))
	require.ErrorIs(t, validateAccountForwardModel(ctx, repo, &account, "private-model", "chat_completions"), ErrAccountModelUnsupported)
	account.Extra = nil
	require.False(t, openAI.supportsOpenAIAccountModel(ctx, &account, "gpt-5.1", OpenAIEndpointCapabilityChatCompletions), "static candidate is not account evidence")
}

func TestGatewayInputUnknownAndExplicitDenial(t *testing.T) {
	disabled := false
	requirement := parseGatewayInputRequirement([]byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/image"}}]}],"tools":[{"type":"function","function":{"name":"lookup"}}],"max_tokens":20,"duration":9,"n":3}`))
	require.True(t, requirement.Image)
	require.True(t, requirement.Tools)
	require.True(t, requirement.SupportedBy(UpstreamModelMetadata{}), "unknown must not become false")
	require.False(t, requirement.SupportedBy(UpstreamModelMetadata{SupportsVision: &disabled}))
	require.False(t, requirement.SupportedBy(UpstreamModelMetadata{SupportsFunctionCalling: &disabled}))
	require.False(t, requirement.SupportedBy(UpstreamModelMetadata{MaxOutputTokens: 10}))
	require.False(t, requirement.SupportedBy(UpstreamModelMetadata{TaskLimits: map[string]json.RawMessage{"max_duration_seconds": json.RawMessage(`8`)}}))
	require.True(t, requirement.SupportedBy(UpstreamModelMetadata{InputModalities: []string{"text"}, InputModalitiesStatus: "partial"}))
	require.False(t, requirement.SupportedBy(UpstreamModelMetadata{InputModalities: []string{"text"}, InputModalitiesStatus: "complete"}))
	require.False(t, parseGatewayInputRequirement([]byte(`{"tools":[{"type":"web_search"}]}`)).Tools, "provider built-in tools are not function calling")
}

func TestGatewayInputAdmissionUsesVisibleEvidenceAndRetainsBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := catalogEvidenceAccount(1, PlatformOpenAI, "private-model")
	disabled := false
	for _, vision := range []*bool{&disabled, nil} {
		account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"private-model": {ID: "private-model", ProviderProfile: resolveCapabilityProfile(&account), SupportsVision: vision}}})
		gateway := &GatewayService{accountRepo: &forwardCatalogRepo{current: &account}}
		router := gin.New()
		called := false
		payload := `{"model":"private-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/image"}}]}]}`
		router.Use(func(requestContext *gin.Context) {
			requestContext.Set("api_key", &APIKey{Group: &Group{Platform: PlatformOpenAI}})
		}, gateway.ActionRequirement)
		router.POST("/chat/completions", func(requestContext *gin.Context) {
			called = true
			body, err := io.ReadAll(requestContext.Request.Body)
			require.NoError(t, err)
			require.Equal(t, payload, string(body))
			ctx, plan := gateway.CaptureRequestPlan(requestContext.Request.Context(), GatewayRequestSpec{Model: "private-model", Route: "/chat/completions"})
			require.Equal(t, "chat", plan.Snapshot().Action)
			require.True(t, AccountSupportsResolvedModel(ctx, &account, "private-model", AccountModelBindingOptions{Endpoint: "chat_completions"}))
			requestContext.Status(http.StatusNoContent)
		})
		request := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if vision != nil {
			require.False(t, called)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), "model_input_unsupported")
		} else {
			require.True(t, called)
			require.Equal(t, http.StatusNoContent, recorder.Code)
		}
	}
}

func TestGatewayActionCredentialsDoNotNeedDevelopmentUnlock(t *testing.T) {
	for _, platform := range []string{PlatformVolcengine, PlatformQianfan, PlatformHunyuan, PlatformMistral, PlatformAnthropic} {
		account := catalogEvidenceAccount(1, platform, "fixture")
		action, endpoint := "chat", "chat_completions"
		if platform == PlatformAnthropic {
			account.Type = AccountTypeBedrock
			action, endpoint = "messages", "messages"
		}
		account.Credentials["api_key"] = "fixture-credential"
		snapshot, _ := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
		snapshot.CapabilityStatus = "incomplete"
		ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Action: action}))
		require.True(t, AccountSupportsResolvedModel(ctx, &account, "fixture", AccountModelBindingOptions{Endpoint: endpoint, FinalModel: "fixture"}), platform)
	}
}

func TestGatewayInputRequirementsRecheckedBeforeRetry(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "private-model")
	ctx := context.WithValue(context.Background(), gatewayInputRequirementKey{}, GatewayInputRequirement{Image: true})
	ctx = WithGatewayRequestPlan(ctx, NewGatewayRequestPlan(GatewayRequestSpec{Action: "chat", Model: "private-model"}))
	repo := &forwardCatalogRepo{current: &account}
	require.NoError(t, validateAccountForwardModel(ctx, repo, &account, "private-model", "chat_completions"), "unknown input capability passes")
	denied := false
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"private-model": {ProviderProfile: resolveCapabilityProfile(&account), SupportsVision: &denied}}})
	require.ErrorIs(t, validateAccountForwardModel(ctx, repo, &account, "private-model", "chat_completions"), ErrAccountModelUnsupported, "retry must revalidate current input facts")
}
