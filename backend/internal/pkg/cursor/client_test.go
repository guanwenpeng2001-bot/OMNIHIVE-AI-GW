package cursor

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These synthetic fixtures encode the documented AgentServerMessage contract;
// they are not captured traffic and make no claim of real credential validation.
func TestRunHTTP2DuplexKVAndUsage(t *testing.T) {
	serverErrors := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ProtoMajor != 2 {
			serverErrors <- io.ErrUnexpectedEOF
			return
		}
		first, err := readFrame(request.Body)
		if err != nil {
			serverErrors <- err
			return
		}
		decoded, err := fields(first.payload)
		if err != nil || findField(decoded, clientRun).number == 0 {
			serverErrors <- io.ErrUnexpectedEOF
			return
		}
		writer.Header().Set("Content-Type", "application/connect+proto")
		var setter, operation, serverMessage message
		setter.text(blobIdentifier, "hash")
		setter.text(blobContents, "history blob")
		operation.number(kvIdentifier, 7)
		operation.bytes(kvSet, setter)
		serverMessage.bytes(serverKV, operation)
		_, _ = writer.Write(encodeFrame(serverMessage))
		writer.(http.Flusher).Flush()
		if _, err := readFrame(request.Body); err != nil {
			serverErrors <- err
			return
		}
		var getter, getOperation, getMessage message
		getter.text(blobIdentifier, "hash")
		getOperation.number(kvIdentifier, 8)
		getOperation.bytes(kvGet, getter)
		getMessage.bytes(serverKV, getOperation)
		_, _ = writer.Write(encodeFrame(getMessage))
		writer.(http.Flusher).Flush()
		reply, err := readFrame(request.Body)
		if err != nil || !bytes.Contains(reply.payload, []byte("history blob")) {
			serverErrors <- io.ErrUnexpectedEOF
			return
		}
		// text=hello, thinking=why, then turn_ended(input=12, output=5).
		for _, fixture := range []string{"0a090a070a0568656c6c6f", "0a0722050a03776879", "0a067204080c1005"} {
			payload, _ := hex.DecodeString(fixture)
			_, _ = writer.Write(encodeFrame(payload))
			writer.(http.Flusher).Flush()
		}
		serverErrors <- nil
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := &Client{HTTPClient: server.Client(), BaseURL: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var events []StreamEvent
	usage, err := client.Run(ctx, Credentials{AccessToken: "session", MachineID: strings.Repeat("a", 64), MacMachineID: strings.Repeat("b", 64)}, []ChatMessage{{Role: "user", Content: "hello"}}, "default", func(event StreamEvent) error { events = append(events, event); return nil })
	require.NoError(t, err)
	require.NoError(t, <-serverErrors)
	require.Len(t, events, 2)
	require.Equal(t, "hello", events[0].Text)
	require.Equal(t, "thinking", events[1].Type)
	require.Equal(t, 12, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.False(t, usage.Estimated)
}

func TestRunCancellationClosesDuplexBody(t *testing.T) {
	accepted := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = readFrame(request.Body)
		writer.WriteHeader(200)
		writer.(http.Flusher).Flush()
		close(accepted)
		<-request.Context().Done()
		close(closed)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := (&Client{HTTPClient: server.Client(), BaseURL: server.URL}).Run(ctx, Credentials{}, []ChatMessage{{Role: "user", Content: "hello"}}, "default", func(StreamEvent) error { return nil })
		finished <- err
	}()
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start")
	}
	cancel()
	select {
	case err := <-finished:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not cancel")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("server body remained open")
	}
}

func TestWireRejectsMalformedInput(t *testing.T) {
	for _, fixture := range []string{"00", "0affffffffffffffffff01", "08ffffffffffffffffff02", "0aff", "09ff"} {
		payload, _ := hex.DecodeString(fixture)
		_, err := fields(payload)
		require.Error(t, err, fixture)
	}
	_, err := readFrame(bytes.NewReader([]byte{0, 255, 255, 255, 255}))
	require.Error(t, err)
	_, err = readFrame(bytes.NewReader([]byte{0, 0, 0, 0, 2, 1}))
	require.Error(t, err)
	_, err = ResolveModel("unknown", "", nil)
	require.Error(t, err)
	model, err := ResolveModel("picker", "high", []AvailableModel{{ID: "picker", LegacySlugs: []string{"live-medium", "live-high"}}})
	require.NoError(t, err)
	require.Equal(t, "live-high", model)
}

func TestExchangeKeyAndExpiry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer crsr_example", request.Header.Get("Authorization"))
		require.Equal(t, "/auth/exchange_user_api_key", request.URL.Path)
		_, _ = io.WriteString(writer, `{"accessToken":"temporary-session","expires_in":600}`)
	}))
	defer server.Close()
	session, err := ExchangeKey(context.Background(), server.Client(), server.URL, "crsr_example")
	require.NoError(t, err)
	require.Equal(t, "temporary-session", session.Token)
	require.InDelta(t, 540, time.Until(session.ExpiresAt).Seconds(), 2)
}
