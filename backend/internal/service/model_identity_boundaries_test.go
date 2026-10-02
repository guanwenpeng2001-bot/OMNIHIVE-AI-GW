package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestModelIdentityAndPresetBoundaries(t *testing.T) {
	t.Run("official_profiles", testModelOfficialProfileBoundaries)
	t.Run("frozen_aliases", testModelFrozenAliasBoundaries)
	t.Run("domestic_candidates", testDomesticCandidateBoundaries)
}

func testModelOfficialProfileBoundaries(t *testing.T) {
	for _, testCase := range []struct {
		name, platform, accountType, baseURL, mode, profile string
	}{
		{"coding_default", PlatformKimi, AccountTypeAPIKey, "", AccountModeCoding, "kimi-for-coding"},
		{"payg_default", PlatformKimi, AccountTypeAPIKey, "", AccountModePayG, "moonshot"},
		{"coding_chat", PlatformKimi, AccountTypeAPIKey, DefaultKimiCodingBaseURL, "", "kimi-for-coding"},
		{"coding_anthropic", PlatformKimi, AccountTypeAPIKey, DefaultKimiCodingAnthropicBaseURL, AccountModeCoding, "kimi-for-coding"},
		{"coding_normalized_host", PlatformKimi, AccountTypeAPIKey, "https://API.KIMI.COM/coding/v1/", "", "kimi-for-coding"},
		{"payg_china", PlatformKimi, AccountTypeAPIKey, DefaultKimiPayGBaseURL, "", "moonshot"},
		{"payg_global", PlatformKimi, AccountTypeAPIKey, "https://api.moonshot.ai/v1", "", "moonshot"},
		{"payg_anthropic", PlatformKimi, AccountTypeAPIKey, DefaultKimiPayGAnthropicBaseURL, "", "moonshot"},
		{"coding_host_without_path", PlatformKimi, AccountTypeAPIKey, "https://api.kimi.com/v1", AccountModeCoding, "custom:"},
		{"coding_path_boundary", PlatformKimi, AccountTypeAPIKey, "https://api.kimi.com/coding-other/v1", "", "custom:"},
		{"coding_unknown_path", PlatformKimi, AccountTypeAPIKey, "https://api.kimi.com/coding/private", "", "custom:"},
		{"coding_wrong_mode", PlatformKimi, AccountTypeAPIKey, DefaultKimiCodingBaseURL, AccountModePayG, "custom:"},
		{"payg_wrong_mode", PlatformKimi, AccountTypeAPIKey, DefaultKimiPayGBaseURL, AccountModeCoding, "custom:"},
		{"payg_unknown_path", PlatformKimi, AccountTypeAPIKey, "https://api.moonshot.cn/private", "", "custom:"},
		{"coding_similar_host", PlatformKimi, AccountTypeAPIKey, "https://api.kimi.com.example/coding/v1", "", "custom:"},
		{"payg_similar_host", PlatformKimi, AccountTypeAPIKey, "https://api.moonshot.cn.example/v1", "", "custom:"},
		{"self_hosted", PlatformKimi, AccountTypeAPIKey, "https://private.example/coding/v1", "", "custom:"},
		{"alibaba_china", PlatformQianwen, AccountTypeAPIKey, "https://dashscope.aliyuncs.com/compatible-mode/v1", "", "alibaba-cn"},
		{"alibaba_global", PlatformQianwen, AccountTypeAPIKey, "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "", "alibaba"},
		{"zhipu_cn", PlatformZhipu, AccountTypeAPIKey, "https://open.bigmodel.cn/api/paas/v4", "", "zhipuai"},
		{"zhipu_coding", PlatformZhipu, AccountTypeAPIKey, "https://open.bigmodel.cn/api/coding/paas/v4", "", "zhipuai-coding-plan"},
		{"zai_api", PlatformZhipu, AccountTypeAPIKey, "https://api.z.ai/api/paas/v4", "", "zai"},
		{"zai_coding", PlatformZhipu, AccountTypeAPIKey, "https://api.z.ai/api/coding/paas/v4", "", "zai-coding-plan"},
		{"zhipu_shared_messages", PlatformZhipu, AccountTypeAPIKey, "https://open.bigmodel.cn/api/anthropic", "coding", "zhipuai-coding-plan"},
		{"zhipu_private", PlatformZhipu, AccountTypeAPIKey, "https://open.bigmodel.cn/api/coding/paas/v4/private", "", "custom:"},
		{"minimax_cn", PlatformMiniMax, AccountTypeAPIKey, "https://api.minimaxi.com/v1", "", "minimax-cn"},
		{"minimax_cn_coding", PlatformMiniMax, AccountTypeAPIKey, "https://api.minimaxi.com/anthropic", "coding", "minimax-cn-coding-plan"},
		{"minimax_global_coding", PlatformMiniMax, AccountTypeAPIKey, "https://api.minimax.io/v1", "coding", "minimax-coding-plan"},
		{"minimax_private", PlatformMiniMax, AccountTypeAPIKey, "https://api.minimax.io/private", "", "custom:"},
		{"qianwen_coding", PlatformQianwen, AccountTypeAPIKey, "https://coding.dashscope.aliyuncs.com/v1", "", "alibaba-coding-plan-cn"},
		{"qianwen_coding_messages", PlatformQianwen, AccountTypeAPIKey, "https://coding.dashscope.aliyuncs.com/apps/anthropic", "coding", "alibaba-coding-plan-cn"},
		{"qianwen_global_coding_unknown", PlatformQianwen, AccountTypeAPIKey, "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "coding", "custom:"},
		{"qianwen_private", PlatformQianwen, AccountTypeAPIKey, "https://dashscope.aliyuncs.com/private", "", "custom:"},
		{"qianwen_wrong_mode", PlatformQianwen, AccountTypeAPIKey, "https://coding.dashscope.aliyuncs.com/v1", "payg", "custom:"},
		{"openai_key", PlatformOpenAI, AccountTypeAPIKey, "https://api.openai.com/v1", "", "openai"},
		{"openai_oauth", PlatformOpenAI, AccountTypeOAuth, "https://api.openai.com/v1", "", "openai-codex"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			account := &Account{Platform: testCase.platform, Type: testCase.accountType, Credentials: map[string]any{"base_url": testCase.baseURL, "account_mode": testCase.mode}}
			profile := resolveCapabilityProfile(account)
			if testCase.profile == "custom:" {
				require.True(t, strings.HasPrefix(profile, "custom:"), profile)
			} else {
				require.Equal(t, testCase.profile, profile)
			}
		})
	}
}

func testModelFrozenAliasBoundaries(t *testing.T) {
	// Synthetic reviewed records exercise the matcher, not a claim that a real
	// dated model is equivalent to a rolling alias. Real evidence goes to release.
	for _, platform := range domain.ConcretePlatforms() {
		t.Run(platform, func(t *testing.T) {
			profile := "fixture:" + platform
			frozenID := "sample-20260901"
			record := CapabilityRecord{Profile: profile, ModelID: "sample", Aliases: []string{frozenID, frozenID}, Source: "reviewed:test"}
			records := map[string]CapabilityRecord{capabilityKey(profile, record.ModelID): record}
			snapshot := &ModelCapabilitySnapshot{records: records, aliases: buildCapabilityAliasIndex(records)}
			match := lookupCapabilityMetadata(snapshot, profile, frozenID)
			require.Equal(t, "matched", match.Status)
			require.Equal(t, "reviewed_alias", match.Rule)
			require.Equal(t, "sample", match.CanonicalID)
			require.Equal(t, frozenID, match.Metadata.ID)
			require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, profile+":other-deployment", frozenID).Status)
			require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, profile, "sample-20260902").Status)
			require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, profile, strings.ToUpper(frozenID)).Status)
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"client-model": frozenID}}}
			require.Equal(t, frozenID, account.GetMappedModel("client-model"))
			record.ModelID = "other-sample"
			records[capabilityKey(profile, record.ModelID)] = record
			snapshot.aliases = buildCapabilityAliasIndex(records)
			require.Equal(t, "ambiguous", lookupCapabilityMetadata(snapshot, profile, frozenID).Status)
			require.Equal(t, "exact", lookupCapabilityMetadata(snapshot, profile, "sample").Rule)
			require.Equal(t, frozenID, account.GetMappedModel("client-model"))
		})
	}
}

