package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAttemptBudgetWriterCommitBlocksReplay(t *testing.T) {
	for _, kind := range []string{"body", "string", "terminal", "mixed_heartbeat_content", "cr_heartbeat_content", "ping_with_content"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			cleanup := installAttemptBudget(c)
			service.ConfigureAttemptBudget(c.Request.Context(), 2)
			defer cleanup()
			ctx := c.Request.Context()
			finish, err := service.BeginGenerationAttempt(ctx, 1)
			require.NoError(t, err)
			finish(503, nil)
			switch kind {
			case "body":
				_, _ = c.Writer.Write([]byte("x"))
			case "string":
				_, _ = c.Writer.WriteString("x")
			case "terminal":
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString("event: response.failed\ndata: {}\n\n")
			case "mixed_heartbeat_content":
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString(": keepalive\n\ndata: [DONE]\n\n")
			case "cr_heartbeat_content":
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString(": keepalive\r\rdata: [DONE]\r\r")
			case "ping_with_content":
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString("data: {\"type\":\"ping\",\"content\":\"hello\"}\n\n")
			}
			_, err = service.BeginGenerationAttempt(ctx, 2)
			require.ErrorIs(t, err, service.ErrResponseCommitted)
			require.Len(t, service.AttemptBudgetFromContext(ctx).Snapshot(), 1)
		})
	}
}

func TestAttemptBudgetKeepaliveAllowsGeneration(t *testing.T) {
	for _, frames := range [][]string{
		{""}, {" \n\t"}, {": keepalive\n\n"},
		{": split", " comment\n", "\n"},
		{"event: pi", "ng\ndata: {\"type\":", "\"ping\"}\n\n"},
		{"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"\"}}\n\n"},
		{string(SSEPingFormatClaude)},
		{"event: ping\ndata: {\"type\":\"ping\"}\n\n"},
	} {
		t.Run(frames[0], func(t *testing.T) {
			c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
			cleanup := installAttemptBudget(c)
			service.ConfigureAttemptBudget(c.Request.Context(), 2)
			defer cleanup()
			c.Header("Content-Type", "text/event-stream")
			c.Writer.WriteHeaderNow()
			for _, frame := range frames {
				_, err := c.Writer.WriteString(frame)
				require.NoError(t, err)
				c.Writer.Flush()
			}
			ctx := c.Request.Context()
			require.Empty(t, service.AttemptBudgetFromContext(ctx).Snapshot())
			if c.Writer.Size() > 0 {
				require.True(t, gatewayStreamHasOnlyHeartbeats(c))
			}
			finish, err := service.BeginGenerationAttempt(ctx, 1)
			require.NoError(t, err)
			finish(http.StatusServiceUnavailable, nil)
			_, err = c.Writer.WriteString(": retry keepalive\n\n")
			require.NoError(t, err)
			_, err = service.BeginGenerationAttempt(ctx, 2)
			require.NoError(t, err)
			_, err = c.Writer.WriteString("data: {\"text\":\"hello\"}\n\n")
			require.NoError(t, err)
			_, err = service.BeginGenerationAttempt(ctx, 3)
			require.ErrorIs(t, err, service.ErrResponseCommitted)
			require.Len(t, service.AttemptBudgetFromContext(ctx).Snapshot(), 2)
		})
	}
}

func TestAttemptBudgetFirstGenerationAfterQueuedHeartbeat(t *testing.T) {
	for _, format := range []SSEPingFormat{SSEPingFormatComment, SSEPingFormatClaude} {
		t.Run(string(format), func(t *testing.T) {
			cache := &helperConcurrencyCacheStub{userSeq: []bool{false, true}, waitAllowed: true}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), format, time.Millisecond)
			c, rec := newHelperTestContext(http.MethodPost, "/v1/responses")
			cleanup := installAttemptBudget(c)
			service.ConfigureAttemptBudget(c.Request.Context(), 2)
			defer cleanup()
			started := false
			release, err := helper.acquireUserSlotWithWaitTimeout(c, 202, 3, time.Second, true, &started)
			require.NoError(t, err)
			defer release()
			require.True(t, started)
			require.Contains(t, rec.Body.String(), string(format))
			require.Empty(t, service.AttemptBudgetFromContext(c.Request.Context()).Snapshot())
			_, err = service.BeginGenerationAttempt(c.Request.Context(), 1)
			require.NoError(t, err, "first generation must be admitted after a real concurrency heartbeat")
		})
	}
}

