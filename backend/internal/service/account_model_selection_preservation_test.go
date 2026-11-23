//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountModelSelectionPreservationDelta(t *testing.T) {
	before := map[string]any{"model_mapping": map[string]any{"old": "old"}, "model_whitelist": []any{"old"}, "compact_model_mapping": map[string]any{"old": "old"}, "site": "cn"}
	current := map[string]any{"model_mapping": map[string]any{"new": "new"}, "model_whitelist": []any{"new"}, "compact_model_mapping": map[string]any{"new": "new"}, "access_token": "refreshed", "site": "cn"}
	for _, compactEdit := range []bool{false, true} {
		after := map[string]any{"site": "intl"}
		if compactEdit {
			after["compact_model_mapping"] = map[string]any{}
		}
		ctx := WithAccountModelSelectionPreserved(context.Background(), compactEdit)
		merged, ok := MergeAccountCredentialChanges(WithAccountCredentialChanges(ctx, before, after), current)
		require.True(t, ok)
		require.Equal(t, current["model_mapping"], merged["model_mapping"])
		require.Equal(t, current["model_whitelist"], merged["model_whitelist"])
		require.Equal(t, "refreshed", merged["access_token"])
		require.Equal(t, "intl", merged["site"])
		if compactEdit {
			require.Empty(t, merged["compact_model_mapping"])
		} else {
			require.Equal(t, current["compact_model_mapping"], merged["compact_model_mapping"])
		}
	}
	// Absence of the flag retains old full-document omission/clear behavior.
	merged, _ := MergeAccountCredentialChanges(WithAccountCredentialChanges(context.Background(), before, map[string]any{"site": "intl"}), current)
	require.NotContains(t, merged, "model_mapping")
	require.NotContains(t, merged, "model_whitelist")
}
