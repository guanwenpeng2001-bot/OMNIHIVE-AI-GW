package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func legacyCatalogSeedAccount(platform string) Account {
	observedAt := "2026-09-20T00:00:00Z"
	return Account{
		ID: 51, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"public-model": "actual-model"}},
		Extra: map[string]any{UpstreamModelCatalogExtraKey: map[string]any{
			"legacy_account_id": int64(51), "revision": int64(1), "identity_revision": int64(0), "rules_revision": int64(0),
			"source_kind": "legacy-mapping-upgrade", "discovery_scope": "legacy-mapping-upgrade", "complete": true,
			"observed_at": observedAt, "discovered_models": []string{},
			"eligible_models": []AccountCatalogModel{{ID: "actual-model", EvidenceKind: "legacy-mapping-upgrade", EvidenceObservedAt: observedAt}},
		}},
	}
}

func TestLegacyModelCatalogSeedRestoresMappingWithoutDiscovery(t *testing.T) {
	account := legacyCatalogSeedAccount(PlatformAnthropic)
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Protocol: "messages", Model: "public-model"}))
	options := AccountModelBindingOptions{Endpoint: "messages"}
	binding := ResolveAccountModelBinding(ctx, &account, "public-model", options)
	require.True(t, binding.Supported, "an upgraded mapped account must remain schedulable")
	require.Equal(t, "actual-model", binding.UpstreamModel)
	require.Equal(t, "legacy-mapping-upgrade", binding.EvidenceKind)
	require.Empty(t, account.GetUpstreamModelCatalog().DiscoveredModels, "configuration is not native discovery")

	repository := &forwardCatalogRepo{current: &account}
	require.NoError(t, validateAccountForwardModel(ctx, repository, &account, "actual-model", "messages"))
	ids, err := (&GatewayService{accountRepo: repository}).CatalogModelIDs(ctx, nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, []string{"public-model"}, ids)
	require.ErrorIs(t, validateAccountForwardModel(ctx, repository, &account, "actual-model", "embeddings"), ErrAccountModelUnsupported)
}

func TestLegacyModelCatalogSeedPreservesNativeTextProtocols(t *testing.T) {
	for _, scenario := range []struct{ name, platform, accountType, endpoint string }{
		{"anthropic", PlatformAnthropic, AccountTypeAPIKey, "messages"},
		{"openai passthrough", PlatformOpenAI, AccountTypeAPIKey, "chat_completions"},
		{"openai default responses", PlatformOpenAI, AccountTypeAPIKey, "responses"},
		{"openai oauth", PlatformOpenAI, AccountTypeOAuth, "responses"},
		{"gemini", PlatformGemini, AccountTypeAPIKey, "generate_content"},
		{"antigravity", PlatformAntigravity, AccountTypeOAuth, "generate_content"},
		{"deepseek", PlatformDeepseek, AccountTypeAPIKey, "chat_completions"},
		{"mistral", PlatformMistral, AccountTypeAPIKey, "chat_completions"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := legacyCatalogSeedAccount(scenario.platform)
			account.Type = scenario.accountType
			account.Credentials["model_mapping"] = map[string]any{"actual-model": "actual-model"}
			if scenario.name == "openai passthrough" {
				account.Extra["openai_passthrough"] = true
			}
			require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "actual-model", AccountModelBindingOptions{Endpoint: scenario.endpoint}))
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "unseeded-model", AccountModelBindingOptions{Endpoint: scenario.endpoint}))
		})
	}
}

