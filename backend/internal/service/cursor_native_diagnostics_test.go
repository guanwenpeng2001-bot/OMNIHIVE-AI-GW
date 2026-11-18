package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorMetadataDiagnosticsUseExistingRecorder(test *testing.T) {
	for _, scenario := range []struct{ encoded, reason string }{
		{"1209088080808010a20200", "malformed_mcp_metadata_identity"},
		{"12070829a202020801", "malformed_mcp_server_filter"},
		{"1205d202020801", "malformed_mcp_precheck"},
		{"1208a202050a03617069", "mcp_metadata_reply_failed"},
	} {
		test.Run(scenario.reason, func(test *testing.T) {
			payload, err := hex.DecodeString(scenario.encoded)
			require.NoError(test, err)
			frame := append(binary.BigEndian.AppendUint32([]byte{0}, uint32(len(payload))), payload...)
			client := &cursor.Client{HTTPClient: &http.Client{Transport: cursorMetadataDiagnosticTransport{frame: frame}}}
			_, err = client.Run(context.Background(), cursor.Credentials{}, nil, "default", func(cursor.StreamEvent) error {
				test.Fatal("metadata must not emit an event")
				return nil
			})
			require.Error(test, err)
			requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			appendCursorNativeFailure(context.Background(), requestContext, &Account{Platform: PlatformCursor}, "native_run", fmt.Errorf("private-metadata-context: %w", err))
			raw, _ := requestContext.Get(OpsUpstreamErrorsKey)
			storedEvents, ok := raw.([]*OpsUpstreamErrorEvent)
			require.True(test, ok)
			entry := &OpsInsertErrorLogInput{UpstreamErrors: storedEvents}
			require.NoError(test, SanitizeOpsUpstreamErrorsForQueue(entry))
			require.NotContains(test, *entry.UpstreamErrorsJSON, "private-metadata-context")
			events, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
			require.NoError(test, err)
			require.Len(test, events, 1)
			require.Equal(test, scenario.reason, events[0].Reason)
			require.Equal(test, "Cursor native request failed: "+scenario.reason, events[0].Message)
			require.Empty(test, events[0].Detail)
		})
	}
}

type cursorMetadataDiagnosticTransport struct{ frame []byte }

func (transport cursorMetadataDiagnosticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// Closing the upload makes the valid metadata fixture exercise reply failure.
	_ = request.Body.Close()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(transport.frame))}, nil
}

func TestCursorConnectMessageClassification(t *testing.T) {
	for _, test := range []struct{ message, label string }{
		{"unknown option '--system-prompt'", "system_prompt"},
		{"Quota exhausted", "quota"}, {"too many requests", "rate_limit"},
		{"context window exceeded", "context_length"}, {"invalid tool schema", "tool_or_schema"},
		{"model is not available", "model_unavailable"}, {"permission denied", "permission"},
		{"deadline exceeded", "timeout"}, {" \n", "empty"}, {"internal failure", "unclassified"},
		{strings.Repeat("x", 16*1024) + " quota", "unclassified"},
	} {
		require.Equal(t, test.label, classifyCursorConnectMessage(test.message))
	}
}

func TestCursorConnectDiagnosticsEmptyAndOriginalBytes(t *testing.T) {
	for _, message := range []string{"", "上下文", "  internal  ", "{\n\"z\": 1, \"a\": 2}"} {
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		appendCursorNativeFailure(context.Background(), requestContext, &Account{Platform: PlatformCursor}, "native_run", &cursor.ConnectError{Code: "internal", Message: message})
		raw, _ := requestContext.Get(OpsUpstreamErrorsKey)
		storedEvents, ok := raw.([]*OpsUpstreamErrorEvent)
		require.True(t, ok)
		entry := &OpsInsertErrorLogInput{UpstreamErrors: storedEvents}
		require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
		events, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
		require.NoError(t, err)
		require.Equal(t, len(message), *events[0].ConnectMessageBytes)
		require.False(t, *events[0].CredentialLike)
		if message == "" {
			require.Equal(t, "empty", events[0].ConnectMessageClass)
		}
	}
}

func TestCursorConnectDiagnosticsQueueNeverStoresMessage(t *testing.T) {
	for _, message := range []string{
		"quota exceeded sk-fakeIncidentCredential123456",
		"permission denied Authorization: Bearer fakeIncidentCredential123456",
		`{"access_token":"fakeIncidentCredential123456","message":"quota exceeded"}`,
		"quota exceeded https://fake.example/error?api_key=fakeIncidentCredential123456",
	} {
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		run := &cursor.RunDiagnostics{Model: "composer-2.5-fast", ToolCount: 1, InstructionPresent: true, MessageCount: 3, EncodedBytes: 123}
		err := fmt.Errorf("wrapped: %w", &cursor.ConnectError{Code: "internal", Message: message})
		appendCursorNativeFailure(context.Background(), requestContext, &Account{ID: 50, Platform: PlatformCursor}, "native_run", err, run)
		run.Model = "changed-after-event"
		raw, _ := requestContext.Get(OpsUpstreamErrorsKey)
		storedEvents, ok := raw.([]*OpsUpstreamErrorEvent)
		require.True(t, ok)
		entry := &OpsInsertErrorLogInput{UpstreamErrors: storedEvents}
		require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
		stored := *entry.UpstreamErrorsJSON
		for _, private := range []string{message, "fakeIncidentCredential123456", "fake.example", "changed-after-event"} {
			require.NotContains(t, stored, private)
		}
		events, err := ParseOpsUpstreamErrors(stored)
		require.NoError(t, err)
		require.Len(t, events, 1)
		event := events[0]
		require.Equal(t, "connect_internal", event.Reason)
		require.Equal(t, "Cursor native request failed: connect_internal", event.Message)
		require.Equal(t, 200, event.UpstreamStatusCode)
		require.Equal(t, len(message), *event.ConnectMessageBytes)
		require.True(t, *event.CredentialLike)
		require.Equal(t, "composer-2.5-fast", event.CursorRun.Model)
		require.Equal(t, 123, event.CursorRun.EncodedBytes)
	}
	old, err := ParseOpsUpstreamErrors(`[{"kind":"connect_error","reason":"connect_internal","message":"Cursor native request failed: connect_internal"}]`)
	require.NoError(t, err)
	require.Empty(t, old[0].ConnectMessageClass)
	require.Nil(t, old[0].ConnectMessageBytes)
	require.Nil(t, old[0].CredentialLike)
	require.Nil(t, old[0].CursorRun)
}