func testDomesticCandidateBoundaries(t *testing.T) {
	presets, err := PlatformModelPresets()
	require.NoError(t, err)
	expectedProfiles := map[string][]string{
		PlatformKimi:     {"moonshotai", "moonshotai-cn", "kimi-for-coding"},
		PlatformZhipu:    {"zhipuai", "zhipuai-coding-plan", "zai", "zai-coding-plan"},
		PlatformDeepseek: {"deepseek"},
		PlatformMiniMax:  {"minimax", "minimax-cn", "minimax-coding-plan", "minimax-cn-coding-plan"},
	}
	for platform, profiles := range expectedProfiles {
		t.Run(platform, func(t *testing.T) {
			actualProfiles := []string{}
			for _, preset := range presets {
				if preset.Platform != platform {
					continue
				}
				actualProfiles = append(actualProfiles, preset.ProviderProfile)
				require.NotEmpty(t, preset.Models)
				require.NotEmpty(t, preset.SourceRefs)
				for _, mode := range []string{AccountModePayG, AccountModeCoding} {
					mapping := make(map[string]any, len(preset.Models))
					for _, model := range preset.Models {
						mapping[model.ID] = model.ID
					}
					account := &Account{Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"account_mode": mode, "model_mapping": mapping}, Extra: map[string]any{}}
					// Listing a preset does not select its candidates for an account.
					account.Credentials["model_mapping"] = map[string]any{}
					for _, model := range preset.Models {
						require.False(t, AccountSupportsResolvedModel(context.Background(), account, model.ID, AccountModelBindingOptions{}))
					}
					account.Credentials["model_mapping"] = mapping
					before, err := json.Marshal(account)
					require.NoError(t, err)
					for _, model := range preset.Models {
						require.True(t, AccountSupportsResolvedModel(context.Background(), account, model.ID, AccountModelBindingOptions{}), "an explicit saved selection may attempt the installed adapter")
					}
					_, err = PlatformModelPresets()
					require.NoError(t, err)
					for _, model := range preset.Models {
						require.True(t, AccountSupportsResolvedModel(context.Background(), account, model.ID, AccountModelBindingOptions{}), "an explicit saved selection may attempt the installed adapter")
					}
					require.False(t, AccountSupportsResolvedModel(context.Background(), account, "unselected-private-model", AccountModelBindingOptions{}))
					after, err := json.Marshal(account)
					require.NoError(t, err)
					require.JSONEq(t, string(before), string(after))
				}
			}
			require.ElementsMatch(t, profiles, actualProfiles)
			require.Empty(t, DefaultModelIDsForPlatform(platform), "unscoped public fallback must not flatten plan candidates")
		})
	}
}

