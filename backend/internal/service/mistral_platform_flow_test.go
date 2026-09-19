//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMistralPlatformFlow(test *testing.T) {
	account := &Account{ID: 101, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	require.Equal(test, PlatformMistral, NormalizeOpenAICompatiblePlatform(account.Platform))
	require.True(test, account.IsOpenAICompatible())
	test.Run("model cards through public discovery", func(test *testing.T) {
		responseBody := `{"data":[{"id":"chat-fixture","max_context_length":32768,"capabilities":{"completion_chat":true,"vision":false,"function_calling":true}},{"id":"embedding-fixture","capabilities":{"completion_chat":false}}]}`
		transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody))}}
		service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		models, body, err := service.fetchUpstreamModelList(context.WithValue(context.Background(), catalogDiscoveryKey{}, true), account)
		require.NoError(test, err)
		require.Len(test, models, 2)
		_, metadata, err := extractUpstreamModelCatalog(body, false, PlatformMistral)
		require.NoError(test, err)
		require.EqualValues(test, 32768, metadata["chat-fixture"].ContextWindow)
		require.NotNil(test, metadata["chat-fixture"].SupportsVision)
		require.False(test, *metadata["chat-fixture"].SupportsVision)
		require.Equal(test, []string{}, metadata["embedding-fixture"].SupportedMethods)
		require.Equal(test, mistralDefaultBaseURL+"/models", transport.lastReq.URL.String())
	})
	for _, stream := range []bool{false, true} {
		for _, scenario := range []struct {
			name, usage string
			known       bool
		}{
			{"missing", "", false},
			{"null", `,"usage":null`, false},
			{"partial", `,"usage":{"prompt_tokens":1}`, false},
			{"negative", `,"usage":{"prompt_tokens":-1,"completion_tokens":1}`, false},
			{"known zero", `,"usage":{"prompt_tokens":0,"completion_tokens":0}`, true},
			{"known positive", `,"usage":{"prompt_tokens":7,"completion_tokens":3}`, true},
		} {
			test.Run(fmt.Sprintf("stream=%t/%s", stream, scenario.name), func(test *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"chat-fixture","messages":[{"role":"user","content":"hello"}],"stream":%t,"stream_options":{"include_usage":true}}`, stream))
				responseBody := `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]` + scenario.usage + `}`
				if stream {
					responseBody = "data: " + `{"choices":[{"delta":{"content":"hello"},"finish_reason":"stop"}]` + scenario.usage + "}\n\ndata: [DONE]\n\n"
				}
				transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
				requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, "", "")
				if scenario.known {
					require.NoError(test, err)
					require.NotNil(test, result)
				} else {
					require.ErrorContains(test, err, "unknown")
					require.Nil(test, result, "unknown usage must never produce a trusted-zero settlement result")
				}
				require.NotNil(test, transport.lastReq)
				require.Equal(test, mistralDefaultBaseURL+"/chat/completions", transport.lastReq.URL.String())
				require.False(test, gjson.GetBytes(transport.lastBody, "stream_options").Exists(), "remove after shared stream usage injection")
			})
		}
	}
}