func TestLegacyModelCatalogCurrentSelectionAndRevocation(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		change  func(*Account)
		allowed bool
	}{
		{"identity changed", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		}, true},
		{"rules changed", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
		}, true},
		{"invalid credentials", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
		}, true},
		{"copied account retains saved selection", func(account *Account) { account.ID++ }, true},
		{"disabled account", func(account *Account) { account.Schedulable = false }, false},
		{"expired account", func(account *Account) { account.ExpiresAt = new(time.Now().Add(-time.Hour)) }, false},
		{"allowlist restriction", func(account *Account) {
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"different-model"}}
		}, false},
		{"new account without migration", func(account *Account) { delete(account.Extra, UpstreamModelCatalogExtraKey) }, true},
		{"removed selection", func(account *Account) {
			account.Credentials["model_mapping"] = map[string]string{}
			NormalizeEmptyAccountModelSelection(account)
		}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := legacyCatalogSeedAccount(PlatformAnthropic)
			scenario.change(&account)
			require.Equal(t, scenario.allowed, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}))
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "unselected", AccountModelBindingOptions{Endpoint: "messages"}))
		})
	}
}

func TestLegacyModelCatalogSeedKeepsFactsSeparateFromMembership(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		metadata UpstreamModelMetadata
		endpoint string
		allowed  bool
	}{
		{"explicit empty methods keep selected text adapter", UpstreamModelMetadata{SupportedMethods: []string{}}, "chat_completions", true},
		{"known image task", UpstreamModelMetadata{TaskType: "image_generation"}, "images", true},
		{"task does not override selected text adapter", UpstreamModelMetadata{TaskType: "image_generation"}, "chat_completions", true},
		{"unknown permits selected media adapter", UpstreamModelMetadata{}, "images", true},
		{"unknown declaration permits selected media adapter", UpstreamModelMetadata{TaskType: "unknown"}, "images", true},
		{"known task does not remove selected media adapter", UpstreamModelMetadata{TaskType: "text_generation"}, "images", true},
		{"unimplemented endpoint remains rejected", UpstreamModelMetadata{}, "unimplemented", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := legacyCatalogSeedAccount(PlatformOpenAI)
			account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"actual-model": scenario.metadata}})
			require.Equal(t, scenario.allowed, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: scenario.endpoint}))
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "gpt-5.1", AccountModelBindingOptions{Endpoint: "responses"}), "an offline catalog must not grant an unseeded ID")
		})
	}
}

func TestLegacyModelCatalogSeedReplacementAndCopy(t *testing.T) {
	account := legacyCatalogSeedAccount(PlatformAnthropic)
	account.Extra = StripAccountModelCatalogExtra(account.Extra)
	require.Nil(t, account.GetUpstreamModelCatalog(), "copy/import must discard migration evidence")

	account = legacyCatalogSeedAccount(PlatformAnthropic)
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
	require.NotNil(t, account.EffectiveUpstreamModelCatalog(), "current mapping survives discarded stale migration evidence")
	require.Equal(t, ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
	require.EqualValues(t, 1, account.ModelCatalogBaseline().SnapshotRevision, "refresh CAS must retain the stale snapshot revision")

	account.Extra = catalogEvidenceAccount(account.ID, account.Platform).Extra
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}), "empty discovery does not revoke the saved mapping")
	account.Credentials["model_mapping"] = map[string]string{}
	NormalizeEmptyAccountModelSelection(&account)
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}), "removing the selection does revoke access")
}

func TestLegacyModelCatalogBindingRequiresSchedulableAccount(t *testing.T) {
	for _, scenario := range []struct{ platform, endpoint string }{
		{PlatformOpenAI, CompositeRouteEndpointResponses},
		{PlatformAntigravity, "generate_content"},
	} {
		t.Run(scenario.platform, func(t *testing.T) {
			account := legacyCatalogSeedAccount(scenario.platform)
			account.Type = AccountTypeOAuth
			account.Credentials["model_mapping"] = map[string]any{"actual-model": "actual-model"}
			options := AccountModelBindingOptions{Endpoint: scenario.endpoint}
			for _, composite := range []bool{false, true} {
				ctx := context.Background()
				if composite {
					ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{
						Matched: true, Source: CompositeRouteSourceExplicit, GroupID: 4,
						PublicModel: "actual-model", UpstreamModel: "actual-model",
						TargetPlatform: scenario.platform, Endpoint: scenario.endpoint,
					})
				}
				// A zero-value Schedulable caused the production-dump reproduction
				// to exit before checking any catalog member. Keep that account
				// gate distinct from the valid seed's lack of native discovery.
				account.Schedulable = false
				require.NotNil(t, account.EffectiveUpstreamModelCatalog())
				require.False(t, ResolveAccountModelBinding(ctx, &account, "actual-model", options).Supported)
				account.Schedulable = true
				binding := ResolveAccountModelBinding(ctx, &account, "actual-model", options)
				require.True(t, binding.Supported)
				require.Equal(t, modelEvidenceLegacyMappingUpgrade, binding.EvidenceKind)
				require.Empty(t, account.GetUpstreamModelCatalog().DiscoveredModels)
			}
		})
	}
}

