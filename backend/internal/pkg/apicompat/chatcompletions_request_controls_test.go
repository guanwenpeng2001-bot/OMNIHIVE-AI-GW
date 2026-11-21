package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatBridgePreservesOutputAndSamplingControls(t *testing.T) {
	temperature, topP := 0.25, 0.75
	maxTokens := 2
	t.Run("responses to chat", func(t *testing.T) {
		out, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{
			Model: "gpt-5.4", Input: json.RawMessage(`"hello"`),
			MaxOutputTokens: &maxTokens, Temperature: &temperature, TopP: &topP,
		})
		require.NoError(t, err)
		require.Equal(t, &maxTokens, out.MaxTokens)
		require.Nil(t, out.MaxCompletionTokens)
		require.Equal(t, &temperature, out.Temperature)
		require.Equal(t, &topP, out.TopP)

		roundTrip, err := ChatCompletionsToResponses(out)
		require.NoError(t, err)
		require.Equal(t, &maxTokens, roundTrip.MaxOutputTokens)
	})

	t.Run("messages to chat", func(t *testing.T) {
		out, err := AnthropicToChatCompletionsRequest(&AnthropicRequest{
			Model: "claude-sonnet-4", MaxTokens: maxTokens,
			Messages: []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
			StopSeqs: []string{"STOP"}, Temperature: &temperature, TopP: &topP,
		})
		require.NoError(t, err)
		require.Equal(t, &maxTokens, out.MaxTokens)
		require.Nil(t, out.MaxCompletionTokens)
		require.JSONEq(t, `["STOP"]`, string(out.Stop))
		require.Equal(t, &temperature, out.Temperature)
		require.Equal(t, &topP, out.TopP)

		roundTrip, err := ChatCompletionsToResponses(out)
		require.NoError(t, err)
		require.Equal(t, &maxTokens, roundTrip.MaxOutputTokens)
	})
}

func TestChatBridgeMapsOutputLimitFinishReasons(t *testing.T) {
	finishReason := "length"
	chat := ResponsesToChatCompletions(&ResponsesResponse{Status: "incomplete", IncompleteDetails: &ResponsesIncompleteDetails{Reason: "max_output_tokens"}}, "gpt-5.4")
	require.Equal(t, "length", chat.Choices[0].FinishReason)

	state := NewChatCompletionsToAnthropicStreamState("gpt-5.4")
	events := ChatCompletionsChunkToAnthropicEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{FinishReason: &finishReason}}}, state)
	events = append(events, FinalizeChatCompletionsAnthropicStream(state)...)
	require.Len(t, events, 3)
	require.Equal(t, "message_delta", events[1].Type)
	require.Equal(t, "max_tokens", events[1].Delta.StopReason)
}
