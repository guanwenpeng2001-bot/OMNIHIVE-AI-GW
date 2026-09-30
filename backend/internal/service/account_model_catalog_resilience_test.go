package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLegacyMappingFingerprintCanonicalEncoding(test *testing.T) {
	first, valid := LegacyModelMappingFingerprint(map[string]any{"model_mapping": map[string]string{"中文": "<target>", "alias": "target"}})
	require.True(test, valid)
	second, valid := LegacyModelMappingFingerprint(map[string]any{"model_mapping": map[string]any{"alias": "target", "中文": "<target>"}})
	require.True(test, valid)
	require.Equal(test, first, second)
	missing, valid := LegacyModelMappingFingerprint(nil)
	require.True(test, valid)
	null, valid := LegacyModelMappingFingerprint(map[string]any{"model_mapping": nil})
	require.True(test, valid)
	require.Equal(test, missing, null)
	empty, valid := LegacyModelMappingFingerprint(map[string]any{"model_mapping": map[string]string{}})
	require.True(test, valid)
	require.NotEqual(test, missing, empty)
	for _, malformed := range []any{[]string{}, "mapping", map[string]any{"alias": nil}, map[string]any{"alias": 1}, map[string]any{"alias": map[string]string{}}} {
		_, valid := LegacyModelMappingFingerprint(map[string]any{"model_mapping": malformed})
		require.False(test, valid)
	}
}

func TestLegacyEvidenceRotationPreservesCurrentSelectionAndRevokesStaleRequests(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account)
	}{
		{"target", func(account *Account) {
			account.Credentials["model_mapping"] = map[string]string{"public-model": "other"}
		}},
		{"alias", func(account *Account) {
			account.Credentials["model_mapping"] = map[string]string{"other-alias": "actual-model"}
		}},
		{"empty", func(account *Account) { account.Credentials["model_mapping"] = map[string]string{} }},
		{"removed", func(account *Account) { delete(account.Credentials, "model_mapping") }},
		{"protocol", func(account *Account) { account.Credentials["protocol_rules"] = "changed" }},
		{"proxy", func(account *Account) { proxyID := int64(42); account.ProxyID = &proxyID }},
		{"release", func(account *Account) { account.Extra[ModelCapabilityReleaseExtraKey] = "changed" }},
		{"subject", func(account *Account) { account.Credentials["sub"] = "different-subject" }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := legacyCatalogSeedAccount(PlatformAntigravity)
			account.Type = AccountTypeOAuth
			require.True(test, UpgradeLegacyModelCatalogEvidence(&account))
			require.False(test, UpgradeLegacyModelCatalogEvidence(&account))
			clone := func(account Account) Account {
				encoded, err := json.Marshal(account)
				require.NoError(test, err)
				var copied Account
				require.NoError(test, json.Unmarshal(encoded, &copied))
				return copied
			}
			for _, token := range []string{"renewed", "renewed-again"} {
				incoming := clone(account)
				incoming.Credentials["access_token"] = token
				AdvanceAccountModelCatalogRevisions(&account, &incoming)
				require.Equal(test, account.ModelCatalogState().IdentityRevision+1, incoming.ModelCatalogState().IdentityRevision)
				require.Equal(test, account.ModelCatalogState().RulesRevision+1, incoming.ModelCatalogState().RulesRevision)
				require.Zero(test, incoming.ModelCatalogState().LegacyPolicyRevision)
				require.True(test, ResolveAccountModelBinding(context.Background(), &incoming, "public-model", AccountModelBindingOptions{Endpoint: "generate_content"}).Supported)
				repository := &forwardCatalogRepo{current: &incoming}
				ids, err := (&GatewayService{accountRepo: repository}).CatalogModelIDs(context.Background(), nil, PlatformAntigravity)
				require.NoError(test, err)
				require.Equal(test, []string{"public-model"}, ids)
				require.NoError(test, validateAccountForwardModel(context.Background(), repository, &incoming, "actual-model", "generate_content"))
				account = incoming
			}
			changed := clone(account)
			changed.Credentials["access_token"] = "simultaneous-renewal"
			scenario.change(&changed)
			AdvanceAccountModelCatalogRevisions(&account, &changed)
			require.EqualValues(test, 1, changed.ModelCatalogState().LegacyPolicyRevision)
			require.False(test, legacyModelCatalogValidFor(&changed, changed.GetUpstreamModelCatalog()), "changed policy invalidates the historical seed")
			options := AccountModelBindingOptions{Endpoint: "generate_content"}
			for alias, target := range configuredModelMapping(&changed) {
				binding := ResolveAccountModelBinding(context.Background(), &changed, alias, options)
				require.True(test, binding.Supported, "current explicit selection remains authoritative")
				require.Equal(test, target, binding.UpstreamModel)
			}
			if scenario.name == "empty" || scenario.name == "removed" || scenario.name == "alias" {
				require.False(test, ResolveAccountModelBinding(context.Background(), &changed, "public-model", options).Supported)
			}
			require.False(test, ResolveAccountModelBinding(context.Background(), &changed, "unselected", options).Supported)
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Protocol: "generate_content", Model: "public-model"}))
			require.ErrorIs(test, validateAccountForwardModel(ctx, &forwardCatalogRepo{current: &changed}, &account, "actual-model", "generate_content"), ErrAccountModelUnsupported, "a request selected before the credential change must not send")
			restored := clone(account)
			AdvanceAccountModelCatalogRevisions(&changed, &restored)
			require.EqualValues(test, 2, restored.ModelCatalogState().LegacyPolicyRevision)
			require.False(test, legacyModelCatalogValidFor(&restored, restored.GetUpstreamModelCatalog()), "restoring configuration does not resurrect historical evidence")
			require.True(test, ResolveAccountModelBinding(context.Background(), &restored, "public-model", options).Supported, "a newly saved selection can attempt the adapter")
			require.ErrorIs(test, validateAccountForwardModel(ctx, &forwardCatalogRepo{current: &restored}, &changed, "actual-model", "generate_content"), ErrAccountModelUnsupported)
		})
	}
}

