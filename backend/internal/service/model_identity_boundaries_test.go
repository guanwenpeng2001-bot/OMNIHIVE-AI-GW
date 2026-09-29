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
					before, err := json.Marshal(account)
					require.NoError(t, err)
					for _, model := range preset.Models {
						require.False(t, AccountSupportsResolvedModel(context.Background(), account, model.ID, AccountModelBindingOptions{}))
					}
					_, err = PlatformModelPresets()
					require.NoError(t, err)
					for _, model := range preset.Models {
						require.False(t, AccountSupportsResolvedModel(context.Background(), account, model.ID, AccountModelBindingOptions{}))
					}
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
