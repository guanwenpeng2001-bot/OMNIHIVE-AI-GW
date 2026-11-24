//go:build unit

package repository

import (
	"encoding/json"
	"maps"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestWorkBuddyCatalogProjectionAndRotationPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated job_test")
	}
	db := testresources.OpenSchema(t, dsn)
	ctx := domain.WithWorkspaceScope(t.Context(), 1)
	_, err := db.ExecContext(ctx, `
 CREATE TABLE accounts(id bigint PRIMARY KEY,workspace_id bigint DEFAULT 1,platform text,type text,
 status text DEFAULT 'active',schedulable boolean DEFAULT true,parent_account_id bigint,expires_at timestamptz,
 auto_pause_on_expired boolean DEFAULT false,temp_unschedulable_until timestamptz,proxy_id bigint,proxy_group_id bigint,
 deleted_at timestamptz,credentials jsonb,extra jsonb);
 CREATE TABLE groups(id bigint,platform text,account_type text);
 CREATE TABLE account_groups(account_id bigint,group_id bigint);
 INSERT INTO accounts(id,platform,type,credentials,extra) VALUES(1,'workbuddy','oauth',
 '{"site":"global","uid":"user","api_key":"private","access_token":"old","refresh_token":"old-refresh","credential_secret_version":1}',
 '{"upstream_model_catalog":{"complete":true,"discovered_models":["default-model"]},"workbuddy_usage_snapshot":{"base_total":25}}');
 `)
	require.NoError(t, err)
	repo := &accountRepository{sql: db}
	accounts, err := repo.ListModelCatalogAccounts(ctx, nil, []string{service.PlatformWorkBuddy}, true)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, map[string]any{"site": "global"}, accounts[0].Credentials)
	enricher, err := service.NewCapabilityReadOnlyEnricher(nil, nil)
	require.NoError(t, err)
	require.Equal(t, "workbuddy-global", enricher.Project(&accounts[0], false)["default-model"].ProviderProfile)

	for _, tc := range []struct {
		field  string
		manual bool
		want   int64
	}{
		{"", false, 0}, {"", true, 1}, {"site", false, 1}, {"domain", false, 1}, {"enterprise_id", false, 1}, {"uid", false, 1},
	} {
		patch := map[string]any{"credential_secret_version": 2, "access_token": "rotated", "refresh_token": "renewed"}
		if tc.field != "" {
			patch[tc.field] = "changed"
		}
		body, err := json.Marshal(patch)
		require.NoError(t, err)
		incoming := "(credentials || $1::jsonb)"
		expr := accountCatalogRevisionPatchSQL(incoming, "", "", tc.manual)
		cleared := clearChangedAccountRuntimeObservationsSQL(incoming, "extra", "")
		var revision int64
		var retained bool
		err = db.QueryRowContext(ctx, "SELECT ("+expr+")->'upstream_model_catalog_state'->>'identity_revision', ("+cleared+") ? 'workbuddy_usage_snapshot' FROM accounts WHERE id=1", string(body)).Scan(&revision, &retained)
		require.NoError(t, err)
		require.Equal(t, tc.want, revision, tc.field)
		require.False(t, retained)
	}
}

