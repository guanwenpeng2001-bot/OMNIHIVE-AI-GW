package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func mcpFixture(id uint32, callID string, arguments map[string]any) message {
	var args, exec, server message
	args.text(1, "weather")
	for key, value := range arguments {
		encoded, _ := structpb.NewValue(value)
		data, _ := proto.Marshal(encoded)
		var entry message
		entry.text(1, key)
		entry.bytes(2, data)
		args.bytes(2, entry)
	}
	args.text(3, callID)
	args.text(4, "api")
	args.text(5, "weather")
	args.text(9, "api")
	exec.number(1, uint64(id))
	exec.text(15, "exec-"+callID)
	exec.bytes(11, args)
	server.bytes(2, exec)
	return server
}

func TestMCPWireDefinitionArgumentsAndReply(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)
	run, _ := encodeRun([]ChatMessage{{Role: "user", Content: "weather"}}, "default", ToolDefinition{Name: "weather", Server: "api", InputSchema: schema})
	outer, err := fields(run)
	require.NoError(t, err)
	request, err := fields(findField(outer, 1).data)
	require.NoError(t, err)
	tools, err := fields(findField(request, 4).data)
	require.NoError(t, err)
	definition, err := fields(findField(tools, 1).data)
	require.NoError(t, err)
	require.Equal(t, "api-weather", string(findField(definition, 1).data))
	require.Equal(t, "api", string(findField(definition, 4).data))
	require.Equal(t, "weather", string(findField(definition, 5).data))
	require.Zero(t, findField(definition, 6).number)
	schemaField := findField(definition, 3)
	require.EqualValues(t, wireBytes, schemaField.wire)
	var decodedSchema structpb.Value
	require.NoError(t, proto.Unmarshal(schemaField.data, &decodedSchema))
	require.Equal(t, map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}}, decodedSchema.AsInterface())
	arguments := map[string]any{"city": "Paris", "nested": map[string]any{"a": []any{true, nil, 2.5}}, "count": float64(3)}
	events, err := parseEvents(mcpFixture(42, "call-1", arguments))
	require.NoError(t, err)
	require.Len(t, events, 1)
	call := events[0].ToolCall
	require.Equal(t, uint32(42), call.ID)
	require.Equal(t, "exec-call-1", call.ExecID)
	require.Equal(t, "api", call.ServerIdentifier)
	var actual map[string]any
	require.NoError(t, json.Unmarshal(call.Arguments, &actual))
	require.Equal(t, arguments, actual)
	reply, err := fields(encodeToolReply(*call, ToolResult{Text: []string{"sunny"}, IsError: true}, ""))
	require.NoError(t, err)
	exec, err := fields(findField(reply, 2).data)
	require.NoError(t, err)
	require.Equal(t, uint64(42), findField(exec, 1).integer)
	require.Equal(t, "exec-call-1", string(findField(exec, 15).data))
	outcome, _ := fields(findField(exec, 11).data)
	success, _ := fields(findField(outcome, 1).data)
	require.Equal(t, uint64(1), findField(success, 2).integer)
	item, _ := fields(findField(success, 1).data)
	text, _ := fields(findField(item, 1).data)
	require.Equal(t, "sunny", string(findField(text, 1).data))
}

