package service

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountCredentialChangesPreservesQueryClearIntent(t *testing.T) {
	for _, reference := range []any{"reference", "", nil, 42} {
		before := map[string]any{"api_key": "inference", "note": "old", "usage_query_token": "query", CredentialSecretVersionKey: int64(7)}
		current := map[string]any{CredentialSecretReferenceKey: reference, CredentialSecretVersionKey: int64(8), "note": "old", "concurrent": "keep"}
		after := maps.Clone(before)
		for _, field := range SensitiveCredentialKeys {
			if IsManagementQuerySecret(field) {
				after[field] = nil
			}
		}
		after["api_key"], after["note"] = nil, nil
		merged, ok := MergeAccountCredentialChanges(WithAccountCredentialChanges(context.Background(), before, after), current)
		require.True(t, ok)
		require.Equal(t, int64(7), merged[CredentialSecretVersionKey], "same-type equal version is caller CAS intent")
		require.Equal(t, "keep", merged["concurrent"])
		require.NotContains(t, merged, "api_key")
		require.NotContains(t, merged, "note")
		for _, field := range SensitiveCredentialKeys {
			if !IsManagementQuerySecret(field) {
				continue
			}
			if reference == "reference" {
				require.Contains(t, merged, field)
				require.Nil(t, merged[field])
			} else {
				require.NotContains(t, merged, field)
			}
		}
		require.Equal(t, int64(8), current[CredentialSecretVersionKey], "locked row input is not mutated")
	}
}

func TestAccountCredentialChangesMissingVersionDoesNotAdoptLockedVersion(t *testing.T) {
	before := map[string]any{CredentialSecretVersionKey: int64(7), "usage_query_token": "query"}
	after := map[string]any{"usage_query_token": nil}
	current := map[string]any{CredentialSecretReferenceKey: "reference", CredentialSecretVersionKey: int64(8)}
	merged, ok := MergeAccountCredentialChanges(WithAccountCredentialChanges(context.Background(), before, after), current)
	require.True(t, ok)
	require.NotContains(t, merged, CredentialSecretVersionKey)
}
