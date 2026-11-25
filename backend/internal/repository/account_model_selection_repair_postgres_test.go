//go:build unit || integration

package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAccountModelSelectionRepairPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	migrated, err := testresources.OpenPostgres(context.Background(), dsn, "job_test")
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(context.Background(), migrated))
	require.NoError(t, migrated.Close())
	db := testresources.OpenSchema(t, dsn)
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts (LIKE public.accounts INCLUDING ALL);
		CREATE TABLE scheduler_outbox (LIKE public.scheduler_outbox INCLUDING ALL);
		CREATE TABLE provider_connections (LIKE public.provider_connections INCLUDING ALL)`)
	require.NoError(t, err)
	insert := func(scope map[string]any, frozen any, mapping any) int64 {
		t.Helper()
		extra := map[string]any{"upstream_model_allowlist": scope, "upstream_model_catalog_state": map[string]any{"identity_revision": 4, "rules_revision": 7}}
		if frozen != nil {
			extra["model_selection_baseline_frozen"] = frozen
		}
		extraJSON, err := json.Marshal(extra)
		require.NoError(t, err)
		credentialsJSON, err := json.Marshal(map[string]any{
			"model_mapping": mapping, "sub": "fixture-subject", "access_token": "fixture-token",
			"_token_version": 15, "credential_secret_version": 7,
		})
		require.NoError(t, err)
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable,credentials,extra) VALUES(1,'repair','openai','apikey','active',true,$1,$2) RETURNING id`, string(credentialsJSON), string(extraJSON)).Scan(&id))
		return id
	}
	withMapping := insert(map[string]any{"enabled": true, "models": []string{}, "reason": "system_upgrade_baseline_freeze"}, nil, map[string]string{"b": "target-b", "a": "target-a"})
	withoutMapping := insert(map[string]any{"enabled": true, "models": []string{}}, true, map[string]string{})
	admin := insert(map[string]any{"enabled": true, "models": []string{}, "reason": "maintenance", "confirmed_by": 1}, true, map[string]string{"a": "target-a"})
	invalid := insert(map[string]any{"enabled": true, "models": []string{}, "reason": "system_upgrade_baseline_freeze"}, nil, map[string]string{"bad": "bad*"})
	managed := insert(map[string]any{"enabled": true, "models": []string{}}, true, map[string]string{"target": "target"})
	mismatched := insert(map[string]any{"enabled": true, "models": []string{}}, true, map[string]string{"legacy-target": "legacy-target"})
	_, err = db.ExecContext(ctx, `INSERT INTO provider_connections (account_id,workspace_id,provider,auth_type,name,concurrency,state,model_ids) VALUES
		($1,1,'openai','api_key','consistent',1,'active','{target}'), ($2,1,'openai','api_key','mismatch',1,'active','{other-target}')`, managed, mismatched)
	require.NoError(t, err)
	require.NoError(t, RepairAccountModelSelections(ctx, db))
	read := func(id int64) (service.AccountModelAllowlist, map[string]any, int) {
		t.Helper()
		var raw []byte
		require.NoError(t, db.QueryRowContext(ctx, `SELECT extra FROM accounts WHERE id=$1`, id).Scan(&raw))
		var extra map[string]any
		require.NoError(t, json.Unmarshal(raw, &extra))
		encoded, _ := json.Marshal(extra[service.UpstreamModelAllowlistExtraKey])
		var scope service.AccountModelAllowlist
		require.NoError(t, json.Unmarshal(encoded, &scope))
		var outbox int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, id).Scan(&outbox))
		return scope, extra, outbox
	}
	scope, extra, outbox := read(withMapping)
	require.Equal(t, []string{"target-a", "target-b"}, scope.Models)
	require.Equal(t, modelSelectionRepairReason, scope.Reason)
	require.NotContains(t, extra, service.ModelSelectionBaselineFrozenExtraKey)
	require.Contains(t, extra, service.ModelSelectionRepairBackupExtraKey)
	require.Equal(t, 1, outbox)
	require.EqualValues(t, 4, (&service.Account{Extra: extra}).ModelCatalogState().IdentityRevision)
	var credentialsUnchanged bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT credentials = '{
		"model_mapping":{"b":"target-b","a":"target-a"}, "sub":"fixture-subject", "access_token":"fixture-token",
		"_token_version":15, "credential_secret_version":7
	}'::jsonb FROM accounts WHERE id=$1`, withMapping).Scan(&credentialsUnchanged))
	require.True(t, credentialsUnchanged)
	scope, _, _ = read(withoutMapping)
	require.False(t, scope.Enabled)
	require.Empty(t, scope.Models)
	scope, adminExtra, adminOutbox := read(admin)
	require.True(t, scope.Enabled)
	require.Empty(t, scope.Models)
	require.NotContains(t, adminExtra, service.ModelSelectionRepairBackupExtraKey)
	require.Zero(t, adminOutbox)
	_, invalidExtra, _ := read(invalid)
	require.NotContains(t, invalidExtra, service.ModelSelectionRepairBackupExtraKey)
	scope, _, outbox = read(managed)
	require.Equal(t, []string{"target"}, scope.Models)
	require.Equal(t, 1, outbox)
	scope, extra, outbox = read(mismatched)
	require.True(t, scope.Enabled)
	require.Empty(t, scope.Models)
	require.NotContains(t, extra, service.ModelSelectionRepairBackupExtraKey)
	require.Zero(t, outbox)
	var beforeUpdated string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT updated_at::text FROM accounts WHERE id=$1`, withMapping).Scan(&beforeUpdated))
	require.NoError(t, RepairAccountModelSelections(ctx, db))
	_, _, outbox = read(withMapping)
	require.Equal(t, 1, outbox)
	var afterUpdated string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT updated_at::text FROM accounts WHERE id=$1`, withMapping).Scan(&afterUpdated))
	require.Equal(t, beforeUpdated, afterUpdated)
	require.NoError(t, RollbackAccountModelSelections(ctx, db))
	scope, extra, outbox = read(withMapping)
	require.True(t, scope.Enabled)
	require.Empty(t, scope.Models)
	require.Equal(t, "system_upgrade_baseline_freeze", scope.Reason)
	backup := extra[service.ModelSelectionRepairBackupExtraKey].(map[string]any)
	require.NotEmpty(t, backup["rolled_back_at"])
	require.Equal(t, 2, outbox)
	require.NoError(t, RollbackAccountModelSelections(ctx, db))
	_, _, outbox = read(withMapping)
	require.Equal(t, 2, outbox)
}

func TestAccountModelSelectionRepairRollbackSkipsBusinessEditPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	migrated, err := testresources.OpenPostgres(context.Background(), dsn, "job_test")
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(context.Background(), migrated))
	require.NoError(t, migrated.Close())
	db := testresources.OpenSchema(t, dsn)
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts (LIKE public.accounts INCLUDING ALL);
		CREATE TABLE scheduler_outbox (LIKE public.scheduler_outbox INCLUDING ALL);
		CREATE TABLE provider_connections (LIKE public.provider_connections INCLUDING ALL)`)
	require.NoError(t, err)
	var id int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable,extra) VALUES(1,'repair','openai','apikey','active',true,'{"upstream_model_allowlist":{"enabled":true,"models":[]},"model_selection_baseline_frozen":true}') RETURNING id`).Scan(&id))
	require.NoError(t, RepairAccountModelSelections(ctx, db))
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{upstream_model_allowlist}','{"enabled":true,"models":["business-edit"],"reason":"operator"}') WHERE id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, RollbackAccountModelSelections(ctx, db))
	var models []string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(extra->'upstream_model_allowlist'->'models')) FROM accounts WHERE id=$1`, id).Scan(pq.Array(&models)))
	require.Equal(t, []string{"business-edit"}, models)
}
