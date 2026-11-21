package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func runtimeIdentityAccount() *service.Account {
	return &service.Account{ID: 7, Platform: "volcengine", Type: "apikey",
		Credentials: map[string]any{"api_key": "synthetic-api-secret", "account_mode": "coding"},
		Extra:       map[string]any{"volcengine_5h_used_percent": 18},
	}
}

func TestAccountRuntimeIdentityAdminProjection(t *testing.T) {
	account := runtimeIdentityAccount()
	detail := AccountFromService(account)
	compact := AccountListItemFromAccount(detail)
	require.Regexp(t, "^[a-f0-9]{64}$", detail.RuntimeIdentity)
	require.Equal(t, strings.SplitN(service.AccountRuntimeIdentityKey(account), ":", 2)[1], detail.RuntimeIdentity)
	require.Equal(t, detail.RuntimeIdentity, compact.RuntimeIdentity)
	for _, projection := range []any{detail, compact} {
		encoded, err := json.Marshal(projection)
		require.NoError(t, err)
		require.Contains(t, string(encoded), "runtime_identity")
		require.NotContains(t, string(encoded), "synthetic-api-secret")
	}
	summary, err := json.Marshal(AccountSummaryFromService(account))
	require.NoError(t, err)
	require.NotContains(t, string(summary), "runtime_identity")
	group, err := json.Marshal(GroupFromService(&service.Group{AccountGroups: []service.AccountGroup{{Account: account}}}))
	require.NoError(t, err)
	require.NotContains(t, string(group), "runtime_identity")
}

func TestAccountRuntimeIdentityIgnoresOrdinaryUpdates(t *testing.T) {
	account := runtimeIdentityAccount()
	before := AccountFromServiceShallow(account).RuntimeIdentity
	note := "updated note"
	account.Notes = &note
	account.Name = "new name"
	account.UpdatedAt = time.Now()
	account.Extra["volcengine_usage_updated_at"] = "2099-10-04T10:01:00Z"
	account.Extra["volcengine_5h_used_percent"] = 42
	account.Extra["total_spend"] = 100
	multiplier := 2.0
	account.RateMultiplier = &multiplier
	require.Equal(t, before, AccountFromServiceShallow(account).RuntimeIdentity)
}

func TestAccountRuntimeIdentityChangesWithCredentials(t *testing.T) {
	for _, field := range []string{"api_key", "volcengine_access_key_id", "volcengine_secret_access_key", "volcengine_plan_edition", "volcengine_seat_id", "base_url"} {
		t.Run(field, func(t *testing.T) {
			account := runtimeIdentityAccount()
			account.Credentials[field] = "synthetic-original"
			before := AccountFromServiceShallow(account).RuntimeIdentity
			account.Credentials[field] = "synthetic-replacement"
			after := AccountFromServiceShallow(account).RuntimeIdentity
			require.NotEqual(t, before, after)
			require.Regexp(t, "^[a-f0-9]{64}$", after)
		})
	}
}

func TestAccountRuntimeIdentityRecognizesRedactedRotation(t *testing.T) {
	account := runtimeIdentityAccount()
	before := AccountFromServiceShallow(account)
	account.Credentials["api_key"] = "synthetic-rotated-secret"
	after := AccountFromServiceShallow(account)
	require.Equal(t, before.Credentials, after.Credentials)
	require.Equal(t, before.CredentialsStatus, after.CredentialsStatus)
	require.NotEqual(t, before.RuntimeIdentity, after.RuntimeIdentity)

	account.Credentials[service.CredentialSecretReferenceKey] = "synthetic-reference"
	account.Credentials[service.CredentialSecretVersionKey] = int64(1)
	before = AccountFromServiceShallow(account)
	account.Credentials[service.CredentialSecretVersionKey] = int64(2)
	after = AccountFromServiceShallow(account)
	require.NotEqual(t, before.RuntimeIdentity, after.RuntimeIdentity)
	require.Regexp(t, "^[a-f0-9]{64}$", after.RuntimeIdentity)
	require.NotContains(t, after.RuntimeIdentity, "synthetic-reference")
}
