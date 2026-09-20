//go:build unit

package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	ws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// Run in a separate OS process so this exercises SIGTERM, not a simulated
// client disconnect. The fake provider sends response.done only after drain.
func TestRealtimeSIGTERMReceiptBeforeCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot deliver SIGTERM; TestRealtimeShutdownReceiptBeforeCleanup covers drain and cleanup")
	}
	if os.Getenv("REALTIME_SHUTDOWN_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRealtimeSIGTERMReceiptBeforeCleanup$", "-test.v")
		cmd.Env = append(os.Environ(), "REALTIME_SHUTDOWN_CHILD=1")
		stdout, err := cmd.StdoutPipe()
		require.NoError(t, err)
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Start())
		scanner := bufio.NewScanner(stdout)
		ready := false
		for scanner.Scan() {
			if scanner.Text() == "COMMIT_ACCEPTED" {
				ready = true
				require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
			}
		}
		require.NoError(t, scanner.Err())
		require.NoError(t, cmd.Wait())
		require.True(t, ready)
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	testRealtimeReceiptBeforeCleanup(t, func() { fmt.Println("COMMIT_ACCEPTED") }, signals)
}

func TestRealtimeShutdownReceiptBeforeCleanup(t *testing.T) {
	shutdown := make(chan os.Signal, 1)
	testRealtimeReceiptBeforeCleanup(t, func() { shutdown <- os.Interrupt }, shutdown)
}

func testRealtimeReceiptBeforeCleanup(t *testing.T, commitAccepted func(), shutdown <-chan os.Signal) {
	t.Helper()
	svc := service.NewUpstreamJobService(nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	relayCtx, release, err := svc.RegisterQianwenRealtime(ctx)
	require.NoError(t, err)
	var receipt, closed atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			switch {
			case strings.Contains(string(raw), "session.update"):
				require.NoError(t, conn.Write(ctx, ws.MessageText, []byte(`{"type":"session.updated","session":{"mode":"commit"}}`)))
			case strings.Contains(string(raw), "input_text_buffer.commit"):
				commitAccepted()
			case strings.Contains(string(raw), "session.finish"):
				require.NoError(t, conn.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"late","status":"completed","usage":{"characters":5}}}`)))
				return
			}
		}
	}))
	defer provider.Close()
	upstream, _, err := ws.Dial(ctx, strings.Replace(provider.URL, "http", "ws", 1), nil)
	require.NoError(t, err)
	relayDone := make(chan error, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer release()
		conn, err := ws.Accept(w, r, nil)
		if err != nil {
			relayDone <- err
			return
		}
		relayDone <- service.RelayQianwenRealtime(relayCtx, conn, &service.QianwenRealtimeUpstream{Conn: upstream, Created: []byte(`{"type":"session.created"}`)}, service.QianwenRealtimeCallbacks{
			BeforeCommit: func(context.Context, string) error { return nil },
			Receipt: func(_ context.Context, id string, raw []byte, status string) error {
				require.False(t, closed.Load())
				require.Equal(t, "late", id)
				require.JSONEq(t, `{"characters":5}`, string(raw))
				receipt.Store(true)
				return nil
			},
		})
	}))
	defer gateway.Close()
	client, _, err := ws.Dial(ctx, strings.Replace(gateway.URL, "http", "ws", 1), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	_, _, err = client.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, ws.MessageText, []byte(`{"type":"session.update","session":{"mode":"commit","response_format":"pcm","sample_rate":24000}}`)))
	_, _, err = client.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, ws.MessageText, []byte(`{"type":"input_text_buffer.append","text":"hello"}`)))
	require.NoError(t, client.Write(ctx, ws.MessageText, []byte(`{"type":"input_text_buffer.commit"}`)))
	select {
	case <-shutdown:
	case <-ctx.Done():
		t.Fatal("shutdown trigger missing")
	}
	app := &Application{UpstreamJobs: svc, Server: gateway.Config, Cleanup: func() { require.True(t, receipt.Load(), "receipt precedes DB/Redis close"); closed.Store(true) }}
	require.NoError(t, shutdownApplication(ctx, app))
	require.True(t, closed.Load())
	require.NoError(t, <-relayDone)
}
