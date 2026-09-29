package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCapabilityDeclarationIdentityAndNoAuthorization(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "original"}, Extra: map[string]any{}}
	declaration := map[string]any{
		"schema_version": 1, "reason": "verified private deployment", "identity_revision": "0",
		"identity_fingerprint": ModelCapabilityIdentityFingerprint(account),
		"models": map[string]any{"private": map[string]any{
			"reasoning": false, "input_modalities": []string{}, "supported_methods": []string{"responses"},
			"tools": map[string]any{"function_calling": false},
		}},
	}
	account.Extra[ModelCapabilityDefaultsExtraKey] = declaration
	before, err := json.Marshal(account)
	require.NoError(t, err)
	facts, err := accountDeclaredCapabilities(account)
	require.NoError(t, err)
	require.False(t, *facts["private"].Reasoning)
	require.False(t, *facts["private"].SupportsFunctionCalling)
	require.NotNil(t, facts["private"].InputModalities)
	require.Empty(t, facts["private"].InputModalities)
	require.Nil(t, facts["private"].SupportedMethods, "private specifications cannot declare endpoint entitlement")
	require.Equal(t, "declared", facts["private"].FieldEvidence["reasoning"].Confidence)
	require.False(t, AccountSupportsResolvedModel(context.Background(), account, "private", AccountModelBindingOptions{}))
	after, err := json.Marshal(account)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	account.Credentials["api_key"] = "rotated"
	_, err = accountDeclaredCapabilities(account)
	require.ErrorContains(t, err, "stale or unbound")
	_, err = (&AccountTestService{}).DiscoverModelCapabilities(context.Background(), account, nil)
	require.Error(t, err, "stale declarations must fail before any network access")
}

func TestModelCapabilityIdentityStableSubjectAndParent(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"sub": "subject", "access_token": "old"}}
	initial := ModelCapabilityIdentityFingerprint(account)
	account.Credentials["access_token"] = "refreshed"
	require.Equal(t, initial, ModelCapabilityIdentityFingerprint(account))
	account.ParentAccountID = new(int64(12))
	require.NotEqual(t, initial, ModelCapabilityIdentityFingerprint(account))
	account.ParentAccountID = nil
	delete(account.Credentials, "sub")
	initial = ModelCapabilityIdentityFingerprint(account)
	account.Credentials["access_token"] = "another"
	require.NotEqual(t, initial, ModelCapabilityIdentityFingerprint(account))
}

func TestModelCapabilityExplicitEmptyDeclarationBlocksFallback(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}, Extra: map[string]any{}}
	account.Extra[ModelCapabilityDefaultsExtraKey] = map[string]any{"schema_version": 1, "reason": "verified", "identity_revision": "0", "identity_fingerprint": ModelCapabilityIdentityFingerprint(account), "models": map[string]any{"private": map[string]any{"input_modalities": []string{}, "output_modalities": []string{}, "reasoning": true, "reasoning_control": &ReasoningControl{Kind: "unknown", EffortValues: []string{}}}}}
	raw, err := json.Marshal(account.Extra)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &account.Extra))
	declared, err := accountDeclaredCapabilities(account)
	require.NoError(t, err)
	merged, _ := mergeUpstreamModelMetadata(declared["private"], UpstreamModelMetadata{InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, ReasoningControl: &ReasoningControl{Kind: "effort", EffortValues: []string{"low", "high"}}, SupportedReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: "high"})
	for _, values := range [][]string{merged.InputModalities, merged.OutputModalities, merged.ReasoningControl.EffortValues, merged.SupportedReasoningLevels} {
		require.NotNil(t, values)
		require.Empty(t, values, "a lower priority source cannot refill an explicit empty domain")
	}
	require.Empty(t, merged.DefaultReasoningLevel)
}

func TestAccountModelCatalogTokenUpdateRevisionSemantics(t *testing.T) {
	for _, manual := range []bool{false, true} {
		current := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old", "sub": "stable"}, Extra: map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{IdentityRevision: 3, RulesRevision: 7}}}
		incoming := *current
		incoming.Extra = map[string]any{}
		incoming.Credentials = map[string]any{"access_token": "new", "sub": "stable"}
		AdvanceAccountModelCatalogRevisionsForUpdate(current, &incoming, manual)
		wantIdentity, wantRules := int64(3), int64(7)
		if manual {
			wantIdentity++
			wantRules++
		}
		require.Equal(t, wantIdentity, incoming.ModelCatalogState().IdentityRevision)
		require.Equal(t, wantRules, incoming.ModelCatalogState().RulesRevision)
	}
}
