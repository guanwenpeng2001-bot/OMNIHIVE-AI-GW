//go:build integration

package repository

import (
	"context"
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
