package service

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Keep the original product contract independent of the growing registry.
func originalServicePlatforms() []string {
	return []string{
		"anthropic", "gemini", "openai", "antigravity", "grok", "kimi", "zhipu",
		"deepseek", "minimax", "opencode_go", "cursor", "qianwen", "commandcode",
		"volcengine", "qianfan", "hunyuan", "mistral", "openrouter", "bedrock", "workbuddy", "kilo",
	}
}

// Supplier IDs come from the admission registry, never the service enumeration
// being tested. The original products above remain a separate literal oracle.
func expectedServicePlatforms() []string {
	platforms := originalServicePlatforms()
	for _, provider := range domain.CompatibleProviders() {
		platforms = append(platforms, provider.ID)
	}
	return platforms
}

func requireOriginalPresetCoverage(t *testing.T, covered map[string]bool) {
	t.Helper()
	for _, platform := range originalServicePlatforms() {
		require.True(t, covered[platform], "native product requires a preset: %s", platform)
	}
	for platform := range covered {
		require.Contains(t, expectedServicePlatforms(), platform, "preset must belong to an admitted identity")
	}
}

// Candidate schema checks can run before the separate signed-release binding step.
// Registry trust and account eligibility are still exercised by the tests below.
func TestPlatformModelPresetCandidateSchema(t *testing.T) {
	presets, err := parsePlatformModelPresets(platformModelPresetsJSON)
	require.NoError(t, err)
	covered := map[string]bool{}
	vertex := false
	for _, preset := range presets {
		covered[preset.Platform] = true
		if preset.Platform == "openrouter" {
			require.Equal(t, "unconfigured", preset.CoverageStatus)
			require.Empty(t, preset.Models)
		}
		if preset.Platform == PlatformGemini && preset.ProviderProfile == "vertex-gemini" {
			vertex = true
			for _, model := range preset.Models {
				require.Contains(t, model.FieldSources["id"], "cloud.google.com")
				require.NotEqual(t, int64(64), model.MaxOutputTokens)
				require.NotEqual(t, int64(128), model.ContextWindow)
			}
		}
		for _, model := range preset.Models {
			require.Empty(t, DefaultModelIDsForPlatform(preset.Platform))
			if preset.Platform == "deepseek" {
				require.Zero(t, model.MaxOutputTokens, "ambiguous 384K must stay unknown")
			}
		}
	}
	require.True(t, vertex)
	requireOriginalPresetCoverage(t, covered)
}

func TestPlatformModelPresetCoverageAndIsolation(t *testing.T) {
	presets, err := PlatformModelPresets()
	require.NoError(t, err)
	require.NotEmpty(t, presets)
	covered := map[string]bool{}
	for _, preset := range presets {
		covered[preset.Platform] = true
		for _, model := range preset.Models {
			account := &Account{Platform: preset.Platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
			require.False(t, AccountSupportsResolvedModel(context.Background(), account, model.ID, AccountModelBindingOptions{}), "a preset is never account entitlement")
		}
	}
	requireOriginalPresetCoverage(t, covered)
	original := presets[0].ProviderProfile
	presets[0].ProviderProfile = "mutated"
	again, err := PlatformModelPresets()
	require.NoError(t, err)
	require.Equal(t, original, again[0].ProviderProfile)
}

func TestPlatformModelCatalogDoesNotBorrowClaudeDefaults(t *testing.T) {
	for _, descriptor := range domain.PlatformDescriptors() {
		t.Run(descriptor.ID, func(t *testing.T) {
			ids := DefaultModelIDsForPlatform(descriptor.ID)
			if descriptor.CNProvider && descriptor.ID != PlatformQianwen {
				require.Empty(t, ids)
			}
			if len(ids) > 0 {
				original := ids[0]
				ids[0] = "mutated"
				require.Equal(t, original, DefaultModelIDsForPlatform(descriptor.ID)[0])
			}
		})
	}
	require.Empty(t, DefaultModelIDsForPlatform("unknown"))
	require.Empty(t, DefaultModelIDsForPlatform(PlatformComposite))
	require.NotEmpty(t, NewModelRegistry().Snapshot().CandidateIDs(PlatformCursor, ""))
}
