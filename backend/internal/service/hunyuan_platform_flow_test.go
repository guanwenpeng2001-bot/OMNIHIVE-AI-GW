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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHunyuanPlatformFlow(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	account := &Account{ID: 101, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	require.True(test, account.IsOpenAICompatible())
	require.Equal(test, PlatformHunyuan, NormalizeOpenAICompatiblePlatform(account.Platform))
	discoveryTransport := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[{"id":"native-listed"}]}`)}
	discoveryService := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: discoveryTransport}
	discoveryRequest, err := discoveryService.buildUpstreamModelsRequest(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, hunyuanCompatibilityBaseURL+"/models", discoveryRequest.URL.String())
	require.Equal(test, "Bearer fixture-key", discoveryRequest.Header.Get("Authorization"))
	// The default PAYG profile already declares live discovery; only returned IDs are discovered.
	preview, err := discoveryService.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, "live", preview.CatalogSource)
	require.Equal(test, []string{"native-listed"}, preview.DiscoveredModels)
	require.True(test, preview.DiscoveryComplete)
	require.NotNil(test, preview.Review)
	require.Equal(test, hunyuanCompatibilityBaseURL+"/models", discoveryTransport.lastReq.URL.String())
	for _, scenario := range []struct {
		name, body, response string
		status               int
		valid, sent          bool
	}{
		{"nonstream", `{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`, `{"choices":[{"message":{"content":"hello","reasoning_content":"thought"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`, 200, true, true},
		{"unknown usage", `{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`, `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`, 200, false, true},
		{"stream without usage", `{"model":"fixture","messages":[{"role":"user","content":"hello"}],"stream":true}`, `{}`, 200, false, true},
		{"rate limited", `{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`, `{"error":{"code":"unverified"}}`, 429, false, true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: scenario.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(scenario.response))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			body := []byte(scenario.body)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, "", "")
			if scenario.valid {
				require.NoError(test, err)
				require.Equal(test, 7, result.Usage.InputTokens)
				require.Contains(test, recorder.Body.String(), "reasoning_content")
			} else {
				require.Error(test, err)
				require.Nil(test, result, "no trusted-zero settlement")
			}
			require.Equal(test, scenario.sent, transport.lastReq != nil)
			if scenario.sent {
				require.Equal(test, hunyuanCompatibilityBaseURL+"/chat/completions", transport.lastReq.URL.String())
			}
		})
	}
}

func TestHunyuanTokenHubDiscovery(t *testing.T) {
	account := &Account{ID: 101, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	for _, scenario := range []struct {
		name, body string
		status     int
		valid      bool
	}{
		{"online only", `{"data":[{"id":"hy3","status":"online","name":"Hy3"},{"id":"old","status":"pre-offline"}]}`, 200, true},
		{"empty", `{"data":[]}`, 200, false},
		{"offline only", `{"data":[{"id":"old","status":"pre-offline"}]}`, 200, false},
		{"missing status", `{"data":[{"id":"hy3"}]}`, 200, false},
		{"malformed", `{`, 200, false},
		{"invalid key", `{"error":{"code":"401002"}}`, 401, false},
		{"quota", `{"error":{"code":"402001"}}`, 402, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: scenario.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(scenario.body))}}
			service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			models, body, err := service.fetchHunyuanModels(context.Background(), account)
			require.Equal(t, http.MethodGet, transport.lastReq.Method)
			require.Equal(t, hunyuanCompatibilityBaseURL+"/models", transport.lastReq.URL.String())
			require.Equal(t, "Bearer fixture-key", transport.lastReq.Header.Get("Authorization"))
			if scenario.valid {
				require.NoError(t, err)
				require.Equal(t, []string{"hy3"}, models)
				require.JSONEq(t, `{"data":[{"id":"hy3","status":"online","name":"Hy3"}]}`, string(body))
			} else {
				require.Error(t, err)
				require.Nil(t, models)
				require.Nil(t, body)
			}
		})
	}
}

func TestHunyuanTokenHubStreamUsage(t *testing.T) {
	const content = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\",\"reasoning_content\":\"thought\"}}]}\n\n"
	const usage = "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\n"
	const done = "data: [DONE]\n\n"
	for _, scenario := range []struct {
		name, wire      string
		valid, observed bool
	}{
		{"tail usage", content + usage + done, true, true},
		{"missing usage", content + done, false, false},
		{"partial usage", content + "data: {\"usage\":{\"prompt_tokens\":7}}\n\n" + done, false, false},
		{"error after usage", content + usage + "data: {\"error\":{\"code\":\"502001\"}}\n\n", false, true},
		{"usage retained after empty usage", content + usage + "data: {\"usage\":null}\n\n" + done, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(scenario.wire))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			body := []byte(`{"model":"hy3","messages":[{"role":"user","content":"hello"}],"stream":true}`)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			account := &Account{ID: 101, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
			result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, "", "")
			if scenario.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if scenario.observed {
				require.NotNil(t, result)
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 3, result.Usage.OutputTokens)
			} else {
				require.Nil(t, result)
			}
			require.NotNil(t, transport.lastReq)
			require.Equal(t, "Bearer fixture-key", transport.lastReq.Header.Get("Authorization"))
			require.Equal(t, hunyuanCompatibilityBaseURL+"/chat/completions", transport.lastReq.URL.String())
			require.Contains(t, recorder.Body.String(), "reasoning_content")
		})
	}
}

// The TokenHub text protocol cells forward on the shared trunk to the paths the
// first-party documentation fixes: POST {base}/responses with Bearer on the
// Responses compatibility mode, and POST {host}/v1/messages with x-api-key on the
// Anthropic path (1823/133813, 1823/130078, 1823/130079).
func TestHunyuanResponsesAndMessagesForwardsUseTheDocumentedTokenHubPaths(test *testing.T) {
	test.Run("responses", func(test *testing.T) {
		body := []byte(`{"model":"hy3","input":"hello"}`)
		transport := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":7,"output_tokens":3}}`)),
		}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		SetOpenAIClientTransport(requestContext, OpenAIClientTransportHTTP)
		account := &Account{ID: 101, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
		result, err := gateway.Forward(context.Background(), requestContext, account, body)
		require.NoError(test, err)
		require.NotNil(test, result)
		require.Equal(test, hunyuanCompatibilityBaseURL+"/responses", transport.lastReq.URL.String())
		require.Equal(test, "Bearer fixture-key", transport.lastReq.Header.Get("Authorization"))
	})
	test.Run("protocol selection", func(test *testing.T) {
		// Publishing the TokenHub protocols must not move the default path of an
		// existing Hunyuan account: without an explicit api_protocol credential the
		// OpenAI-shaped chat trunk stays the effective protocol, and an adaptive
		// account resolves the Anthropic base to the same TokenHub site.
		plain := &Account{Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
		require.Equal(test, APIProtocolChatCompletions, plain.GetAPIProtocol())
		adaptive := &Account{Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "api_protocol": APIProtocolAdaptive}}
		require.Equal(test, APIProtocolAdaptive, adaptive.GetAPIProtocol())
		require.Equal(test, hunyuanCompatibilityBaseURL, adaptive.GetAnthropicProtocolBaseURL())
	})
	test.Run("messages", func(test *testing.T) {
		body := []byte(`{"model":"hy3","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
		transport := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"hy3","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":7,"output_tokens":3}}`)),
		}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		account := &Account{ID: 101, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "api_protocol": APIProtocolAnthropic}}
		result, err := gateway.ForwardAsAnthropic(context.Background(), requestContext, account, body, "", "")
		require.NoError(test, err)
		require.NotNil(test, result)
		require.Equal(test, hunyuanCompatibilityBaseURL+"/messages", transport.lastReq.URL.String())
		require.Equal(test, "fixture-key", transport.lastReq.Header.Get("x-api-key"))
		require.Empty(test, transport.lastReq.Header.Get("Authorization"))
	})
}