func TestMCPInputSchemaValue(t *testing.T) {
	// Cursor agent 2026.09.26-dd393fe writes Run MCP input_schema (field 3)
	// with google.protobuf.Value.fromJson, leaving input_schema_json unset.
	for _, fixture := range []struct {
		name, schema string
	}{
		{"validation_echo", `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`},
		{"nested_values", `{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"amount":{"type":"number","minimum":0,"multipleOf":0.25},"optional":{"type":["string","null"],"default":null},"enabled":{"type":"boolean","default":false}},"additionalProperties":false}}},"required":["items"],"additionalProperties":false}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tools, err := fields(encodeTools([]ToolDefinition{{Name: fixture.name, Description: "Return the supplied value.", Server: "api", InputSchema: json.RawMessage(fixture.schema)}}))
			require.NoError(t, err)
			require.Len(t, tools, 1)
			definition, err := fields(tools[0].data)
			require.NoError(t, err)
			require.Equal(t, "api-"+fixture.name, string(findField(definition, 1).data))
			require.Equal(t, "Return the supplied value.", string(findField(definition, 2).data))
			require.Equal(t, "api", string(findField(definition, 4).data))
			require.Equal(t, fixture.name, string(findField(definition, 5).data))
			require.Zero(t, findField(definition, 6).number)
			schemaField := findField(definition, 3)
			require.EqualValues(t, 3, schemaField.number)
			require.EqualValues(t, wireBytes, schemaField.wire)
			var decoded structpb.Value
			require.NoError(t, proto.Unmarshal(schemaField.data, &decoded))
			var expected map[string]any
			require.NoError(t, json.Unmarshal([]byte(fixture.schema), &expected))
			require.Equal(t, expected, decoded.AsInterface())
		})
	}
}

func TestMCPHTTP2MultiRoundAndFailures(t *testing.T) {
	for _, mode := range []string{"deferred", "success", "executor_error", "missing_turn", "cancel_reply"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			serverErrors := make(chan error, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fail := func(err error) { serverErrors <- err }
				if r.ProtoMajor != 2 {
					fail(fmt.Errorf("not h2"))
					return
				}
				if _, err := readFrame(r.Body); err != nil {
					fail(err)
					return
				}
				write := func(m message) {
					_, _ = w.Write(encodeFrame(m))
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}
				for i := 0; i < 2; i++ {
					write(mcpFixture(uint32(i), fmt.Sprint(i), map[string]any{"city": "Paris"}))
					if mode == "cancel_reply" {
						cancel()
						<-r.Context().Done()
						fail(nil)
						return
					}
					reply, err := readFrame(r.Body)
					if err != nil {
						fail(err)
						return
					}
					outer, _ := fields(reply.payload)
					exec, _ := fields(findField(outer, 2).data)
					if findField(exec, 1).integer != uint64(i) || string(findField(exec, 15).data) != "exec-"+fmt.Sprint(i) {
						fail(fmt.Errorf("wrong identity"))
						return
					}
					outcome, _ := fields(findField(exec, 11).data)
					if mode == "success" {
						if !bytes.Contains(findField(outcome, 1).data, []byte("sunny")) {
							fail(fmt.Errorf("missing success"))
							return
						}
					} else {
						failure, _ := fields(findField(outcome, 2).data)
						expected := "deferred_to_client"
						if mode == "executor_error" {
							expected = "tool_execution_failed"
						}
						if string(findField(failure, 1).data) != expected {
							fail(fmt.Errorf("wrong failure"))
							return
						}
					}
				}
				if mode != "missing_turn" {
					var delta, update, envelope message
					delta.number(1, 100)
					update.bytes(8, delta)
					envelope.bytes(1, update)
					write(envelope)
					var usage, ended, end message
					usage.number(1, 12)
					usage.number(2, 5)
					ended.bytes(14, usage)
					end.bytes(1, ended)
					write(end)
				}
				fail(nil)
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			options := ToolOptions{Definitions: []ToolDefinition{{Name: "weather", Server: "api", InputSchema: json.RawMessage(`{}`)}}}
			if mode == "success" || mode == "executor_error" {
				options.Execute = func(context.Context, ToolCall) (ToolResult, error) {
					if mode == "executor_error" {
						return ToolResult{}, errors.New("private executor detail")
					}
					return ToolResult{Text: []string{"sunny"}}, nil
				}
			}
			count := 0
			usage, err := (&Client{HTTPClient: server.Client(), BaseURL: server.URL}).RunWithTools(ctx, Credentials{}, []ChatMessage{{Role: "user", Content: "weather"}}, "default", options, func(event StreamEvent) error { require.NotNil(t, event.ToolCall); count++; return nil })
			require.NoError(t, <-serverErrors)
			if mode == "missing_turn" || mode == "cancel_reply" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 2, count)
			require.Equal(t, 12, usage.InputTokens)
			require.Equal(t, 5, usage.OutputTokens)
			require.False(t, usage.Estimated)
		})
	}
}

func TestMCPHistoryRetainsCallIdentity(t *testing.T) {
	messages := []ChatMessage{{Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"call-1","function":{"name":"weather","arguments":"{}"}}]`)}, {Role: "tool", ToolCallID: "call-1", Content: "sunny"}}
	encoded, _ := encodeRun(messages, "default")
	require.Contains(t, string(encoded), `"tool_call_id":"call-1"`)
	require.Contains(t, string(encoded), `"id":"call-1"`)
	require.Contains(t, string(encoded), "sunny")
}

