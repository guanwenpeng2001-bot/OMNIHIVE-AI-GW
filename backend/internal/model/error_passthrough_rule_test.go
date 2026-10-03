package model

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAllPlatformsIncludesEveryConcretePlatform(t *testing.T) {
	expected := []string{
		"anthropic",
		"openai",
		"gemini",
		"antigravity",
		"grok",
		"kimi",
		"zhipu",
		"deepseek",
		"minimax",
		"opencode_go",
		"cursor",
		"qianwen",
		"commandcode", "volcengine", "qianfan", "hunyuan", "mistral", "openrouter", "bedrock",
	}
	for _, provider := range domain.CompatibleProviders() {
		expected = append(expected, provider.ID)
	}
	actual := AllPlatforms()
	require.ElementsMatch(t, expected, actual)
	require.Equal(t, domain.ConcretePlatforms(), actual)
	for _, platform := range actual {
		require.True(t, domain.IsConcretePlatform(platform))
		require.NotContains(t, actual, strings.ToUpper(platform))
		require.False(t, domain.IsConcretePlatform(strings.ToUpper(platform)))
	}
	for _, excluded := range []string{"", "unknown-provider", "composite", "workbuddy"} {
		require.NotContains(t, actual, excluded)
		require.False(t, domain.IsConcretePlatform(excluded))
	}
	actual[0] = "mutated"
	require.ElementsMatch(t, expected, AllPlatforms(), "callers cannot mutate the shared platform inventory")
}
