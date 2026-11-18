//go:build unit

package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// streamResumeTestRouter mounts the real generation-idempotency middleware for the
// streaming routes this fixture exercises. It is deliberately local so the fixture
// covers chat completions as well as the responses/messages routes.
func streamResumeTestRouter(t *testing.T, next gin.HandlerFunc) *gin.Engine {
	t.Helper()
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newUserMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(GatewayGenerationIdempotency(func(c *gin.Context) (service.GatewayGenerationScope, error) {
		return service.GatewayGenerationScope{WorkspaceID: 3, APIKeyID: 8, Action: c.Request.URL.Path}, nil
	}))
	for _, path := range []string{"/v1/responses", "/v1/messages", "/v1/chat/completions"} {
		router.POST(path, next)
	}
	return router
}

// T3-3.3 fault fixture: a provider stream cannot resume. Once a partial 200 has
// been written, the gateway can only append a protocol-standard terminal error
// frame in place, and an idempotent retry of the same operation must not start a
// second generation.
func TestPartialStreamInterruptEmitsTerminalFrameAndNeverRegenerates(t *testing.T) {
	cases := []struct {
		name           string
		path           string
		terminalMarker string
		// The upstream transcript every real provider sends before the break.
		partialFrame string
	}{
		{
			name:           "responses",
			path:           "/v1/responses",
			terminalMarker: "event: response.failed\n",
			partialFrame:   "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\n\n",
		},
		{
			name:           "messages",
			path:           "/v1/messages",
			terminalMarker: "event: error\ndata: ",
			partialFrame:   "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n",
		},
		{
			name:           "chat_completions",
			path:           "/v1/chat/completions",
			terminalMarker: "event: error\n",
			partialFrame:   "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var upstreamHandler GatewayHandler
			dispatches := 0
			router := streamResumeTestRouter(t, func(c *gin.Context) {
				dispatches++
				c.Header("Content-Type", "text/event-stream")
				c.Writer.WriteHeader(http.StatusOK)
				_, _ = c.Writer.WriteString(testCase.partialFrame)
				c.Writer.Flush()
				// The upstream breaks mid-stream: the status is already committed as
				// 200, so the only honest signal left is a terminal error frame.
				upstreamHandler.handleStreamingAwareError(
					c, http.StatusBadGateway, "upstream_error",
					"upstream stream ended before completion", true,
				)
			})

			first := generationTestRequest(router, testCase.path, `{"stream":true}`)
			require.Equal(t, http.StatusOK, first.Code, "partial output keeps the committed 200")
			require.Contains(t, first.Body.String(), testCase.partialFrame, "partial content reaches the client")
			require.Contains(t, first.Body.String(), testCase.terminalMarker, "the break ends with a protocol terminal marker")
			require.Contains(t, first.Body.String(), "upstream stream ended before completion")

			retry := generationTestRequest(router, testCase.path, `{"stream":true}`)
			require.Equal(t, http.StatusConflict, retry.Code, "a retry is not silently regenerated")
			// The recorded stream state is reported; the partial stream body is never
			// replayed as if it were the original answer.
			require.Equal(t, "stream_not_replayable", retry.Header().Get("X-Idempotency-State"))
			require.Contains(t, retry.Body.String(), "IDEMPOTENCY_STREAM_NOT_REPLAYABLE")
			require.NotContains(t, retry.Body.String(), testCase.partialFrame)
			require.Equal(t, 1, dispatches, "the idempotent retry must not start a second generation")
		})
	}
}

// A client that disappears mid-stream is recorded as interrupted, and the retry
// still cannot restart generation.
func TestClientDisconnectMidStreamIsInterruptedAndNotRegenerated(t *testing.T) {
	dispatches := 0
	router := generationTestRouter(t, func(c *gin.Context) {
		dispatches++
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\n\n")
		c.Writer.Flush()
		requestContext, cancel := context.WithCancel(c.Request.Context())
		c.Request = c.Request.WithContext(requestContext)
		cancel()
	})

	first := generationTestRequest(router, "/v1/responses", `{"stream":true}`)
	require.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), "hel")

	retry := generationTestRequest(router, "/v1/responses", `{"stream":true}`)
	require.Equal(t, http.StatusConflict, retry.Code)
	require.Equal(t, "interrupted", retry.Header().Get("X-Idempotency-State"))
	require.Contains(t, retry.Body.String(), "IDEMPOTENCY_INTERRUPTED")
	require.Equal(t, 1, dispatches)
}

// Same-operation retries remain keyed to the original request: a different body
// under the same key is a conflict, never a second generation.
func TestStreamRetryWithDifferentBodyConflictsInsteadOfRegenerating(t *testing.T) {
	dispatches := 0
	router := generationTestRouter(t, func(c *gin.Context) {
		dispatches++
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\n\n")
		c.Writer.Flush()
	})

	generationTestRequest(router, "/v1/responses", `{"stream":true}`)
	conflict := generationTestRequest(router, "/v1/responses", `{"stream":true,"input":"different"}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.True(t, strings.Contains(conflict.Body.String(), "IDEMPOTENCY_KEY_CONFLICT"))
	require.Equal(t, 1, dispatches)
}