func TestMCPMalformedExecRejected(t *testing.T) {
	for _, payload := range [][]byte{
		{0x10, 0x01}, // exec must be bytes
		cursorMalformedMCPIdentity(1<<32, "exec"),
		cursorMalformedMCPIdentity(0, ""),
	} {
		_, err := parseEvents(payload)
		require.Error(t, err)
	}
}

func cursorMalformedMCPIdentity(id uint64, execID string) message {
	var args, exec, server message
	args.text(3, "call")
	exec.number(1, id)
	exec.text(15, execID)
	exec.bytes(11, args)
	server.bytes(2, exec)
	return server
}

type mcpRoundTripper func(*http.Request) (*http.Response, error)

func (f mcpRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMCPReplyWriteFailureDoesNotFailover(t *testing.T) {
	attempts := 0
	client := &Client{RunHosts: []string{"https://first.invalid", "https://second.invalid"}, HTTPClient: &http.Client{Transport: mcpRoundTripper(func(r *http.Request) (*http.Response, error) {
		attempts++
		if _, err := readFrame(r.Body); err != nil {
			return nil, err
		}
		_ = r.Body.Close()
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(encodeFrame(mcpFixture(1, "call", nil))))}, nil
	})}}
	_, err := client.Run(context.Background(), Credentials{}, nil, "default", func(StreamEvent) error { t.Fatal("unacknowledged call emitted"); return nil })
	require.ErrorIs(t, err, errToolReply)
	require.Equal(t, 1, attempts)
}

func TestCursorToolCallIDNormalizationAndWireIdentity(t *testing.T) {
	raw := "call-12345678-1234-1234-1234-123456789012-0\nfc_" + strings.Repeat("a", 40)
	id := NormalizeToolCallID(raw)
	require.Len(t, id, 55)
	require.Regexp(t, `^[A-Za-z0-9_-]+$`, id)
	require.Equal(t, id, NormalizeToolCallID(raw))
	require.NotEqual(t, id, NormalizeToolCallID(strings.ReplaceAll(raw, "\n", "")))
	require.Equal(t, "call-1", NormalizeToolCallID("call-1"))
	for _, input := range []string{"\x00\r\n\t\x7f\u0085", strings.Repeat("x", 1000), "", "cursor_call_reserved"} {
		require.Len(t, NormalizeToolCallID(input), 55)
		require.Regexp(t, `^[A-Za-z0-9_-]+$`, NormalizeToolCallID(input))
	}
	call, err := parseToolCall(mcpFixture(1, raw, nil))
	require.NoError(t, err)
	require.Equal(t, raw, call.CallID)
	require.Equal(t, "exec-"+raw, call.ExecID)
	reply, err := fields(encodeToolReply(*call, ToolResult{}, "deferred_to_client"))
	require.NoError(t, err)
	exec, err := fields(findField(reply, 2).data)
	require.NoError(t, err)
	require.Equal(t, "exec-"+raw, string(findField(exec, 15).data))
}
