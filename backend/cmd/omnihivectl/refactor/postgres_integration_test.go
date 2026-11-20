//go:build integration

package refactor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/migration"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore/testfixture"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

const testDSNEnv = "MIGRATION_INTEGRATION_TEST_DSN"

func TestPostgresInventoryAndCheckpoints(t *testing.T) {
	app, appCfg := testfixture.Database(t, testDSNEnv, "job_test")
	journal, journalCfg := testfixture.Database(t, testDSNEnv, "job_test")
	ctx := t.Context()
	var serverVersion int
	require.NoError(t, app.QueryRow("SHOW server_version_num").Scan(&serverVersion))
	require.Equal(t, 16, serverVersion/10000, "use a disposable PostgreSQL 16 container")
	t.Setenv("OMNIHIVE_REFACTOR_DSN", appCfg.Database.DSN())
	t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", "")

	applySQLFile(t, app, "001_init.sql")
	execSQL(t, app, `
INSERT INTO users (email,password_hash,balance) VALUES ('sentinel_email@example.invalid','sentinel_password',5);
INSERT INTO accounts (name,platform,type,credentials)
VALUES ('sentinel_account','openai','apikey','{"api_key":"sentinel_inline"}');
INSERT INTO api_keys (user_id,key,name) VALUES (1,'sentinel_api_key','sentinel_key_name');
`)
	oldInventory := cliReport(t, "legacy-inventory", "inventory")
	require.Equal(t, "observed", oldInventory.Checkpoint.State)
	require.NotNil(t, oldInventory.RunID)
	require.Equal(t, int64(1), *oldInventory.Counts["api_keys"].Rows)
	require.Equal(t, int64(1), *oldInventory.Counts["legacy_wallet_accounts"].Rows)
	require.Nil(t, oldInventory.Counts["projects"].Rows)
	require.Nil(t, oldInventory.Counts["restore_operations"].Rows)
	require.Nil(t, oldInventory.Counts["billing_accounts"].Rows)
	oldStatus := cliReport(t, "legacy-status", "status")
	require.Nil(t, oldStatus.RunID)
	require.Equal(t, "unknown", oldStatus.Checkpoint.State)
	require.Nil(t, oldStatus.Unmapped["open_mapping_exceptions"])

	// Apply the expand file twice on seeded data and verify both report
	// coverage and the original credential, wallet, and account values.
	for range 2 {
		applySQLFile(t, app, "312_refactor_checkpoints.sql")
	}
	status := cliReport(t, "expanded-status", "status")
	require.Equal(t, "not_started", status.Checkpoint.State)
	require.Equal(t, int64(0), *status.Unmapped["open_mapping_exceptions"])
	require.Equal(t, oldInventory.Counts, status.Counts)
	var intact bool
	require.NoError(t, app.QueryRow(`
SELECT (SELECT key = 'sentinel_api_key' FROM api_keys WHERE id = 1)
   AND (SELECT balance = 5 AND password_hash = 'sentinel_password' FROM users WHERE id = 1)
   AND (SELECT credentials->>'api_key' = 'sentinel_inline' FROM accounts WHERE id = 1)
`).Scan(&intact))
	require.True(t, intact)
	require.NoError(t, repository.ApplyMigrations(ctx, app))

	execSQL(t, app, `
UPDATE workspaces SET scope_version = 9 WHERE id = 1;
UPDATE projects SET version = 3 WHERE workspace_id = 1;
INSERT INTO credential_secrets (reference,account_id,version,ciphertext)
VALUES ('sentinel_reference',1,4,'sentinel_ciphertext');
INSERT INTO provider_connections (account_id,workspace_id,provider,auth_type,name,endpoint,concurrency,state,version)
VALUES (1,1,'openai','apikey','sentinel_connection','https://sentinel_endpoint.invalid',1,'draft',12);
INSERT INTO upstream_jobs (public_request_id,request_fingerprint,submission_key,platform,family,
    account_id,user_id,api_key_id,expires_at,billing_snapshot,version)
VALUES ('sentinel_job','sentinel_fingerprint','sentinel_submission','openai','batch',
    1,1,1,now() + interval '1 day','{}',9223372036854775807);
INSERT INTO batch_image_jobs (batch_id,user_id,provider,model,item_count,version)
VALUES ('sentinel_image_job',1,'openai','sentinel_model',1,0);
INSERT INTO identity_scope_quarantine (kind,resource_id,reason)
VALUES ('account',1,'sentinel_identity_reason');
INSERT INTO connection_migration_quarantine (account_id,reason,reviewed_revision)
SELECT id,'sentinel_connection_reason',updated_at FROM accounts WHERE id = 1;
`)
	restoreDDL, err := migrations.FS.ReadFile("298_restore_journal.sql")
	require.NoError(t, err)
	tx, err := journal.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`SET LOCAL omnihive.restore_journal = 'on'`)
	require.NoError(t, err)
	_, err = tx.Exec(string(restoreDDL))
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	execSQL(t, journal, `
INSERT INTO restore_journal.targets VALUES ('isolated','sentinel_target');
INSERT INTO restore_journal.backups (id,record,review_required)
VALUES ('sentinel_backup','{"s3":"sentinel_object"}',true);
INSERT INTO restore_journal.previews (id,workspace_id,actor_id,request_key,fingerprint,backup_id,record,target,digest,evidence)
VALUES ('sentinel_preview',1,1,'sentinel_preview_key','sentinel_preview_fp','sentinel_backup','{}','isolated','sentinel_digest','{}');
INSERT INTO restore_journal.operations (id,workspace_id,actor_id,request_key,session_key,preview_id,target,generation)
VALUES ('sentinel_operation',1,1,'sentinel_op_key','sentinel_session','sentinel_preview','isolated',6);
`)
	t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", journalCfg.Database.DSN())

	inventory := cliReport(t, "current-inventory", "inventory")
	for source, version := range map[string]string{
		"workspaces": "9", "projects": "3", "legacy_credential_references": "4",
		"connection_credential_versions": "4", "provider_connections": "12",
		"upstream_jobs": "9223372036854775807", "batch_image_jobs": "0",
		"restore_operations": "6", "restore_operations_outstanding": "6",
	} {
		count := inventory.Counts[source]
		require.NotNil(t, count.Rows, source)
		require.Equal(t, int64(1), *count.Rows, source)
		require.NotNil(t, count.Version, source)
		require.Equal(t, version, *count.Version.Min, source)
		require.Equal(t, version, *count.Version.Max, source)
	}
	require.Equal(t, int64(1), *inventory.Counts["restore_backups_requiring_review"].Rows)
	require.Equal(t, int64(1), *inventory.Counts["api_key_credentials"].Rows)
	require.Equal(t, int64(1), *inventory.Unmapped["identity_scope_quarantine"])
	require.Equal(t, int64(1), *inventory.Unmapped["connection_migration_quarantine"])

	id := uuid.New()
	execSQL(t, app, `
INSERT INTO refactor_checkpoints (run_id,module,workspace_id,revision,cursor,updated_at)
VALUES ($1,'instance',0,2,'{"cursor":"sentinel_cursor"}','2026-01-01T00:00:00Z'),
       (gen_random_uuid(),'instance',0,5,'{}','2025-01-01T00:00:00Z'),
       (gen_random_uuid(),'billing',1,4,'{}',clock_timestamp());
`, id)
	for range 2 {
		execSQL(t, app, `
INSERT INTO refactor_mapping_exceptions (run_id,module,workspace_id,source_kind,source_ref,reason_code)
VALUES ($1,'instance',0,'account','sentinel_source_ref','sentinel_reason'),
       ($1,'instance',0,'account','sentinel_resolved_ref','sentinel_reason')
ON CONFLICT DO NOTHING
`, id)
	}
	execSQL(t, app, `UPDATE refactor_mapping_exceptions SET resolved_at=clock_timestamp() WHERE source_ref='sentinel_resolved_ref'`)
	for _, artifact := range []string{"recorded-status", "repeated-status"} {
		report := cliReport(t, artifact, "status")
		require.Equal(t, id, *report.RunID)
		require.Equal(t, "recorded", report.Checkpoint.State)
		require.Equal(t, int64(2), *report.Checkpoint.Revision)
		require.Equal(t, "2026-01-01T00:00:00Z", report.Checkpoint.RecordedAt.Format("2006-01-02T15:04:05Z07:00"))
		require.Equal(t, int64(1), *report.Unmapped["open_mapping_exceptions"])
		require.Equal(t, inventory.Counts, report.Counts)
	}
}

