package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnthropicChatMidConversationInstructions(t *testing.T) {
	for _, model := range []string{"deepseek-v4-pro", "gpt-6-astra", "composer-2.5"} {
		t.Run(model, func(t *testing.T) {
			var request AnthropicRequest
			require.NoError(t, json.Unmarshal([]byte(`{"system":"STATIC","messages":[
			{"role":"user","content":"first"},
			{"role":"assistant","content":"reply"},
			{"role":"system","content":[{"type":"text","text":"  dynamic  "},{"type":"text","text":"next"}]},
			{"role":"developer","content":"developer instruction"},
			{"role":"user","content":"last"},
			{"role":"system","content":"trailing"}]}`), &request))
			request.Model = model
			converted, err := AnthropicToChatCompletionsRequest(&request)
			require.NoError(t, err)
			require.Equal(t, []string{"system", "user", "assistant", "system", "developer", "user", "system"}, chatMessageRoles(converted.Messages))
			for index, expected := range []string{"STATIC", "first", "reply", "  dynamic  \n\nnext", "developer instruction", "last", "trailing"} {
				var text string
				require.NoError(t, json.Unmarshal(converted.Messages[index].Content, &text))
				require.Equal(t, expected, text)
			}
		})
	}
}

func TestAnthropicChatInstructionsKeepToolAndImageSemantics(t *testing.T) {
	var request AnthropicRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"deepseek-v4-pro","messages":[
	{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]},
	{"role":"assistant","content":[{"type":"text","text":"calling"},{"type":"tool_use","id":"call_1","name":"lookup","input":{"q":"safe"}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"result"}]},
	{"role":"system","content":[{"type":"text","text":"retain instruction"}]}]}`), &request))
	converted, err := AnthropicToChatCompletionsRequest(&request)
	require.NoError(t, err)
	require.Equal(t, []string{"user", "assistant", "tool", "system"}, chatMessageRoles(converted.Messages))
	require.Contains(t, string(converted.Messages[0].Content), "data:image/png;base64,aGVsbG8=")
	require.Equal(t, "call_1", converted.Messages[1].ToolCalls[0].ID)
	require.Equal(t, "call_1", converted.Messages[2].ToolCallID)
	require.JSONEq(t, `"result"`, string(converted.Messages[2].Content))
	require.JSONEq(t, `"retain instruction"`, string(converted.Messages[3].Content))
}
