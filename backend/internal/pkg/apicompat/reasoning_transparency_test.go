package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Replays R01-R04 against the known model capability catalog. Manual models
// cannot express an effort without an approximation, so strict conversion fails.
func TestReasoningTransparencyResponsesReplay(testingT *testing.T) {
	for _, scenario := range []struct {
		name, model, effort, thinking string
		limit                         int
		unsupported                   bool
	}{
		{name: "disabled", model: "claude-sonnet-4-6", effort: "none", thinking: "disabled"},
		{name: "manual high default limit", model: "claude-sonnet-4-5", effort: "high", unsupported: true},
		{name: "manual max explicit limit", model: "claude-sonnet-4-5", effort: "max", limit: 8192, unsupported: true},
		{name: "adaptive low", model: "claude-sonnet-4-6", effort: "low", thinking: "adaptive", limit: 1024},
		{name: "adaptive medium", model: "claude-opus-4-6", effort: "medium", thinking: "adaptive"},
		{name: "adaptive high", model: "claude-opus-4-6", effort: "high", thinking: "adaptive", limit: 8192},
		{name: "adaptive max", model: "claude-opus-4-6", effort: "max", thinking: "adaptive"},
		{name: "xhigh is not max", model: "claude-opus-4-6", effort: "xhigh", unsupported: true},
		{name: "supported xhigh", model: "claude-opus-4-7", effort: "xhigh", thinking: "adaptive"},
		{name: "minimal", model: "claude-opus-4-6", effort: "minimal", unsupported: true},
		{name: "unknown effort", model: "claude-opus-4-6", effort: "turbo", unsupported: true},
		{name: "unknown capability", model: "unknown-model", effort: "high", unsupported: true},
	} {
		testingT.Run(scenario.name, func(testingT *testing.T) {
			request := &ResponsesRequest{Model: scenario.model, Input: json.RawMessage(`"Hello"`), Reasoning: &ResponsesReasoning{Effort: scenario.effort}}
			if scenario.limit > 0 {
				request.MaxOutputTokens = &scenario.limit
			}
			converted, err := ResponsesToAnthropicRequest(request)
			if scenario.unsupported {
				require.ErrorContains(testingT, err, "unsupported reasoning.effort")
				require.Nil(testingT, converted)
				return
			}
			require.NoError(testingT, err)
			require.Equal(testingT, scenario.thinking, converted.Thinking.Type)
			require.Zero(testingT, converted.Thinking.BudgetTokens)
			if scenario.limit > 0 {
				require.Equal(testingT, scenario.limit, converted.MaxTokens)
			} else {
				require.Equal(testingT, 8192, converted.MaxTokens)
			}
			if scenario.effort == "none" {
				require.Nil(testingT, converted.OutputConfig)
			} else {
				require.Equal(testingT, scenario.effort, converted.OutputConfig.Effort)
			}
			encoded, err := json.Marshal(converted)
			require.NoError(testingT, err)
			require.NotContains(testingT, string(encoded), "budget_tokens")
		})
	}
}

// Positive budgets become approximate effort preferences in either OpenAI bridge.
func TestReasoningTransparencyMessagesReplay(testingT *testing.T) {
	for _, budget := range []int{2048, 4096} {
		request := &AnthropicRequest{Model: "gpt-5.2", MaxTokens: 8192, Messages: []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"Hello"`)}}, Thinking: &AnthropicThinking{Type: "enabled", BudgetTokens: budget}}
		responses, err := AnthropicToResponses(request)
		require.NoError(testingT, err)
		require.Equal(testingT, "low", responses.Reasoning.Effort)
		chat, err := AnthropicToChatCompletionsRequest(request)
		require.NoError(testingT, err)
		require.Equal(testingT, "low", chat.ReasoningEffort)
		require.Equal(testingT, budget, request.Thinking.BudgetTokens)
	}
}
