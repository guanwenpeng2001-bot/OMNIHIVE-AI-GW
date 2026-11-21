package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThinkingBudgetCompatibility(t *testing.T) {
	for _, test := range []struct {
		name, explicit, want string
		budget               int
	}{
		{"small budget", "", "low", 2048},
		{"low boundary", "", "low", 4096},
		{"medium budget", "", "medium", 8192},
		{"medium boundary", "", "medium", 16384},
		{"large budget", "", "high", 32768},
		{"explicit preference", "low", "low", 32768},
		{"explicit max", "max", "xhigh", 2048},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &AnthropicRequest{
				Model: "test-model", MaxTokens: 64000,
				Messages:     []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"Hello"`)}},
				Thinking:     &AnthropicThinking{Type: "enabled", BudgetTokens: test.budget},
				OutputConfig: &AnthropicOutputConfig{Effort: test.explicit},
			}
			responses, err := AnthropicToResponses(request)
			require.NoError(t, err)
			require.Equal(t, test.want, responses.Reasoning.Effort)
			require.Equal(t, 64000, *responses.MaxOutputTokens)
			chat, err := AnthropicToChatCompletionsRequest(request)
			require.NoError(t, err)
			require.Equal(t, test.want, chat.ReasoningEffort)
			require.Equal(t, test.budget, request.Thinking.BudgetTokens)
			request.Thinking.BudgetTokens = -1
			_, err = AnthropicToResponses(request)
			require.ErrorContains(t, err, "non-negative")
		})
	}
}

func TestThinkingWithoutBudgetPreservesDefault(t *testing.T) {
	for _, mode := range []string{"enabled", "adaptive"} {
		for _, raw := range []string{`{"type":"` + mode + `"}`, `{"type":"` + mode + `","budget_tokens":0}`} {
			var thinking AnthropicThinking
			require.NoError(t, json.Unmarshal([]byte(raw), &thinking))
			req := &AnthropicRequest{Model: "unknown-model", MaxTokens: 4096, Thinking: &thinking, Messages: []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
			resp, err := AnthropicToResponses(req)
			require.NoError(t, err)
			require.Nil(t, resp.Reasoning)
			chat, err := AnthropicToChatCompletionsRequest(req)
			require.NoError(t, err)
			require.Empty(t, chat.ReasoningEffort)
			require.Equal(t, 4096, *resp.MaxOutputTokens)
		}
	}
}

func TestThinkingInvalidControlsRemainRejected(t *testing.T) {
	for _, raw := range []string{`{"type":"enabled","budget_tokens":-1}`, `{"type":"invalid"}`, `{"type":true}`, `{"type":"enabled","budget_tokens":"2048"}`} {
		var req AnthropicRequest
		err := json.Unmarshal([]byte(`{"model":"test","max_tokens":8192,"messages":[{"role":"user","content":"hi"}],"thinking":`+raw+`}`), &req)
		if err != nil {
			continue
		}
		_, err = AnthropicToResponses(&req)
		require.Error(t, err)
		_, err = AnthropicToChatCompletionsRequest(&req)
		require.Error(t, err)
	}
}

func TestThinkingCompatibilityKeepsStreamAndToolHistory(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, controls := range []string{`"thinking":{"type":"enabled","budget_tokens":32768}`, `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}`, `"thinking":{"type":"disabled"}`} {
			var req AnthropicRequest
			raw := `{"model":"unknown-model","max_tokens":10,"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"id":{"type":"string"}}}}],"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_lookup","name":"lookup","input":{"id":"42"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_lookup","content":"found"}]}],` + controls + `}`
			require.NoError(t, json.Unmarshal([]byte(raw), &req))
			req.Stream = stream
			resp, err := AnthropicToResponses(&req)
			require.NoError(t, err)
			require.Equal(t, stream, resp.Stream)
			require.Equal(t, 10, *resp.MaxOutputTokens)
			require.Len(t, resp.Tools, 1)
			var items []ResponsesInputItem
			require.NoError(t, json.Unmarshal(resp.Input, &items))
			require.Len(t, items, 2)
			require.Equal(t, "function_call", items[0].Type)
			require.Equal(t, "function_call_output", items[1].Type)
			require.Equal(t, items[0].CallID, items[1].CallID)
			chat, err := AnthropicToChatCompletionsRequest(&req)
			require.NoError(t, err)
			require.Equal(t, stream, chat.Stream)
			require.NotNil(t, chat.MaxTokens)
			require.Equal(t, 10, *chat.MaxTokens)
			require.Len(t, chat.Tools, 1)
			require.Len(t, chat.Messages, 2)
			require.Len(t, chat.Messages[0].ToolCalls, 1)
			require.Equal(t, chat.Messages[0].ToolCalls[0].ID, chat.Messages[1].ToolCallID)
		}
	}
}
