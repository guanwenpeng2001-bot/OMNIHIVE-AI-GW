package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestModelCatalogAllowlistAndCredentialRevocations(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "first", "second")
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"first", "not-discovered"}}
	options := AccountModelBindingOptions{FinalModel: "second", Endpoint: "responses"}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "second", options))
	options.FinalModel = "first"
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "first", options))
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "first", options))
	delete(account.Extra, UpstreamModelCatalogStateExtraKey)
	account.ExpiresAt = new(time.Now().Add(-time.Minute))
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "first", options))
	account.ExpiresAt = nil
	account.Schedulable = false
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "first", options))
}

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
	require.True(t, ResolveAccountModelBinding(ctx, &first, "company", AccountModelBindingOptions{}).Supported)
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

func TestOpenAIAPIKeyChatOnlyCatalogUsesChatCompletionsBridge(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "chat-only")
	snapshot, ok := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	require.True(t, ok)
	snapshot.EligibleModels[0].Endpoints = []string{CompositeRouteEndpointChatCompletions}

	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(&account))
	require.True(t, ResolveAccountModelBinding(context.Background(), &account, "chat-only", AccountModelBindingOptions{
		Endpoint: CompositeRouteEndpointResponses,
	}).Supported)

	responsesOnly := catalogEvidenceAccount(2, PlatformOpenAI, "responses-only")
	responsesSnapshot, ok := responsesOnly.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	require.True(t, ok)
	responsesSnapshot.EligibleModels[0].Endpoints = []string{CompositeRouteEndpointResponses}
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(&responsesOnly))

	mixed := catalogEvidenceAccount(3, PlatformOpenAI, "chat", "responses")
	mixedSnapshot, ok := mixed.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	require.True(t, ok)
	mixedSnapshot.EligibleModels[0].Endpoints = []string{CompositeRouteEndpointChatCompletions}
	mixedSnapshot.EligibleModels[1].Endpoints = []string{CompositeRouteEndpointResponses}
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(&mixed),
		"a Responses-capable model keeps the account on its native Responses path")
}

func TestLegacyCatalogEndpointSynthesisDoesNotReenterForwardPolicy(t *testing.T) {
	account := Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{
			Complete:   true,
			SourceKind: modelEvidenceLegacyMappingUpgrade,
			EligibleModels: []AccountCatalogModel{{
				ID:           "legacy-chat",
				EvidenceKind: ModelEvidenceConfiguredSelection,
			}},
		}},
	}

	require.NotPanics(t, func() {
		require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(&account))
	})
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

func TestModelCatalogUpgradeFreezeDoesNotSuppressNativeDiscovery(t *testing.T) {
	fixtures := map[string]string{
		PlatformAntigravity: "claude-opus-5-5-high",
		PlatformOpenCodeGo:  "beam-free",
		PlatformOpenRouter:  "baidu/ernie-5.5-flash-preview:free",
	}
	for _, platform := range []string{PlatformAntigravity, PlatformOpenCodeGo, PlatformOpenRouter} {
		t.Run(platform, func(t *testing.T) {
			model := fixtures[platform]
			account := catalogEvidenceAccount(1, platform, model)
			if platform == PlatformAntigravity {
				account.Type = AccountTypeOAuth
				account.Credentials["project_id"] = "project"
				account.Credentials["model_mapping"] = map[string]any{"claude-opus-4-7": "claude-opus-4-7"}
				snapshot, ok := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
				require.True(t, ok)
				snapshot.EligibleModels[0].Endpoints = []string{"generate_content"}
			}
			if platform == PlatformOpenCodeGo {
				account.Credentials["account_mode"] = AccountModeZenFree
				free := true
				account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &free}}})
			}
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}, Reason: "system_upgrade_baseline_freeze"}
			endpoint := CompositeRouteEndpointChatCompletions
			if platform == PlatformAntigravity {
				endpoint = CompositeRouteEndpointMessages
			}
			require.True(t, AccountSupportsResolvedModel(context.Background(), &account, model, AccountModelBindingOptions{Endpoint: endpoint}))

			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}, ConfirmedBy: 42, Reason: "administrator deny-all"}
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, model, AccountModelBindingOptions{Endpoint: endpoint}))
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}, ConfirmedBy: 42, ConfirmedAt: "2026-11-25T00:00:00Z", Reason: "system_upgrade_baseline_freeze"}
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, model, AccountModelBindingOptions{Endpoint: endpoint}))
		})
	}
}

func TestModelCatalogExplicitDenyAllIsNotUpgradeFreeze(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "native", AccountModelBindingOptions{Endpoint: CompositeRouteEndpointChatCompletions}))
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}, Reason: "system_upgrade_baseline_freeze"}
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "native", AccountModelBindingOptions{Endpoint: CompositeRouteEndpointChatCompletions}))
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
		require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "foreign", AccountModelBindingOptions{}))
		require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "native", AccountModelBindingOptions{}))
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