func TestPostgresInventoryWithLimitedAccess(t *testing.T) {
	app, cfg := testfixture.Database(t, testDSNEnv, "job_test")
	// The role can count workspaces without selecting their version, can
	// observe an empty projects table, and sees neither RLS-filtered rows nor
	// a view masquerading as a credential table.
	role := "migration_inventory_" + uuid.New().String()[:8]
	execSQL(t, app, "CREATE ROLE "+role)
	t.Cleanup(func() {
		_, err := app.Exec("DROP OWNED BY " + role)
		require.NoError(t, err)
		_, err = app.Exec("DROP ROLE " + role)
		require.NoError(t, err)
	})
	execSQL(t, app, `
CREATE TABLE workspaces (id bigint, scope_version bigint);
INSERT INTO workspaces VALUES (1, 10);
CREATE TABLE projects (version bigint);
CREATE TABLE users (balance numeric);
INSERT INTO users VALUES (20);
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY filter_users ON users USING (false);
CREATE VIEW credential_secrets AS SELECT 'sentinel_secret'::text AS version;
CREATE TABLE provider_connections (version text);
INSERT INTO provider_connections VALUES ('sentinel_text_version');
GRANT SELECT (id) ON workspaces TO `+role+`;
GRANT SELECT ON projects, users, credential_secrets, provider_connections TO `+role)
	t.Setenv("OMNIHIVE_REFACTOR_DSN", cfg.Database.DSN()+" options='-c role="+role+"'")
	t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", "")
	report := cliReport(t, "limited-inventory", "inventory")
	require.Equal(t, int64(1), *report.Counts["workspaces"].Rows)
	require.Nil(t, report.Counts["workspaces"].Version)
	require.Equal(t, int64(0), *report.Counts["projects"].Rows)
	require.NotNil(t, report.Counts["projects"].Version)
	require.Nil(t, report.Counts["projects"].Version.Min)
	require.Nil(t, report.Counts["projects"].Version.Max)
	require.Nil(t, report.Counts["legacy_wallet_accounts"].Rows)
	require.Nil(t, report.Counts["legacy_credential_references"].Rows)
	require.Equal(t, int64(1), *report.Counts["provider_connections"].Rows)
	require.Nil(t, report.Counts["provider_connections"].Version)
}

func cliReport(t *testing.T, artifact, command string) migration.Report {
	t.Helper()
	args := []string{command, "--format=json"}
	var stdout, stderr bytes.Buffer
	if binary := os.Getenv("MIGRATION_SMOKE_BINARY"); binary != "" {
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		require.NoError(t, cmd.Run(), "CLI failed: %s", stderr.String())
	} else {
		require.Zero(t, Run(context.Background(), args, &stdout, &stderr), stderr.String())
	}
	require.Empty(t, stderr.String())
	require.NotContains(t, stdout.String(), "sentinel")
	var report migration.Report
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.Equal(t, "instance", report.Module)
	require.False(t, report.EligibleForSwitch)
	require.Nil(t, report.RollbackFloor)
	require.Nil(t, report.UnexplainedDifferences)
	if dir := os.Getenv("MIGRATION_SMOKE_REPORT_DIR"); dir != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, artifact+".json"), stdout.Bytes(), 0o600))
	}
	return report
}

func applySQLFile(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	ddl, err := migrations.FS.ReadFile(name)
	require.NoError(t, err)
	execSQL(t, db, string(ddl))
}

func execSQL(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), statement, args...)
	require.NoError(t, err)
}