func TestLegacyFingerprintEvidenceIsSeparateFromCurrentSelection(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account, *AccountModelCatalogSnapshot)
	}{
		{"wrong account", func(account *Account, snapshot *AccountModelCatalogSnapshot) { account.ID++ }},
		{"wrong scope", func(account *Account, snapshot *AccountModelCatalogSnapshot) { snapshot.DiscoveryScope = "account" }},
		{"wrong source", func(account *Account, snapshot *AccountModelCatalogSnapshot) {
			snapshot.SourceKind = "native_account_catalog"
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		}},
		{"member timestamp", func(account *Account, snapshot *AccountModelCatalogSnapshot) {
			snapshot.EligibleModels[0].EvidenceObservedAt = "different-time"
		}},
		{"invalid credentials", func(account *Account, snapshot *AccountModelCatalogSnapshot) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
		}},
		{"owner required", func(account *Account, snapshot *AccountModelCatalogSnapshot) { snapshot.CredentialAccountID = 99 }},
		{"disabled", func(account *Account, snapshot *AccountModelCatalogSnapshot) { account.Schedulable = false }},
		{"expired", func(account *Account, snapshot *AccountModelCatalogSnapshot) {
			expired := time.Now().Add(-time.Hour)
			account.ExpiresAt = &expired
		}},
		{"allowlist", func(account *Account, snapshot *AccountModelCatalogSnapshot) {
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true}
		}},
		{"untracked mapping write", func(account *Account, snapshot *AccountModelCatalogSnapshot) {
			account.Credentials["model_mapping"] = map[string]string{"public-model": "other"}
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := legacyCatalogSeedAccount(PlatformAnthropic)
			require.True(test, UpgradeLegacyModelCatalogEvidence(&account))
			snapshot := account.GetUpstreamModelCatalog()
			scenario.change(&account, snapshot)
			account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			options := AccountModelBindingOptions{Endpoint: "messages"}
			denied := scenario.name == "disabled" || scenario.name == "expired" || scenario.name == "allowlist"
			binding := ResolveAccountModelBinding(context.Background(), &account, "public-model", options)
			require.Equal(test, !denied, binding.Supported)
			if !denied {
				require.Equal(test, account.GetMappedModel("public-model"), binding.UpstreamModel)
			}
			require.False(test, AccountSupportsResolvedModel(context.Background(), &account, "unselected", options))
			// Without the current selection, none of these invalid historical
			// snapshots can confer model access, including a foreign owner.
			delete(account.Credentials, "model_mapping")
			require.False(test, AccountSupportsResolvedModel(context.Background(), &account, "actual-model", options))
		})
	}
}

func TestLegacyEvidenceUpgradeAndFormatGuards(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account)
	}{
		{"stale identity", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		}},
		{"stale rules", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
		}},
		{"invalid credentials", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
		}},
		{"disabled", func(account *Account) { account.Schedulable = false }},
		{"wrong account", func(account *Account) { account.ID++ }},
		{"malformed mapping", func(account *Account) { account.Credentials["model_mapping"] = map[string]any{"alias": nil} }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := legacyCatalogSeedAccount(PlatformAnthropic)
			scenario.change(&account)
			require.False(test, UpgradeLegacyModelCatalogEvidence(&account))
			require.Empty(test, account.GetUpstreamModelCatalog().LegacyMappingFingerprint)
		})
	}
	account := legacyCatalogSeedAccount(PlatformAnthropic)
	require.True(test, UpgradeLegacyModelCatalogEvidence(&account))
	snapshot := account.GetUpstreamModelCatalog()
	snapshot.LegacyMappingFingerprint = "sha256:999:unknown"
	account.Extra[UpstreamModelCatalogExtraKey] = snapshot
	require.False(test, legacyModelCatalogValidFor(&account, snapshot))
	require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}))
	require.Equal(test, ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
	account = legacyCatalogSeedAccount(PlatformAnthropic)
	require.True(test, UpgradeLegacyModelCatalogEvidence(&account))
	snapshot = account.GetUpstreamModelCatalog()
	snapshot.LegacyPolicyRevision = nil
	account.Extra[UpstreamModelCatalogExtraKey] = snapshot
	require.False(test, legacyModelCatalogValidFor(&account, snapshot))
	require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}))
	require.Equal(test, ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
	account.Extra = catalogEvidenceAccount(account.ID, account.Platform).Extra
	require.False(test, UpgradeLegacyModelCatalogEvidence(&account), "empty discovery cannot become a seed")
	require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}), "empty discovery cannot revoke a saved selection")
	account.Credentials["model_mapping"] = map[string]string{}
	NormalizeEmptyAccountModelSelection(&account)
	require.False(test, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "messages"}), "removing the saved selection revokes access")
}
