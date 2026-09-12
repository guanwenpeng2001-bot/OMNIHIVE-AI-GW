//go:build unit

package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cursorDriverStub struct {
	run func(context.Context, cursor.Credentials, func(cursor.StreamEvent) error) (cursor.TokenUsage, error)
}

func (driver cursorDriverStub) Run(ctx context.Context, credentials cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
	return driver.run(ctx, credentials, emit)
}
func (driver cursorDriverStub) AvailableModels(context.Context, cursor.Credentials) ([]cursor.AvailableModel, error) {
	return nil, errors.New("picker unavailable")
}

func cursorTestGateway() (*CursorGatewayService, *Account, *atomic.Int32) {
	provider := NewCursorSessionProvider(newCursorMemoryCache())
	exchanges := &atomic.Int32{}
	provider.exchange = func(context.Context, string) (cursor.Session, error) {
		return cursor.Session{Token: fmt.Sprintf("session-%d", exchanges.Add(1)), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	account := &Account{ID: 1, Platform: PlatformCursor, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "key"}}
	return NewCursorGatewayService(provider, nil), account, exchanges
}

func TestCursorGatewayChatRetryUsageAndStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			gateway, account, exchanges := cursorTestGateway()
			gateway.native = cursorDriverStub{run: func(_ context.Context, credentials cursor.Credentials, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				if credentials.AccessToken == "session-1" {
					return cursor.TokenUsage{}, &cursor.HTTPError{Status: 401}
				}
				require.NoError(t, emit(cursor.StreamEvent{Type: "thinking", Text: "reason"}))
				require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "hello"}))
				return cursor.TokenUsage{InputTokens: 12, OutputTokens: 5}, nil
			}}
			writer := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(writer)
			result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(fmt.Sprintf(`{"model":"default","stream":%t,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`, stream)))
			require.NoError(t, err)
			require.Equal(t, int32(2), exchanges.Load())
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Contains(t, writer.Body.String(), "hello")
			require.Contains(t, writer.Body.String(), "reasoning_content")
			if stream {
				require.Contains(t, writer.Body.String(), "[DONE]")
			} else {
				require.Contains(t, writer.Body.String(), "chat.completion")
			}
		})
	}
}

func TestCursorGatewayFailoverOnlyBeforeOutput(t *testing.T) {
	for _, emitContent := range []bool{false, true} {
		t.Run(fmt.Sprint(emitContent), func(t *testing.T) {
			gateway, account, exchanges := cursorTestGateway()
			gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				if emitContent {
					_ = emit(cursor.StreamEvent{Type: "text", Text: "partial"})
				}
				return cursor.TokenUsage{InputTokens: 2, OutputTokens: 1}, &cursor.HTTPError{Status: 401}
			}}
			writer := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(writer)
			result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
			require.Error(t, err)
			var failover *UpstreamFailoverError
			if emitContent {
				require.False(t, errors.As(err, &failover))
				require.NotNil(t, result)
				require.Equal(t, int32(1), exchanges.Load())
				require.NotContains(t, writer.Body.String(), "[DONE]")
			} else {
				require.True(t, errors.As(err, &failover))
				require.False(t, requestContext.Writer.Written())
				require.Equal(t, int32(2), exchanges.Load())
			}
		})
	}
}

func TestCursorGatewayNativeConcurrentCallsSerialize(t *testing.T) {
	gateway, account, _ := cursorTestGateway()
	var active, peak atomic.Int32
	gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); count > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, count) {
				break
			}
		}
		_ = emit(cursor.StreamEvent{Type: "text", Text: "ok"})
		return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, nil
	}}
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			_, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}]}`))
			require.NoError(t, err)
		}()
	}
	workers.Wait()
	require.Equal(t, int32(1), peak.Load())
}

func TestCursorRelayChatFakeServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/chat/completions", request.URL.Path)
		require.Equal(t, "Bearer relay-key", request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("X-Cursor-Checksum"))
		_, _ = fmt.Fprint(writer, `{"id":"relay","choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`)
	}))
	defer server.Close()
	gateway, account, exchanges := cursorTestGateway()
	account.Type = AccountTypeUpstream
	account.Credentials = map[string]any{"base_url": server.URL, "api_key": "relay-key"}
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, int32(0), exchanges.Load())
	require.True(t, strings.Contains(writer.Body.String(), "hello"))
}

func TestCursorGatewayNativeHTTP2FakeServer(t *testing.T) {
	// Synthetic AgentService fixtures, identical to the protocol contract test.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != cursor.RunEndpoint {
			writer.WriteHeader(503)
			return
		}
		if request.ProtoMajor != 2 {
			writer.WriteHeader(505)
			return
		}
		prefix := make([]byte, 5)
		if _, err := io.ReadFull(request.Body, prefix); err != nil {
			return
		}
		writer.Header().Set("Content-Type", "application/connect+proto")
		for _, fixture := range []string{"000000000b0a090a070a0568656c6c6f", "00000000080a067204080c1005"} {
			frame, _ := hex.DecodeString(fixture)
			_, _ = writer.Write(frame)
			writer.(http.Flusher).Flush()
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	for _, stream := range []bool{false, true} {
		gateway, account, _ := cursorTestGateway()
		gateway.native = &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL}
		writer := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(writer)
		result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(fmt.Sprintf(`{"model":"default","stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)))
		require.NoError(t, err)
		require.Equal(t, 12, result.Usage.InputTokens)
		require.Contains(t, writer.Body.String(), "hello")
	}
}

func TestCursorRelayStreamUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, "data: {\"id\":\"relay\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		writer.(http.Flusher).Flush()
		_, _ = fmt.Fprint(writer, "data: {\"id\":\"relay\",\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	gateway, account, exchanges := cursorTestGateway()
	account.Type = AccountTypeUpstream
	account.Credentials = map[string]any{"api_key": "relay-key", "base_url": server.URL}
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, int32(0), exchanges.Load())
	require.Contains(t, writer.Body.String(), "hello")
	require.Contains(t, writer.Body.String(), "[DONE]")
}

func TestCursorMissingTurnEndedMarksEstimates(t *testing.T) {
	gateway, account, _ := cursorTestGateway()
	gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
		_ = emit(cursor.StreamEvent{Type: "text", Text: "hello"})
		return cursor.TokenUsage{Estimated: true}, nil
	}}
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Positive(t, result.Usage.InputTokens)
	require.Positive(t, result.Usage.OutputTokens)
	require.Equal(t, "estimated_missing_turn_ended", writer.Header().Get("X-Sub2API-Usage-Source"))
	require.Contains(t, writer.Body.String(), `"estimated":true`)
}
