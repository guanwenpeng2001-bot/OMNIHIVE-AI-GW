package cursor

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestAnthropicBridgeTrailingInstructionRunAction(t *testing.T) {
	for _, role := range []string{"system", "developer"} {
		for _, options := range []bool{false, true} {
			request := apicompat.AnthropicRequest{Model: "composer-2.5", MaxTokens: 128, Messages: []apicompat.AnthropicMessage{
				{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"ACTUAL_USER_ACTION"}]`)},
				{Role: role, Content: json.RawMessage(`[{"type":"text","text":"DYNAMIC_INSTRUCTION"}]`)},
			}}
			var expected []string
			if options {
				request.System = json.RawMessage(`"STATIC_INSTRUCTION"`)
				request.Stream = true
				expected = append(expected, "system: STATIC_INSTRUCTION")
			}
			expected = append(expected, role+": DYNAMIC_INSTRUCTION")
			converted, err := apicompat.AnthropicToChatCompletionsRequest(&request)
			require.NoError(t, err)
			var messages []ChatMessage
			for _, item := range converted.Messages {
				var text string
				require.NoError(t, json.Unmarshal(item.Content, &text))
				messages = append(messages, ChatMessage{Role: item.Role, Content: text})
			}
			encoded, _ := encodeRun(messages, "composer-2.5-fast")
			run := runFieldsOf(t, encoded)
			prepends, action := userActionParts(t, run)
			require.Equal(t, "ACTUAL_USER_ACTION", action)
			require.Equal(t, expected, prepends)
			require.Zero(t, findField(run, 8).number)
		}
	}
}
