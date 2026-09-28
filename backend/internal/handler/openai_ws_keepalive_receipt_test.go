package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the real handler, turn plan, outbound attempt and durable receipt.
// A heartbeat must reach the client without committing staged model metadata;
// disconnecting afterward must cancel generation and retain only observed usage.
func TestWebSocketKeepaliveDisconnectPersistsObservedUsage(t *testing.T) {
	var attempts atomic.Int32
	canceled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		response.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(response, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_heartbeat\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n")
		fmt.Fprint(response, "data: {\"type\":\"keepalive\"}\n\n")
		response.(http.Flusher).Flush()
		<-request.Context().Done()
		canceled <- struct{}{}
	}))
	t.Cleanup(upstream.Close)
	client, done, receipts := newInterruptedWSHandlerHarness(t, upstream.URL, service.OpenAIWSIngressModeHTTPBridge, 0, nil)
	requestContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Write(requestContext, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":[]}`)))
	_, message, err := client.Read(requestContext)
	require.NoError(t, err)
	require.Equal(t, "keepalive", gjson.GetBytes(message, "type").String())
	require.NoError(t, client.CloseNow())
	select {
	case receipt := <-receipts.receipts:
		require.Equal(t, "interrupted", receipt.CompletionState)
		require.Equal(t, "resp_heartbeat", receipt.Key.RequestID)
		require.Equal(t, int64(1851), receipt.Key.APIKeyID)
		require.Equal(t, int64(1751), receipt.Key.UserID)
		require.Equal(t, 7, receipt.Usage.InputTokens)
		require.Equal(t, 2, receipt.Usage.OutputTokens)
		require.Nil(t, receipt.Usage.FirstTokenMs, "transport heartbeat is not model output")
	case <-requestContext.Done():
		t.Fatal("heartbeat disconnect did not persist observed usage")
	}
	select {
	case <-canceled:
	case <-requestContext.Done():
		t.Fatal("heartbeat disconnect did not cancel upstream")
	}
	select {
	case <-done:
	case <-requestContext.Done():
		t.Fatal("heartbeat disconnect did not release handler")
	}
	require.Equal(t, int32(1), attempts.Load(), "disconnected clients must never replay")
	require.Empty(t, receipts.receipts)
	receipts.mutex.Lock()
	defer receipts.mutex.Unlock()
	require.Len(t, receipts.stored, 1)
	require.Len(t, receipts.settled, 1)
	for _, count := range receipts.settled {
		require.Equal(t, 1, count)
	}
}

func TestWebSocketContextWindowRolloverReceiptIsolation(t *testing.T) {
	forwarded := make(chan []byte, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := coderws.Accept(response, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		for turn := 1; turn <= 3; turn++ {
			_, payload, readErr := connection.Read(request.Context())
			if readErr != nil {
				return
			}
			forwarded <- payload
			terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_window_%d","model":"gpt-5.1","usage":{"input_tokens":%d,"output_tokens":2}}}`, turn, turn*7)
			if connection.Write(request.Context(), coderws.MessageText, []byte(terminal)) != nil {
				return
			}
		}
		_, _, _ = connection.Read(request.Context())
	}))
	t.Cleanup(upstream.Close)
	client, done, receipts := newInterruptedWSHandlerHarness(t, upstream.URL, service.OpenAIWSIngressModeCtxPool, 0, nil)
	requestContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for turn, payload := range []string{
		`{"type":"response.create","model":"gpt-5.1","input":[],"client_metadata":{"x-codex-window-id":"window-first"}}`,
		`{"type":"response.create","model":"gpt-5.1","input":[],"previous_response_id":"resp_window_1","client_metadata":{"x-codex-window-id":"window-first"}}`,
		`{"type":"response.create","model":"gpt-5.1","input":[],"previous_response_id":"resp_window_2","client_metadata":{"x-codex-window-id":"window-next"}}`,
	} {
		require.NoError(t, client.Write(requestContext, coderws.MessageText, []byte(payload)))
		_, _, err := client.Read(requestContext)
		require.NoError(t, err)
		select {
		case sent := <-forwarded:
			if turn == 1 {
				require.Equal(t, "resp_window_1", gjson.GetBytes(sent, "previous_response_id").String())
			} else {
				require.False(t, gjson.GetBytes(sent, "previous_response_id").Exists())
			}
		case <-requestContext.Done():
			t.Fatal("turn was not forwarded")
		}
		select {
		case receipt := <-receipts.receipts:
			require.Equal(t, "complete", receipt.CompletionState)
			require.Equal(t, fmt.Sprintf("resp_window_%d", turn+1), receipt.Key.RequestID)
			require.Equal(t, (turn+1)*7, receipt.Usage.InputTokens)
			require.Equal(t, 2, receipt.Usage.OutputTokens)
		case <-requestContext.Done():
			t.Fatal("turn did not persist its own usage")
		}
	}
	require.NoError(t, client.CloseNow())
	select {
	case <-done:
	case <-requestContext.Done():
		t.Fatal("handler did not release context-window session")
	}
	require.Empty(t, forwarded)
	require.Empty(t, receipts.receipts)
}
