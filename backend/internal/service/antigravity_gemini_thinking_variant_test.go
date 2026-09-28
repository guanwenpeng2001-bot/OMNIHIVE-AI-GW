package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func approvedAntigravityVariantAccount() *Account {
	account := catalogEvidenceAccount(73, PlatformAntigravity, "gemini-3-flash-low", "gemini-3-flash-medium", "gemini-3-flash-high")
	account.Type = AccountTypeOAuth
	snapshot := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	for index := range snapshot.EligibleModels {
		snapshot.EligibleModels[index].Endpoints = []string{"generate_content"}
	}
	return &account
}

func TestAntigravityThinkingVariantRequiresApprovedIdentity(t *testing.T) {
	account := approvedAntigravityVariantAccount()
	for _, level := range []string{"low", "medium", "high"} {
		mapped, matched := resolveGeminiThinkingVariantForLevel(account, "models/gemini-3-flash", level)
		require.True(t, matched)
		require.Equal(t, "gemini-3-flash-"+level, mapped)
	}
	account.Credentials["model_mapping"] = map[string]any{"gemini-3-flash": "explicit-target"}
	_, matched := resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.False(t, matched, "explicit mapping wins")
	account.Credentials["model_mapping"] = map[string]any{"gemini-*": "explicit-target"}
	_, matched = resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.False(t, matched, "explicit wildcard wins")
	account.Credentials = map[string]any{}
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"gemini-3-flash-low"}}
	mapped, matched := resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.True(t, matched)
	require.Equal(t, "gemini-3-flash-low", mapped)
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
	_, matched = resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.False(t, matched, "stale identity cannot authorize variants")
	account.Extra = nil
	_, matched = resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.False(t, matched, "default names do not supply evidence")
}

func TestAntigravityThinkingLevelProtocolParity(t *testing.T) {
	for _, scenario := range []struct {
		body     string
		thinking *antigravity.ThinkingConfig
		want     string
	}{
		{`{}`, nil, "high"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`, &antigravity.ThinkingConfig{Type: "disabled"}, "low"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":1024}}}`, &antigravity.ThinkingConfig{Type: "enabled", BudgetTokens: 1024}, "low"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":4000}}}`, &antigravity.ThinkingConfig{Type: "enabled", BudgetTokens: 4000}, "medium"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":20000}}}`, &antigravity.ThinkingConfig{Type: "enabled", BudgetTokens: 20000}, "high"},
	} {
		require.Equal(t, scenario.want, geminiThinkingLevelFromBody([]byte(scenario.body)))
		require.Equal(t, scenario.want, geminiThinkingLevelFromClaudeThinking(scenario.thinking))
	}
}

func TestAntigravityThinkingVariantBindingRetainsPublicModel(t *testing.T) {
	account := approvedAntigravityVariantAccount()
	account.Credentials["model_mapping"] = map[string]any{"gemini-3-flash-high": "gemini-3-flash-high"}
	binding := ResolveAccountModelBinding(context.Background(), account, "gemini-3-flash", AccountModelBindingOptions{Endpoint: "generate_content"})
	require.True(t, binding.Supported)
	require.Equal(t, "gemini-3-flash", binding.PublicModel)
	require.Equal(t, "gemini-3-flash-high", binding.UpstreamModel)
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "gemini-3-flash", Protocol: "gemini"})
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	require.NoError(t, validateAccountForwardModel(ctx, nil, account, binding.UpstreamModel, "generate_content"))
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}}
	require.ErrorIs(t, validateAccountForwardModel(ctx, nil, account, binding.UpstreamModel, "generate_content"), ErrAccountModelUnsupported)
}
