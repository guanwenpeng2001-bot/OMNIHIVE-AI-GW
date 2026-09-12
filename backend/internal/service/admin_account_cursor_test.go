//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorAccountNativeIdentityAndConcurrency(t *testing.T) {
	account := &Account{Platform: PlatformCursor, Type: AccountTypeAPIKey, Concurrency: 10, Credentials: map[string]any{"api_key": "crsr_test"}}
	require.NoError(t, NormalizeCursorAccount(account))
	require.Equal(t, 10, account.Concurrency)
	machineID := account.GetCredential("machine_id")
	require.Len(t, machineID, 64)
	require.NotEqual(t, machineID, account.GetCredential("mac_machine_id"))
	require.NoError(t, NormalizeCursorAccount(account))
	require.Equal(t, machineID, account.GetCredential("machine_id"))
	account.Credentials["machine_id"] = "invalid"
	require.Error(t, NormalizeCursorAccount(account))
}

func TestCursorAccountRelayAndInvalidModes(t *testing.T) {
	account := &Account{Platform: PlatformCursor, Type: AccountTypeUpstream, Concurrency: 10, Credentials: map[string]any{"api_key": "relay-key", "base_url": "https://relay.example/v1"}}
	require.NoError(t, NormalizeCursorAccount(account))
	require.Equal(t, 10, account.Concurrency)
	require.Equal(t, "adaptive", account.GetCredential("api_protocol"))
	for _, endpoint := range []string{"", "file:///tmp/key", "https://user:pass@relay.example", "https://relay.example?key=secret"} {
		account.Credentials["base_url"] = endpoint
		require.Error(t, NormalizeCursorAccount(account))
	}
	account.Type = AccountTypeOAuth
	require.Error(t, NormalizeCursorAccount(account))
	require.Equal(t, 99, normalizeAccountConcurrency(PlatformCursor, AccountTypeAPIKey, 99))
	require.Equal(t, 99, normalizeAccountConcurrency(PlatformCursor, AccountTypeUpstream, 99))
}
