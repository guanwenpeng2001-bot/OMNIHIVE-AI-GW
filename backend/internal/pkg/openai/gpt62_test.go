package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGPT62ModelIdentityAndEffort(t *testing.T) {
	for _, base := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, suffix := range []string{"", "-low", "-medium", "-high", "-xhigh", "-max", "-none", "-minimal", "-openai-compact"} {
			require.Equal(t, base, GPT62ModelID("openai/"+base+suffix))
		}
		for _, effort := range []string{"none", "minimal", " NONE ", "MINIMAL"} {
			require.ErrorContains(t, ValidateGPT62ReasoningEffort(base, effort), "does not support reasoning")
		}
		for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
			require.NoError(t, ValidateGPT62ReasoningEffort(base, effort))
		}
	}
	require.Equal(t, "gpt-6.2-sol", GPT62ModelID(" OpenAI/GPT_6.2_SOL_MAX "))
	for _, model := range []string{"gpt-6", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.2", "gpt-6.2-sol-preview", "gpt-6.2-astra-law", "gpt-6.2-sol-2026-11-17", "gpt-6.2-sol-max-extra", "gpt-6.20-sol"} {
		require.Empty(t, GPT62ModelID(model), model)
		require.NoError(t, ValidateGPT62ReasoningEffort(model, "none"), model)
	}
}
