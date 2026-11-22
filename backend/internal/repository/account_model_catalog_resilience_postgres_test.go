//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestLegacyCatalogResiliencePostgres(test *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		test.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	database := testresources.OpenSchema(test, dsn)
	ctx := context.Background()
	// Reuse the migrated test database's shape in an invocation-owned schema.
	// No migration or test fixture can modify another runner's account rows.
	// These are legacy accounts with no connection owner. An empty authority
	// table establishes that fact; a missing table must remain a hard error.
	_, err := database.ExecContext(ctx, `CREATE TABLE accounts (LIKE public.accounts INCLUDING ALL);
	CREATE TABLE provider_connections (LIKE public.provider_connections INCLUDING ALL);
	CREATE TABLE scheduler_outbox (LIKE public.scheduler_outbox INCLUDING ALL);
	CREATE TABLE account_groups (LIKE public.account_groups INCLUDING ALL);
	CREATE TABLE groups (LIKE public.groups INCLUDING ALL);
	CREATE TABLE credential_secrets (LIKE public.credential_secrets INCLUDING ALL);
	CREATE TABLE audit_logs (LIKE public.audit_logs INCLUDING ALL);
	CREATE TABLE proxies (LIKE public.proxies INCLUDING ALL);`)
	require.NoError(test, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	repository.credentialEncryptor = persistentTestAES(test, true)
	create := func(test *testing.T) *service.Account {
		row := createEncryptedCatalogFixture(test, ctx, client, database, repository, &service.Account{Name: "legacy-resilience", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "original-token", "model_mapping": map[string]string{"alias": "target"}}})
		snapshot := service.AccountModelCatalogSnapshot{LegacyAccountID: row.ID, Revision: 1, Complete: true,
			SourceKind: "legacy-mapping-upgrade", DiscoveryScope: "legacy-mapping-upgrade", ObservedAt: "2026-09-20T00:00:00Z",
			DiscoveredModels: []string{}, EligibleModels: []service.AccountCatalogModel{{ID: "target", EvidenceKind: "legacy-mapping-upgrade", EvidenceObservedAt: "2026-09-20T00:00:00Z"}}}
		encoded, err := json.Marshal(snapshot)
		require.NoError(test, err)
		_, err = database.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_build_object('upstream_model_catalog',$2::jsonb) WHERE id=$1`, row.ID, string(encoded))
		require.NoError(test, err)
		_, err = database.ExecContext(ctx, `DELETE FROM audit_logs WHERE extra->>'account_id'=$1`, strconv.FormatInt(row.ID, 10))
		require.NoError(test, err)
		account, err := repository.GetByID(ctx, row.ID)
		require.NoError(test, err)
		return account
	}
	get := func(test *testing.T, id int64) *service.Account {
		account, err := repository.GetByID(ctx, id)
		require.NoError(test, err)
		return account
	}
	for _, writer := range []string{"credentials SQL", "locked account update"} {
		test.Run(writer, func(test *testing.T) {
			account := create(test)
			oldBaseline := account.ModelCatalogBaseline()
			write := func(account *service.Account) {
				if writer == "credentials SQL" {
					require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
				} else {
					require.NoError(test, repository.Update(ctx, account))
				}
			}
			for _, token := range []string{"renewed-token", "renewed-again"} {
				require.NoError(test, repository.UpdateAccountRuntimeSnapshot(ctx, account, map[string]any{"legacy_renewal_marker": token}, nil))
				account = get(test, account.ID)
				account.Credentials["access_token"] = token
				write(account)
				account = get(test, account.ID)
				require.NotNil(test, account.EffectiveUpstreamModelCatalog())
				require.NotEmpty(test, account.GetUpstreamModelCatalog().LegacyMappingFingerprint)
				require.Zero(test, account.ModelCatalogState().LegacyPolicyRevision, "token rotation keeps legacy policy unchanged")
				require.EqualValues(test, 2, account.GetUpstreamModelCatalog().Revision, "upgrade runs once")
			}
			require.EqualValues(test, 2, account.ModelCatalogState().IdentityRevision)
			require.EqualValues(test, 2, account.ModelCatalogState().RulesRevision)
			require.ErrorIs(test, repository.CommitAccountModelCatalog(ctx, account.ID, oldBaseline, nil, service.AccountModelCatalogState{}, nil, false), service.ErrAccountModelCatalogConflict)
			account.Credentials["model_mapping"] = map[string]string{"changed-alias": "target"}
			write(account)
			account = get(test, account.ID)
			require.EqualValues(test, 1, account.ModelCatalogState().LegacyPolicyRevision)
			require.NotNil(test, account.EffectiveUpstreamModelCatalog())
			require.Equal(test, service.ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
			account.Credentials["model_mapping"] = map[string]string{"alias": "target"}
			write(account)
			account = get(test, account.ID)
			require.EqualValues(test, 2, account.ModelCatalogState().LegacyPolicyRevision)
			require.NotNil(test, account.EffectiveUpstreamModelCatalog())
			require.Equal(test, service.ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
		})
	}
	test.Run("stale old seed remains strict", func(test *testing.T) {
		account := create(test)
		_, err := database.ExecContext(ctx, `UPDATE accounts SET extra=extra||'{"upstream_model_catalog_state":{"identity_revision":1,"rules_revision":1}}'::jsonb WHERE id=$1`, account.ID)
		require.NoError(test, err)
		account.Credentials["access_token"] = "renewed"
		require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
		account = get(test, account.ID)
		require.Empty(test, account.GetUpstreamModelCatalog().LegacyMappingFingerprint)
		require.NotNil(test, account.EffectiveUpstreamModelCatalog())
		require.Equal(test, service.ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
	})
	test.Run("allowlist remains deny_all_across_discovery_auth_failure", func(test *testing.T) {
		account := create(test)
		require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
		account = get(test, account.ID)
		require.NoError(test, repository.UpdateAccountModelAllowlist(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: true, Models: []string{}}))
		account = get(test, account.ID)
		require.Zero(test, account.ModelCatalogState().LegacyPolicyRevision)
		require.NotEmpty(test, account.GetUpstreamModelCatalog().EligibleModels)
		require.Empty(test, account.EffectiveUpstreamModelCatalog().EligibleModels)
		state := account.ModelCatalogState()
		state.Result = "credentials_invalid"
		require.NoError(test, repository.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), nil, state, nil, false))
		account = get(test, account.ID)
		require.EqualValues(test, 1, account.ModelCatalogState().LegacyPolicyRevision)
		account.Credentials["access_token"] = "renewed"
		require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
		require.Empty(test, get(test, account.ID).EffectiveUpstreamModelCatalog().EligibleModels)
	})
	test.Run("empty discovery remains empty after renewal", func(test *testing.T) {
		account := create(test)
		require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
		account = get(test, account.ID)
		baseline := account.ModelCatalogBaseline()
		snapshot := &service.AccountModelCatalogSnapshot{Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, RulesRevision: baseline.RulesRevision, Complete: true, SourceKind: "native_account_catalog", DiscoveredModels: []string{}, EligibleModels: []service.AccountCatalogModel{}}
		require.NoError(test, repository.CommitAccountModelCatalog(ctx, account.ID, baseline, snapshot, account.ModelCatalogState(), nil, false))
		account.Credentials["access_token"] = "renewed"
		require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
		account = get(test, account.ID)
		require.Empty(test, account.GetUpstreamModelCatalog().EligibleModels)
		require.Empty(test, account.GetUpstreamModelCatalog().LegacyMappingFingerprint)
		require.Equal(test, "target", account.GetMappedModel("alias"), "renewal preserves the configured selection")
		require.Nil(test, account.EffectiveUpstreamModelCatalog(), "a stale native publication cannot be replaced by configured evidence")
	})
	test.Run("protocol release and bulk mapping changes revoke", func(test *testing.T) {
		for _, change := range []string{"protocol", "capability release", "bulk mapping", "bulk proxy"} {
			test.Run(change, func(test *testing.T) {
				account := create(test)
				require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
				account = get(test, account.ID)
				switch change {
				case "protocol":
					account.Credentials["access_token"] = "renewed"
					account.Credentials["protocol_rules"] = map[string]string{"protocol": "changed"}
					require.NoError(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
				case "capability release":
					require.NoError(test, repository.UpdateExtra(ctx, account.ID, map[string]any{service.ModelCapabilityReleaseExtraKey: map[string]any{"source_generation": 10}}))
				case "bulk mapping":
					_, err := repository.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"model_mapping": map[string]string{"alias": "other"}}})
					require.NoError(test, err)
				case "bulk proxy":
					proxyID := int64(77)
					_, err := repository.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{ProxyID: &proxyID})
					require.NoError(test, err)
				}
				account = get(test, account.ID)
				require.EqualValues(test, 1, account.ModelCatalogState().LegacyPolicyRevision)
				require.NotNil(test, account.EffectiveUpstreamModelCatalog())
				require.Equal(test, service.ModelEvidenceConfiguredSelection, account.EffectiveUpstreamModelCatalog().EligibleModels[0].EvidenceKind)
			})
		}
	})
	test.Run("outbox failure rolls upgrade and rotation back", func(test *testing.T) {
		account := create(test)
		_, err := database.ExecContext(ctx, `ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_legacy_outbox CHECK (false) NOT VALID`)
		require.NoError(test, err)
		account.Credentials["access_token"] = "renewed"
		require.Error(test, repository.UpdateCredentials(ctx, account.ID, account.Credentials))
		_, err = database.ExecContext(ctx, `ALTER TABLE scheduler_outbox DROP CONSTRAINT reject_legacy_outbox`)
		require.NoError(test, err)
		account = get(test, account.ID)
		require.Equal(test, "original-token", account.GetCredential("access_token"))
		require.Empty(test, account.GetUpstreamModelCatalog().LegacyMappingFingerprint)
		require.EqualValues(test, 1, account.GetUpstreamModelCatalog().Revision)
		require.Zero(test, account.ModelCatalogState().IdentityRevision)
	})
	test.Run("concurrent upgrade and empty refresh retain CAS", func(test *testing.T) {
		account := create(test)
		baseline := account.ModelCatalogBaseline()
		snapshot := &service.AccountModelCatalogSnapshot{Revision: baseline.SnapshotRevision + 1, Complete: true, SourceKind: "native_account_catalog", DiscoveredModels: []string{}, EligibleModels: []service.AccountCatalogModel{}}
		start := make(chan struct{})
		renewalResult := make(chan error, 1)
		refreshResult := make(chan error, 1)
		go func() {
			<-start
			account.Credentials["access_token"] = "renewed"
			renewalResult <- repository.UpdateCredentials(ctx, account.ID, account.Credentials)
		}()
		go func() {
			<-start
			refreshResult <- repository.CommitAccountModelCatalog(ctx, account.ID, baseline, snapshot, service.AccountModelCatalogState{}, nil, false)
		}()
		close(start)
		require.NoError(test, <-renewalResult)
		refreshError := <-refreshResult
		account = get(test, account.ID)
		if refreshError == nil {
			require.Equal(test, "native_account_catalog", account.GetUpstreamModelCatalog().SourceKind)
			require.Empty(test, account.GetUpstreamModelCatalog().EligibleModels)
		} else {
			require.ErrorIs(test, refreshError, service.ErrAccountModelCatalogConflict)
			require.NotNil(test, account.EffectiveUpstreamModelCatalog())
			require.NotEmpty(test, account.GetUpstreamModelCatalog().LegacyMappingFingerprint)
		}
	})
}
