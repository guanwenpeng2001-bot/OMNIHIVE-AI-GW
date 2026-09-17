package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAttemptBudgetWriterCommitBlocksReplay(t *testing.T) {
	for _, kind := range []string{"body", "string", "terminal", "mixed_heartbeat_content", "cr_heartbeat_content", "ping_with_content"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			cleanup := installAttemptBudget(c)
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
		{string(SSEPingFormatClaude)},
		{"event: ping\ndata: {\"type\":\"ping\"}\n\n"},
	} {
		t.Run(frames[0], func(t *testing.T) {
			c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
			cleanup := installAttemptBudget(c)
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
