//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
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
