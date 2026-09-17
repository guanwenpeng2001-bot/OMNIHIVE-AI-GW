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
