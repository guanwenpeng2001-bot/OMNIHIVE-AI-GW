package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	process "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRuntimeDrainsSSEBeforeWorkerAndCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	admission := process.NewAdmission()
	supervisor := process.New(process.All)
	var persisted, workerStopped, cleaned atomic.Bool
	supervisor.Add(func(ctx context.Context) {
		<-ctx.Done()
		workerStopped.Store(persisted.Load())
	})
	supervisor.Start()
	handler := admission.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: accepted\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		persisted.Store(true)
	}))
	server := httptest.NewServer(handler)
	defer server.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	res, err := server.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: accepted\n", line)
	app := &Application{
		Admission: admission, Supervisor: supervisor, Server: server.Config, UpstreamJobs: service.NewUpstreamJobService(nil, nil, nil),
		Cleanup: func() { cleaned.Store(workerStopped.Load()) },
	}
	require.NoError(t, shutdownApplication(ctx, app))
	require.True(t, cleaned.Load())
	late := httptest.NewRecorder()
	handler.ServeHTTP(late, req)
	require.Equal(t, http.StatusServiceUnavailable, late.Code)
	_, _, ok := admission.Acquire()
	require.False(t, ok)
}

func TestRuntimeClosesUnresponsiveHijackedTransport(t *testing.T) {
	admission := process.NewAdmission()
	ended := make(chan struct{})
	server := httptest.NewUnstartedServer(admission.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(ended)
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		_ = rw.Flush()
		_, _ = rw.ReadByte() // A client that sends no further frames.
	})))
	server.Config.ConnContext = admission.ConnContext
	server.Config.ConnState = admission.ConnState
	server.Start()
	defer server.Close()
	res, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer res.Body.Close()
	var cleaned atomic.Bool
	app := &Application{
		Admission: admission, Server: server.Config, UpstreamJobs: service.NewUpstreamJobService(nil, nil, nil),
		Cleanup: func() {
			select {
			case <-ended:
				cleaned.Store(true)
			default:
			}
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 900*time.Millisecond)
	defer cancel()
	require.True(t, errors.Is(shutdownApplication(ctx, app), context.DeadlineExceeded))
	require.True(t, cleaned.Load())
}

func TestRuntimeRoutesByRole(t *testing.T) {
	const project = "/api/v2/workspaces/1/projects/2"
	for _, tc := range []struct {
		method, path string
		owner        process.Mode
	}{
		{"GET", "/v1/models", process.Gateway},
		{"POST", "/api/v1/services/aigc/text-generation/generation", process.Gateway},
		{"GET", "/backend-api/codex/models", process.Gateway},
		{"POST", project + "/playground/runs", process.Gateway},
		{"POST", project + "/requests/id/replay", process.Gateway},
		{"POST", project + "/tasks", process.Gateway},
		{"POST", project + "/tasks/id/retry", process.Gateway},
		{"GET", project + "/tasks", process.Control},
		{"POST", project + "/tasks/id/cancel", process.Control},
		{"GET", "/api/v2/public/status", process.Control},
		{"GET", "/api/v1/subscriptions", process.Control},
		{"GET", "/api/v1/user/mcp/grants", process.Control},
		{"POST", "/api/v1/user/mcp/grants", process.Control},
		{"DELETE", "/api/v1/user/mcp/grants/123", process.Control},
		{"POST", "/api/v1/user/mcp/authorize", process.Control},
		{"PATCH", "/api/v1/user/mcp/grants", ""},
		{"DELETE", "/api/v1/user/mcp/grants/123/nested", ""},
		{"GET", "/api/v1/user/mcp/unknown", ""},
		{"GET", "/api/v1/user/onboarding", process.Control},
		{"PUT", "/api/v1/user/onboarding", process.Control},
		{"GET", "/api/v1/user/audit-logs", process.Control},
		{"GET", "/api/v1/user/routing-policies", process.Control},
		{"PUT", "/api/v1/user/routing-policies", process.Control},
		{"GET", "/api/v1/public/rankings", process.Control},
		{"POST", "/api/v1/public/rankings", ""},
		{"GET", "/api/v1/model-plaza/rankings", process.Control},
	} {
		for _, mode := range []process.Mode{process.All, process.Gateway, process.Control, process.Worker} {
			handler := roleHandler(mode, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			want := http.StatusNotFound
			if tc.owner != "" && (mode == process.All || mode == tc.owner) {
				want = http.StatusNoContent
			}
			require.Equal(t, want, w.Code, "%s %s %s", mode, tc.method, tc.path)
		}
	}
}
