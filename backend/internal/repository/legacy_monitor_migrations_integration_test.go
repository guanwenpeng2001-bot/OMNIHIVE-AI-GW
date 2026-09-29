//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func legacyMigrationDatabase(t *testing.T) *sql.DB {
	testresources.RequireDisposableDatabase(t, "NEW_PLATFORMS_TEST_POSTGRES_DSN")
	t.Helper()
	dsn := os.Getenv("NEW_PLATFORMS_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("NEW_PLATFORMS_TEST_POSTGRES_DSN is required in CI")
		}
		t.Skip("set NEW_PLATFORMS_TEST_POSTGRES_DSN for disposable migration databases")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	name := fmt.Sprintf("legacy_monitor_upgrade_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, err := admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		require.NoError(t, err)
		_ = admin.Close()
	})
	return db
}

// Fixtures are exact pre-repair files, not checksum edits to an existing database.
func monitorMigrationFiles(t *testing.T, through string, historical bool, crlf bool) fstest.MapFS {
	t.Helper()
	files, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	result := fstest.MapFS{}
	for _, name := range files {
		if name > through+"~" {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		if historical && (strings.HasPrefix(name, "240_") || strings.HasPrefix(name, "243_") || strings.HasPrefix(name, "244_")) {
			body, err = os.ReadFile("testdata/legacy_monitor_migrations/" + name)
			require.NoError(t, err)
			body = []byte(strings.ReplaceAll(string(body), "\r\n", "\n"))
		}
		// Only the known files have an approved CRLF exception.
		if crlf && (strings.HasPrefix(name, "239_") || strings.HasPrefix(name, "240_") || strings.HasPrefix(name, "241_") || strings.HasPrefix(name, "242_") || strings.HasPrefix(name, "243_") || strings.HasPrefix(name, "244_")) {
			body = []byte(strings.ReplaceAll(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n", "\r\n"))
		}
		result[name] = &fstest.MapFile{Data: body}
	}
	return result
}

func seedLegacyMonitor(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO channel_monitor_request_templates(name,provider,description,extra_headers)
		VALUES ($1::text,$1::text,'historical template','{"x-preserve":"original"}');
	`, provider)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO channel_monitors(name,provider,endpoint,api_key_encrypted,primary_model,interval_seconds,created_by,template_id)
		SELECT $1::text,$1::text,'https://example.invalid','historical-ciphertext','historical-model',60,1,id
		FROM channel_monitor_request_templates WHERE name=$1`, provider)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO channel_monitor_histories(monitor_id,model,status,message)
		SELECT id,primary_model,'operational','historical result' FROM channel_monitors WHERE name=$1`, provider)
	require.NoError(t, err)
}

func legacyMonitorSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot string
	require.NoError(t, db.QueryRow(`SELECT jsonb_build_array(
		(SELECT jsonb_agg(jsonb_build_object('workspace_id',1) || to_jsonb(m) ORDER BY id) FROM channel_monitors m),
		(SELECT jsonb_agg(jsonb_build_object('workspace_id',1) || to_jsonb(t) ORDER BY id) FROM channel_monitor_request_templates t),
		(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM channel_monitor_histories h))::text`).Scan(&snapshot))
	return snapshot
}

func migrationLedgerSnapshot(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT filename, checksum || ':' || applied_at::text FROM schema_migrations`)
	require.NoError(t, err)
	defer rows.Close()
	ledger := map[string]string{}
	for rows.Next() {
		var name, record string
		require.NoError(t, rows.Scan(&name, &record))
		ledger[name] = record
	}
	require.NoError(t, rows.Err())
	return ledger
}

func monitorConstraintIdentity(t *testing.T, db *sql.DB) string {
	t.Helper()
	var identity string
	require.NoError(t, db.QueryRow(`SELECT string_agg(oid::text, ',' ORDER BY conname) FROM pg_constraint
		WHERE conname IN ('channel_monitors_provider_check','channel_monitor_request_templates_provider_check')`).Scan(&identity))
	return identity
}

func assertMonitorStorageContract(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"channel_monitors", "channel_monitor_request_templates"} {
		var definition string
		require.NoError(t, db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, table, table+"_provider_check").Scan(&definition))
		for _, provider := range []string{"openai", "anthropic", "gemini", "grok", "antigravity", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "cursor", "qianwen", "commandcode"} {
			require.Contains(t, definition, "'"+provider+"'")
		}
		require.NotContains(t, definition, "'composite'")
		require.NotContains(t, definition, "'bogus'")
		_, err := db.Exec("UPDATE " + table + " SET provider='bogus'")
		if table == "channel_monitors" {
			var count int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
			if count > 0 {
				require.Error(t, err)
			}
		}
	}
}

