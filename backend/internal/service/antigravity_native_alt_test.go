//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAntigravityNativeAltNegotiation(t *testing.T) {
	payload := `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4}}`
	for _, alt := range []string{"", "json", "sse"} {
		gateway := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("POST", "/antigravity/v1beta/models/gemini:streamGenerateContent?alt="+alt, nil)
		upstream := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\n"))}
		result, err := gateway.handleGeminiStreamingResponse(ctx, upstream, time.Now())
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 9, result.usage.InputTokens)
		require.Equal(t, 4, result.usage.OutputTokens)
		if alt == "sse" {
			require.Equal(t, "data: "+payload+"\n\n", recorder.Body.String())
			require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
		} else {
			require.JSONEq(t, "["+payload+"]", recorder.Body.String())
			require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		}
	}
}
