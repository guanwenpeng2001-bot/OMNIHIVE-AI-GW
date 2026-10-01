package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The four Mistral data-plane actions implemented by service/mistral_actions.go
// must resolve through the real dispatcher to their own mistral.* adapter. Before
// this task the action matrix left every one of those cells unverified, so the
// dispatch answered action_not_implemented and the whole adapter file was dead
// code no ingress could reach.
func TestMistralActionsReachOnlyTheirBoundAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bound := map[string]string{
		"messages":         "openai.messages",
		"responses":        "openai.responses",
		"generate_content": "gateway.generate_content_compat",
		"embeddings":       "mistral.embeddings",
		"audio.stt":        "mistral.audio.stt",
		"audio.tts":        "mistral.audio.tts",
		"voice":            "mistral.voice",
	}
	called := ""
	handlers := GatewayActionHandlers{}
	for _, adapter := range bound {
		handlers[adapter] = func(requestContext *gin.Context) {
			called = adapter
			requestContext.Status(http.StatusNoContent)
		}
	}
	dispatch := NewGatewayActionDispatch(handlers)
	router := gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set("api_key", &APIKey{Group: &Group{Platform: PlatformMistral}})
	})
	for _, action := range domain.GatewayActions() {
		router.POST("/probe/"+action, dispatch.Handler(action))
	}

	for action, adapter := range bound {
		called = ""
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/probe/"+action, strings.NewReader(`{"model":"mistral-embed"}`)))
		require.Equal(t, adapter, called, "mistral %s must reach its own adapter", action)
		require.Equal(t, http.StatusNoContent, recorder.Code, "mistral %s", action)
	}

	// Every remaining Mistral cell still fails closed with the local action
	// rejection instead of borrowing another platform's adapter: the two verified
	// protocol absences stay hard conflicts, and the cells this task does not
	// implement stay unverified. The chat cell is handled separately below because
	// it binds the shared trunk.
	for _, action := range domain.GatewayActions() {
		if _, ok := bound[action]; ok || action == "chat" {
			continue
		}
		called = ""
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/probe/"+action, strings.NewReader(`{"model":"mistral-embed"}`)))
		require.Empty(t, called, "mistral %s must not borrow another platform's adapter", action)
		require.Equal(t, http.StatusBadRequest, recorder.Code, "mistral %s must fail closed", action)
		require.Contains(t, recorder.Body.String(), "action_not_implemented", "mistral %s", action)
		cell := domain.PlatformAction(domain.ActionIdentity{Platform: PlatformMistral}, action)
		require.NotEqual(t, domain.ActionImplemented, cell.Status, "mistral %s", action)
	}

	// The shared chat trunk the siblings already bind is untouched.
	called = ""
	handlers["openai.chat"] = func(requestContext *gin.Context) {
		called = "openai.chat"
		requestContext.Status(http.StatusNoContent)
	}
	router = gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set("api_key", &APIKey{Group: &Group{Platform: PlatformMistral}})
	})
	router.POST("/probe/chat", NewGatewayActionDispatch(handlers).Handler("chat"))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/probe/chat", strings.NewReader(`{"model":"mistral-large"}`)))
	require.Equal(t, "openai.chat", called, "the mistral chat trunk must stay on the shared adapter")
}

// mistralForwardScenarioForTest drives the forwarder an adapter key wraps, so the
// dispatch below exercises the same call the ingress handler makes.
func mistralForwardScenarioForTest(
	gateway *OpenAIGatewayService,
	action string,
	account *Account,
	requestContext *gin.Context,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	ctx := requestContext.Request.Context()
	switch action {
	case "embeddings":
		return gateway.ForwardMistralEmbeddings(ctx, requestContext, account, body, "")
	case "audio.stt":
		return gateway.ForwardMistralAudioTranscription(ctx, requestContext, account, body, contentType, "")
	case "audio.tts":
		return gateway.ForwardMistralAudioSpeech(ctx, requestContext, account, body, "")
	case "voice":
		return gateway.ForwardMistralVoices(ctx, requestContext, account, "", body, contentType, requestContext.Request.Method)
	default:
		return nil, errors.New("unbound mistral action: " + action)
	}
}

