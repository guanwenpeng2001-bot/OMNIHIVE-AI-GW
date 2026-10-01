package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiMessagesControlsFollowSelectedProtocol(t *testing.T) {
	var request AnthropicRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"text-model","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"top_k":7,"stop_sequences":["END","STOP"]}`), &request))
	require.NotNil(t, request.TopK)
	require.Equal(t, 7, *request.TopK)
	native, err := json.Marshal(request)
	require.NoError(t, err)
	require.Contains(t, string(native), `"top_k":7`)
	require.Contains(t, string(native), `"stop_sequences":["END","STOP"]`)
	_, err = AnthropicToResponses(&request)
	require.ErrorContains(t, err, "stop_sequences")
	_, err = AnthropicToChatCompletionsRequest(&request)
	require.ErrorContains(t, err, "top_k")
	request.TopK = nil
	chat, err := AnthropicToChatCompletionsRequest(&request)
	require.NoError(t, err)
	require.JSONEq(t, `["END","STOP"]`, string(chat.Stop))
	request.StopSeqs = nil
	topK := 7
	request.TopK = &topK
	_, err = AnthropicToResponses(&request)
	require.ErrorContains(t, err, "top_k")
	request.TopK = nil
	_, err = AnthropicToResponses(&request)
	require.NoError(t, err)
}
