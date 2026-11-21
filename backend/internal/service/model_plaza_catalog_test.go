//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func catalogFixtureContext() context.Context {
	return WithModelRegistrySnapshot(context.Background(), &ModelRegistrySnapshot{presets: []PlatformModelPreset{
		{Platform: PlatformOpenAI, Models: []PlatformPresetModel{{ID: "live"}, {ID: "image-only", TaskType: "image_generation"}}},
		{Platform: PlatformGrok, Models: []PlatformPresetModel{{ID: "video-only", TaskType: "video_generation"}}},
		{Platform: PlatformGemini, Models: []PlatformPresetModel{{ID: "embedding-only", TaskType: "embedding"}}},
		{Platform: PlatformAnthropic, Models: []PlatformPresetModel{{ID: "unconfigured"}}},
	}})
}

func catalogGroupModels(groups []PlazaGroup) map[int64]map[PlazaModelIdentity]PlazaModel {
	out := map[int64]map[PlazaModelIdentity]PlazaModel{}
	for _, group := range groups {
		out[group.ID] = map[PlazaModelIdentity]PlazaModel{}
		for _, model := range group.Models {
			out[group.ID][PlazaModelIdentity{Platform: model.Platform, Name: model.Name}] = model
		}
	}
	return out
}

func TestModelPlazaConfiguredPlatformInventory(t *testing.T) {
	ctx := catalogFixtureContext()
	channel := plazaPricedChannel(1, "priced", []int64{10}, PlatformOpenAI, "live")
	service := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil)
	legacy, err := service.ListVisibleGroups(ctx, nil, false)
	require.NoError(t, err)
	require.Len(t, legacy[0].Models, 1)
	full, err := service.ListPublicCatalogGroups(ctx)
	require.NoError(t, err)
	require.Len(t, full, 1)
	require.Len(t, full[0].Models, 2)
	models := catalogGroupModels(full)[10]
	require.False(t, models[PlazaModelIdentity{PlatformOpenAI, "live"}].CatalogOnly)
	require.Equal(t, legacy[0].Models[0].Pricing, models[PlazaModelIdentity{PlatformOpenAI, "live"}].Pricing)
	inventory := models[PlazaModelIdentity{PlatformOpenAI, "image-only"}]
	require.True(t, inventory.CatalogOnly)
	require.Nil(t, inventory.Pricing)
	require.Empty(t, inventory.CatalogBindings)

	t.Run("no groups means no platforms", func(t *testing.T) {
		empty, err := newPlazaService([]Channel{channel}, nil, nil).ListPublicCatalogGroups(ctx)
		require.NoError(t, err)
		require.Empty(t, empty)
	})
	t.Run("group alone configures platform", func(t *testing.T) {
		full, err := newPlazaService(nil, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil).ListPublicCatalogGroups(ctx)
		require.NoError(t, err)
		require.Len(t, full[0].Models, 2)
		for _, model := range full[0].Models {
			require.True(t, model.CatalogOnly)
		}
	})
	t.Run("OAuth group configures its complete platform", func(t *testing.T) {
		full, err := newPlazaService(nil, []Group{{ID: 10, Platform: PlatformOpenAI, AccountType: AccountTypeOAuth}}, nil).ListPublicCatalogGroups(ctx)
		require.NoError(t, err)
		require.Len(t, full, 1)
		require.Len(t, full[0].Models, 2)
	})
	t.Run("exclusive group is not public", func(t *testing.T) {
		full, err := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI, IsExclusive: true}}, nil).ListPublicCatalogGroups(ctx)
		require.NoError(t, err)
		require.Empty(t, full)
	})
	t.Run("allowlist restricts offers not inventory", func(t *testing.T) {
		group := Group{ID: 10, Platform: PlatformOpenAI}
		group.ModelAllowlist.Enabled, group.ModelAllowlist.Models = true, []string{"live"}
		full, err := newPlazaService([]Channel{channel}, []Group{group}, nil).ListPublicCatalogGroups(ctx)
		require.NoError(t, err)
		require.Len(t, full[0].Models, 2)
	})
}