func TestWorkBuddyReferencedRefreshPreservesPublication(t *testing.T) {
	f := newCredentialSecretFixture(t)
	repo := f.accounts
	current := f.createAccount(t, f.ctx, uniqueName("workbuddy-refresh"), service.PlatformWorkBuddy, service.AccountTypeOAuth, map[string]any{"site": "cn", "uid": "user", "enterprise_id": "first", "access_token": "old", "refresh_token": "refresh", "model_mapping": map[string]string{"alias": "auto"}})
	read := func() *service.Account {
		a, err := repo.GetByID(f.ctx, current.ID)
		require.NoError(t, err)
		return a
	}
	current = read()
	baseline := current.ModelCatalogBaseline()
	snapshot := &service.AccountModelCatalogSnapshot{Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, Complete: true, SourceKind: "live", DiscoveredModels: []string{"auto"}, EligibleModels: []service.AccountCatalogModel{{ID: "auto"}}}
	require.NoError(t, repo.CommitAccountModelCatalog(f.ctx, current.ID, baseline, snapshot, current.ModelCatalogState(), nil, false))
	current = read()
	observation := map[string]any{service.WorkBuddyUsageSnapshotExtraKey: map[string]any{"base_total": 20}}
	require.NoError(t, repo.UpdateAccountRuntimeSnapshot(f.ctx, current, observation, nil))
	creds := maps.Clone(current.Credentials)
	creds["access_token"], creds["refresh_token"] = "rotated", "renewed"
	require.NoError(t, repo.UpdateAccountRuntimeCredentials(f.ctx, current, creds))
	rotated := read()
	require.Greater(t, service.CredentialVersion(rotated.Credentials), service.CredentialVersion(current.Credentials))
	require.Equal(t, baseline.IdentityRevision, rotated.ModelCatalogState().IdentityRevision)
	require.NotNil(t, rotated.EffectiveUpstreamModelCatalog())
	require.Equal(t, "live", rotated.EffectiveUpstreamModelCatalog().SourceKind)
	require.NotEmpty(t, rotated.EffectiveUpstreamModelCatalog().EligibleModels)
	require.NoError(t, repo.UpdateAccountRuntimeSnapshot(f.ctx, rotated, observation, nil))
	creds = maps.Clone(rotated.Credentials)
	creds["enterprise_id"] = "second"
	require.NoError(t, repo.UpdateAccountRuntimeCredentials(f.ctx, rotated, creds))
	changed := read()
	require.Nil(t, changed.EffectiveUpstreamModelCatalog())
	require.NotContains(t, changed.Extra, service.WorkBuddyUsageSnapshotExtraKey)
	require.ErrorIs(t, repo.UpdateAccountRuntimeSnapshot(f.ctx, rotated, observation, nil), service.ErrAccountRuntimeIdentityChanged)
}

func TestAntigravityReferencedRefreshPreservesPublication(t *testing.T) {
	f := newCredentialSecretFixture(t)
	repo := f.accounts
	current := f.createAccount(t, f.ctx, uniqueName("antigravity-refresh"), service.PlatformAntigravity, service.AccountTypeOAuth, map[string]any{"project_id": "stable-project", "access_token": "old", "refresh_token": "refresh"})
	read := func() *service.Account {
		account, err := repo.GetByID(f.ctx, current.ID)
		require.NoError(t, err)
		return account
	}
	current = read()
	baseline := current.ModelCatalogBaseline()
	snapshot := &service.AccountModelCatalogSnapshot{Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, Complete: true, SourceKind: "live", DiscoveredModels: []string{"native"}, EligibleModels: []service.AccountCatalogModel{{ID: "native"}}}
	require.NoError(t, repo.CommitAccountModelCatalog(f.ctx, current.ID, baseline, snapshot, current.ModelCatalogState(), nil, false))
	current = read()
	credentials := maps.Clone(current.Credentials)
	credentials["access_token"], credentials["refresh_token"] = "rotated", "renewed"
	require.NoError(t, repo.UpdateAccountRuntimeCredentials(f.ctx, current, credentials))
	rotated := read()
	require.Greater(t, service.CredentialVersion(rotated.Credentials), service.CredentialVersion(current.Credentials))
	require.Equal(t, current.ModelCatalogBaseline().IdentityRevision, rotated.ModelCatalogBaseline().IdentityRevision)
	require.NotNil(t, rotated.EffectiveUpstreamModelCatalog())
	require.NoError(t, repo.UpdateAccountRuntimeSnapshot(f.ctx, rotated, map[string]any{"privacy_mode": service.AntigravityPrivacySet}, nil))
	require.ErrorIs(t, repo.UpdateAccountRuntimeSnapshot(f.ctx, current, map[string]any{"privacy_mode": service.AntigravityPrivacySet}, nil), service.ErrAccountRuntimeIdentityChanged)

	credentials = maps.Clone(rotated.Credentials)
	credentials["project_id"] = "different-project"
	require.NoError(t, repo.UpdateAccountRuntimeCredentials(f.ctx, rotated, credentials))
	changed := read()
	require.Nil(t, changed.EffectiveUpstreamModelCatalog())
	require.ErrorIs(t, repo.UpdateAccountRuntimeSnapshot(f.ctx, rotated, map[string]any{"privacy_mode": service.AntigravityPrivacySet}, nil), service.ErrAccountRuntimeIdentityChanged)
}
