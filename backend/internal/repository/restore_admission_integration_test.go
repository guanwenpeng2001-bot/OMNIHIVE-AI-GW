//go:build integration

package repository

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore/testfixture"
	"github.com/stretchr/testify/require"
)

func TestRestoreDrainsTelemetryWriter(t *testing.T) {
	db, cfg := testfixture.Database(t, "UPSTREAM_JOB_TEST_DSN", "job_test")
	journal, jcfg := testfixture.Database(t, "BE09_TEST_JOURNAL_DSN", "be09_journal_test")
	_, err := journal.Exec("CREATE SCHEMA restore_journal; CREATE TABLE restore_journal.operations (target text,state text,stage text)")
	require.NoError(t, err)
	t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", jcfg.Database.DSN())
	workers, err := OpenApplicationDB(cfg.Database.DSN(), false)
	require.NoError(t, err)
	defer workers.Close()
	_, err = db.Exec("CREATE TABLE telemetry_export_jobs(id int primary key, attempts int); INSERT INTO telemetry_export_jobs VALUES(1,0)")
	require.NoError(t, err)
	source := filepath.Join(t.TempDir(), "source.sql")
	require.NoError(t, os.WriteFile(source, []byte("ALTER TABLE telemetry_export_jobs DROP CONSTRAINT telemetry_export_jobs_pkey;"), 0600))
	checks := filepath.Join(t.TempDir(), "checks.sql")
	require.NoError(t, os.WriteFile(checks, []byte("SELECT 1;"), 0600))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	locked := func() error {
		_, err := journal.Exec("INSERT INTO restore_journal.operations VALUES ('production','running','snapshot')")
		require.NoError(t, err)
		go func() {
			tx, err := workers.BeginTx(ctx, nil)
			if err == nil {
				defer tx.Rollback()
				_, err = tx.ExecContext(ctx, "SELECT id FROM telemetry_export_jobs; UPDATE telemetry_export_jobs SET attempts=attempts+1")
			}
			done <- err
		}()
		select {
		case err := <-done:
			require.ErrorContains(t, err, "restore maintenance")
		case <-time.After(time.Second):
			t.Error("worker entered target during restore")
		}
		return nil
	}
	noop := func() error { return nil }
	require.NoError(t, NewPgDumper(cfg, db).RestoreChecked(ctx, source, checks, locked, noop, noop))
	_, err = journal.Exec("UPDATE restore_journal.operations SET state='succeeded',stage='verified'")
	require.NoError(t, err)
	// A reader outside application admission can still prevent the exclusive
	// upgrade. Surface the relation to the journal without leaking archive SQL.
	reader, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer reader.Rollback()
	_, err = reader.ExecContext(ctx, "SELECT id FROM telemetry_export_jobs")
	require.NoError(t, err)
	err = NewPgDumper(cfg, db).RestoreChecked(ctx, source, checks, noop, noop, noop)
	require.EqualError(t, err, "lock_timeout public.telemetry_export_jobs")
	_, err = workers.ExecContext(ctx, "UPDATE telemetry_export_jobs SET attempts=1")
	require.NoError(t, err)
}

func TestRestoreN1(t *testing.T) {
	db, cfg := testfixture.Database(t, "UPSTREAM_JOB_TEST_DSN", "job_test")
	_, err := db.Exec("CREATE TABLE users(id integer PRIMARY KEY); INSERT INTO users VALUES (1); CREATE TABLE memberships(uid integer REFERENCES users)")
	require.NoError(t, err)
	d := NewPgDumper(cfg, db)
	archive, err := d.Dump(t.Context())
	require.NoError(t, err)
	data, err := io.ReadAll(archive)
	require.NoError(t, err)
	require.NoError(t, archive.Close())
	source := filepath.Join(t.TempDir(), "archive.sql")
	require.NoError(t, os.WriteFile(source, data, 0600))
	_, err = db.Exec("CREATE TABLE newer(uid integer REFERENCES users); INSERT INTO newer VALUES(1); INSERT INTO users VALUES(2)")
	require.NoError(t, err)
	checks := filepath.Join(t.TempDir(), "checks.sql")
	noop := func() error { return nil }
	for _, tc := range []struct {
		name, sql string
		count     int
	}{
		{"rollback", "SELECT 'private-row-secret'::integer;", 2},
		{"apply", "SELECT 1;", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(checks, []byte(tc.sql), 0600))
			err := d.RestoreChecked(t.Context(), source, checks, noop, noop, noop)
			if tc.name == "rollback" {
				require.ErrorContains(t, err, "SQLSTATE 22P02")
				require.NotContains(t, err.Error(), "private-row-secret")
			} else {
				require.NoError(t, err)
			}
			var count int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM users").Scan(&count))
			require.Equal(t, tc.count, count)
			_, err = db.Exec("INSERT INTO newer VALUES(999)")
			require.ErrorContains(t, err, "foreign key")
		})
	}
}

func TestRestoreN1MigratedSchema(t *testing.T) {
	db, cfg := testfixture.Database(t, "UPSTREAM_JOB_TEST_DSN", "job_test")
	ctx := t.Context()
	require.NoError(t, ApplyMigrations(ctx, db))
	d := NewPgDumper(cfg, db)
	dump, err := d.Dump(ctx)
	require.NoError(t, err)
	data, err := io.ReadAll(dump)
	require.NoError(t, err)
	require.NoError(t, dump.Close())
	source := filepath.Join(t.TempDir(), "archive.sql")
	require.NoError(t, os.WriteFile(source, data, 0600))
	_, err = db.Exec("ALTER TABLE users ADD CONSTRAINT target_only_fk FOREIGN KEY (id) REFERENCES users (id)")
	require.NoError(t, err)
	noop := func() error { return nil }
	checks := filepath.Join(t.TempDir(), "checks.sql")
	require.NoError(t, os.WriteFile(checks, []byte("SELECT count(*) FROM public.schema_migrations;"), 0600))
	require.NoError(t, d.RestoreChecked(ctx, source, checks, noop, noop, noop))
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM pg_constraint WHERE conrelid='users'::regclass AND conname='target_only_fk'").Scan(&count))
	require.Zero(t, count)
}