func TestAttemptBudgetFirstGenerationAfterCompactHeartbeat(t *testing.T) {
	c, rec := newHelperTestContext(http.MethodPost, "/v1/responses")
	cleanup := installAttemptBudget(c)
	service.ConfigureAttemptBudget(c.Request.Context(), 2)
	defer cleanup()
	service.MarkOpenAICompactClientStream(c)
	stop := service.StartOpenAICompactSSEKeepalive(c, time.Millisecond)
	defer stop()
	// Size is synchronized by the real compact writer; do not read the recorder
	// while its heartbeat goroutine may still be writing.
	require.Eventually(t, func() bool { return c.Writer.Size() > 0 }, time.Second, time.Millisecond)
	stop()
	require.Contains(t, rec.Body.String(), ": keepalive\n\n")
	ctx := c.Request.Context()
	require.Empty(t, service.AttemptBudgetFromContext(ctx).Snapshot())
	_, err := service.BeginGenerationAttempt(ctx, 1)
	require.NoError(t, err, "compact pre-send heartbeat must not deny initial generation")
	_, err = c.Writer.WriteString("event: response.completed\ndata: {}\n\n")
	require.NoError(t, err)
	_, err = service.BeginGenerationAttempt(ctx, 2)
	require.ErrorIs(t, err, service.ErrResponseCommitted)
	require.Len(t, service.AttemptBudgetFromContext(ctx).Snapshot(), 1)
}

func TestAttemptBudgetSelection503CannotResetRoot(t *testing.T) {
	ctx, cancel := service.WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancel()
	finish, err := service.BeginGenerationAttempt(ctx, 1)
	require.NoError(t, err)
	finish(503, nil)
	state := NewFailoverState(10, false)
	state.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}
	require.Equal(t, FailoverExhausted, state.HandleSelectionExhausted(ctx))
	require.Len(t, service.AttemptBudgetFromContext(ctx).Snapshot(), 1)
}

type failedAttemptWriter struct {
	gin.ResponseWriter
	writes int
}

func (w *failedAttemptWriter) Write([]byte) (int, error) { w.writes++; return 0, io.ErrClosedPipe }

func TestAttemptLifecycleNativeTerminals(t *testing.T) {
	for _, tc := range []struct{ path, event string }{
		{"/v1/responses", "response.completed"},
		{"/v1/messages", "message_stop"},
	} {
		t.Run(tc.event, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", tc.path, nil)
			defer installAttemptBudget(c)()
			c.Header("Content-Type", "text/event-stream")
			r := attemptResponse(c)
			for _, s := range []string{": keep", "alive\r", "\n\r\n"} {
				_, err := c.Writer.WriteString(s)
				require.NoError(t, err)
			}
			require.Equal(t, lifecycle.HeartbeatOnly, r.State())
			_, err := c.Writer.WriteString("data: {\"text\":\"ok\"}\n\n")
			require.NoError(t, err)
			require.Equal(t, lifecycle.SemanticOutput, r.State())
			for _, s := range []string{"event: " + tc.event + "\r", "\ndata: {}\r", "\n\r", "\n"} {
				_, err = c.Writer.WriteString(s)
				require.NoError(t, err)
			}
			require.Equal(t, lifecycle.Terminal, r.State())
			wire := rec.Body.String()
			(&GatewayHandler{}).handleStreamingAwareError(c, 502, "api_error", "late failure", false)
			require.Equal(t, wire, rec.Body.String())
		})
	}
}

func TestAttemptLifecycleFailedTerminalOnce(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			c, _ := newHelperTestContext(http.MethodPost, path)
			failing := &failedAttemptWriter{ResponseWriter: c.Writer}
			c.Writer = failing
			defer installAttemptBudget(c)()
			c.Header("Content-Type", "text/event-stream")
			c.Writer.Flush()
			h := &GatewayHandler{}
			h.handleStreamingAwareError(c, 502, "api_error", "interrupted", false)
			h.handleStreamingAwareError(c, 502, "api_error", "late failure", false)
			require.Equal(t, 1, failing.writes)
			require.Equal(t, lifecycle.TerminalFailed, attemptResponse(c).State())
			require.ErrorIs(t, service.CheckAttemptBudget(c.Request.Context()), service.ErrResponseCommitted)
		})
	}
}

func BenchmarkAttemptSSEWrite(b *testing.B) {
	for _, tracked := range []bool{false, true} {
		name := "native"
		if tracked {
			name = "lifecycle"
		}
		b.Run(name, func(b *testing.B) {
			rec := httptest.NewRecorder()
			rec.Body = nil
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			if tracked {
				defer installAttemptBudget(c)()
			}
			c.Header("Content-Type", "text/event-stream")
			chunk := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
			b.ReportAllocs()
			for b.Loop() {
				_, _ = c.Writer.Write(chunk)
			}
		})
	}
}

func TestAttemptLifecycleNativeJSONOnce(t *testing.T) {
	c, rec := newHelperTestContext(http.MethodPost, "/v1/messages")
	defer installAttemptBudget(c)()
	h := &GatewayHandler{}
	h.errorResponse(c, 403, "permission_error", "forbidden")
	first := rec.Body.String()
	require.Contains(t, first, "forbidden")
	h.errorResponse(c, 502, "api_error", "late failure")
	require.Equal(t, first, rec.Body.String())
	require.Equal(t, lifecycle.Terminal, attemptResponse(c).State())
}