func TestModelProfileSignedLimitIsolation(t *testing.T) {
	records := map[string]CapabilityRecord{}
	for _, profile := range []string{"zhipuai", "zhipuai-coding-plan", "zai", "zai-coding-plan", "minimax", "minimax-cn", "minimax-coding-plan", "minimax-cn-coding-plan", "alibaba", "alibaba-cn", "alibaba-coding-plan-cn", "gemini", "anthropic", "vertex_ai"} {
		records[capabilityKey(profile, "shared-id")] = CapabilityRecord{Profile: profile, ModelID: "shared-id", ContextWindow: 1234, Source: "reviewed:test"}
	}
	snapshot := &ModelCapabilitySnapshot{records: records}
	for _, tc := range []struct{ platform, base, mode, protocol, profile string }{
		{PlatformMiniMax, "https://api.minimax.io/anthropic", "coding", "anthropic", "minimax-coding-plan"},
		{PlatformZhipu, "https://api.z.ai/api/anthropic", "coding", "anthropic", "zai-coding-plan"},
		{PlatformQianwen, "https://dashscope-intl.aliyuncs.com/apps/anthropic", "payg", "anthropic", "alibaba"},
		{PlatformMiniMax, "https://relay.example/anthropic", "coding", "anthropic", ""},
		{PlatformZhipu, "https://api.z.ai.example/api/anthropic", "coding", "anthropic", ""},
		{PlatformQianwen, "https://coding-intl.dashscope.aliyuncs.com/v1", "coding", "", ""},
	} {
		t.Run(tc.base, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": tc.base, "account_mode": tc.mode, "api_protocol": tc.protocol, "provider_profile": "anthropic"}}
			profile := resolveCapabilityProfile(account)
			match := lookupCapabilityMetadata(snapshot, profile, "shared-id")
			if tc.profile == "" {
				require.True(t, strings.HasPrefix(profile, "custom:"), profile)
				require.Equal(t, "not_found", match.Status)
				require.Zero(t, match.Metadata.ContextWindow)
			} else {
				require.Equal(t, tc.profile, profile)
				require.Equal(t, "matched", match.Status)
				require.Equal(t, tc.profile, match.Metadata.ProviderProfile)
				delete(records, capabilityKey(profile, "shared-id"))
				require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, profile, "shared-id").Status, "same ID in another provider must not supply limits")
			}
		})
	}
	for _, platform := range []string{PlatformGemini, PlatformAnthropic} {
		for _, base := range []string{"https://generativelanguage.googleapis.com", "https://api.anthropic.com", "https://relay.example"} {
			account := &Account{Platform: platform, Type: AccountTypeServiceAccount, Credentials: map[string]any{"base_url": base}}
			require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, resolveCapabilityProfile(account), "shared-id").Status, "hosted credentials cannot borrow public API metadata")
		}
		account := &Account{Platform: platform, Type: AccountTypeServiceAccount, Credentials: map[string]any{"base_url": "https://aiplatform.googleapis.com"}}
		require.Equal(t, "vertex_ai", resolveCapabilityProfile(account))
		delete(records, capabilityKey("vertex_ai", "shared-id"))
		require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, resolveCapabilityProfile(account), "shared-id").Status)
	}
}