// mistralTranscriptionMultipartBody builds the documented multipart upload the
// transcriptions endpoint expects: a model text field plus the audio part.
func mistralTranscriptionMultipartBody(t *testing.T) ([]byte, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	require.NoError(t, writer.WriteField("model", "voxtral-mini-latest"))
	part, err := writer.CreateFormFile("file", "fixture.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte("RIFF....WAVEfmt "))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes(), writer.FormDataContentType()
}

// TestMistralActionForwardsReachTheDocumentedEndpoints proves the dispatch seam
// end to end against a stub upstream: once the matrix binds the cell, the
// forwarder reaches the endpoint the official specification publishes
// (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21) with the
// account API key as a Bearer token.
func TestMistralActionForwardsReachTheDocumentedEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	transcriptionBody, transcriptionContentType := mistralTranscriptionMultipartBody(t)
	scenarios := []struct {
		name        string
		action      string
		adapter     string
		body        []byte
		contentType string
		response    string
		baseURL     string
		wantURL     string
	}{
		{
			name:        "embeddings",
			action:      "embeddings",
			adapter:     "mistral.embeddings",
			body:        []byte(`{"model":"mistral-embed","input":"hello"}`),
			contentType: "application/json",
			response:    `{"object":"list","data":[],"model":"mistral-embed","usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`,
			wantURL:     "https://api.mistral.ai/v1/embeddings",
		},
		{
			// A relay base URL must be honoured verbatim: the endpoint shape is
			// appended to the account target, never to a hardcoded default.
			name:        "embeddings through a relay base URL",
			action:      "embeddings",
			adapter:     "mistral.embeddings",
			body:        []byte(`{"model":"mistral-embed","input":"hello"}`),
			contentType: "application/json",
			response:    `{"object":"list","data":[],"model":"mistral-embed","usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`,
			baseURL:     "https://relay.example/mistral/v1",
			wantURL:     "https://relay.example/mistral/v1/embeddings",
		},
		{
			name:        "audio transcriptions",
			action:      "audio.stt",
			adapter:     "mistral.audio.stt",
			body:        transcriptionBody,
			contentType: transcriptionContentType,
			response:    `{"model":"voxtral-mini-latest","text":"hello","usage":{"prompt_audio_seconds":12.5,"prompt_tokens":3,"completion_tokens":2}}`,
			wantURL:     "https://api.mistral.ai/v1/audio/transcriptions",
		},
		{
			name:        "audio speech",
			action:      "audio.tts",
			adapter:     "mistral.audio.tts",
			body:        []byte(`{"model":"mistral-tts","input":"hello"}`),
			contentType: "application/json",
			response:    `{"audio_data":"AAAA"}`,
			wantURL:     "https://api.mistral.ai/v1/audio/speech",
		},
		{
			name:        "voices",
			action:      "voice",
			adapter:     "mistral.voice",
			body:        []byte(`{"name":"fixture","gender":"female"}`),
			contentType: "application/json",
			response:    `{"id":"voice-1","name":"fixture"}`,
			wantURL:     "https://api.mistral.ai/v1/audio/voices",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			baseURL := scenario.baseURL
			if baseURL == "" {
				baseURL = "https://api.mistral.ai/v1"
			}
			account := &Account{
				ID: 101, Platform: PlatformMistral, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test", "base_url": baseURL},
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(scenario.response)),
			}}
			gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			reached := ""
			handlers := GatewayActionHandlers{
				scenario.adapter: func(requestContext *gin.Context) {
					reached = scenario.adapter
					body, err := io.ReadAll(requestContext.Request.Body)
					require.NoError(t, err)
					_, err = mistralForwardScenarioForTest(gateway, scenario.action, account, requestContext, body, requestContext.GetHeader("Content-Type"))
					require.NoError(t, err)
				},
			}
			router := gin.New()
			router.Use(func(requestContext *gin.Context) {
				requestContext.Set("api_key", &APIKey{Group: &Group{Platform: PlatformMistral}})
			})
			router.POST("/probe/"+scenario.action, NewGatewayActionDispatch(handlers).Handler(scenario.action))

			request := httptest.NewRequest(http.MethodPost, "/probe/"+scenario.action, bytes.NewReader(scenario.body))
			request.Header.Set("Content-Type", scenario.contentType)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code, "the dispatch must reach %s: %s", scenario.adapter, recorder.Body.String())
			require.Equal(t, scenario.adapter, reached)
			require.Len(t, upstream.requests, 1, "one forward is exactly one upstream call")
			require.Equal(t, scenario.wantURL, upstream.lastReq.URL.String())
			require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, scenario.response, recorder.Body.String(), "the upstream envelope is relayed unchanged")
		})
	}
}