func TestLegacyMonitorUpgradeChain(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "NEW_PLATFORMS_TEST_POSTGRES_DSN")
	for _, checkpoint := range []string{"fresh", "237", "239", "240", "243", "244", "248", "253"} {
		t.Run(checkpoint, func(t *testing.T) {
			db := legacyMigrationDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			before := ""
			ledger := map[string]string{}
			if checkpoint != "fresh" {
				// Seed under the actual historical constraints before 240, then
				// advance using the full runner, including every intermediate step.
				require.NoError(t, applyMigrationsFS(ctx, db, monitorMigrationFiles(t, "237", false, false)))
				seedLegacyMonitor(t, db, "minimax")
				if checkpoint != "237" {
					require.NoError(t, applyMigrationsFS(ctx, db, monitorMigrationFiles(t, "239", false, false)))
					seedLegacyMonitor(t, db, "opencode_go")
					seedLegacyMonitor(t, db, "cursor")
				}
				before = legacyMonitorSnapshot(t, db)
				require.NoError(t, applyMigrationsFS(ctx, db, monitorMigrationFiles(t, checkpoint, false, false)))
				ledger = migrationLedgerSnapshot(t, db)
			}
			started := time.Now()
			require.NoError(t, ApplyMigrations(ctx, db))
			t.Logf("checkpoint=%s full runner elapsed=%s", checkpoint, time.Since(started))
			if before != "" {
				require.Equal(t, before, legacyMonitorSnapshot(t, db))
			}
			completedLedger := migrationLedgerSnapshot(t, db)
			for name, record := range ledger {
				require.Equal(t, record, completedLedger[name], name)
			}
			require.Contains(t, completedLedger, "255_channel_monitor_storage_compatibility.sql")
			assertMonitorStorageContract(t, db)
			constraints := monitorConstraintIdentity(t, db)
			require.NoError(t, ApplyMigrations(ctx, db))
			require.Equal(t, constraints, monitorConstraintIdentity(t, db), "restart must not recreate CHECKs")
			require.Equal(t, completedLedger, migrationLedgerSnapshot(t, db), "restart must not replay any migration")
		})
	}
}

func TestLegacyMonitorAppliedChecksums(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "NEW_PLATFORMS_TEST_POSTGRES_DSN")
	for _, checkpoint := range []string{"240", "243", "244", "248", "253"} {
		for _, crlf := range []bool{false, true} {
			t.Run(fmt.Sprintf("applied_%s_crlf_%t", checkpoint, crlf), func(t *testing.T) {
				db := legacyMigrationDatabase(t)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				require.NoError(t, applyMigrationsFS(ctx, db, monitorMigrationFiles(t, checkpoint, true, crlf)))
				seedLegacyMonitor(t, db, "openai")
				if checkpoint >= "244" {
					seedLegacyMonitor(t, db, "minimax")
					seedLegacyMonitor(t, db, "opencode_go")
				}
				before := legacyMonitorSnapshot(t, db)
				ledger := migrationLedgerSnapshot(t, db)
				require.NoError(t, ApplyMigrations(ctx, db))
				require.Equal(t, before, legacyMonitorSnapshot(t, db))
				completedLedger := migrationLedgerSnapshot(t, db)
				for name, record := range ledger {
					require.Equal(t, record, completedLedger[name], name)
				}
				seedLegacyMonitor(t, db, "cursor") // 255 must repair already-applied CHECKs.
				assertMonitorStorageContract(t, db)
				constraints := monitorConstraintIdentity(t, db)
				require.NoError(t, ApplyMigrations(ctx, db))
				require.Equal(t, constraints, monitorConstraintIdentity(t, db))
				require.Equal(t, completedLedger, migrationLedgerSnapshot(t, db))
			})
		}
	}
}

func TestLegacyMonitorChecksumDrift(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "NEW_PLATFORMS_TEST_POSTGRES_DSN")
	db := legacyMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, db))
	ledger := migrationLedgerSnapshot(t, db)
	for _, name := range []string{"240_channel_monitor_provider_capabilities.sql", "243_channel_monitor_qianwen_commandcode.sql", "244_channel_monitor_minimax_opencode.sql"} {
		t.Run(name, func(t *testing.T) {
			files := monitorMigrationFiles(t, "999", false, false)
			files[name].Data = append(files[name].Data, []byte("\n-- unknown drift")...)
			err := applyMigrationsFS(ctx, db, files)
			require.ErrorContains(t, err, name+" checksum mismatch")
			require.Equal(t, ledger, migrationLedgerSnapshot(t, db))
		})
	}
	// The repaired contents also accept their exact CRLF checkout and outer whitespace.
	files := monitorMigrationFiles(t, "999", false, true)
	for _, file := range files {
		file.Data = append([]byte(" \n"), append(file.Data, []byte("\n\t ")...)...)
	}
	require.NoError(t, applyMigrationsFS(ctx, db, files))
	require.Equal(t, ledger, migrationLedgerSnapshot(t, db))
}

