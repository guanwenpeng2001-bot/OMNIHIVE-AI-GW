//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/stretchr/testify/require"
)

func TestGeminiNativeAltJSONAndSSE(t *testing.T) {
	payload := `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"totalTokenCount":13}}`
	for _, alt := range []string{"", "json", "sse"} {
		upstreamBody := "[" + payload + "]"
		contentType := "application/json"
		if alt == "sse" {
			upstreamBody = "data: " + payload + "\n\n"
			contentType = "text/event-stream"
		}
		upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
		gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
		ctx, recorder := newGeminiNativeTestContext(t)
		ctx.Request.URL.RawQuery = "alt=" + alt
		result, err := gateway.ForwardNative(context.Background(), ctx, geminiSignalTestAccount(), "gemini-fixture", "streamGenerateContent", true, geminiSignalTestRequest())
		require.NoError(t, err)
		require.Equal(t, 9, result.Usage.InputTokens)
		require.Equal(t, 4, result.Usage.OutputTokens)
		require.Equal(t, upstreamBody, recorder.Body.String(), "native representation must not be converted")
		require.Equal(t, alt == "sse", upstream.lastReq.URL.Query().Get("alt") == "sse")
	}
}

type arrayWriteProbe struct {
	gin.ResponseWriter
	afterWrite func([]byte)
	shortClose bool
}

func (w *arrayWriteProbe) Write(p []byte) (int, error) {
	if w.shortClose && string(p) == "]" {
		return 0, nil
	}
	n, err := w.ResponseWriter.Write(p)
	w.afterWrite(p)
	return n, err
}

func TestGeminiNativeArrayCommitAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		sse, short  bool
		want        lifecycle.State
	}{
		{"native", "[{}]", false, false, lifecycle.Terminal},
		{"truncated", "[{}", false, false, lifecycle.TerminalFailed},
		{"closing short write", "[{}]", false, true, lifecycle.TerminalFailed},
		{"invalid before opening", "!", false, false, lifecycle.PreOutput},
		{"converted", "data:{}\n\n", true, false, lifecycle.Terminal},
		{"converted invalid", "data:x\n\n", true, false, lifecycle.TerminalFailed},
		{"converted short write", "data:{}\n\n", true, true, lifecycle.TerminalFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newGeminiNativeTestContext(t)
			state := &lifecycle.Response{}
			tracker := protocol.TrackResponseWriter(c, state, IsStreamHeartbeatData)
			c.Writer = &arrayWriteProbe{ResponseWriter: tracker, shortClose: tc.short, afterWrite: func(p []byte) {
				require.Equal(t, lifecycle.SemanticOutput, state.State(), string(p))
				require.True(t, state.ReplayBlocked())
			}}
			resp := &http.Response{Header: http.Header{}, StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.input))}
			_, err := (&GeminiMessagesCompatService{}).handleNativeJSONStreamingResponse(c, resp, time.Now(), false, tc.sse, nil, "")
			if tc.want == lifecycle.Terminal {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.want, state.State())
		})
	}
}

func TestAntigravityNativeArrayLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		short         bool
		want          lifecycle.State
	}{
		{"complete", `{"candidates":[{"finishReason":"STOP"}]}`, false, lifecycle.Terminal},
		{"truncated", `{"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`, false, lifecycle.TerminalFailed},
		{"close failed", `{"candidates":[{"finishReason":"STOP"}]}`, true, lifecycle.TerminalFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newGeminiNativeTestContext(t)
			c.Request.URL.Path = "/v1beta/models/gemini:streamGenerateContent"
			c.Request.URL.RawQuery = "alt=json"
			state := &lifecycle.Response{}
			c.Writer = &arrayWriteProbe{ResponseWriter: protocol.TrackResponseWriter(c, state, IsStreamHeartbeatData), shortClose: tc.short, afterWrite: func([]byte) {
				require.Equal(t, lifecycle.SemanticOutput, state.State())
			}}
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data:" + tc.payload + "\n\n"))}
			gateway := newAntigravityTestService(&config.Config{})
			result, err := gateway.handleGeminiStreamingResponse(c, resp, time.Now())
			require.NoError(t, err)
			require.Equal(t, tc.short, result.clientDisconnect)
			require.Equal(t, tc.want, state.State())
		})
	}
}
