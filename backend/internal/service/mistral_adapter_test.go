//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fixtures are synthetic examples of the official models/chat schema, not live
// account receipts: https://docs.mistral.ai/api/endpoint/models and /chat.
// These tests perform no network, database, Redis or billable upstream actions.
func TestMistralAdapterContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("model cards preserve identity and unknown facts", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_model_cards.json")
		require.NoError(t, err)
		cards, err := parseMistralModelCards(fixture)
		require.NoError(t, err)
		require.Len(t, cards, 3)
		require.Equal(t, []string{"mistral-small-latest"}, cards[0].Aliases)
		require.Equal(t, "ft:mistral-small:synthetic:example", cards[1].ID)
		require.Equal(t, "fine-tuned", cards[1].Type)
		require.True(t, *cards[1].Archived)
		require.NotNil(t, cards[1].Aliases)
		require.Empty(t, cards[1].Aliases)
		require.Nil(t, cards[2].Aliases)
		require.Nil(t, cards[2].Archived)
		metadata := cards[0].metadata(UpstreamModelMetadata{})
		require.EqualValues(t, 32768, metadata.ContextWindow)
		require.Zero(t, metadata.MaxOutputTokens)
		require.Equal(t, "total", metadata.ContextLimitKind)
		require.False(t, *metadata.SupportsVision)
		require.Equal(t, []string{"chat_completions"}, metadata.SupportedMethods)
		previous := UpstreamModelMetadata{ID: cards[1].ID, ProviderProfile: "mistral", MaxOutputTokens: 4096, ContextWindow: 8192}
		metadata = cards[1].metadata(previous)
		require.EqualValues(t, 4096, metadata.MaxOutputTokens)
		require.EqualValues(t, 8192, metadata.ContextWindow)
		require.False(t, *metadata.SupportsFunctionCalling)
		require.Nil(t, metadata.SupportsVision)
		require.Zero(t, cards[2].metadata(previous).MaxOutputTokens, "no cross-ID fallback")
		previous.ProviderProfile = "custom:other"
		require.Zero(t, cards[1].metadata(previous).MaxOutputTokens, "no cross-profile fallback")
		arrayCards, err := parseMistralModelCards([]byte(gjson.GetBytes(fixture, "data").Raw))
		require.NoError(t, err)
		require.Equal(t, cards, arrayCards)
	})
	t.Run("empty lists and invalid or error envelopes differ", func(t *testing.T) {
		for _, valid := range []string{`[]`, `{"object":"list","data":[]}`} {
			cards, err := parseMistralModelCards([]byte(valid))
			require.NoError(t, err)
			require.NotNil(t, cards)
			require.Empty(t, cards)
		}
		for _, invalid := range []string{"", `null`, `{}`, `{"data":null}`, `{"data":{}}`, `{"error":{"message":"secret"},"data":[]}`, `{"detail":"denied","data":[]}`, `{"message":"denied"}`, `[{"id":"ok"},{}]`, `[{"id":"same"},{"id":"same"}]`, `[{"id":"bad","capabilities":{"vision":"false"}}]`, `[{"id":"bad","max_context_length":-1}]`, `[null]`, `[] trailing`} {
			cards, err := parseMistralModelCards([]byte(invalid))
			require.Error(t, err, invalid)
			require.Nil(t, cards)
			require.NotContains(t, err.Error(), "secret")
		}
	})
	t.Run("models request uses validated base and caller context", func(t *testing.T) {
		service := &AccountTestService{cfg: rawChatCompletionsTestConfig()}
		account := mistralContractAccount()
		for _, base := range []string{"", "https://api.mistral.ai", "https://api.mistral.ai/v1/"} {
			account.Credentials["base_url"] = base
			ctx, cancel := context.WithCancel(context.Background())
			request, err := service.buildMistralModelsRequest(ctx, account)
			require.NoError(t, err)
			require.Equal(t, "https://api.mistral.ai/v1/models", request.URL.String())
			require.Equal(t, "Bearer sk-test", request.Header.Get("Authorization"))
			require.Equal(t, http.MethodGet, request.Method)
			cancel()
			require.ErrorIs(t, request.Context().Err(), context.Canceled)
		}
		account.Credentials["base_url"] = "https://relay.example/mistral/v1"
		request, err := service.buildMistralModelsRequest(context.Background(), account)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example/mistral/v1/models", request.URL.String())
		account.Credentials["api_key"] = ""
		_, err = service.buildMistralModelsRequest(context.Background(), account)
		require.Error(t, err)
		_, err = service.buildMistralModelsRequest(context.Background(), nil)
		require.Error(t, err)
	})
	t.Run("chat preparation preserves tools and removes unsupported stream options", func(t *testing.T) {
		body := []byte(`{"model":"ft:mistral:synthetic","messages":[{"role":"assistant","tool_calls":[{"id":"toolABCDE","type":"function","function":{"name":"weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"toolABCDE","content":"sunny"}],"tools":[{"type":"function","function":{"name":"weather"}}],"stream":true,"stream_options":{"include_usage":true}}`)
		original := bytes.Clone(body)
		prepared, err := prepareMistralChatRequest(body)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(prepared, "stream_options").Exists())
		for _, key := range []string{"model", "messages", "tools", "stream"} {
			require.Equal(t, gjson.GetBytes(body, key).Raw, gjson.GetBytes(prepared, key).Raw)
		}
		require.Equal(t, original, body)
		for _, invalid := range []string{`{}`, `{"model":"test","input":"Responses"}`, `{"model":"test","messages":[{}],"tools":[{"type":"web_search"}]}`} {
			_, err := prepareMistralChatRequest([]byte(invalid))
			require.Error(t, err)
		}
	})
	t.Run("shared raw chat preserves tool deltas finish usage and proxy", func(t *testing.T) {
		streamFixture, err := os.ReadFile("testdata/mistral_tools.sse")
		require.NoError(t, err)
		for _, stream := range []bool{false, true} {
			body := []byte(fmt.Sprintf(`{"model":"ft:mistral:synthetic","messages":[{"role":"user","content":"weather"}],"stream":%t}`, stream))
			responseBody := `{"id":"synthetic-mistral-chat","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"toolABCDE","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":17,"completion_tokens":8,"total_tokens":25}}`
			contentType := "application/json"
			if stream {
				responseBody, contentType = string(streamFixture), "text/event-stream"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := mistralContractAccount()
			// Mistral is not registered until task 23. Exercise the shared
			// transport using today's compatible API-key platform identity.
			account.Platform = PlatformOpenAI
			proxyID := int64(45)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8081}
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			result, err := service.forwardAsRawChatCompletions(context.Background(), ginContext, account, body, "")
			require.NoError(t, err)
			require.Equal(t, 17, result.Usage.InputTokens)
			require.Equal(t, 8, result.Usage.OutputTokens)
			require.Equal(t, "ft:mistral:synthetic", result.UpstreamModel)
			require.Equal(t, "http://proxy.example:8081", upstream.lastProxyURL)
			require.Equal(t, "https://api.mistral.ai/v1/chat/completions", upstream.lastReq.URL.String())
			require.Contains(t, recorder.Body.String(), "toolABCDE")
			require.Contains(t, recorder.Body.String(), "tool_calls")
			if stream {
				require.Contains(t, recorder.Body.String(), "data: [DONE]")
				var arguments strings.Builder
				var finishReason string
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					payload := strings.TrimPrefix(line, "data: ")
					_, err := arguments.WriteString(gjson.Get(payload, "choices.0.delta.tool_calls.0.function.arguments").String())
					require.NoError(t, err)
					if tool := gjson.Get(payload, "choices.0.delta.tool_calls.0"); tool.Exists() {
						require.True(t, tool.Get("index").Exists())
						require.EqualValues(t, 0, tool.Get("index").Int())
					}
					if reason := gjson.Get(payload, "choices.0.finish_reason").String(); reason != "" {
						finishReason = reason
					}
				}
				require.JSONEq(t, `{"city":"Paris"}`, arguments.String())
				require.Equal(t, "tool_calls", finishReason)
			} else {
				require.JSONEq(t, responseBody, recorder.Body.String())
			}
		}
	})
	t.Run("missing stream usage remains absent", func(t *testing.T) {
		require.Nil(t, extractCCStreamUsage(`{"choices":[{"delta":{"content":"hello"}}]}`))
		require.Nil(t, extractCCStreamUsage(`{"usage":null}`))
	})
	t.Run("shared stream rejects truncation and propagates cancellation", func(t *testing.T) {
		for _, cancelled := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			body := []byte(`{"model":"mistral-example","messages":[{"role":"user","content":"hello"}],"stream":true}`)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))}}
			if cancelled {
				cancel()
				upstream.err = context.Canceled
			}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
			account := mistralContractAccount()
			account.Platform = PlatformOpenAI
			_, err := service.forwardAsRawChatCompletions(ctx, ginContext, account, body, "")
			require.Error(t, err)
			if cancelled {
				require.Nil(t, upstream.lastReq, "an already canceled request must not be sent")
				require.ErrorIs(t, err, context.Canceled)
			}
			cancel()
		}
	})
	t.Run("shared stream cancels an in flight body", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		stream := newContextBoundBlockingReadCloser([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
		t.Cleanup(stream.forceUnblock)
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &contextBoundHTTPUpstream{body: stream}}
		account := mistralContractAccount()
		account.Platform = PlatformOpenAI
		body := []byte(`{"model":"mistral-example","messages":[{"role":"user","content":"hello"}],"stream":true}`)
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
		finished := make(chan error, 1)
		go func() {
			_, err := service.forwardAsRawChatCompletions(ctx, ginContext, account, body, "")
			finished <- err
		}()
		select {
		case err := <-finished:
			require.Error(t, err)
			require.Error(t, stream.ctx.Err())
		// The shared transport deliberately allows a five-second usage drain
		// after client cancellation. Preserve that policy for Mistral too.
		case <-time.After(8 * time.Second):
			stream.forceUnblock()
			<-finished
			t.Fatal("shared stream failed to propagate cancellation")
		}
	})
	t.Run("HTTP errors are not successful or retried model discovery", func(t *testing.T) {
		for _, status := range []int{401, 403, 404, 429, 500} {
			response := &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"23"}}, Body: io.NopCloser(strings.NewReader(`{"message":"synthetic failure"}`))}
			upstream := &httpUpstreamRecorder{resp: response}
			service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := mistralContractAccount()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			request, err := service.buildMistralModelsRequest(ctx, account)
			require.NoError(t, err)
			actual, err := service.doUpstreamModelsRequest(request, "", account)
			require.NoError(t, err)
			require.Equal(t, status, actual.StatusCode)
			require.Equal(t, "23", actual.Header.Get("Retry-After"))
			require.Len(t, upstream.requests, 1)
			responseBody, err := io.ReadAll(actual.Body)
			require.NoError(t, err)
			cards, err := parseMistralModelsResponse(actual.StatusCode, responseBody)
			require.Nil(t, cards)
			var syncError *UpstreamModelSyncError
			require.ErrorAs(t, err, &syncError)
			require.Equal(t, status, syncError.StatusCode)
			expectedKind := UpstreamModelSyncErrorUpstream
			if status == 401 {
				expectedKind = UpstreamModelSyncErrorCredentials
			}
			if status == 404 {
				expectedKind = UpstreamModelSyncErrorUnsupported
			}
			require.Equal(t, expectedKind, syncError.Kind)
			require.NotContains(t, err.Error(), "synthetic failure")
			require.NoError(t, actual.Body.Close())
			cancel()
		}
		cards, err := parseMistralModelsResponse(http.StatusOK, []byte(`{"data":[]}`))
		require.NoError(t, err)
		require.Empty(t, cards)
		_, err = parseMistralModelsResponse(http.StatusOK, []byte(`{"detail":"synthetic failure"}`))
		require.Error(t, err)
		_, err = parseMistralModelsResponse(http.StatusOK, bytes.Repeat([]byte(" "), int(upstreamModelsBodyLimit)+1))
		require.Error(t, err)
	})
}

func mistralContractAccount() *Account {
	account := rawChatCompletionsTestAccount()
	account.Platform = "mistral" // private fixture; shared registration belongs to integration
	account.Credentials["base_url"] = mistralDefaultBaseURL
	return account
}
