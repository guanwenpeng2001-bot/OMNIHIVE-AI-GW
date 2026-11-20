package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

const boundedGeminiChunk = `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}`

func boundedGeminiReaders(svc *GeminiMessagesCompatService) map[string]func(*gin.Context, *http.Response) (*ClaudeUsage, error) {
	return map[string]func(*gin.Context, *http.Response) (*ClaudeUsage, error){
		"native": func(c *gin.Context, resp *http.Response) (*ClaudeUsage, error) {
			r, err := svc.handleNativeStreamingResponse(c, resp, time.Now(), false, &Account{ID: 1}, "")
			if err != nil {
				return nil, err
			}
			return r.usage, nil
		},
		"messages": func(c *gin.Context, resp *http.Response) (*ClaudeUsage, error) {
			r, err := svc.handleStreamingResponse(c, resp, time.Now(), "gemini-2.5-pro")
			if r == nil {
				return nil, err
			}
			return r.usage, err
		},
		"chat_completions": func(c *gin.Context, resp *http.Response) (*ClaudeUsage, error) {
			r, err := svc.handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-2.5-pro", false, true)
			if err != nil {
				return nil, err
			}
			return r.usage, nil
		},
		"collector": func(_ *gin.Context, resp *http.Response) (*ClaudeUsage, error) {
			_, usage, err := collectGeminiSSE(svc.cfg, resp.Body, false)
			return usage, err
		},
	}
}

func TestGeminiStreamsRejectOversizedUpstreamLines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 256}}
	for name, read := range boundedGeminiReaders(&GeminiMessagesCompatService{cfg: cfg}) {
		for _, prefix := range []string{"", "data: " + boundedGeminiChunk + "\n\n"} {
			for _, ending := range []string{"", "\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/prior_event=%t/ending_bytes=%d", name, prefix != "", len(ending)), func(t *testing.T) {
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
					// Valid JSON plus padding must not be mistaken for a complete event.
					oversized := `data: {"modelVersion":"rejected-model","usageMetadata":{"promptTokenCount":999}}` + strings.Repeat(" ", 4096)
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(prefix + oversized + ending))}
					usage, err := read(c, resp)
					require.ErrorIs(t, err, bufio.ErrTooLong)
					if name == "messages" {
						// The local Messages bridge returns observed usage alongside
						// read errors. Preserve this partial billing receipt.
						require.NotNil(t, usage)
						if prefix != "" {
							require.Equal(t, 10, usage.InputTokens)
							require.Equal(t, 4, usage.OutputTokens)
						} else {
							require.Zero(t, usage.InputTokens)
							require.Zero(t, usage.OutputTokens)
						}
					} else {
						require.Nil(t, usage, "overflow follows existing read-error billing behavior")
					}
					require.NotContains(t, w.Body.String(), "rejected-model")
					require.NotContains(t, w.Body.String(), "999")
					if name == "messages" {
						require.NotContains(t, w.Body.String(), "event: message_stop")
					}
					if name == "chat_completions" {
						require.NotContains(t, w.Body.String(), "data: [DONE]")
					}
					require.Empty(t, observedUpstreamResponseModel(c))
				})
			}
		}
	}
}