func TestLegacyModelCatalogEvidenceBelongsToSeedSnapshot(t *testing.T) {
	snapshot := &AccountModelCatalogSnapshot{SourceKind: modelEvidenceLegacyMappingUpgrade, ObservedAt: "2026-09-20T00:00:00Z"}
	member := AccountCatalogModel{EvidenceKind: modelEvidenceLegacyMappingUpgrade, EvidenceObservedAt: snapshot.ObservedAt}
	require.True(t, hasLegacyModelCatalogEvidence(snapshot, member))
	member.EvidenceObservedAt = "2026-09-19T00:00:00Z"
	require.False(t, hasLegacyModelCatalogEvidence(snapshot, member), "a stale seed member cannot borrow the snapshot's evidence")
	member.EvidenceObservedAt = snapshot.ObservedAt
	snapshot.SourceKind = "native_account_catalog"
	require.False(t, hasLegacyModelCatalogEvidence(snapshot, member), "replacement discovery revokes legacy evidence")
}

func TestLegacyModelCatalogSeedUsesSavedOpenAIProtocol(t *testing.T) {
	account := legacyCatalogSeedAccount(PlatformOpenAI)
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Protocol: "responses", Model: "public-model"}))
	repository := &forwardCatalogRepo{current: &account}
	require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "public-model", OpenAIEndpointCapabilityResponses))
	require.NoError(t, validateAccountForwardModel(ctx, repository, &account, "actual-model", "responses"))
	account.Extra["openai_responses_supported"] = false
	require.ErrorIs(t, validateAccountForwardModel(ctx, repository, &account, "actual-model", "responses"), ErrAccountModelUnsupported)
	require.NoError(t, validateAccountForwardModel(ctx, repository, &account, "actual-model", "chat_completions"))
}

func TestLegacyModelCatalogSeedUsesExistingWireModelNormalization(t *testing.T) {
	for _, scenario := range []struct {
		name, platform, accountType, mappedModel, wireModel, endpoint string
	}{
		{"anthropic oauth", PlatformAnthropic, AccountTypeOAuth, "claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "messages"},
		{"vertex anthropic", PlatformAnthropic, AccountTypeServiceAccount, "claude-sonnet-4-5", "claude-sonnet-4-5@20250929", "messages"},
		{"gemini prefix", PlatformGemini, AccountTypeAPIKey, "models/private-model", "private-model", "generate_content"},
		{"codex alias", PlatformOpenAI, AccountTypeOAuth, "gpt-5.1-codex", "gpt-5.3-codex", "responses"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := legacyCatalogSeedAccount(scenario.platform)
			account.Type = scenario.accountType
			account.Credentials["model_mapping"] = map[string]any{"public-model": scenario.mappedModel}
			snapshot := account.GetUpstreamModelCatalog()
			snapshot.EligibleModels[0].ID = scenario.mappedModel
			account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "public-model"}))
			binding := ResolveAccountModelBinding(ctx, &account, "public-model", AccountModelBindingOptions{Endpoint: scenario.endpoint})
			require.Equal(t, scenario.wireModel, binding.UpstreamModel)
			require.True(t, binding.Supported)
			require.NoError(t, validateAccountForwardModel(ctx, &forwardCatalogRepo{current: &account}, &account, scenario.wireModel, scenario.endpoint))
			require.Equal(t, scenario.mappedModel, account.GetUpstreamModelCatalog().EligibleModels[0].ID, "persisted mapping identity must not be rewritten")
		})
	}
}
