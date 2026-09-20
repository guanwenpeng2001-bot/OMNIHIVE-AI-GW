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
		{"unverified stream", `{"model":"fixture","messages":[{"role":"user","content":"hello"}],"stream":true}`, `{}`, 200, false, false},
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
