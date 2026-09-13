//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseQianwenNativeBody(t *testing.T) {
	for _, body := range []string{
		`{"model":"qwen-plus","input":{"prompt":"Hello"}}`,
		`{"model":"qwen-plus","input":{"messages":[{"role":"user","content":"Hello"}]}}`,
		`{"model":"qwen-plus","input":{"messages":[{"role":"user","content":[{"image":"https://example.com/a.jpg"},{"text":"Describe"}]}]}}`,
	} {
		model, err := ParseQianwenNativeBody([]byte(body))
		require.NoError(t, err)
		require.Equal(t, "qwen-plus", model)
	}
	for _, body := range []string{`null`, `{}`, `{"model":123}`, `{"model":" "}`, `{"model":"x","messages":[]}`, `{"model":"x","input":{"messages":[{"content":[{"audio":"a"}]}]}}`, `{"model":"x","input":{"messages":[{"content":[{"video":["a"]}]}]}}`, `{"model":"x","input":{"prompt":"a"},"parameters":{"modalities":["audio"]}}`} {
		_, err := ParseQianwenNativeBody([]byte(body))
		require.Error(t, err, body)
	}
}

func TestQianwenNativeForwardWireAndBilling(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			payload := `{"request_id":"native-id","output":{"choices":[{"finish_reason":"stop","message":{"content":"Hi"}}]},"usage":{"input_tokens":20,"output_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`
			wire := payload
			if stream {
				wire = ": ping\r\n\r\nevent: result\r\nid: 1\r\ndata: {\"usage\":{\"input_tokens\":20,\"output_tokens\":2}}\r\n\r\ndata:" + payload + "\r\n\r\n"
			}
			u := &httpUpstreamRecorder{resp: qianwenModelPage(wire, 200)}
			if stream {
				u.resp.Header.Set("Content-Type", "text/event-stream")
			}
			s := &OpenAIGatewayService{httpUpstream: u}
			a := qianwenTestAccount()
			a.Credentials["model_mapping"] = map[string]any{"alias": "qwen-plus"}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
			body := []byte(`{"model":"alias","input":{"prompt":"hello"},"parameters":{"incremental_output":true}}`)
			result, err := s.ForwardQianwenNative(context.Background(), c, a, QianwenNativeText, body, stream)
			require.NoError(t, err)
			require.Equal(t, 200, w.Code)
			require.Equal(t, "qwen-plus", gjson.GetBytes(u.bodies[0], "model").String())
			require.Equal(t, "hello", gjson.GetBytes(u.bodies[0], "input.prompt").String())
			require.True(t, gjson.GetBytes(u.bodies[0], "parameters.incremental_output").Bool())
			require.Equal(t, 20, result.Usage.InputTokens)
			require.Equal(t, 4, result.Usage.OutputTokens)
			require.Equal(t, 3, result.Usage.CacheReadInputTokens)
			require.Equal(t, "native-id", result.RequestID)
			require.Equal(t, QianwenNativeTextPath, result.UpstreamEndpoint)
			require.Contains(t, w.Body.String(), payload)
			if stream {
				require.Contains(t, w.Body.String(), "event: result\nid: 1\n")
				require.Equal(t, "enable", u.requests[0].Header.Get("X-DashScope-SSE"))
			}
		})
	}
}

func TestQianwenNativeStreamPartialUsage(t *testing.T) {
	for _, tail := range []string{"", "data:{\"code\":\"Throttling\",\"message\":\"slow down\"}\n\n", "data:bad-json\n\n"} {
		u := &httpUpstreamRecorder{resp: qianwenModelPage("data:{\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}\n\n"+tail, 200)}
		u.resp.Header.Set("Content-Type", "text/event-stream")
		s := &OpenAIGatewayService{httpUpstream: u}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
		result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hi"}}`), true)
		require.Error(t, err)
		require.NotNil(t, result)
		require.Equal(t, 7, result.Usage.InputTokens)
		require.Equal(t, 2, result.Usage.OutputTokens)
	}
}

func TestQianwenNativeMultilineSSE(t *testing.T) {
	u := &httpUpstreamRecorder{resp: qianwenModelPage("data: {\"output\":{\"finish_reason\":\"stop\"},\n"+"data: \"usage\":{\"input_tokens\":4,\"output_tokens\":1}}\n\n", 200)}
	u.resp.Header.Set("Content-Type", "text/event-stream")
	s := &OpenAIGatewayService{httpUpstream: u}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
	result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hi"}}`), true)
	require.NoError(t, err)
	require.Equal(t, 4, result.Usage.InputTokens)
}

func TestQianwenNativeErrorMapping(t *testing.T) {
	u := &httpUpstreamRecorder{resp: qianwenModelPage(`{"code":"InvalidParameter","message":"bad input"}`, 400)}
	s := &OpenAIGatewayService{httpUpstream: u}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
	result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hi"}}`), false)
	require.Error(t, err)
	require.Nil(t, result)
	require.GreaterOrEqual(t, w.Code, 400)
}

func TestQianwenNativeTerminalCancelsUpstreamBeforeClose(t *testing.T) {
	stream := newContextBoundBlockingReadCloser([]byte("data:{\"output\":{\"finish_reason\":\"stop\"},\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}\n\n"))
	t.Cleanup(stream.forceUnblock)
	s := &OpenAIGatewayService{httpUpstream: &contextBoundHTTPUpstream{body: stream}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
	done := make(chan error, 1)
	go func() {
		result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hi"}}`), true)
		if result == nil || result.Usage.InputTokens != 3 {
			done <- context.Canceled
			return
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("terminal frame did not release upstream")
	}
}

func TestQianwenNativeClientCancellationReleasesUpstream(t *testing.T) {
	stream := newContextBoundBlockingReadCloser(nil)
	t.Cleanup(stream.forceUnblock)
	s := &OpenAIGatewayService{httpUpstream: &contextBoundHTTPUpstream{body: stream}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.ForwardQianwenNative(ctx, c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hi"}}`), true)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation did not release upstream")
	}
}
