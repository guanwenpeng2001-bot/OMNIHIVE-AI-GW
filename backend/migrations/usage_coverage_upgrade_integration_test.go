//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// Exercise the real, one-shot ALTER migration against a minimal pre-258 schema.
// Runner replay safety is tested separately; the SQL itself is not idempotent.
func TestUsageCoverageMigrationUpgrade(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	require.NoError(t, testresources.ValidatePostgres(dsn, "job_test"))
	db := testresources.OpenSchema(t, dsn)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
 CREATE TABLE usage_dashboard_aggregation_watermark (
  id integer PRIMARY KEY, last_aggregated_at timestamptz NOT NULL, updated_at timestamptz NOT NULL DEFAULT now());
 CREATE TABLE usage_group_rollup_state (id integer PRIMARY KEY, closed_before date DEFAULT '1970-01-01');
 CREATE TABLE users (id bigint PRIMARY KEY);
 CREATE TABLE usage_logs (id bigint PRIMARY KEY, user_id bigint REFERENCES users(id), created_at timestamptz NOT NULL);
 INSERT INTO usage_group_rollup_state(id) VALUES (1);
 INSERT INTO users VALUES (7);
 INSERT INTO usage_logs VALUES (1,7,'2024-01-01');
 INSERT INTO usage_dashboard_aggregation_watermark(id,last_aggregated_at) VALUES (1,'2025-01-01');`)
	require.NoError(t, err)
	var before, after time.Time
	require.NoError(t, db.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&before))
	body, err := migrations.FS.ReadFile("258_usage_dashboard_coverage.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(body))
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&after))
	var published, raw time.Time
	var hourly, daily, dirty sql.NullTime
	var zone string
	var version int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT last_aggregated_at,raw_retained_from,hourly_retained_from,daily_retained_from,dirty_from,timezone_name,projection_version FROM usage_dashboard_aggregation_watermark WHERE id=1`).Scan(&published, &raw, &hourly, &daily, &dirty, &zone, &version))
	require.True(t, published.Equal(time.Unix(0, 0)), "unverified legacy projection must be reset")
	require.False(t, raw.Before(before), "surviving historical logs do not prove raw coverage")
	require.False(t, raw.After(after))
	require.False(t, hourly.Valid)
	require.False(t, daily.Valid)
	require.False(t, dirty.Valid)
	require.Empty(t, zone)
	require.Equal(t, 2, version)
	var logs, triggers int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM usage_logs WHERE user_id=7").Scan(&logs))
	require.Equal(t, 1, logs)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='usage_logs'::regclass AND tgname IN ('usage_logs_dashboard_writer_lock','usage_logs_dashboard_invalidate')`).Scan(&triggers))
	require.Equal(t, 2, triggers)
	_, err = db.ExecContext(ctx, "UPDATE usage_dashboard_aggregation_watermark SET raw_retained_from=NULL WHERE id=1")
	require.Error(t, err, "raw retention is NOT NULL")
	_, err = db.ExecContext(ctx, "UPDATE usage_dashboard_aggregation_watermark SET projection_version=NULL WHERE id=1")
	require.Error(t, err, "projection version is NOT NULL")
	exec := func(query string) { _, err := db.ExecContext(ctx, query); require.NoError(t, err) }
	assertDirty := func(want string) {
		var got string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT COALESCE(to_char(dirty_from AT TIME ZONE 'UTC','YYYY-MM-DD'),'') FROM usage_dashboard_aggregation_watermark WHERE id=1").Scan(&got))
		require.Equal(t, want, got)
	}
	exec("UPDATE usage_dashboard_aggregation_watermark SET last_aggregated_at='2025-01-01' WHERE id=1")
	exec("INSERT INTO usage_logs VALUES (2,7,'2025-02-01')")
	assertDirty("")
	exec("INSERT INTO usage_logs VALUES (3,7,'2024-06-01')")
	assertDirty("2024-06-01")
	exec("UPDATE usage_logs SET created_at='2024-05-01' WHERE id=3")
	assertDirty("2024-05-01")
	exec("DELETE FROM usage_logs WHERE id=1")
	assertDirty("2024-01-01")
	exec("UPDATE usage_dashboard_aggregation_watermark SET dirty_from=NULL WHERE id=1")
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "SET LOCAL sub2api.usage_retention='on'; DELETE FROM usage_logs WHERE id=3")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	assertDirty("")
}

func TestUsageCoverageHistoryOriginMigration(test *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	require.NoError(test, testresources.ValidatePostgres(dsn, "job_test"))
	for _, layout := range []string{"ordinary", "partition_parent", "partition_direct"} {
		test.Run(layout, func(test *testing.T) {
			database := testresources.OpenSchema(test, dsn)
			execute := func(statement string) { test.Helper(); _, err := database.Exec(statement); require.NoError(test, err) }
			execute(`CREATE TABLE usage_group_rollup_state(id integer PRIMARY KEY,closed_before date); INSERT INTO usage_group_rollup_state VALUES(1,'1970-01-01');
    CREATE TABLE usage_dashboard_aggregation_watermark(id integer PRIMARY KEY,last_aggregated_at timestamptz,updated_at timestamptz DEFAULT now());
    INSERT INTO usage_dashboard_aggregation_watermark(id,last_aggregated_at) VALUES(1,'1970-01-01')`)
			definition := `CREATE TABLE usage_logs(id bigint,created_at timestamptz NOT NULL)`
			if layout != "ordinary" {
				definition += ` PARTITION BY RANGE(created_at); CREATE TABLE usage_logs_default PARTITION OF usage_logs DEFAULT`
			}
			execute(definition)
			for _, migration := range []string{"258_usage_dashboard_coverage.sql", "279_usage_history_origin.sql"} {
				body, err := migrations.FS.ReadFile(migration)
				require.NoError(test, err)
				execute(string(body))
			}
			var origin sql.NullTime
			readOrigin := func() {
				test.Helper()
				require.NoError(test, database.QueryRow(`SELECT history_known_empty_before FROM usage_dashboard_aggregation_watermark`).Scan(&origin))
			}
			readOrigin()
			require.False(test, origin.Valid, "upgrade must not invent origin")
			var baselineRaw time.Time
			require.NoError(test, database.QueryRow(`SELECT raw_retained_from FROM usage_dashboard_aggregation_watermark`).Scan(&baselineRaw))
			table := "usage_logs"
			if layout == "partition_direct" {
				table = "usage_logs_default"
			}
			declareOrigin := func() {
				execute(`UPDATE usage_dashboard_aggregation_watermark SET history_known_empty_before='2026-03-01 10:15Z',dirty_from=NULL`)
			}
			declareOrigin()
			execute(`INSERT INTO ` + table + ` VALUES(1,'2026-03-01 10:15Z')`)
			readOrigin()
			require.True(test, origin.Valid, "origin is exclusive")
			execute(`INSERT INTO ` + table + ` VALUES(2,'2026-03-01 10:14Z')`)
			readOrigin()
			require.False(test, origin.Valid, "INSERT revokes even before any publication")
			declareOrigin()
			execute(`UPDATE ` + table + ` SET created_at='2026-03-02' WHERE id=2`)
			readOrigin()
			require.False(test, origin.Valid, "UPDATE old time is counterevidence")
			declareOrigin()
			execute(`UPDATE ` + table + ` SET created_at='2026-03-01 10:14Z' WHERE id=1`)
			readOrigin()
			require.False(test, origin.Valid, "UPDATE new time is counterevidence")
			declareOrigin()
			transaction, err := database.Begin()
			require.NoError(test, err)
			_, err = transaction.Exec(`INSERT INTO ` + table + ` VALUES(3,'2026-02-01')`)
			require.NoError(test, err)
			require.NoError(test, transaction.Rollback())
			readOrigin()
			require.True(test, origin.Valid, "rollback restores declaration")
			transaction, err = database.Begin()
			require.NoError(test, err)
			_, err = transaction.Exec(`SET LOCAL sub2api.usage_retention='on'; DELETE FROM ` + table + ` WHERE id=2`)
			require.NoError(test, err)
			require.NoError(test, transaction.Commit())
			readOrigin()
			require.True(test, origin.Valid, "normal retention does not move declaration")
			transaction, err = database.Begin()
			require.NoError(test, err)
			_, err = transaction.Exec(`SET LOCAL sub2api.usage_retention='on'; DELETE FROM ` + table + ` WHERE id=1`)
			require.NoError(test, err)
			require.NoError(test, transaction.Commit())
			readOrigin()
			require.False(test, origin.Valid, "retention cannot conceal OLD counterevidence")
			execute(`INSERT INTO ` + table + ` VALUES(4,'2026-02-01')`)
			declareOrigin()
			execute(`DELETE FROM ` + table + ` WHERE id=4`)
			readOrigin()
			require.False(test, origin.Valid, "ordinary DELETE revokes")
			var raw time.Time
			var dirty sql.NullTime
			require.NoError(test, database.QueryRow(`SELECT raw_retained_from,dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&raw, &dirty))
			require.True(test, raw.Equal(baselineRaw), "revocation never lowers raw")
			require.False(test, dirty.Valid, "Epoch publication does not require dirty history")
		})
	}
}
