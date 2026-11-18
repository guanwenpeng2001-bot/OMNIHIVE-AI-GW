package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatCacheWritePresenceBackport(t *testing.T) {
	for _, tc := range []struct {
		name, details string
		want          int
	}{
		{"absent", `{"cached_tokens":10,"cache_creation_tokens":20}`, 20},
		{"zero", `{"cached_tokens":10,"cache_creation_tokens":20,"cache_write_tokens":0}`, 0},
		{"write wins", `{"cached_tokens":10,"cache_creation_tokens":20,"cache_write_tokens":7}`, 7},
		{"negative write", `{"cached_tokens":10,"cache_creation_tokens":20,"cache_write_tokens":-1}`, 0},
		{"negative creation", `{"cached_tokens":10,"cache_creation_tokens":-1}`, 0},
		{"null is absent", `{"cached_tokens":10,"cache_creation_tokens":20,"cache_write_tokens":null}`, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var usage ChatUsage
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":`+tc.details+`}`), &usage))
			for cycle := 0; cycle < 3; cycle++ {
				direct := chatUsageToAnthropicUsage(&usage)
				require.Equal(t, tc.want, direct.CacheCreationInputTokens)
				require.Equal(t, 90-tc.want, direct.InputTokens)
				state := NewChatCompletionsToAnthropicStreamState("public-model")
				ChatCompletionsChunkToAnthropicEvents(&ChatCompletionsChunk{Usage: &usage}, state)
				events := FinalizeChatCompletionsAnthropicStream(state)
				var terminalUsage *AnthropicUsage
				for _, event := range events {
					if event.Type == "message_delta" {
						terminalUsage = event.Usage
					}
				}
				require.Equal(t, &direct, terminalUsage)
				require.Empty(t, FinalizeChatCompletionsAnthropicStream(state), "terminal usage must be emitted once")
				converted := ChatUsageToResponsesUsage(&usage)
				require.Equal(t, direct, anthropicUsageFromResponsesUsage(converted))
				encoded, err := json.Marshal(converted)
				require.NoError(t, err)
				var replay ResponsesUsage
				require.NoError(t, json.Unmarshal(encoded, &replay))
				require.Equal(t, direct, anthropicUsageFromResponsesUsage(&replay), "Responses JSON replay must not resurrect the fallback alias")
				encoded, err = json.Marshal(usage)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(encoded, &usage))
			}
		})
	}
}

func TestChatCacheWritePresenceRejectsMalformedBackport(t *testing.T) {
	for _, value := range []string{`"0"`, `{}`, `[]`, `0.5`, `true`} {
		var details ChatTokenDetails
		require.Error(t, json.Unmarshal([]byte(`{"cache_write_tokens":`+value+`}`), &details))
	}
}
