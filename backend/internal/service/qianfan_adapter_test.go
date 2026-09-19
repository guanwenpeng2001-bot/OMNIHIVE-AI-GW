//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These are synthetic domestic contract fixtures, not captured account replies.
// Sources: cloud.baidu.com/doc/qianfan-api/s/{Dmba8k71y,3m7of64lb}.
// SSE framing below tests compatibility with our existing raw-chat reader;
// vendor terminal framing and account availability still need live evidence.
func TestQianfanAdapterContract(t *testing.T) {
	identityPolicy := func(base string) (string, error) { return base, nil }
	newAccount := func(base string) *Account {
		return &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic-key", "base_url": base}}
	}
	parseList := func(status int, body string) (*qianfanModelList, error) {
		return parseQianfanModelsResponse(&http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))})
	}
	t.Run("domestic URL and Bearer", func(t *testing.T) {
		for _, base := range []string{"", "https://qianfan.baidubce.com", "https://qianfan.baidubce.com/v2", "https://qianfan.baidubce.com/v2/"} {
			request, err := buildQianfanRequest(context.Background(), newAccount(base), "models", nil, identityPolicy)
			require.NoError(t, err)
			require.Equal(t, qianfanDefaultBaseURL+"/models", request.URL.String())
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "Bearer synthetic-key", request.Header.Get("Authorization"))
		}
		request, err := buildQianfanRequest(context.Background(), newAccount("https://relay.example/prefix/v2"), "models", nil, identityPolicy)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example/prefix/v2/models", request.URL.String())
	})
	t.Run("reject unsupported endpoints and credentials before outbound", func(t *testing.T) {
		for _, endpoint := range []string{"responses", "embeddings", "images/generations", "audio/speech", "realtime", "../models", "chat/completions?access_token=secret"} {
			request, err := buildQianfanRequest(context.Background(), newAccount(""), endpoint, nil, identityPolicy)
			require.Error(t, err)
			require.Nil(t, request)
		}
		for _, account := range []*Account{nil, {Type: AccountTypeOAuth}, {Type: AccountTypeAPIKey, Credentials: map[string]any{"access_token": "legacy-secret", "secret_key": "legacy-secret"}}} {
			request, err := buildQianfanRequest(context.Background(), account, "models", nil, identityPolicy)
			require.Error(t, err)
			require.Nil(t, request)
			require.NotContains(t, err.Error(), "legacy-secret")
		}
		for _, base := range []string{"https://relay.example/v2/v1", "https://relay.example/v2/v2", "https://relay.example/v2/chat/completions", "https://relay.example/v1", "https://relay.example/v2?access_token=secret", "https://user:secret@relay.example/v2", "https://relay.example/%76%32", "https://relay.example/../v2"} {
			request, err := buildQianfanRequest(context.Background(), newAccount(base), "models", nil, identityPolicy)
			require.Error(t, err, base)
			require.Nil(t, request)
			require.NotContains(t, err.Error(), "secret")
		}
		_, err := buildQianfanRequest(context.Background(), newAccount(""), "models", nil, nil)
		require.Error(t, err)
		_, err = buildQianfanRequest(context.Background(), newAccount(""), "models", nil, func(string) (string, error) { return "", errors.New("secret URL") })
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	})
	t.Run("rich fields and nullable limits remain distinct", func(t *testing.T) {
		body := `{"object":"list","data":[{"id":"synthetic-text","object":"model","owned_by":"Baidu","created":123,"type":"chat","context_length":32768,"max_completions_tokens":8192,"max_tokens":4096,"prompt_tokens":24576,"architecture":{"modality":"text+image->text","input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.01"},"supports_tools":false,"aliases":[],"lifecycle":"unknown"},{"id":"synthetic-embedding","type":"embedding","context_length":null,"max_completions_tokens":null,"architecture":{"input_modalities":[],"output_modalities":null}}]}`
		result, err := parseList(200, body)
		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Len(t, result.Models, 2)
		model := result.Models[0]
		require.EqualValues(t, 32768, *model.ContextLength)
		require.EqualValues(t, 8192, *model.MaxCompletionsTokens)
		require.EqualValues(t, 4096, *model.MaxTokens)
		require.EqualValues(t, 24576, *model.PromptTokens)
		require.Equal(t, "Baidu", model.OwnedBy)
		require.EqualValues(t, 123, *model.Created)
		var original struct {
			Data []json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &original))
		for index, model := range result.Models {
			require.JSONEq(t, string(original.Data[index]), string(model.Raw))
		}
		require.Nil(t, result.Models[1].ContextLength)
		require.Nil(t, result.Models[1].MaxCompletionsTokens)
		require.JSONEq(t, `{"input_modalities":[],"output_modalities":null}`, string(result.Models[1].Architecture))
		// A category does not create a supported endpoint.
		_, err = buildQianfanRequest(context.Background(), newAccount(""), "embeddings", nil, identityPolicy)
		require.Error(t, err)
	})
	t.Run("empty success differs from permission and partial failure", func(t *testing.T) {
		result, err := parseList(200, `{"object":"list","data":[]}`)
		require.NoError(t, err)
		require.True(t, result.Complete)
		require.NotNil(t, result.Models)
		require.Empty(t, result.Models)
		for _, status := range []int{401, 403, 404, 429, 500} {
			result, err := parseList(status, `{"error":{"message":"secret"}}`)
			require.Error(t, err)
			require.Nil(t, result)
			require.Contains(t, err.Error(), fmt.Sprintf("HTTP status %d", status))
			require.NotContains(t, err.Error(), "secret")
		}
		for _, body := range []string{`{}`, `{"object":"list","data":null}`, `{"object":"list","data":[{"id":"valid"},{}]}`, `{"object":"list","data":[{"id":"duplicate"},{"id":"duplicate"}]}`, `{"object":"list","data":[],"has_more":true}`, `{"object":"list","data":[],"next_cursor":"later"}`, `{"object":"list","data":[],"error":{"code":"denied"}}`, `{"object":"list","data":[{"id":"bad","context_length":"untrusted"}]}`, `{"object":"list","data":[]`} {
			result, err := parseList(200, body)
			require.Error(t, err, body)
			require.Nil(t, result)
		}
		_, err = parseList(200, strings.Repeat(" ", int(upstreamModelsBodyLimit)+1))
		require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
	})
	t.Run("multi turn tools preserve outbound bytes", func(t *testing.T) {
		body := []byte(`{"model":"alias:original","messages":[{"role":"user","content":"weather"},{"role":"assistant","content":null,"tool_calls":[{"id":"call-weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"北京\"}"}}]},{"role":"tool","tool_call_id":"call-weather","content":"sunny"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}],"tool_choice":"auto","stream":true,"stream_options":{"include_usage":true}}`)
		request, err := buildQianfanRequest(context.Background(), newAccount(""), "chat/completions", body, identityPolicy)
		require.NoError(t, err)
		require.Equal(t, qianfanDefaultBaseURL+"/chat/completions", request.URL.String())
		require.Equal(t, "text/event-stream", request.Header.Get("Accept"))
		actual, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, body, actual)
	})
	t.Run("existing raw response and SSE preserve tools reasoning usage", func(t *testing.T) {
		account := newAccount("")
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
		usage := `"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17,"prompt_tokens_details":{"cached_tokens":2}}`
		nonstream := `{"id":"synthetic-reply","model":"synthetic-text","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"thinking","content":null,"tool_calls":[{"id":"call-weather","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":"tool_calls"}],` + usage + `}`
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		result, err := service.bufferRawChatCompletions(ginContext, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(nonstream))}, account, "synthetic-text", "synthetic-text", "synthetic-text", nil, nil, time.Now())
		require.NoError(t, err)
		require.JSONEq(t, nonstream, recorder.Body.String())
		require.Equal(t, 12, result.Usage.InputTokens)
		require.Equal(t, 5, result.Usage.OutputTokens)
		require.Equal(t, 2, result.Usage.CacheReadInputTokens)
		frames := []string{
			`{"choices":[{"index":0,"delta":{"reasoning_content":"thinking"}}],"usage":null}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-weather","type":"function","function":{"name":"weather","arguments":"{"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],` + usage + `}`,
			`{"choices":[],` + usage + `}`,
		}
		var stream strings.Builder
		for _, frame := range frames {
			_, err := stream.WriteString("data: " + frame + "\n\n")
			require.NoError(t, err)
		}
		_, err = stream.WriteString("data: [DONE]\n\n")
		require.NoError(t, err)
		recorder = httptest.NewRecorder()
		ginContext, _ = gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		streamResult, err := service.streamRawChatCompletions(ginContext, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(stream.String()))}, account, "synthetic-text", "synthetic-text", "synthetic-text", nil, nil, time.Now(), 0)
		require.NoError(t, err)
		require.Equal(t, result.Usage, streamResult.Usage, "repeated cumulative usage must not be summed")
		for _, frame := range frames {
			require.Contains(t, recorder.Body.String(), frame)
		}
		require.Contains(t, recorder.Body.String(), "data: [DONE]")
	})
	t.Run("transport cancellation before and during response", func(t *testing.T) {
		started := make(chan struct{})
		cancelled := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.WriteHeader(200)
			flusher, ok := writer.(http.Flusher)
			if !ok {
				t.Error("response writer does not support flushing")
				close(started)
				close(cancelled)
				return
			}
			flusher.Flush()
			close(started)
			<-request.Context().Done()
			close(cancelled)
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		request, err := buildQianfanRequest(ctx, newAccount(server.URL), "chat/completions", []byte(`{"model":"synthetic","messages":[{"role":"user","content":"hello"}],"stream":true}`), identityPolicy)
		require.NoError(t, err)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		defer func() { require.NoError(t, response.Body.Close()) }()
		<-started
		cancel()
		_, err = io.ReadAll(response.Body)
		require.ErrorIs(t, err, context.Canceled)
		select {
		case <-cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("upstream did not observe cancellation")
		}
		_, err = server.Client().Do(request)
		require.ErrorIs(t, err, context.Canceled)
	})
}