// The chat-only catalog admits an endpoint family exactly when the action matrix
// binds the action that family executes on. Before this task mistral.embeddings
// was unbound, so a Mistral model that declared the embeddings family still got
// no embeddings endpoint and could not be selected for the action.
func TestMistralDeclaredEndpointFamiliesFollowTheActionMatrix(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	for _, action := range []string{"embeddings", "audio.stt", "audio.tts", "voice"} {
		require.True(t, platformActionAdapterBound(account, action), "mistral %s must be admitted by the matrix, not by a platform list", action)
	}

	require.Equal(t,
		[]string{string(OpenAIEndpointCapabilityEmbeddings)},
		accountCatalogModelEndpoints(account, "mistral-embed", UpstreamModelMetadata{SupportedMethods: []string{string(OpenAIEndpointCapabilityEmbeddings)}}))
	require.Equal(t,
		[]string{CompositeRouteEndpointChatCompletions, string(OpenAIEndpointCapabilityEmbeddings)},
		accountCatalogModelEndpoints(account, "mistral-large", UpstreamModelMetadata{SupportedMethods: []string{CompositeRouteEndpointChatCompletions, string(OpenAIEndpointCapabilityEmbeddings)}}))
	// A family whose cell is a verified absence or still unverified stays out.
	require.Empty(t, accountCatalogModelEndpoints(account, "mistral-large", UpstreamModelMetadata{SupportedMethods: []string{
		CompositeRouteEndpointMessages, CompositeRouteEndpointResponses, "generate_content", "images",
	}}))
	// Text models keep the family set they had before this task.
	require.Equal(t,
		[]string{CompositeRouteEndpointChatCompletions},
		accountCatalogModelEndpoints(account, "mistral-large", UpstreamModelMetadata{}))

	// A hosted credential carries no Mistral data-plane credential, so the
	// matrix binds no family for it.
	require.False(t, platformActionAdapterBound(&Account{ID: 2, Platform: PlatformMistral, Type: AccountTypeOAuth}, "embeddings"))

	// The sibling chat-only entries keep their own family admission: this task
	// moved only the Mistral cells, so the embeddings family they already bind is
	// still declared next to the text families their own protocol facts publish.
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan} {
		sibling := &Account{ID: 3, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		declared := accountCatalogModelEndpoints(sibling, "embed-model", UpstreamModelMetadata{SupportedMethods: []string{string(OpenAIEndpointCapabilityEmbeddings)}})
		require.Contains(t, declared, string(OpenAIEndpointCapabilityEmbeddings), platform)
		for _, family := range declared {
			require.NotContains(t, family, "mistral", "%s must not inherit a Mistral family", platform)
		}
	}
}

// The forwarders keep the account contract the adapter keys rely on: a non-Mistral
// platform or a non-API-key credential is rejected before any upstream call, so a
// mis-routed key cannot spend another platform's credential.
func TestMistralForwardersRejectForeignAccounts(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	body := []byte(`{"model":"mistral-embed","input":"hello"}`)
	for _, account := range []*Account{
		{ID: 1, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}},
		{ID: 2, Platform: PlatformMistral, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "sk-test"}},
	} {
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
		_, err := gateway.ForwardMistralEmbeddings(context.Background(), requestContext, account, body, "")
		require.Error(t, err)
	}
	require.Empty(t, upstream.requests, "a foreign account must never reach the upstream")
}

