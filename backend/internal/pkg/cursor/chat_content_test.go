package cursor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Cursor accepts both Chat Completions content shapes on the shared chat
// ingress. Every other message field keeps the plain decoder behaviour.
func TestChatMessageAcceptsBothContentShapes(t *testing.T) {
	var text ChatMessage
	require.NoError(t, json.Unmarshal([]byte(`{"role":"user","content":"plain"}`), &text))
	require.Equal(t, ChatMessage{Role: "user", Content: "plain"}, text)

	var parts ChatMessage
	require.NoError(t, json.Unmarshal([]byte(`{"role":"user","content":[{"type":"text","text":"first "},{"type":"text","text":"second"}]}`), &parts))
	require.Equal(t, ChatMessage{Role: "user", Content: "first second"}, parts)

	// Null content and an empty part array are the standard assistant
	// tool-call history shapes; both stay valid empty text.
	for _, body := range []string{
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]}`,
		`{"role":"assistant","content":[],"tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]}`,
		`{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]}`,
	} {
		var history ChatMessage
		require.NoError(t, json.Unmarshal([]byte(body), &history))
		require.Empty(t, history.Content)
		require.JSONEq(t, `[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]`, string(history.ToolCalls))
	}

	var toolResult ChatMessage
	require.NoError(t, json.Unmarshal([]byte(`{"role":"tool","tool_call_id":"call-1","content":[{"type":"text","text":"sunny"}]}`), &toolResult))
	require.Equal(t, ChatMessage{Role: "tool", Content: "sunny", ToolCallID: "call-1"}, toolResult)
}

// Media parts have no field in the verified Run text contract, so they must be
// rejected with ErrNonTextContent instead of reaching upstream as text or being
// dropped. This is the Cursor side of the visual-input boundary: the gateway
// never passes an image on as if it were text.
func TestChatMessageRejectsNonTextContent(t *testing.T) {
	for name, part := range map[string]string{
		"openai_image_url":   `{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}`,
		"openai_input_image": `{"type":"input_image","image_url":"data:image/png;base64,AAAA"}`,
		"anthropic_image":    `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}`,
		"audio":              `{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}`,
		"file":               `{"type":"file","file":{"filename":"notes.pdf"}}`,
	} {
		body := `{"role":"user","content":[{"type":"text","text":"look"},` + part + `]}`
		var message ChatMessage
		err := json.Unmarshal([]byte(body), &message)
		require.Error(t, err, name)
		require.ErrorIs(t, err, ErrNonTextContent, name)
	}

	var message ChatMessage
	require.Error(t, json.Unmarshal([]byte(`{"role":"user","content":{"type":"text","text":"hi"}}`), &message))
	require.Error(t, json.Unmarshal([]byte(`{"role":"user","content":12}`), &message))
}

// Marshalling stays the plain struct encoding: the Run conversation history is
// built from these values, so a codec round trip must not add or lose fields.
func TestChatMessageMarshalsUnchanged(t *testing.T) {
	message := ChatMessage{Role: "tool", Content: "sunny", ToolCallID: "call-1", ToolExecID: "exec-1", ToolCalls: json.RawMessage(`[{"id":"call-1"}]`)}
	encoded, err := json.Marshal(message)
	require.NoError(t, err)
	require.JSONEq(t, `{"role":"tool","content":"sunny","tool_calls":[{"id":"call-1"}],"tool_call_id":"call-1","cursor_exec_id":"exec-1"}`, string(encoded))

	var decoded ChatMessage
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, message, decoded)
	var missing ChatMessage
	require.NoError(t, json.Unmarshal([]byte(`{}`), &missing))
	require.Equal(t, ChatMessage{}, missing)
	var null ChatMessage
	require.NoError(t, json.Unmarshal([]byte(`null`), &null))
	require.Equal(t, ChatMessage{}, null)
}