func TestGeminiForwardCollectorsUseConfiguredLineLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "chat_completions", "native"} {
		t.Run(endpoint, func(t *testing.T) {
			body := &trackedLimitedResponseBody{Reader: &endlessSSELine{}}
			svc := &GeminiMessagesCompatService{
				cfg:           &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 256}},
				tokenProvider: &GeminiTokenProvider{},
				httpUpstream: &geminiCompatHTTPUpstreamStub{response: &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body,
				}},
			}
			account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture", "project_id": "project-1"}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			var result *ForwardResult
			var err error
			request := []byte(`{"model":"gemini-2.5-pro","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
			switch endpoint {
			case "messages":
				result, err = svc.Forward(context.Background(), c, account, request)
			case "chat_completions":
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, request)
			case "native":
				result, err = svc.ForwardNative(context.Background(), c, account, "gemini-2.5-pro", "generateContent", false, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
			}
			require.Error(t, err)
			require.Nil(t, result)
			require.True(t, body.closed)
			require.Positive(t, body.bytes)
			require.LessOrEqual(t, body.bytes, 512)
			require.Equal(t, http.StatusBadGateway, w.Code)
			require.Contains(t, w.Body.String(), "Failed to read upstream stream")
		})
	}
}

func TestGeminiForwardLineLimitKeepsPartialMessagesReceipt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &trackedLimitedResponseBody{Reader: strings.NewReader("data: " + boundedGeminiChunk + "\n\n" + "data: " + strings.Repeat("x", 513))}
	svc := &GeminiMessagesCompatService{
		cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 512}},
		httpUpstream: &geminiCompatHTTPUpstreamStub{response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"partial-receipt"}},
			Body:       body,
		}},
	}
	account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gemini-2.5-pro","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	require.ErrorIs(t, err, bufio.ErrTooLong)
	require.True(t, body.closed)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, "partial-receipt", result.RequestID)
	require.True(t, result.Stream)
	require.Contains(t, w.Body.String(), "hello")
	require.NotContains(t, w.Body.String(), "xxxx")
}

func TestGeminiStreamsBoundedReaderPreservesUsageAndEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ending := range []string{"", "\n", "\r\n"} {
		wire := "data: " + boundedGeminiChunk + ending
		cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: len(wire)}}
		for name, read := range boundedGeminiReaders(&GeminiMessagesCompatService{cfg: cfg}) {
			t.Run(fmt.Sprintf("%s/ending_bytes=%d", name, len(ending)), func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}
				usage, err := read(c, resp)
				require.NoError(t, err)
				require.Equal(t, 10, usage.InputTokens)
				require.Equal(t, 4, usage.OutputTokens)
				if name == "native" {
					require.Equal(t, wire, w.Body.String())
				}
				if name == "messages" || name == "chat_completions" {
					require.Contains(t, w.Body.String(), "hello")
				}
			})
		}
	}
}

// A generated, never-ending line proves that rejection does not wait for EOF or
// consume the rest of the response. No huge fixture allocation is needed.
type endlessSSELine struct{ bytes int }

func (r *endlessSSELine) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.bytes += len(p)
	return len(p), nil
}

func TestBoundedSSELineReader(t *testing.T) {
	for _, cfg := range []*config.Config{nil, {}, {Gateway: config.GatewayConfig{MaxLineSize: -1}}} {
		require.Equal(t, defaultMaxLineSize, newBoundedSSELineReader(strings.NewReader(""), cfg).maxLineSize)
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 8192}}
	t.Run("one byte over including line ending", func(t *testing.T) {
		for _, ending := range []string{"", "\n", "\r\n"} {
			wire := strings.Repeat("x", cfg.Gateway.MaxLineSize+1-len(ending)) + ending
			line, err := newBoundedSSELineReader(strings.NewReader(wire), cfg).ReadLineString()
			require.ErrorIs(t, err, bufio.ErrTooLong)
			require.Empty(t, line)
		}
	})
	t.Run("never ending line", func(t *testing.T) {
		body := &endlessSSELine{}
		line, err := newBoundedSSELineReader(body, cfg).ReadLineString()
		require.ErrorIs(t, err, bufio.ErrTooLong)
		require.Empty(t, line)
		require.LessOrEqual(t, body.bytes, cfg.Gateway.MaxLineSize+4096)
	})
	t.Run("matches ReadString across buffer boundaries", func(t *testing.T) {
		wire := strings.Repeat("x", 8190) + "\r\n\n: comment\r\ndata: [DONE]\nlast"
		want := bufio.NewReader(strings.NewReader(wire))
		got := newBoundedSSELineReader(iotest.OneByteReader(strings.NewReader(wire)), cfg)
		for {
			wantLine, wantErr := want.ReadString('\n')
			gotLine, gotErr := got.ReadLineString()
			require.Equal(t, wantLine, gotLine)
			require.Equal(t, wantErr, gotErr)
			if wantErr != nil {
				break
			}
		}
	})
	t.Run("preserves partial line and transport error", func(t *testing.T) {
		failure := errors.New("upstream reset")
		body := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(failure))
		line, err := newBoundedSSELineReader(body, cfg).ReadLineString()
		require.Equal(t, "partial", line)
		require.ErrorIs(t, err, failure)
	})
}

func TestCollectGeminiSSEObservedLimitPreservesOAuthAndObservation(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 512}}
	valid := `data: {"response":` + boundedGeminiChunk + "}\r\n\r\n"
	for _, oversized := range []bool{false, true} {
		wire := valid + "data: [DONE]"
		if oversized {
			wire = valid + "data: " + strings.Repeat("x", 513)
		}
		var observed []string
		collected, usage, stats, err := collectGeminiSSEObserved(cfg, strings.NewReader(wire), true, func(raw []byte) { observed = append(observed, string(raw)) })
		require.Equal(t, []string{boundedGeminiChunk}, observed)
		require.Equal(t, 1, stats.dataEvents)
		if oversized {
			require.ErrorIs(t, err, bufio.ErrTooLong)
			require.Nil(t, collected)
			require.Nil(t, usage)
		} else {
			require.NoError(t, err)
			require.Equal(t, "hello", extractGeminiParts(collected)[0]["text"])
			require.Equal(t, 10, usage.InputTokens)
			require.Equal(t, 4, usage.OutputTokens)
		}
	}
}
