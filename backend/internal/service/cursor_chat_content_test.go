//go:build unit

package service

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// cursorRunRequest records one native AgentService/Run request as the fake
// upstream sees it: method, path, protocol version, credentials and the first
// (non-idempotent) Run frame.
type cursorRunRequest struct {
	method, path                  string
	proto                         int
	authorization, contentType    string
	protocolVersion, clientType   string
	clientVersion, acceptEncoding string
	frame                         []byte
}

// Chat Completions content parts are part of the standard request shape, so a
// text-part array must reach the native Run as flattened text instead of being
// rejected as malformed. The frame assertion pins the real upstream shape:
// Connect path, HTTP/2, session bearer, model and the exact prompt text.
func TestCursorChatContentPartsReachTheRunWire(t *testing.T) {
	requests := make(chan cursorRunRequest, 4)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != cursor.RunEndpoint {
			// The model picker and any other native probe are not this contract.
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		header := make([]byte, 5)
		if _, err := io.ReadFull(request.Body, header); err != nil {
			return
		}
		frame := make([]byte, binary.BigEndian.Uint32(header[1:]))
		if _, err := io.ReadFull(request.Body, frame); err != nil {
			return
		}
		requests <- cursorRunRequest{
			method: request.Method, path: request.URL.Path, proto: request.ProtoMajor,
			authorization: request.Header.Get("Authorization"), contentType: request.Header.Get("Content-Type"),
			protocolVersion: request.Header.Get("Connect-Protocol-Version"), clientType: request.Header.Get("X-Cursor-Client-Type"),
			clientVersion: request.Header.Get("X-Cursor-Client-Version"), acceptEncoding: request.Header.Get("Connect-Accept-Encoding"),
			frame: frame,
		}
		writer.Header().Set("Content-Type", "application/connect+proto")
		for _, fixture := range []string{"000000000b0a090a070a0568656c6c6f", "00000000080a067204080c1005"} {
			payload, _ := hex.DecodeString(fixture)
			_, _ = writer.Write(payload)
			writer.(http.Flusher).Flush()
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	gateway, account, exchanges := cursorTestGateway()
	gateway.native = &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL}
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	body := []byte(`{"model":"default","messages":[{"role":"system","content":[{"type":"text","text":"instruction"}]},{"role":"user","content":[{"type":"text","text":"first "},{"type":"text","text":"second"}]}]}`)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, writer.Code)
	require.Contains(t, writer.Body.String(), "hello")
	require.Equal(t, int32(1), exchanges.Load())

	captured := <-requests
	require.Equal(t, http.MethodPost, captured.method)
	require.Equal(t, cursor.RunEndpoint, captured.path)
	require.Equal(t, 2, captured.proto)
	require.Equal(t, "Bearer session-1", captured.authorization)
	require.Equal(t, "application/connect+proto", captured.contentType)
	require.Equal(t, "1", captured.protocolVersion)
	require.Equal(t, "cli", captured.clientType)
	require.Equal(t, cursor.DefaultClientVersion, captured.clientVersion)
	require.Equal(t, "gzip", captured.acceptEncoding)
	require.Contains(t, string(captured.frame), "first second", "part texts must reach the Run frame in order")
	require.Contains(t, string(captured.frame), result.UpstreamModel, "the resolved Cursor model must reach the Run frame")
}

// Media parts have no verified field in the Run text contract. The request must
// fail closed on every Cursor ingress, before any upstream call, and must never
// be forwarded as if the media were text.
func TestCursorMediaContentPartsFailClosed(t *testing.T) {
	chatBody := []byte(`{"model":"default","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`)
	anthropicBody := []byte(`{"model":"default","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`)
	for name, call := range map[string]func(*CursorGatewayService, *gin.Context, *Account, []byte) (*ForwardResult, error){
		"chat": func(gateway *CursorGatewayService, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
			return gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body)
		},
		"anthropic": func(gateway *CursorGatewayService, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
			return gateway.ForwardAsAnthropic(context.Background(), requestContext, account, body)
		},
	} {
		body := chatBody
		if name == "anthropic" {
			body = anthropicBody
		}
		nativeCalls := 0
		gateway, account, _ := cursorTestGateway()
		gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
			nativeCalls++
			_ = emit(cursor.StreamEvent{Type: "text", Text: "unexpected"})
			return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, nil
		}}
		writer := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(writer)
		result, err := call(gateway, requestContext, account, body)
		require.Error(t, err, name)
		require.Nil(t, result, name)
		require.Equal(t, http.StatusBadRequest, writer.Code, name)
		require.Contains(t, writer.Body.String(), "only text content is supported", name)
		require.Zero(t, nativeCalls, "%s must not start an upstream Run", name)
	}
}

// The relay account form owns its own modality contract: the configured
// upstream decides what it accepts, so the native text-only boundary must not
// rewrite or reject the body on that path.
func TestCursorRelayForwardsMediaContentPartsUnchanged(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		received <- string(body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"relay","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()
	gateway, account, _ := cursorTestGateway()
	account.Type = AccountTypeUpstream
	account.Credentials = map[string]any{"api_key": "relay-key", "base_url": server.URL}
	body := []byte(`{"model":"default","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`)
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	_, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, writer.Code)
	forwarded := <-received
	require.Contains(t, forwarded, "image_url")
	require.Contains(t, forwarded, "https://example.test/a.png")
}

// A text-part history is also the shape standard tool-calling clients replay,
// so the flattened text and the tool identity must survive the chat ingress.
func TestCursorChatContentPartsPreserveToolHistory(t *testing.T) {
	seen := make(chan []cursor.ChatMessage, 1)
	gateway, account, _ := cursorTestGateway()
	gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, messages []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
		seen <- messages
		_ = emit(cursor.StreamEvent{Type: "text", Text: "hello"})
		return cursor.TokenUsage{InputTokens: 3, OutputTokens: 1}, nil
	}}
	body := []byte(`{"model":"default","messages":[{"role":"assistant","content":[],"tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":[{"type":"text","text":"sunny"}]},{"role":"user","content":[{"type":"text","text":"and tomorrow?"}]}]}`)
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	_, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, writer.Code)
	messages := <-seen
	require.Len(t, messages, 3)
	require.Empty(t, messages[0].Content)
	require.Contains(t, string(messages[0].ToolCalls), "call-1")
	require.Equal(t, "sunny", messages[1].Content)
	require.Equal(t, "call-1", messages[1].ToolCallID)
	require.Equal(t, "and tomorrow?", messages[2].Content)
}
