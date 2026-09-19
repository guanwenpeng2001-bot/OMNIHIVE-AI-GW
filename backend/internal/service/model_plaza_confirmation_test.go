//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelPlazaConfirmationContract(t *testing.T) {
	channel := plazaPricedChannel(1, "legacy", []int64{10, 20}, PlatformOpenAI, "historical")
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]string{"alias": "mapped", "*": "concrete", "bad": "wild*"}}, Extra: map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"discovered", "shared"}}}},
		{ID: 2, Platform: PlatformAnthropic, Extra: map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"shared"}}}},
	}
	view, err := BuildPlazaCandidates(&channel, 10, PlatformComposite, accounts)
	require.NoError(t, err)
	require.Len(t, view.Candidates, 5)
	require.Equal(t, []PlazaConfirmedModel{{PlazaModelIdentity: PlazaModelIdentity{PlatformOpenAI, "historical"}}}, view.Selected)
	require.Empty(t, view.Selected[0].ConfirmedAt)
	beforePricing, _ := json.Marshal(channel.ModelPricing)
	beforeAccounts, _ := json.Marshal(accounts)
	input := PlazaConfirmationInput{GroupID: 10, Revision: view.Revision, Models: []PlazaModelIdentity{{PlatformOpenAI, "discovered"}, {PlatformAnthropic, "shared"}}}
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	require.NoError(t, ApplyPlazaConfirmation(&channel, view, input, 42, now))
	require.Equal(t, int64(42), ChannelPlazaConfirmation(&channel).Groups["10"][0].ConfirmedBy)
	require.Equal(t, now.Format(time.RFC3339Nano), ChannelPlazaConfirmation(&channel).Groups["10"][0].ConfirmedAt)
	require.Len(t, PlazaVisibleModels(&channel, 10), 2)
	require.Equal(t, "historical", PlazaVisibleModels(&channel, 20)[0].Name)
	afterPricing, _ := json.Marshal(channel.ModelPricing)
	afterAccounts, _ := json.Marshal(accounts)
	require.Equal(t, string(beforePricing), string(afterPricing))
	require.Equal(t, string(beforeAccounts), string(afterAccounts))

	t.Run("unconfirmed discovery stays private", func(t *testing.T) {
		service := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformComposite}}, nil)
		groups, err := service.ListGroups(context.Background())
		require.NoError(t, err)
		require.Len(t, groups, 1)
		require.Len(t, groups[0].Models, 2)
		for _, model := range groups[0].Models {
			require.NotEqual(t, "mapped", model.Name)
		}
	})
	t.Run("conflict and invalid selection preserve saved configuration", func(t *testing.T) {
		original, _ := json.Marshal(channel.FeaturesConfig)
		invalid := input
		invalid.Revision = "outdated"
		require.ErrorIs(t, ApplyPlazaConfirmation(&channel, view, invalid, 42, now), ErrPlazaConfirmationConflict)
		invalid = input
		invalid.Models = []PlazaModelIdentity{{PlatformOpenAI, "preset-only"}}
		require.ErrorIs(t, ApplyPlazaConfirmation(&channel, view, invalid, 42, now), ErrPlazaConfirmationInvalid)
		invalid.Models = append(input.Models, input.Models[0])
		require.ErrorIs(t, ApplyPlazaConfirmation(&channel, view, invalid, 42, now), ErrPlazaConfirmationInvalid)
		actual, _ := json.Marshal(channel.FeaturesConfig)
		require.Equal(t, string(original), string(actual))
	})
	t.Run("association platform and stale identity", func(t *testing.T) {
		_, err := BuildPlazaCandidates(&channel, 99, PlatformOpenAI, accounts)
		require.ErrorIs(t, err, ErrPlazaConfirmationInvalid)
		accounts[0].Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
		fresh, err := BuildPlazaCandidates(&channel, 10, PlatformOpenAI, accounts)
		require.NoError(t, err)
		require.NotEqual(t, view.Revision, fresh.Revision)
		require.Equal(t, []PlazaModelIdentity{{PlatformOpenAI, "concrete"}, {PlatformOpenAI, "mapped"}}, fresh.Candidates)
	})
	t.Run("new channel remains empty", func(t *testing.T) {
		channel.FeaturesConfig = map[string]any{PlazaConfirmationConfigKey: PlazaConfirmationConfig{Groups: map[string][]PlazaConfirmedModel{}}}
		require.Empty(t, PlazaVisibleModels(&channel, 10))
	})
}

func TestModelPlazaConfirmationGenericSaveProtection(t *testing.T) {
	channel := plazaPricedChannel(1, "legacy", []int64{10}, PlatformOpenAI, "historical")
	repository := &mockChannelRepository{
		getByIDFn: func(context.Context, int64) (*Channel, error) { copyChannel := channel; return &copyChannel, nil },
		updateFn:  func(_ context.Context, updated *Channel) error { channel = *updated; return nil },
		createFn:  func(_ context.Context, created *Channel) error { channel = *created; return nil },
	}
	service := NewChannelService(repository, nil, nil, nil, nil)
	changedPricing := []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"new-price-model"}, BillingMode: BillingModeToken}}
	forged := map[string]any{PlazaConfirmationConfigKey: map[string]any{"groups": map[string]any{"10": []any{map[string]any{"platform": "openai", "name": "forged", "confirmed_by": 999}}}}}
	_, err := service.Update(context.Background(), 1, &UpdateChannelInput{FeaturesConfig: forged, ModelPricing: &changedPricing})
	require.NoError(t, err)
	selected := ChannelPlazaConfirmation(&channel).Groups["10"]
	require.Len(t, selected, 1)
	require.Equal(t, "historical", selected[0].Name)
	require.Zero(t, selected[0].ConfirmedBy)
	_, err = service.Create(context.Background(), &CreateChannelInput{Name: "new-channel", GroupIDs: []int64{10}, ModelPricing: changedPricing, FeaturesConfig: forged})
	require.NoError(t, err)
	require.Empty(t, PlazaVisibleModels(&channel, 10))
}
