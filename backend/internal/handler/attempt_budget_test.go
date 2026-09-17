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
	for _, kind := range []string{"body", "string", "flush", "headers", "heartbeat"} {
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
			case "flush":
				c.Writer.Flush()
			case "headers":
				c.Writer.WriteHeaderNow()
			case "heartbeat":
				service.MarkOpenAICompactClientStream(c)
				stop := service.StartOpenAICompactSSEKeepalive(c, time.Millisecond)
				require.Eventually(t, func() bool { return service.CheckAttemptBudget(ctx) == service.ErrResponseCommitted }, time.Second, time.Millisecond)
				stop()
			}
			_, err = service.BeginGenerationAttempt(ctx, 2)
			require.ErrorIs(t, err, service.ErrResponseCommitted)
			require.Len(t, service.AttemptBudgetFromContext(ctx).Snapshot(), 1)
		})
	}
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
