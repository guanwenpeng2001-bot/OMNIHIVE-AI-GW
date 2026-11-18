package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The converter has neither the selected provider nor the final mapped model.
// Keep its established behavior; the forwarding boundary owns GPT-6.2 policy.
func TestGPT62ConvertersDoNotInferProvider(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "other-provider/gpt-6.2-sol"} {
		var req AnthropicRequest
		require.NoError(t, json.Unmarshal([]byte(`{"model":"`+model+`","messages":[{"role":"user","content":"hello"}],"temperature":0.3,"top_p":0.8,"thinking":{"type":"disabled"}}`), &req))
		responses, err := AnthropicToResponses(&req)
		require.NoError(t, err)
		require.Equal(t, "none", responses.Reasoning.Effort)
		require.NotNil(t, responses.Temperature)
		require.NotNil(t, responses.TopP)
		chat, err := AnthropicToChatCompletionsRequest(&req)
		require.NoError(t, err)
		require.Equal(t, "none", chat.ReasoningEffort)
		require.NotNil(t, chat.Temperature)
		require.NotNil(t, chat.TopP)
	}
}
