package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamChatSchemaStrictDefault(t *testing.T) {
	for _, tc := range []struct{ name, field, want string }{
		{"absent", "", "false"},
		{"true", `,"strict":true`, "true"},
		{"false", `,"strict":false`, "false"},
		{"null", `,"strict":null`, "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := json.RawMessage(`{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}` + tc.field + `}}`)
			before := string(input)
			resp, err := ChatCompletionsToResponses(&ChatCompletionsRequest{
				Model: "gpt-4o", Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"Return JSON"`)}}, ResponseFormat: input,
			})
			require.NoError(t, err)
			require.NotNil(t, resp.Text)
			var format map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(resp.Text.Format, &format))
			require.Equal(t, tc.want, string(format["strict"]))
			require.JSONEq(t, `{"type":"object","properties":{"ok":{"type":"boolean"}}}`, string(format["schema"]))
			require.Equal(t, before, string(input), "caller-owned request must not mutate")
		})
	}
	for _, input := range []string{`{"type":"json_object"}`, `{"type":"text"}`, `{"type":"json_schema","json_schema":null}`, `{"type":"json_schema","json_schema":[]}`} {
		require.Equal(t, input, string(chatResponseFormatToResponsesTextFormat(json.RawMessage(input))))
	}
}

func TestUpstreamBufferedSealedCallMetadata(t *testing.T) {
	for _, marker := range []string{"", "null", "[]", `["sealed-argument"]`} {
		t.Run("marker="+marker, func(t *testing.T) {
			item := `{"type":"function_call","call_id":"call_a","name":"read","namespace":"files","encrypted_content":"sealed-content"`
			if marker != "" {
				item += `,"encrypted_function_args":` + marker
			}
			item += `}`
			var evt ResponsesStreamEvent
			require.NoError(t, json.Unmarshal([]byte(`{"type":"response.output_item.added","output_index":3,"item":`+item+`}`), &evt))
			acc := NewBufferedResponseAccumulator()
			acc.ProcessEvent(&evt)
			acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.function_call_arguments.delta", OutputIndex: 3, Delta: `{"path":`})
			acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: 3, Arguments: `{"path":"a"}`})
			usage := &ResponsesUsage{InputTokens: 13, OutputTokens: 4}
			resp := &ResponsesResponse{Model: "public-alias", Status: "completed", Usage: usage}
			acc.SupplementResponseOutput(resp)
			require.Len(t, resp.Output, 1)
			require.Equal(t, "public-alias", resp.Model)
			require.Same(t, usage, resp.Usage)
			wire, err := json.Marshal(resp.Output[0])
			require.NoError(t, err)
			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(wire, &decoded))
			require.Equal(t, `"files"`, string(decoded["namespace"]))
			require.Equal(t, `"sealed-content"`, string(decoded["encrypted_content"]))
			require.Equal(t, marker, string(decoded["encrypted_function_args"]))
			require.Equal(t, `{"path":"a"}`, resp.Output[0].Arguments)
			// A complete terminal item stays authoritative over buffered metadata.
			resp.Output = []ResponsesOutput{{Type: "function_call", CallID: "call_a", Namespace: "terminal", Arguments: `{}`}}
			acc.SupplementResponseOutput(resp)
			require.Equal(t, "terminal", resp.Output[0].Namespace)
			require.Equal(t, `{}`, resp.Output[0].Arguments)
		})
	}
}
