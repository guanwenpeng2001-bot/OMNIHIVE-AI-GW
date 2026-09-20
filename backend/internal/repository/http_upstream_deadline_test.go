package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamSlowDripStopsAtRequestDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-request.Context().Done():
				return
			case <-ticker.C:
				if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
					return
				}
				writer.(http.Flusher).Flush()
			}
		}
	}))
	defer server.Close()
	ctx, cancel := service.WithAttemptBudget(context.Background(), 1, 100*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	response, err := NewHTTPUpstream(nil).Do(request, "", 1, 1)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NotEmpty(t, body)
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestHTTPUpstreamWithoutIngressBudgetStillHasDeadline(t *testing.T) {
	ctx, cancel := service.AttemptExecutionContext(context.Background())
	defer cancel()
	deadline, bounded := ctx.Deadline()
	require.True(t, bounded)
	require.LessOrEqual(t, time.Until(deadline), service.DefaultGenerationDeadline)
}
