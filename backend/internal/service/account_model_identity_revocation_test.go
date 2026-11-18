package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfiguredSelectionCannotRestoreRevokedCatalogIdentity(t *testing.T) {
	for _, transport := range []string{"chat", "compact", "http-passthrough", "ws-passthrough"} {
		for _, prepared := range []bool{false, true} {
			name := transport + "/direct"
			if prepared {
				name = transport + "/prepared"
			}
			t.Run(name, func(t *testing.T) {
				account := catalogEvidenceAccount(1, PlatformOpenAI, "target")
				account.Credentials["model_mapping"] = map[string]any{"alias": "target"}
				account.Extra["openai_responses_supported"] = transport != "chat"
				options := AccountModelBindingOptions{Endpoint: "chat_completions"}
				switch transport {
				case "compact":
					account.Credentials["compact_model_mapping"] = map[string]any{"alias": "compact-private"}
					options = AccountModelBindingOptions{Endpoint: "responses", Compact: true}
				case "http-passthrough":
					account.Extra["openai_passthrough"] = true
					options = AccountModelBindingOptions{Endpoint: "responses", FinalModel: "alias"}
				case "ws-passthrough":
					options = AccountModelBindingOptions{Endpoint: "responses", FinalModel: "alias", Passthrough: true}
				}
				require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", options), "fixture must be usable before identity revocation")
				account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1, RulesRevision: 1}
				if prepared {
					prepareCatalogReadAccount(&account, nil)
				}
				binding := ResolveAccountModelBinding(context.Background(), &account, "alias", options)
				require.False(t, binding.Supported, "unchanged mappings cannot authorize the identity that replaced their stored catalog")
			})
		}
	}
}

func TestRevokedCatalogIdentityAcceptsOnlyCurrentEvidence(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "target")
	account.Credentials["model_mapping"] = map[string]any{"alias": "target"}
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1, RulesRevision: 1}
	options := AccountModelBindingOptions{Endpoint: "chat_completions"}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", options))
	declaration := ModelAvailabilityDeclaration{
		AccountID: account.ID, Model: "target", Endpoint: "chat_completions",
		IdentityRevision: 1, RulesRevision: 1, ConfirmedAt: time.Now().Add(-time.Minute),
		ConfirmedBy: 7, Reason: "new identity explicitly confirmed", EvidenceKind: ModelEvidenceAdministrator,
	}
	account.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{declaration}
	binding := ResolveAccountModelBinding(context.Background(), &account, "alias", options)
	require.True(t, binding.Supported)
	require.Equal(t, ModelEvidenceAdministrator, binding.EvidenceKind)
	revoked := time.Now()
	declaration.RevokedAt = &revoked
	account.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{declaration}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", options))
	delete(account.Extra, ModelAvailabilityExtraKey)
	current := account.GetUpstreamModelCatalog()
	current.IdentityRevision = 1
	current.RulesRevision = 1
	account.Extra[UpstreamModelCatalogExtraKey] = current
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", options), "fresh publication restores the new identity")
}

func TestConfiguredSelectionCannotRestoreRevokedShadowCredentialIdentity(t *testing.T) {
	parent := catalogEvidenceAccount(8, PlatformOpenAI)
	parent.Type = AccountTypeOAuth
	parent.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
	shadow := catalogEvidenceAccount(9, PlatformOpenAI, "target")
	shadow.ParentAccountID = &parent.ID
	shadow.Credentials["model_mapping"] = map[string]any{"alias": "target"}
	stored := shadow.GetUpstreamModelCatalog()
	stored.CredentialAccountID = parent.ID
	stored.CredentialIdentityRevision = 0
	shadow.Extra[UpstreamModelCatalogExtraKey] = stored
	require.Nil(t, shadow.EffectiveUpstreamModelCatalogForCredentialAccount(&parent))
	current := *stored
	current.CredentialIdentityRevision = 1
	shadow.Extra[UpstreamModelCatalogExtraKey] = &current
	require.NotNil(t, shadow.EffectiveUpstreamModelCatalogForCredentialAccount(&parent))
}

func TestIdentityRevocationClearsStickyWithoutChangingLegacySelection(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "target")
	account.Credentials["model_mapping"] = map[string]any{"alias": "target"}
	require.False(t, shouldClearStickySession(&account, "alias"))
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
	require.True(t, shouldClearStickySession(&account, "alias"))
	current := account.GetUpstreamModelCatalog()
	current.IdentityRevision = 1
	account.Extra[UpstreamModelCatalogExtraKey] = current
	require.False(t, shouldClearStickySession(&account, "alias"))
	legacy := legacyCatalogSeedAccount(PlatformDeepseek)
	legacy.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
	require.False(t, shouldClearStickySession(&legacy, "public-model"), "historical mapping seeds keep existing explicit-selection semantics")
}
