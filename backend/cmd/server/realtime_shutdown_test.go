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

// The signal entry uses the same cancellation contract as the portable entry.
func TestRealtimeSIGTERMReceiptBeforeCleanup(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("Windows cannot deliver SIGTERM; TestRealtimeShutdownReceiptBeforeCleanup covers cancellation and cleanup")
	}
	if os.Getenv("REALTIME_SHUTDOWN_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRealtimeSIGTERMReceiptBeforeCleanup$", "-test.v")
		command.Env = append(os.Environ(), "REALTIME_SHUTDOWN_CHILD=1")
		stdout, err := command.StdoutPipe()
		require.NoError(test, err)
		command.Stderr = os.Stderr
		require.NoError(test, command.Start())
		scanner := bufio.NewScanner(stdout)
		ready := false
		for scanner.Scan() {
			if scanner.Text() == "COMMIT_ACCEPTED" {
				ready = true
				require.NoError(test, command.Process.Signal(syscall.SIGTERM))
			}
		}
		require.NoError(test, scanner.Err())
		require.NoError(test, command.Wait())
		require.True(test, ready)
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	testRealtimeReceiptBeforeCleanup(test, "observed_receipt", func() { fmt.Println("COMMIT_ACCEPTED") }, signals)
}

func TestRealtimeShutdownReceiptBeforeCleanup(test *testing.T) {
	for _, scenario := range []string{"before_commit", "unobserved_usage", "observed_receipt"} {
		test.Run(scenario, func(test *testing.T) {
			shutdown := make(chan os.Signal, 1)
			testRealtimeReceiptBeforeCleanup(test, scenario, func() { shutdown <- os.Interrupt }, shutdown)
		})
	}
}

func testRealtimeReceiptBeforeCleanup(test *testing.T, scenario string, requestShutdown func(), shutdown <-chan os.Signal) {
	test.Helper()
	jobs := service.NewUpstreamJobService(nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	relayCtx, release, err := jobs.RegisterQianwenRealtime(ctx)
	require.NoError(test, err)
	var receipts, commits, finishes atomic.Int32
	var closed atomic.Bool
	providerStopped := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer close(providerStopped)
		connection, acceptErr := ws.Accept(response, request, nil)
		if acceptErr != nil {
			return
		}
		defer connection.CloseNow()
		for {
			_, raw, readErr := connection.Read(ctx)
			if readErr != nil {
				// Future usage is made available only after cancellation. The relay must
				// already have stopped reading and must never persist this receipt.
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"late","status":"completed","usage":{"characters":99}}}`))
				return
			}
			switch {
			case strings.Contains(string(raw), "session.update"):
				if writeErr := connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.updated","session":{"mode":"commit"}}`)); writeErr != nil {
					return
				}
			case strings.Contains(string(raw), "input_text_buffer.commit"):
				count := commits.Add(1)
				if scenario == "observed_receipt" && count == 1 {
					if writeErr := connection.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"observed","status":"completed","usage":{"characters":5}}}`)); writeErr != nil {
						return
					}
				} else {
					requestShutdown()
				}
			case strings.Contains(string(raw), "session.finish"):
				finishes.Add(1)
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"late","status":"completed","usage":{"characters":99}}}`))
			}
		}
	}))
	defer provider.Close()
	upstream, _, err := ws.Dial(ctx, strings.Replace(provider.URL, "http", "ws", 1), nil)
	require.NoError(test, err)
	relayDone := make(chan error, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer release()
		connection, acceptErr := ws.Accept(response, request, nil)
		if acceptErr != nil {
			relayDone <- acceptErr
			return
		}
		relayDone <- service.RelayQianwenRealtime(relayCtx, connection, &service.QianwenRealtimeUpstream{Conn: upstream, Created: []byte(`{"type":"session.created"}`)}, service.QianwenRealtimeCallbacks{
			BeforeCommit: func(commitContext context.Context, text string) error {
				if scenario == "before_commit" {
					requestShutdown()
					<-commitContext.Done()
					return commitContext.Err()
				}
				return nil
			},
			Receipt: func(billingContext context.Context, id string, raw []byte, status string) error {
				require.NoError(test, billingContext.Err())
				require.False(test, closed.Load())
				require.Equal(test, "observed", id)
				require.Equal(test, "completed", status)
				require.JSONEq(test, `{"characters":5}`, string(raw))
				receipts.Add(1)
				return nil
			},
		})
	}))
	defer gateway.Close()
	client, _, err := ws.Dial(ctx, strings.Replace(gateway.URL, "http", "ws", 1), nil)
	require.NoError(test, err)
	defer client.CloseNow()
	_, _, err = client.Read(ctx)
	require.NoError(test, err)
	require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"session.update","session":{"mode":"commit","response_format":"pcm","sample_rate":24000}}`)))
	_, _, err = client.Read(ctx)
	require.NoError(test, err)
	sendTurn := func() {
		require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"input_text_buffer.append","text":"hello"}`)))
		require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"input_text_buffer.commit"}`)))
	}
	sendTurn()
	expectedReceipts, expectedCommits := int32(0), int32(1)
	if scenario == "observed_receipt" {
		_, raw, readErr := client.Read(ctx)
		require.NoError(test, readErr)
		require.Contains(test, string(raw), "observed")
		expectedReceipts, expectedCommits = 1, 2
		sendTurn()
	} else if scenario == "before_commit" {
		expectedCommits = 0
	}
	select {
	case <-shutdown:
	case <-ctx.Done():
		test.Fatal("shutdown trigger missing")
	}
	app := &Application{UpstreamJobs: jobs, Server: gateway.Config, Cleanup: func() {
		require.Equal(test, expectedReceipts, receipts.Load(), "only observed receipts precede DB/Redis close")
		require.ErrorIs(test, relayCtx.Err(), context.Canceled)
		closed.Store(true)
	}}
	shutdownContext, stopShutdown := context.WithTimeout(ctx, time.Second)
	defer stopShutdown()
	require.NoError(test, shutdownApplication(shutdownContext, app))
	require.True(test, closed.Load())
	select {
	case relayErr := <-relayDone:
		require.ErrorIs(test, relayErr, context.Canceled)
	case <-shutdownContext.Done():
		test.Fatal("relay did not stop on shutdown")
	}
	select {
	case <-providerStopped:
	case <-shutdownContext.Done():
		test.Fatal("upstream generation did not stop on shutdown")
	}
	require.Equal(test, expectedCommits, commits.Load(), "canceled unsent commits must not reach the provider")
	require.Zero(test, finishes.Load(), "shutdown must not finish/drain future generation")
	require.Equal(test, expectedReceipts, receipts.Load(), "late usage must not create a receipt")
}
