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

func TestQianfanPlatformFlow(test *testing.T) {
	account := &Account{ID: 101, Platform: PlatformQianfan, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	require.Equal(test, PlatformQianfan, NormalizeOpenAICompatiblePlatform(account.Platform))
	require.True(test, account.IsOpenAICompatible())
	require.False(test, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	test.Run("complete empty malformed and partial discovery", func(test *testing.T) {
		for _, scenario := range []struct {
			name, body string
			valid      bool
			count      int
		}{
			{"rich", `{"object":"list","data":[{"id":"ernie-fixture","context_length":32768,"max_completions_tokens":8192,"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`, true, 1},
			{"empty", `{"object":"list","data":[]}`, true, 0},
			{"partial", `{"object":"list","data":[{"id":"ernie-fixture"}],"has_more":true}`, false, 0},
			{"malformed", `{"object":"list","data":[{}]}`, false, 0},
		} {
			test.Run(scenario.name, func(test *testing.T) {
				transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(scenario.body))}}
				service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
				models, raw, err := service.fetchUpstreamModelList(context.WithValue(context.Background(), catalogDiscoveryKey{}, true), account)
				if !scenario.valid {
					require.Error(test, err)
					require.Nil(test, models)
					return
				}
				require.NoError(test, err)
				require.Len(test, models, scenario.count)
				require.JSONEq(test, scenario.body, string(raw))
				if scenario.name == "rich" {
					_, metadata, parseErr := extractUpstreamModelCatalog(raw, false, PlatformQianfan)
					require.NoError(test, parseErr)
					require.EqualValues(test, 8192, metadata["ernie-fixture"].MaxOutputTokens)
					require.Equal(test, []string{"text"}, metadata["ernie-fixture"].InputModalities)
				}
				require.Equal(test, qianfanDefaultBaseURL+"/models", transport.lastReq.URL.String())
				require.Equal(test, "Bearer fixture-key", transport.lastReq.Header.Get("Authorization"))
			})
		}
	})
	test.Run("shared raw chat transport", func(test *testing.T) {
		body := []byte(`{"model":"ernie-fixture","messages":[{"role":"user","content":"hello"}]}`)
		transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`))}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, "", "")
		require.NoError(test, err)
		require.Equal(test, 5, result.Usage.InputTokens)
		require.Equal(test, qianfanDefaultBaseURL+"/chat/completions", transport.lastReq.URL.String())
	})
}
