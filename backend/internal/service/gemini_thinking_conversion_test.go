package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiThinkingMessagesBudgetConversion(t *testing.T) {
	for _, scenario := range []struct {
		control string
		budget  int64
		invalid bool
	}{
		{`"thinking":{"type":"disabled"}`, 0, false},
		{`"thinking":{"type":"adaptive"}`, -1, false},
		{`"thinking":{"type":"enabled","budget_tokens":8192}`, 8192, false},
		{`"thinking":{"type":"enabled","budget_tokens":1.5}`, 0, true},
		{`"thinking":{"type":"enabled","budget_tokens":-2}`, 0, true},
		{`"thinking":{"type":"typo"}`, 0, true},
		{`"thinking":{"type":"disabled"},"output_config":{"effort":"high"}`, 0, true},
		{`"thinking":{"type":"enabled","budget_tokens":1000},"output_config":{"effort":"low"}`, 0, true},
		{`"output_config":{"effort":"turbo"}`, 0, true},
	} {
		t.Run(scenario.control, func(t *testing.T) {
			body, err := convertClaudeMessagesToGeminiGenerateContent([]byte(`{"model":"custom","messages":[{"role":"user","content":"hello"}],` + scenario.control + `}`))
			if scenario.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			budget := gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingBudget")
			require.True(t, budget.Exists(), string(body))
			require.Equal(t, scenario.budget, budget.Int())
		})
	}
}
