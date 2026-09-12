package cursor

import (
	"bytes"
	"context"
	"encoding/hex"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestCursorHeadersGolden(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	require.Equal(t, "paaotEjtmachine/mac", Checksum("machine", "mac", now))
	actual := headersAt(Credentials{AccessToken: "session", MachineID: "machine", MacMachineID: "mac", ClientVersion: "test-version"}, "run-id", now, "request-id")
	platform, architecture := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if architecture == "amd64" {
		architecture = "x64"
	}
	expected := map[string]string{
		"Authorization": "Bearer session", "Content-Type": "application/connect+proto", "Connect-Protocol-Version": "1", "Connect-Accept-Encoding": "gzip", "User-Agent": "connect-es/1.6.1",
		"X-Client-Key":      "3f3af1ecebbd1410ab417ec0d27bbfcb5d340e177ae159b59fc8626c2dfd9175",
		"X-Cursor-Checksum": "paaotEjtmachine/mac", "X-Cursor-Client-Version": "test-version", "X-Cursor-Client-Type": "cli", "X-Cursor-Client-Layout": "unifiedAgent", "X-Cursor-Client-Os": platform, "X-Cursor-Client-Arch": architecture, "X-Cursor-Client-Device-Type": "desktop", "X-Cursor-Timezone": "UTC", "X-Cursor-Agent-Allowed-Tools": "mcp_tool_call", "X-Ghost-Mode": "true", "X-Request-Id": "request-id", "X-Original-Request-Id": "run-id", "X-Amzn-Trace-Id": "Root=request-id",
	}
	require.Len(t, actual, len(expected))
	for name, value := range expected {
		require.Equal(t, value, actual.Get(name), name)
	}
	require.Equal(t, DefaultClientVersion, headersAt(Credentials{}, "", now, "").Get("X-Cursor-Client-Version"))
}

func TestCursorUnknownInteractionSkipsOpaquePayload(t *testing.T) {
	var updates, payload message
	updates.bytes(99, []byte{255})
	updates.number(100, 7)
	var text message
	text.text(deltaText, "hello")
	updates.bytes(interactionText, text)
	payload.bytes(serverInteraction, updates)
	events, err := parseEvents(payload)
	require.NoError(t, err)
	require.Equal(t, []StreamEvent{{Type: "text", Text: "hello"}}, events)
	var malformed, known message
	malformed.bytes(interactionText, []byte{255})
	known.bytes(serverInteraction, malformed)
	_, err = parseEvents(known)
	require.Error(t, err)
}

// Deterministic synthetic messages; no captured traffic or guessed tool field numbers.
func TestCursorRunUsageBoundaries(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		ending    string
		want      TokenUsage
		wantError bool
	}{
		{"empty_turn_ended", "0a027200", TokenUsage{OutputTokens: 3, Estimated: true}, false},
		{"authoritative_turn_ended", "0a0c720a080c1005180320042802", TokenUsage{InputTokens: 12, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 4, ReasoningTokens: 2}, false},
		{"error_trailer_after_delta", "", TokenUsage{OutputTokens: 3, Estimated: true}, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, err := readFrame(request.Body)
				if err != nil {
					return
				}
				delta, _ := hex.DecodeString("0a0442020803")
				_, _ = writer.Write(encodeFrame(delta))
				if scenario.wantError {
					body := []byte(`{"error":{"code":"internal","message":"private-body"}}`)
					frame := encodeFrame(body)
					frame[0] = 2
					_, _ = writer.Write(frame)
				} else {
					ending, _ := hex.DecodeString(scenario.ending)
					_, _ = writer.Write(encodeFrame(ending))
				}
				writer.(http.Flusher).Flush()
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			usage, err := (&Client{HTTPClient: server.Client(), BaseURL: server.URL}).Run(context.Background(), Credentials{}, []ChatMessage{{Role: "user", Content: "hi"}}, "default", func(StreamEvent) error { return nil })
			if scenario.wantError {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-body")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, scenario.want, usage)
		})
	}
}

func TestCursorUnknownWireFieldsPreserveKnownFields(t *testing.T) {
	decoded, err := fields([]byte{0x08, 0x01, 0x15, 0, 0, 0, 0, 0x19, 0, 0, 0, 0, 0, 0, 0, 0, 0x22, 0x01, 0xff})
	require.NoError(t, err)
	require.Equal(t, uint64(1), findField(decoded, 1).integer)
	_, err = readFrame(bytes.NewReader([]byte{0, 0, 0, 0, 1}))
	require.ErrorIs(t, err, io.EOF)
}

// Cursor 当前服务端对 Run 的 system 字段返回 invalid_argument
// "unknown option '--system-prompt'"（2026-09-12 真机验证），所以请求里必须
// 保持 field 8 空缺，system/developer 内容折进对话的 prepend 条目。
func TestCursorRunRequestOmitsSystemFieldAndFoldsPrompt(t *testing.T) {
	client, runID := encodeRun([]ChatMessage{
		{Role: "system", Content: "be terse"},
		{Role: "developer", Content: "prefer json"},
		{Role: "user", Content: "hello"},
	}, "default")
	require.NotEmpty(t, runID)

	decoded, err := fields(client)
	require.NoError(t, err)
	run := findField(decoded, clientRun)
	require.Equal(t, uint64(wireBytes), run.wire)

	runFields, err := fields(run.data)
	require.NoError(t, err)
	require.Zero(t, findField(runFields, 8).number, "system field must stay unset")

	action := findField(runFields, runAction)
	require.Equal(t, uint64(wireBytes), action.wire)
	actionFields, err := fields(action.data)
	require.NoError(t, err)
	userAction := findField(actionFields, actionUserMessage)
	require.Equal(t, uint64(wireBytes), userAction.wire)
	userActionFields, err := fields(userAction.data)
	require.NoError(t, err)

	var prepends []string
	var actionText string
	for _, field := range userActionFields {
		if field.number != userActionPrepend && field.number != userActionMessage {
			continue
		}
		entryFields, err := fields(field.data)
		require.NoError(t, err)
		text := string(findField(entryFields, userMessageText).data)
		if field.number == userActionPrepend {
			prepends = append(prepends, text)
			continue
		}
		actionText = text
	}
	require.Equal(t, []string{"system: be terse", "developer: prefer json"}, prepends)
	require.Equal(t, "hello", actionText)
}