func TestLegacyMonitorUnknownAppliedChecksum(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "NEW_PLATFORMS_TEST_POSTGRES_DSN")
	for _, name := range []string{"240_channel_monitor_provider_capabilities.sql", "243_channel_monitor_qianwen_commandcode.sql", "244_channel_monitor_minimax_opencode.sql"} {
		t.Run(name, func(t *testing.T) {
			db := legacyMigrationDatabase(t)
			ctx := context.Background()
			files := monitorMigrationFiles(t, name, true, false)
			files[name].Data = append(files[name].Data, []byte("\n-- unrecognized historical edit")...)
			// Produce the unknown stored hash by executing its actual SQL once.
			// No migration record is manually edited, even in this negative test.
			require.NoError(t, applyMigrationsFS(ctx, db, files))
			ledger := migrationLedgerSnapshot(t, db)
			require.ErrorContains(t, ApplyMigrations(ctx, db), name+" checksum mismatch")
			require.Equal(t, ledger, migrationLedgerSnapshot(t, db))
		})
	}
}

// This is a synthetic isolated schema copy, not a production sizing claim.
// Tune row count to the deployment's measured size before approving its window.
func TestLegacyUpgradeDDLMeasurement(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "NEW_PLATFORMS_TEST_POSTGRES_DSN")
	db := legacyMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	require.NoError(t, applyMigrationsFS(ctx, db, monitorMigrationFiles(t, "251", false, false)))
	_, err := db.ExecContext(ctx, `ALTER TABLE upstream_jobs RENAME TO upstream_jobs_source;
		CREATE TABLE upstream_jobs (LIKE upstream_jobs_source INCLUDING ALL);
		INSERT INTO upstream_jobs(public_request_id,request_fingerprint,submission_key,platform,family,account_id,user_id,api_key_id,group_id,expires_at,billing_snapshot)
		SELECT 'request-'||entry,'fingerprint-'||entry,'submission-'||entry,'qianwen',
		CASE WHEN entry%2=0 THEN 'batch' ELSE 'files' END,
		entry%200+1,entry%1000+1,entry%5000+1,entry%20+1,NOW()+INTERVAL '1 day',
		jsonb_build_object('file_batch',jsonb_build_object('input_file_id','file-'||entry),'payload',repeat('x',1024))
		FROM generate_series(1,200000) entry;
		ANALYZE upstream_jobs;`)
	require.NoError(t, err)
	var rows, bytes int64
	require.NoError(t, db.QueryRow(`SELECT count(*), pg_total_relation_size('upstream_jobs') FROM upstream_jobs`).Scan(&rows, &bytes))
	t.Logf("isolated synthetic schema copy: rows=%d total_bytes=%d", rows, bytes)
	for _, checkpoint := range []string{"252", "253"} {
		reader, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = reader.ExecContext(ctx, `SELECT id FROM upstream_jobs LIMIT 1`)
		require.NoError(t, err)
		started := time.Now()
		completed := make(chan error, 1)
		go func() { completed <- applyMigrationsFS(ctx, db, monitorMigrationFiles(t, checkpoint, false, false)) }()
		// Observe the actual old DDL lock wait before releasing the reader.
		require.Eventually(t, func() bool {
			var waiting bool
			err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='upstream_jobs'::regclass AND NOT granted)`).Scan(&waiting)
			return err == nil && waiting
		}, 10*time.Second, 20*time.Millisecond)
		waitElapsed := time.Since(started)
		var observedWaitMilliseconds float64
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COALESCE(MAX(EXTRACT(EPOCH FROM (clock_timestamp()-waitstart))*1000),0) FROM pg_locks WHERE relation='upstream_jobs'::regclass AND NOT granted`).Scan(&observedWaitMilliseconds))
		require.NoError(t, reader.Rollback())
		require.NoError(t, <-completed)
		t.Logf("migration=%s runner_until_lock_observed=%s observed_lock_age_ms=%.3f ddl_after_release=%s total=%s", checkpoint, waitElapsed, observedWaitMilliseconds, time.Since(started)-waitElapsed, time.Since(started))
	}
	var valid bool
	require.NoError(t, db.QueryRow(`SELECT bool_and(indisvalid) FROM pg_index WHERE indrelid='upstream_jobs'::regclass`).Scan(&valid))
	require.True(t, valid)
}