// Text uses Mistral's published chat trunk; transcription requires its own
// audio evidence, even though both enter through OpenAI-compatible handlers.
func TestMistralChatAndAudioPlansSelectOnTheirPublishedFamilies(t *testing.T) {
	gateway := &OpenAIGatewayService{}

	for _, scenario := range []struct {
		protocol string
		route    string
		endpoint string
	}{
		{protocol: "chat", route: "/v1/chat/completions", endpoint: CompositeRouteEndpointChatCompletions},
		{protocol: "audio", route: "/v1/audio/transcriptions", endpoint: "transcriptions"},
	} {
		account := requestPlanGateAccount(PlatformMistral, "voxtral-mini-latest",
			scenario.endpoint)
		ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: scenario.protocol, Route: scenario.route, Model: "voxtral-mini-latest"})
		require.Equal(t, scenario.endpoint, openAICompatiblePlanCatalogEndpoint(ctx, &account), scenario.protocol)
		require.True(t, gateway.supportsOpenAIAccountModel(ctx, &account, "voxtral-mini-latest", ""),
			"a mistral model declaring %s must be selectable for a planned %s request", scenario.endpoint, scenario.protocol)
	}

	// A media plan requires images evidence. Mistral cannot declare that family
	// because its image action has no bound adapter, so it remains unselectable.
	mediaAccount := requestPlanGateAccount(PlatformMistral, "voxtral-mini-latest",
		CompositeRouteEndpointChatCompletions, string(OpenAIEndpointCapabilityEmbeddings))
	mediaCtx := requestPlanGateContext(GatewayRequestSpec{Protocol: "media", Route: "/v1/images/generations", Model: "voxtral-mini-latest"})
	require.Equal(t, CompositeRouteEndpointImages, openAICompatiblePlanCatalogEndpoint(mediaCtx, &mediaAccount),
		"the media branch requires the actual media family even for chat-only entries")
	require.False(t, gateway.supportsOpenAIAccountModel(mediaCtx, &mediaAccount, "voxtral-mini-latest", ""))
	require.NotEqual(t, domain.ActionImplemented,
		domain.PlatformAction(domain.ActionIdentity{Platform: PlatformMistral}, "image.generate").Status,
		"mistral publishes no image endpoint, so no working media cell is gated out above")

	// The embeddings plan asks for the family the Mistral catalog declares, so it
	// passes the same gate: that is why embeddings is reachable end to end.
	embeddingsAccount := requestPlanGateAccount(PlatformMistral, "mistral-embed", string(OpenAIEndpointCapabilityEmbeddings))
	embeddingsCtx := requestPlanGateContext(GatewayRequestSpec{Protocol: "embeddings", Route: "/v1/embeddings", Model: "mistral-embed"})
	require.Equal(t, string(OpenAIEndpointCapabilityEmbeddings), openAICompatiblePlanCatalogEndpoint(embeddingsCtx, &embeddingsAccount))
	require.True(t, gateway.supportsOpenAIAccountModel(embeddingsCtx, &embeddingsAccount, "mistral-embed", OpenAIEndpointCapabilityEmbeddings))

	// Native text protocols on sibling platforms are not audio evidence.
	for _, platform := range []string{PlatformHunyuan, PlatformQianfan} {
		sibling := requestPlanGateAccount(platform, "text-model",
			CompositeRouteEndpointChatCompletions, CompositeRouteEndpointResponses)
		siblingCtx := requestPlanGateContext(GatewayRequestSpec{Protocol: "audio", Route: "/v1/audio/speech", Model: "text-model"})
		require.Equal(t, "speech", openAICompatiblePlanCatalogEndpoint(siblingCtx, &sibling), platform)
		require.False(t, gateway.supportsOpenAIAccountModel(siblingCtx, &sibling, "text-model", ""), platform)
	}
}
