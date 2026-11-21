package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func marshalForTest(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func TestConfigurationDiffRecordsFieldsWithoutValues(t *testing.T) {
	before := map[string]any{
		"refresh_token": "old-secret-value",
		"base_url":      "https://old.example.invalid",
		"removed_key":   "gone",
	}
	after := map[string]any{
		"refresh_token": "new-secret-value",
		"base_url":      "https://old.example.invalid",
		"added_key":     "brand-new",
	}
	diff := ConfigurationDiff(before, after)
	require.Len(t, diff, 3)
	byField := map[string]ConfigurationFieldDiff{}
	for _, change := range diff {
		byField[change.Field] = change
	}
	require.Equal(t, "replace", byField["refresh_token"].Operation)
	require.Equal(t, "add", byField["added_key"].Operation)
	require.Equal(t, "remove", byField["removed_key"].Operation)

	encoded := marshalForTest(t, diff)
	for _, plaintext := range []string{"old-secret-value", "new-secret-value", "https://old.example.invalid"} {
		require.NotContains(t, string(encoded), plaintext, "audit diff must never record values")
	}
	require.Equal(t, "changed", byField["refresh_token"].Before)
	require.Equal(t, "changed", byField["refresh_token"].After)
	require.Nil(t, byField["added_key"].Before)
	require.Equal(t, "brand-new", byField["added_key"].After)
	require.Equal(t, "gone", byField["removed_key"].Before)
	require.Nil(t, byField["removed_key"].After)

	// Unchanged fields are not diff entries at all.
	_, tracked := byField["base_url"]
	require.False(t, tracked)
}

func TestConfigurationDiffIsDeterministic(t *testing.T) {
	before := map[string]any{"b": 1, "a": 1}
	after := map[string]any{"b": 2, "a": 2, "c": 3}
	first := ConfigurationDiff(before, after)
	second := ConfigurationDiff(before, after)
	require.Equal(t, first, second)
	fields := []string{first[0].Field, first[1].Field, first[2].Field}
	require.Equal(t, []string{"a", "b", "c"}, fields)
}

func TestSubmittedConfigurationDiffMarksSubmittedAndRedacts(t *testing.T) {
	raw := []byte(`{"credentials":{"refresh_token":"submitted-secret"},"name":"renamed"}`)
	diff := SubmittedConfigurationDiff(raw)
	require.NotEmpty(t, diff)
	for _, change := range diff {
		require.Equal(t, "submitted", change.Operation)
	}
	encoded := marshalForTest(t, diff)
	require.NotContains(t, string(encoded), "submitted-secret")
	require.True(t, strings.Contains(string(encoded), "credentials"))
}

func TestSubmittedConfigurationDiffSkipsInvalidAndOversize(t *testing.T) {
	require.Nil(t, SubmittedConfigurationDiff([]byte("not-json")))
	oversize := make([]byte, AuditRequestBodyCaptureLimit+1)
	for i := range oversize {
		oversize[i] = 'a'
	}
	require.Nil(t, SubmittedConfigurationDiff(oversize))
	require.Nil(t, SubmittedConfigurationDiff(nil))
}
