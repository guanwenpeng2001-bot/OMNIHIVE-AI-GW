package apicompat

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPreserveResponsesToolIDsRetainsOpaqueParts(t *testing.T) {
	source := &ResponsesRequest{Input: json.RawMessage(`[{"type":"function_call","call_id":"call_original","name":"tool","arguments":"{}"},{"type":"function_call_output","call_id":"call_original","output":"ok"}]`)}
	target, err := ResponsesToAnthropicRequest(source)
	require.NoError(t, err)
	require.NoError(t, PreserveResponsesToolIDs(source, target))
	data, err := json.Marshal(target)
	require.NoError(t, err)
	require.Contains(t, string(data), `"id":"call_original"`)
	require.Contains(t, string(data), `"tool_use_id":"call_original"`)
	raw := json.RawMessage(`[{"type":"thinking","thinking":"private","signature":"opaque","provider_extra":{"n":9007199254740993}}]`)
	target.Messages[0].Content = raw
	require.NoError(t, PreserveResponsesToolIDs(source, target))
	require.Equal(t, raw, target.Messages[0].Content)
}
