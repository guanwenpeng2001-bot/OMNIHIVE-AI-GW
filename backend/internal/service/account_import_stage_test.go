package service

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagedProxyDependencyFailsClosed(t *testing.T) {
	for _, dependency := range []string{"required", "unknown"} {
		a := &Account{Extra: map[string]any{StagedProxyDependencyExtraKey: dependency}}
		_, err := a.EgressProxyURL(context.Background())
		require.ErrorIs(t, err, ErrStagedProxyPending)
		_, _, err = (&ProxyGroupRuntime{}).ApplyEgress(context.Background(), a)
		require.ErrorIs(t, err, ErrStagedProxyPending)
		id := int64(7)
		a.ProxyID = &id
		_, err = a.EgressProxyURL(context.Background())
		require.ErrorIs(t, err, ErrStagedProxyPending, "an unloaded proxy is not direct")
		a.Proxy = &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8080}
		proxyURL, err := a.EgressProxyURL(context.Background())
		require.NoError(t, err)
		require.Equal(t, "http://127.0.0.1:8080", proxyURL)
	}
	for _, extra := range []map[string]any{nil, {StagedProxyDependencyExtraKey: "none"}} {
		url, err := (&Account{Extra: extra}).EgressProxyURL(context.Background())
		require.NoError(t, err)
		require.Empty(t, url, "legacy and explicitly direct behavior is unchanged")
	}
}

func TestStagedImportCredentialBoundaryAndAudit(t *testing.T) {
	const secret = "test-only-secret-not-for-production"
	for _, field := range []string{CredentialSecretReferenceKey, CredentialSecretVersionKey, "credential_ref", "credential_version", "master_key", "TOTP_ENCRYPTION_KEY", "proxy_key", "proxy_id", "group_ids", "header_overrides", "cookie", "sso", "password", "unrecognized"} {
		err := ValidateStagedImportCredentials(map[string]any{"access_token": secret, field: secret})
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
	}
	require.NoError(t, ValidateStagedImportCredentials(map[string]any{"access_token": secret, "refresh_token": secret, "expires_at": "100", "base_url": "https://example.invalid/v1"}))
	credentialURL := url.URL{Scheme: "https", Host: "example.invalid", User: url.UserPassword("test-user", "fixture")}
	for _, base := range []string{credentialURL.String(), "https://example.invalid?key=secret", "https://example.invalid#secret"} {
		require.Error(t, ValidateStagedImportCredentials(map[string]any{"api_key": secret, "base_url": base}))
	}
	for _, key := range []string{"stage_inactive", "STAGE_INACTIVE"} {
		body, err := json.Marshal(map[string]any{key: true, "ordinary_field": "audit-visible", "credentials": map[string]any{"api_key": secret}})
		require.NoError(t, err)
		redacted := RedactAuditBody(body, "application/json")
		require.NotContains(t, redacted, secret)
		var visible map[string]any
		require.NoError(t, json.Unmarshal([]byte(redacted), &visible), "a request field must not suppress generic auditing")
		require.Equal(t, true, visible[key])
		require.Equal(t, "audit-visible", visible["ordinary_field"])
	}
}

func TestStagedProxyMetadataSurvivesOrdinaryEdits(t *testing.T) {
	const id = int64(184)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		id: {ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: "inactive", Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{StagedAccountImportExtraKey: true, StagedProxyDependencyExtraKey: "required"}},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.UpdateAccount(context.Background(), id, &UpdateAccountInput{Extra: map[string]any{StagedProxyDependencyExtraKey: "none"}})
	require.NoError(t, err)
	require.Equal(t, "required", repo.accounts[id].Extra[StagedProxyDependencyExtraKey])
	err = svc.UpdateAccountExtra(context.Background(), id, map[string]any{StagedProxyDependencyExtraKey: "none", "custom": "value"})
	require.NoError(t, err)
	require.Equal(t, "required", repo.accounts[id].Extra[StagedProxyDependencyExtraKey])
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{id}, Extra: map[string]any{StagedProxyDependencyExtraKey: "none", "custom": "value"}})
	require.NoError(t, err)
	require.Len(t, repo.bulkUpdates, 1)
	require.NotContains(t, repo.bulkUpdates[0].Extra, StagedProxyDependencyExtraKey)
}
