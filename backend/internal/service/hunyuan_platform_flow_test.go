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
	account := &Account{ID: 101, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	require.True(test, account.IsOpenAICompatible())
	require.Equal(test, PlatformHunyuan, NormalizeOpenAICompatiblePlatform(account.Platform))
	discoveryTransport := &httpUpstreamRecorder{}
	discoveryService := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: discoveryTransport}
	discoveryRequest, err := discoveryService.buildUpstreamModelsRequest(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, hunyuanCompatibilityBaseURL+"/models", discoveryRequest.URL.String())
	require.Equal(test, "Bearer fixture-key", discoveryRequest.Header.Get("Authorization"))
	// Official accounts expose candidates without treating them as discovered availability.
	preview, err := discoveryService.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, "platform-candidates", preview.CatalogSource)
	require.Empty(test, preview.DiscoveredModels)
	require.Empty(test, preview.EligibleModels)
	require.False(test, preview.DiscoveryComplete)
	require.Nil(test, preview.Review)
	require.Nil(test, discoveryTransport.lastReq)
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