type configuredPlazaAccounts struct {
	*visiblePolicyAccountRepo
	configured []Account
}

func (r *configuredPlazaAccounts) ListConfiguredModelCatalogAccountsForGroups(context.Context, []int64) ([]Account, error) {
	return r.configured, nil
}

func TestModelPlazaCompositeConfiguredPlatformsAndProjectIsolation(t *testing.T) {
	ctx := catalogFixtureContext()
	channels := []Channel{
		plazaPricedChannel(1, "openai", []int64{10}, PlatformOpenAI, "live"),
		plazaPricedChannel(2, "outsider", []int64{11}, PlatformAnthropic, "unconfigured"),
		plazaPricedChannel(3, "inactive", []int64{10}, PlatformAnthropic, "unconfigured"),
	}
	channels[2].Status = "disabled"
	account := catalogEvidenceAccount(7, PlatformGrok, "private-discovered")
	account.GroupIDs, account.Schedulable = []int64{10}, false
	account.Credentials["model_mapping"] = map[string]any{"public-alias": "private-target"}
	metadata := UpstreamModelMetadata{ID: "private-discovered", ProviderProfile: "grok", TaskType: "video_generation", OutputModalities: []string{"video"}}
	stampCapabilityEvidence(&metadata, "upstream", "fixture", "grok", metadata.ID, "native_id", "verified")
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{metadata.ID: metadata}})
	account.Extra[UpstreamModelCatalogExtraKey] = AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"private-discovered", "private-target"}}
	other := catalogEvidenceAccount(8, PlatformGemini, "embedding-only")
	other.GroupIDs = []int64{11}
	repository := &configuredPlazaAccounts{visiblePolicyAccountRepo: &visiblePolicyAccountRepo{}, configured: []Account{account, other}}
	service := newPlazaService(channels, []Group{{ID: 10, Platform: PlatformComposite}}, nil)
	service.resolver = &ModelPricingResolver{channelService: &ChannelService{visibleModels: &VisibleModelResolver{gateway: &GatewayService{accountRepo: repository}}}}
	full, err := service.ListPublicCatalogGroups(ctx)
	require.NoError(t, err)
	models := catalogGroupModels(full)[10]
	require.Len(t, models, 3)
	require.Contains(t, models, PlazaModelIdentity{PlatformGrok, "video-only"})
	require.NotContains(t, models, PlazaModelIdentity{PlatformGrok, "private-discovered"})
	for _, model := range models {
		require.True(t, model.CatalogOnly)
	}
	selection := PlazaCatalogSelection{Groups: map[int64]bool{10: true}, Channels: map[int64]bool{}, Accounts: map[int64]domain.ActionIdentity{}}
	full, err = service.ListProjectCatalogGroups(ctx, nil, false, selection)
	require.NoError(t, err)
	require.Empty(t, full)
	selection.Accounts[7] = domain.ActionIdentity{Platform: PlatformGrok}
	full, err = service.ListProjectCatalogGroups(ctx, nil, false, selection)
	require.NoError(t, err)
	models = catalogGroupModels(full)[10]
	require.Contains(t, models, PlazaModelIdentity{PlatformGrok, "video-only"})
	require.Contains(t, models, PlazaModelIdentity{PlatformGrok, "private-discovered"})
	require.NotContains(t, models, PlazaModelIdentity{PlatformGrok, "private-target"})
	require.Len(t, models, 2)
	observation := models[PlazaModelIdentity{PlatformGrok, "private-discovered"}]
	require.True(t, observation.CatalogOnly)
	require.Empty(t, observation.CatalogBindings)
	require.Len(t, observation.CatalogObservations, 1)
	require.Equal(t, "video_generation", observation.CatalogObservations[0].Metadata.TaskType)
}
