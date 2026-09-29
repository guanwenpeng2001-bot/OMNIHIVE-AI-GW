package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

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
	for _, platform := range domain.ConcretePlatforms() {
		require.True(t, covered[platform], platform)
	}
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
