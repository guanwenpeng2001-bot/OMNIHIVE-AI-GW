package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func catalogEvidenceAccount(id int64, platform string, models ...string) Account {
	snapshot := &AccountModelCatalogSnapshot{Revision: 7, Complete: true, DiscoveryScope: "account", DiscoveredModels: models, EligibleModels: []AccountCatalogModel{}}
	for _, model := range models {
		snapshot.EligibleModels = append(snapshot.EligibleModels, AccountCatalogModel{ID: model, Endpoints: []string{"responses", "chat_completions", "messages"}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: "2026-09-15T00:00:00Z"})
	}
	return Account{ID: id, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{}, Extra: map[string]any{UpstreamModelCatalogExtraKey: snapshot}}
}

func TestModelCatalogBindingUsesSameAccountFinalTarget(t *testing.T) {
	ctx := context.Background()
	first := catalogEvidenceAccount(1, PlatformOpenAI, "native-a")
	second := catalogEvidenceAccount(2, PlatformOpenAI, "native-b")
	first.Credentials["model_mapping"] = map[string]any{"company": "native-b"}
	require.False(t, ResolveAccountModelBinding(ctx, &first, "company", AccountModelBindingOptions{}).Supported)
	require.True(t, ResolveAccountModelBinding(ctx, &second, "native-b", AccountModelBindingOptions{}).Supported)
	first.Credentials["model_mapping"] = map[string]any{"company": "native-a"}
	binding := ResolveAccountModelBinding(ctx, &first, "company", AccountModelBindingOptions{Endpoint: "responses"})
	require.True(t, binding.Supported)
	require.Equal(t, "native-a", binding.UpstreamModel)
	require.Equal(t, int64(1), binding.AccountID)
	require.Equal(t, int64(7), binding.SnapshotRevision)
	require.Equal(t, "native_account_catalog", binding.EvidenceKind)
	require.False(t, ResolveAccountModelBinding(ctx, &first, "company", AccountModelBindingOptions{Endpoint: "embeddings"}).Supported)
}

func TestModelCatalogBindingIdentityRevocationAndUnlistedEvidence(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "native", AccountModelBindingOptions{}))
	delete(account.Extra, UpstreamModelCatalogStateExtraKey)
	snapshot, ok := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	require.True(t, ok)
	snapshot.DiscoveredModels = []string{}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "native", AccountModelBindingOptions{}))
}

func TestModelCatalogPreparationDoesNotChangePublicProjection(t *testing.T) {
	for _, models := range [][]string{{"native"}, {}} {
		account := catalogEvidenceAccount(1, PlatformOpenAI, models...)
		account.Extra["openai_passthrough"] = true
		account.Credentials["model_mapping"] = map[string]any{"foreign": "foreign"}
		body := []byte(`{"data":[{"id":"native"},{"id":"foreign"}]}`)
		projected, err := projectAccountModelsBody(body, &account, nil, false)
		require.NoError(t, err)
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(projected, &result))
		require.Len(t, result.Data, 2, "PREP must retain NEXT public projection until T7")
		require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "foreign", AccountModelBindingOptions{}))
		require.Equal(t, len(models) > 0, AccountSupportsResolvedModel(context.Background(), &account, "native", AccountModelBindingOptions{}))
		if len(models) > 0 {
			require.Equal(t, "native", result.Data[0].ID)
		}
	}
}

func TestModelCatalogResolvedForwardModelAndFinalModelDoNotRepeatAliases(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Credentials["model_mapping"] = map[string]any{"channel-result": "native", "native": "wrong"}
	ctx := WithOpenAIForwardModel(context.Background(), "channel-result", false)
	binding := ResolveAccountModelBinding(ctx, &account, "public", AccountModelBindingOptions{})
	require.True(t, binding.Supported)
	require.Equal(t, "public", binding.PublicModel)
	require.Equal(t, "native", binding.UpstreamModel)
	final := ResolveAccountModelBinding(ctx, &account, "public", AccountModelBindingOptions{FinalModel: "native"})
	require.True(t, final.Supported)
	require.Equal(t, "native", final.UpstreamModel)
}
