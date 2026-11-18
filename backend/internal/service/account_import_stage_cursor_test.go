package service

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagedCursorMachineIdentityValidation(t *testing.T) {
	credentials := map[string]any{
		"access_token": "synthetic-access", "refresh_token": "synthetic-refresh",
		"machine_id": strings.Repeat("ab", 32), "mac_machine_id": strings.Repeat("CD", 32),
		"model_mapping": map[string]any{"alias": "model"},
	}
	require.NoError(t, validateStagedAccountCredentials(PlatformCursor, AccountTypeOAuth, credentials))
	for _, field := range []string{"access_token", "refresh_token", "machine_id", "mac_machine_id"} {
		missing := maps.Clone(credentials)
		delete(missing, field)
		require.Error(t, validateStagedAccountCredentials(PlatformCursor, AccountTypeOAuth, missing))
	}
	for _, field := range []string{"machine_id", "mac_machine_id"} {
		for index, value := range []any{nil, true, 64, map[string]any{"id": "value"}, "", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("g", 64), " " + strings.Repeat("a", 63), strings.Repeat("a", 63) + "\n"} {
			t.Run(fmt.Sprintf("%s/%d", field, index), func(t *testing.T) {
				invalid := maps.Clone(credentials)
				invalid[field] = value
				err := validateStagedAccountCredentials(PlatformCursor, AccountTypeOAuth, invalid)
				require.Error(t, err)
				require.NotContains(t, err.Error(), "synthetic-access")
			})
		}
	}
	for _, field := range []string{"cached_access_token", "cached_expires_at"} {
		invalid := maps.Clone(credentials)
		invalid[field] = "synthetic-cache"
		require.Error(t, validateStagedAccountCredentials(PlatformCursor, AccountTypeOAuth, invalid))
	}
	require.Error(t, validateStagedAccountCredentials(PlatformCursor, AccountTypeUpstream, credentials))
	require.Error(t, validateStagedAccountCredentials(PlatformOpenAI, AccountTypeOAuth, credentials))
}

func TestStagedCursorMachineIDsDoNotBypassDeduplication(t *testing.T) {
	credentials := map[string]any{"api_key": "synthetic-key"}
	original, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeAPIKey, credentials)
	require.NoError(t, err)
	credentials["machine_id"], credentials["mac_machine_id"] = strings.Repeat("01", 32), strings.Repeat("23", 32)
	withIDs, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeAPIKey, credentials)
	require.NoError(t, err)
	require.Equal(t, original, withIDs)
	credentials["machine_id"] = strings.Repeat("45", 32)
	changedIDs, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeAPIKey, credentials)
	require.NoError(t, err)
	require.Equal(t, original, changedIDs)
}

func TestStagedCursorRejectsIneffectiveFingerprintSelectors(t *testing.T) {
	for _, field := range append(append([]string{}, stagedIdentityFields...), "base_url", "api_base_urls", "api_protocol") {
		credentials := map[string]any{"api_key": "synthetic-key", field: "unused-selector"}
		switch field {
		case "base_url":
			credentials[field] = "https://unused.example.invalid"
		case "api_base_urls":
			credentials[field] = map[string]any{"chat_completions": "https://unused.example.invalid"}
		case "api_protocol":
			credentials[field] = "chat_completions"
		}
		_, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeAPIKey, credentials)
		require.Error(t, err, field)
	}
}

func TestStagedCursorRejectsUnusedAuthenticationFields(t *testing.T) {
	for _, field := range SensitiveCredentialKeys {
		if field == "api_key" {
			continue
		}
		credentials := map[string]any{"api_key": "same-native-key", field: "synthetic-unused-auth"}
		_, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeAPIKey, credentials)
		require.Error(t, err, field)
	}
	for _, field := range []string{"api_key", "id_token", "session_key"} {
		credentials := map[string]any{"access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "machine_id": strings.Repeat("ab", 32), "mac_machine_id": strings.Repeat("cd", 32), field: "synthetic-unused-auth"}
		_, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeOAuth, credentials)
		require.Error(t, err, field)
	}
}

func TestStagedCursorRelayFingerprintUsesActualEndpoints(t *testing.T) {
	base := map[string]any{"api_key": "same-relay-key", "base_url": "https://root.example.invalid", "api_protocol": "adaptive", "api_base_urls": map[string]any{"chat_completions": "https://chat.example.invalid", "anthropic": "https://messages.example.invalid", "responses": "https://responses.example.invalid"}}
	fingerprint := func(credentials map[string]any) [32]byte {
		digest, err := StagedAccountImportFingerprint(PlatformCursor, AccountTypeUpstream, credentials)
		require.NoError(t, err)
		return digest
	}
	original := fingerprint(base)
	copy := maps.Clone(base)
	copy["base_url"] = "https://unused-root.example.invalid"
	require.Equal(t, original, fingerprint(copy), "fully overridden base does not create a distinct endpoint")
	copy["api_base_urls"] = map[string]any{"chat_completions": "HTTPS://CHAT.EXAMPLE.INVALID:443/v1/", "anthropic": "https://messages.example.invalid/v1", "responses": "https://responses.example.invalid/v1/"}
	require.Equal(t, original, fingerprint(copy))
	copy["api_base_urls"] = map[string]any{"chat_completions": "https://different.example.invalid", "anthropic": "https://messages.example.invalid", "responses": "https://responses.example.invalid"}
	require.NotEqual(t, original, fingerprint(copy))
	chatOnly := maps.Clone(base)
	chatOnly["api_protocol"] = "chat_completions"
	chatIdentity := fingerprint(chatOnly)
	chatOnly["api_base_urls"] = map[string]any{"chat_completions": "https://chat.example.invalid/v1", "anthropic": "https://unused.example.invalid", "responses": "https://unused.example.invalid"}
	require.Equal(t, chatIdentity, fingerprint(chatOnly), "unused overrides cannot bypass duplicate detection")
}
