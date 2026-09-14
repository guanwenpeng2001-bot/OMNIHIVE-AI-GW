package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const drainMessageStart = "event: message_start\nid: ignored\n: comment\ndata: {\ndata: \"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":7}}}\n\n"
const drainMessageStop = "data: {\"type\":\"message_stop\"}\n\n"

func TestAnthropicCompletionFrames(t *testing.T) {
	for _, path := range []string{"responses", "responses-buffered", "chat", "chat-buffered"} {
		for _, tc := range []struct {
			name, body string
			ok         bool
			input      int
		}{
			{"empty-eof", "", false, 0},
			{"done-only", "data: [DONE]\n\n", false, 0},
			{"empty-output", drainMessageStart + drainMessageStop, true, 7},
			{"partial-usage", drainMessageStart, false, 7},
			{"malformed", drainMessageStart + "data: {bad}\n\n" + drainMessageStop, false, 7},
			{"comments-empty", ": keepalive\nid: 3\n\ndata:\n\n" + drainMessageStart + drainMessageStop, true, 7},
			{"tool", drainMessageStart + "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\",\"name\":\"lookup\",\"input\":{}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":1}\"}}\n\n" + drainMessageStop, true, 7},
			{"half-tool", drainMessageStart + "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\"}}\n\n" + drainMessageStop, false, 7},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
				svc := &GatewayService{}
				var result *ForwardResult
				var err error
				switch path {
				case "responses":
					result, err = svc.handleResponsesStreamingResponse(resp, c, "model", "model", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				case "responses-buffered":
					result, err = svc.handleResponsesBufferedStreamingResponse(resp, c, "model", "model", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				case "chat":
					result, err = svc.handleCCStreamingFromAnthropic(resp, c, "model", "model", nil, time.Now(), true)
				case "chat-buffered":
					result, err = svc.handleCCBufferedFromAnthropic(resp, c, "model", "model", nil, time.Now())
				}
				if tc.ok {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.NotContains(t, rec.Body.String(), "response.completed")
					require.NotContains(t, rec.Body.String(), "[DONE]")
				}
				require.NotNil(t, result)
				require.Equal(t, tc.input, result.Usage.InputTokens)
			})
		}
	}
}

func TestFrameReaderTimeoutAndDrain(t *testing.T) {
	for _, semantic := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-output", true: "idle"}[semantic], func(t *testing.T) {
			body, writer := io.Pipe()
			defer writer.Close()
			r := newUpstreamFrameReader(body, 1024, context.Background(), 30*time.Millisecond)
			defer r.Close()
			if semantic {
				r.Semantic()
			}
			require.False(t, r.Scan())
			require.Error(t, r.Err())
		})
	}
	t.Run("header", func(t *testing.T) {
		ctx, _, closeRequest := boundedStreamContext(context.Background(), 20*time.Millisecond)
		defer closeRequest()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("header wait leaked")
		}
	})
	t.Run("late-usage", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		body, writer := io.Pipe()
		defer writer.Close()
		r := newUpstreamFrameReader(body, 1024, ctx, time.Second)
		defer r.Close()
		cancel()
		go func() {
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(writer, "data: {\"usage\":{\"prompt_tokens\":7}}\n\n")
		}()
		require.True(t, r.Scan())
		require.Contains(t, r.frame.Data, "prompt_tokens")
		require.NoError(t, r.Err())
	})
	t.Run("disconnect-bounded", func(t *testing.T) {
		body, writer := io.Pipe()
		defer writer.Close()
		r := newUpstreamFrameReader(body, 1024, context.Background(), time.Minute)
		defer r.Close()
		r.Disconnected()
		start := time.Now()
		require.False(t, r.Scan())
		require.ErrorIs(t, r.Err(), context.Canceled)
		require.Less(t, time.Since(start), 6*time.Second)
	})
}

func TestRawChatDoneClosesConnection(t *testing.T) {
	for _, envelope := range []bool{false, true} {
		t.Run(map[bool]string{false: "done-only", true: "valid"}[envelope], func(t *testing.T) {
			body, writer := io.Pipe()
			defer writer.Close()
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				if envelope {
					_, _ = io.WriteString(writer, "data: {\"id\":\"cc\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				}
				_, _ = io.WriteString(writer, "data: [DONE]\n\n")
				_, err := io.WriteString(writer, "data: never-read\n\n")
				if !errors.Is(err, io.ErrClosedPipe) {
					return
				}
			}()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			svc := &OpenAIGatewayService{}
			result, err := svc.streamRawChatCompletions(c, &http.Response{Header: http.Header{}, Body: body}, &Account{ID: 1, Platform: PlatformOpenAI}, "model", "model", "model", nil, nil, time.Now(), 0)
			if envelope {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NotNil(t, result)
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal("connection not closed after DONE")
			}
		})
	}
}

// A real HTTP connection must be released even when the server deliberately
// leaves the successful response open after its terminal event.
func TestAnthropicTerminalReleasesHTTPConnection(t *testing.T) {
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, drainMessageStart+drainMessageStop); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(released)
	}))
	defer server.Close()
	resp, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(resp, c, "model", "model", nil, time.Now(), true)
	require.NoError(t, err)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Contains(t, rec.Body.String(), "[DONE]")
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("upstream connection still retained")
	}
}

type completionReadError struct{}

func (completionReadError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAnthropicReadErrorRetainsUsage(t *testing.T) {
	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(io.MultiReader(strings.NewReader(drainMessageStart), completionReadError{}))}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	result, err := (&GatewayService{}).handleResponsesStreamingResponse(resp, c, "model", "model", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.True(t, result.Stream)
	require.NotContains(t, rec.Body.String(), "response.completed")
}

func TestStreamingHTTPHeaderAndBodyTimeout(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-headers", true: "no-body"}[headers], func(t *testing.T) {
			released := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if headers {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(released)
			}))
			defer server.Close()
			ctx, received, cleanup := boundedStreamContext(context.Background(), 50*time.Millisecond)
			defer cleanup()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
			require.NoError(t, err)
			resp, err := server.Client().Do(request)
			received()
			if headers {
				require.NoError(t, err)
				reader := newUpstreamFrameReader(resp.Body, 1024, ctx, 50*time.Millisecond)
				require.False(t, reader.Scan())
				require.Error(t, reader.Err())
				reader.Close()
			} else {
				require.Error(t, err)
			}
			select {
			case <-released:
			case <-time.After(time.Second):
				t.Fatal("timeout did not release connection")
			}
		})
	}
}

func TestRawChatMultilineDataAndPartialTool(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "multiline", true: "half-tool"}[partial], func(t *testing.T) {
			payload := "data: {\ndata: \"id\":\"cc\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"
			if partial {
				payload += "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\"}}]}}]}\n\n"
			}
			payload += "data: [DONE]\n\n"
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			_, err := (&OpenAIGatewayService{}).streamRawChatCompletions(c, resp, &Account{ID: 1, Platform: PlatformOpenAI}, "model", "model", "model", nil, nil, time.Now(), 0)
			if partial {
				require.Error(t, err)
				require.NotContains(t, rec.Body.String(), "[DONE]")
			} else {
				require.NoError(t, err)
			}
			var first string
			forEachOpenAISSEFrame(rec.Body.String(), func(_ string, data []byte) {
				if first == "" {
					first = string(data)
				}
			})
			require.JSONEq(t, `{"id":"cc","choices":[{"index":0,"delta":{"content":"hello"}}]}`, first)
		})
	}
}
