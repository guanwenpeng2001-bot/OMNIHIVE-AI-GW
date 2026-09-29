package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestAntigravitySignatureOnlyStreamDoesNotCommit(t *testing.T) {
	for _, protocol := range []string{"chat", "responses"} {
		for _, parts := range []string{`[]`, `[{"thought":true,"thoughtSignature":"opaque-signature"}]`, `[{"thoughtSignature":"opaque-signature"}]`} {
			t.Run(protocol+parts, func(t *testing.T) {
				service := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
				ginContext, recorder := newAntigravityCompatContext(http.MethodPost, "/", nil)
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
					`data: {"response":{"candidates":[{"content":{"role":"model","parts":` + parts + `},"finishReason":"MALFORMED_FUNCTION_CALL"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}}` + "\n\n"))}
				var result *antigravityStreamResult
				var err error
				if protocol == "chat" {
					result, err = service.handleChatCompletionsStreamingFromAntigravity(ginContext, response, time.Now(), "gemini-3.1-pro-high", true)
				} else {
					result, err = service.handleResponsesStreamingFromAntigravity(ginContext, response, time.Now(), "gemini-3.1-pro-high")
				}
				require.Nil(t, result)
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.True(t, failover.RetryableOnSameAccount)
				require.Empty(t, recorder.Body.String())
				require.False(t, ginContext.Writer.Written())
			})
		}
	}
}

func TestAntigravitySemanticEventsExcludeStreamMetadata(t *testing.T) {
	for _, payload := range []string{
		`{"type":"message_stop"}`,
		`{"type":"content_block_start","content_block":{"type":"thinking","signature":"opaque"}}`,
		`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"opaque"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
	} {
		var event apicompat.AnthropicStreamEvent
		require.NoError(t, json.Unmarshal([]byte(payload), &event))
		require.False(t, isMeaningfulAntigravityCompatEvent(&event), payload)
	}
}
