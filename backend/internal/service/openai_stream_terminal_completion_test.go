package service

import (
	"context"
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

func TestOpenAIStreamTerminalCompletionWithoutEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct {
		name        string
		gateway     config.GatewayConfig
		passthrough bool
	}{
		{name: "synchronous"},
		{name: "asynchronous", gateway: config.GatewayConfig{StreamDataIntervalTimeout: 30}},
		{name: "guarded", gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 30}},
		{name: "passthrough", passthrough: true},
	} {
		for _, terminal := range []string{"completed", "done", "bare_error_then_completed"} {
			t.Run(route.name+"/"+terminal, func(t *testing.T) {
				reader, writer := io.Pipe()
				defer func() { _ = reader.Close() }()
				defer func() { _ = writer.Close() }()
				recorder := httptest.NewRecorder()
				ginContext, _ := gin.CreateTestContext(recorder)
				ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				service := &OpenAIGatewayService{cfg: &config.Config{Gateway: route.gateway}, toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
				finished := make(chan error, 1)
				var usage *OpenAIUsage
				go func() {
					if route.passthrough {
						result, err := service.handleStreamingResponsePassthrough(context.Background(), response, ginContext, account, time.Now(), "gpt-5", "gpt-5")
						if result != nil {
							usage = result.usage
						}
						finished <- err
					} else {
						result, err := service.handleStreamingResponse(context.Background(), response, ginContext, account, time.Now(), "gpt-5", "gpt-5")
						if result != nil {
							usage = result.usage
						}
						finished <- err
					}
				}()
				terminalLine := `data: {"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":5}}}` + "\n"
				switch terminal {
				case "bare_error_then_completed":
					_, err := io.WriteString(writer, "data: {\"type\":\"error\",\"error\":{\"code\":\"transient\",\"message\":\"retrying\"}}\n\n")
					require.NoError(t, err)
				case "done":
					terminalLine = "data: [DONE]\n"
				}
				_, err := io.WriteString(writer, terminalLine)
				require.NoError(t, err)
				select {
				case err := <-finished:
					t.Fatalf("stream ended before the terminal frame's blank line: %v", err)
				case <-time.After(20 * time.Millisecond):
				}
				_, err = io.WriteString(writer, "\n")
				require.NoError(t, err)
				select {
				case err := <-finished:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("stream still waiting for EOF after complete terminal frame")
				}
				require.Equal(t, terminalLine+"\n", recorder.Body.String())
				require.NotContains(t, recorder.Body.String(), `"type":"error"`)
				if terminal != "done" {
					require.NotNil(t, usage)
					require.Equal(t, 7, usage.InputTokens)
					require.Equal(t, 5, usage.OutputTokens)
				}
				_, err = io.WriteString(writer, strings.Repeat("ignored", 2))
				require.ErrorIs(t, err, io.ErrClosedPipe, "upstream body must be closed")
			})
		}
	}
}
