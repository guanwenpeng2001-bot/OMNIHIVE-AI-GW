//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func volcengineTestAccount() *Account {
	return &Account{ID: 101, Platform: "volcengine", Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "inference-test-key", "access_key": "management-test-key", "secret_key": "management-test-secret"}}
}

// These are constructed compatibility fixtures, not captured vendor responses.
// They verify reuse of the core CC implementation without upgrading the frozen
// contract's missing usage/error/SSE evidence to vendor-verified behavior.
func TestVolcengineAdapterContract(test *testing.T) {
	test.Run("exact account targets and tools survive preparation", func(test *testing.T) {
		service := &OpenAIGatewayService{}
		body := []byte(`{"model":"public-alias","messages":[{"role":"assistant","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_weather","content":"sunny"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}],"parallel_tool_calls":true,"thinking":{"type":"enabled"}}`)
		original := string(body)
		for _, target := range []string{"ep-account-deployment", "doubao-foundation-example", "public-alias", "deepseek-example", "qwen-example"} {
			test.Run(target, func(test *testing.T) {
				prepared, err := service.prepareVolcengineChatRequest(volcengineTestAccount(), body, target)
				require.NoError(test, err)
				require.Equal(test, target, gjson.GetBytes(prepared.Body, "model").String())
				require.Equal(test, volcengineRuntimeBaseURL+"/chat/completions", prepared.TargetURL)
				require.Equal(test, "inference-test-key", prepared.APIKey)
				for _, field := range []string{"messages", "tools", "parallel_tool_calls", "thinking"} {
					require.Equal(test, gjson.GetBytes(body, field).Raw, gjson.GetBytes(prepared.Body, field).Raw)
				}
				require.False(test, gjson.GetBytes(prepared.Body, "stream_options").Exists())
			})
		}
		require.Equal(test, original, string(body))
	})

	test.Run("runtime URL and shared allowlist", func(test *testing.T) {
		for _, baseURL := range []string{volcengineRuntimeBaseURL, volcengineRuntimeBaseURL + "/", volcengineRuntimeBaseURL + "/chat/completions", "https://relay.example/api/v3"} {
			account := volcengineTestAccount()
			account.Credentials["base_url"] = baseURL
			prepared, err := (&OpenAIGatewayService{}).prepareVolcengineChatRequest(account, []byte(`{"model":"alias"}`), "ep-target")
			require.NoError(test, err)
			require.NotContains(test, prepared.TargetURL, "/v1")
			require.Equal(test, 1, strings.Count(prepared.TargetURL, "/chat/completions"))
		}
		for _, baseURL := range []string{"https://ark.cn-beijing.volces.com", "https://ark.cn-beijing.volces.com/api/v3/models", "https://management.example/?Action=ListFoundationModels", "https://secret@relay.example/api/v3", volcengineRuntimeBaseURL + "?key=secret", volcengineRuntimeBaseURL + "#secret", "http://relay.example/api/v3"} {
			account := volcengineTestAccount()
			account.Credentials["base_url"] = baseURL
			_, err := (&OpenAIGatewayService{}).prepareVolcengineChatRequest(account, []byte(`{"model":"alias"}`), "ep-target")
			require.Error(test, err, baseURL)
			require.NotContains(test, err.Error(), "secret")
		}
		configuration := rawChatCompletionsTestConfig()
		configuration.Security.URLAllowlist.Enabled = true
		configuration.Security.URLAllowlist.UpstreamHosts = []string{"other.example"}
		_, err := (&OpenAIGatewayService{cfg: configuration}).prepareVolcengineChatRequest(volcengineTestAccount(), []byte(`{"model":"alias"}`), "ep-target")
		require.Error(test, err)
	})

	test.Run("invalid inputs and management credentials never substitute", func(test *testing.T) {
		service := &OpenAIGatewayService{}
		account := volcengineTestAccount()
		for _, body := range []string{`null`, `[]`, `{`, `{}`, `{"model":12}`, `{"model":" "}`, `{"model":"alias","stream":"true"}`} {
			_, err := service.prepareVolcengineChatRequest(account, []byte(body), "ep-target")
			require.Error(test, err, body)
		}
		for _, target := range []string{"", " ep-target"} {
			_, err := service.prepareVolcengineChatRequest(account, []byte(`{"model":"alias"}`), target)
			require.Error(test, err)
		}
		delete(account.Credentials, "api_key")
		_, err := service.prepareVolcengineChatRequest(account, []byte(`{"model":"alias"}`), "ep-target")
		require.ErrorContains(test, err, "inference api_key")
		account.Type = AccountTypeOAuth
		_, err = service.prepareVolcengineChatRequest(account, []byte(`{"model":"alias"}`), "ep-target")
		require.Error(test, err)
		_, err = service.prepareVolcengineChatRequest(nil, []byte(`{"model":"alias"}`), "ep-target")
		require.Error(test, err)
	})

	test.Run("discovery explicitly unsupported", func(test *testing.T) {
		var syncError *UpstreamModelSyncError
		require.ErrorAs(test, volcengineModelDiscoveryUnsupported(), &syncError)
		require.Equal(test, UpstreamModelSyncErrorUnsupported, syncError.Kind)
		require.Contains(test, syncError.SafeMessage(), "availability declaration")
	})

	test.Run("shared HTTP and buffered tool usage", func(test *testing.T) {
		responseBody := `{"id":"fixture-chat","object":"chat.completion","model":"foundation-response-name","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17,"prompt_tokens_details":{"cached_tokens":3}}}`
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		account := volcengineTestAccount()
		prepared, err := service.prepareVolcengineChatRequest(account, []byte(`{"model":"alias","messages":[{"role":"user","content":"weather?"}]}`), "ep-target")
		require.NoError(test, err)
		ginContext, recorder := volcengineTestContext()
		upstreamContext, headersReceived, cancel := boundedStreamContext(context.Background(), time.Second)
		defer cancel()
		response, err := service.sendCCUpstreamRequest(upstreamContext, ginContext, account, prepared.TargetURL, prepared.Body, prepared.Stream, prepared.APIKey, "", "")
		headersReceived()
		require.NoError(test, err)
		defer func() { _ = response.Body.Close() }()
		require.Equal(test, http.MethodPost, upstream.lastReq.Method)
		require.Equal(test, volcengineRuntimeBaseURL+"/chat/completions", upstream.lastReq.URL.String())
		require.Equal(test, "Bearer inference-test-key", upstream.lastReq.Header.Get("Authorization"))
		require.Empty(test, upstream.lastReq.Header.Get("X-Api-Key"))
		require.NotContains(test, upstream.lastReq.Header, "Cookie")
		require.Equal(test, "ep-target", gjson.GetBytes(upstream.lastBody, "model").String())
		result, err := service.bufferRawChatCompletions(ginContext, response, account, "alias", "ep-target", "ep-target", nil, nil, time.Now())
		require.NoError(test, err)
		require.Equal(test, 12, result.Usage.InputTokens)
		require.Equal(test, 5, result.Usage.OutputTokens)
		require.Equal(test, 3, result.Usage.CacheReadInputTokens)
		require.Equal(test, "ep-target", result.UpstreamModel)
		require.JSONEq(test, responseBody, recorder.Body.String())
		cancel()
		require.ErrorIs(test, upstream.lastReq.Context().Err(), context.Canceled)
	})

	test.Run("constructed SSE tool fragments and terminal usage", func(test *testing.T) {
		body := []byte(`{"model":"alias","stream":true,"stream_options":{"include_usage":false,"custom_option":true}}`)
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
		prepared, err := service.prepareVolcengineChatRequest(volcengineTestAccount(), body, "ep-target")
		require.NoError(test, err)
		require.True(test, prepared.Stream)
		require.True(test, gjson.GetBytes(prepared.Body, "stream_options.include_usage").Bool())
		require.True(test, gjson.GetBytes(prepared.Body, "stream_options.custom_option").Bool())
		streamBody := "data: " + `{"id":"fixture-stream","choices":[{"index":0,"delta":{"content":"Checking weather"}}]}` + "\n\n" +
			"data: " + `{"id":"fixture-stream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{"}}]}}]}` + "\n\n" +
			"data: " + `{"id":"fixture-stream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
			"data: " + `{"id":"fixture-stream","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}` + "\n\ndata: [DONE]\n\n"
		ginContext, recorder := volcengineTestContext()
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(streamBody))}}
		service.httpUpstream = upstream
		upstreamContext, headersReceived, cancel := boundedStreamContext(context.Background(), time.Second)
		defer cancel()
		response, err := service.sendCCUpstreamRequest(upstreamContext, ginContext, volcengineTestAccount(), prepared.TargetURL, prepared.Body, prepared.Stream, prepared.APIKey, "", "")
		headersReceived()
		require.NoError(test, err)
		defer func() { _ = response.Body.Close() }()
		require.Equal(test, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
		result, err := service.streamRawChatCompletions(ginContext, response, volcengineTestAccount(), "alias", "ep-target", "ep-target", nil, nil, time.Now(), len(body))
		require.NoError(test, err)
		require.Equal(test, 12, result.Usage.InputTokens)
		require.Equal(test, 5, result.Usage.OutputTokens)
		require.Contains(test, recorder.Body.String(), "Checking weather")
		require.Contains(test, recorder.Body.String(), `"id":"call_weather"`)
		require.Contains(test, recorder.Body.String(), `"arguments":"}"`)
		require.Contains(test, recorder.Body.String(), "data: [DONE]")
	})

	test.Run("client cancellation bounds shared stream drain", func(test *testing.T) {
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
		ginContext, _ := volcengineTestContext()
		clientContext, cancelClient := context.WithCancel(context.Background())
		defer cancelClient()
		ginContext.Request = ginContext.Request.WithContext(clientContext)
		reader, writer := io.Pipe()
		defer func() { _ = writer.Close() }()
		defer func() { _ = reader.Close() }()
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}
		finished := make(chan error, 1)
		go func() {
			_, err := service.streamRawChatCompletions(ginContext, response, volcengineTestAccount(), "alias", "ep-target", "ep-target", nil, nil, time.Now(), 0)
			finished <- err
		}()
		cancelClient()
		select {
		case err := <-finished:
			require.ErrorIs(test, err, context.Canceled)
		case <-time.After(8 * time.Second):
			_ = reader.Close()
			test.Fatal("shared stream reader did not stop after client cancellation")
		}
		_, err := writer.Write([]byte("late frame"))
		require.ErrorIs(test, err, io.ErrClosedPipe)
	})

	test.Run("truncated SSE is not successful completion", func(test *testing.T) {
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
		ginContext, _ := volcengineTestContext()
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"))}
		defer func() { _ = response.Body.Close() }()
		_, err := service.streamRawChatCompletions(ginContext, response, volcengineTestAccount(), "alias", "ep-target", "ep-target", nil, nil, time.Now(), 0)
		require.Error(test, err)
	})

	test.Run("HTTP errors remain evidence not model revocation", func(test *testing.T) {
		for _, status := range []int{400, 401, 403, 404, 429, 500} {
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			body := `{"error":{"message":"constructed upstream failure","code":"fixture_unknown"}}`
			response := &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"4"}}, Body: io.NopCloser(strings.NewReader(body))}
			raw, message := service.readOpenAIUpstreamError(response)
			require.Equal(test, body, string(raw))
			require.Equal(test, "constructed upstream failure", message)
			require.Equal(test, status, response.StatusCode)
			require.Equal(test, "4", response.Header.Get("Retry-After"))
			replayed, err := io.ReadAll(response.Body)
			require.NoError(test, err)
			require.Equal(test, body, string(replayed))
			require.NoError(test, response.Body.Close())
		}
	})
}

func volcengineTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ginContext.Request.Header.Set("Authorization", "Bearer client-secret")
	ginContext.Request.Header.Set("Cookie", "client-cookie")
	return ginContext, recorder
}
