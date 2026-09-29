//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestUsageDashboardPartitionDropWaitsForCoverageReader(t *testing.T) {
	dsn := os.Getenv("USAGE_STATS_TEST_DSN")
	if dsn == "" {
		t.Skip("set USAGE_STATS_TEST_DSN to a disposable stats_test database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	var name string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&name))
	require.Equal(t, "stats_test", name)
	_, err = db.Exec(`CREATE SCHEMA stats_partition_coverage`)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA stats_partition_coverage CASCADE`)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	if parsed.Scheme == "postgres" || parsed.Scheme == "postgresql" {
		query := parsed.Query()
		query.Set("search_path", "stats_partition_coverage")
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=stats_partition_coverage"
	}
	isolated, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer isolated.Close()
	_, err = isolated.Exec(`
 CREATE TABLE usage_logs(created_at timestamptz NOT NULL,actual_cost numeric NOT NULL) PARTITION BY RANGE(created_at);
 CREATE TABLE usage_logs_202603 PARTITION OF usage_logs FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
 INSERT INTO usage_logs VALUES('2026-03-01 00:15:00+00',7);
 CREATE TABLE usage_group_rollup_state(id smallint PRIMARY KEY,closed_before date,updated_at timestamptz);
 INSERT INTO usage_group_rollup_state VALUES(1,'2026-04-01',NOW());
 CREATE TABLE usage_dashboard_aggregation_watermark(id smallint PRIMARY KEY,raw_retained_from timestamptz,hourly_retained_from timestamptz,daily_retained_from timestamptz,last_aggregated_at timestamptz,dirty_from timestamptz,timezone_name text,projection_version integer,updated_at timestamptz);
 INSERT INTO usage_dashboard_aggregation_watermark VALUES(1,'1970-01-01','2026-03-01','2026-03-01','2026-04-01',NULL,'UTC',2,NOW());
 ALTER TABLE usage_dashboard_aggregation_watermark ADD COLUMN history_known_empty_before timestamptz;`)
	require.NoError(t, err)
	previous := timezone.Name()
	require.NoError(t, timezone.Init("UTC"))
	t.Cleanup(func() { require.NoError(t, timezone.Init(previous)) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := &usageLogRepository{sql: isolated}
	checked, proceed := make(chan struct{}), make(chan struct{})
	type readResult struct {
		amount float64
		err    error
	}
	readDone := make(chan readResult, 1)
	go func() {
		amount, err := usageReadSnapshot(ctx, repo, func(snapshot *usageLogRepository, c dashboardCoverage) (float64, error) {
			close(checked)
			select {
			case <-proceed:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			var amount float64
			err := scanSingleRow(ctx, snapshot.sql, `SELECT COALESCE(SUM(actual_cost),0) FROM usage_logs`, nil, &amount)
			return amount, err
		})
		readDone <- readResult{amount, err}
	}()
	select {
	case <-checked:
	case result := <-readDone:
		t.Fatalf("reader failed before coverage: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	dropDone := make(chan error, 1)
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	go func() {
		dropDone <- dropUsageLogsPartitionWithRollupInvalidation(ctx, isolated, "usage_logs_202603", march)
	}()
	select {
	case err := <-dropDone:
		t.Fatalf("physical retention passed an active reader: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(proceed)
	result := <-readDone
	require.NoError(t, result.err)
	require.Equal(t, 7.0, result.amount, "the checked history must not disappear between coverage and data reads")
	require.NoError(t, <-dropDone)
	coverage, err := readDashboardCoverage(ctx, isolated, false)
	require.NoError(t, err)
	require.ErrorIs(t, coverage.requireRaw(march, march.Add(time.Hour)), ErrUsageHistoryUnavailable)
	var relation sql.NullString
	require.NoError(t, isolated.QueryRow(`SELECT to_regclass('usage_logs_202603')::text`).Scan(&relation))
	require.False(t, relation.Valid, "retention finishes and publishes its floor once the reader ends")
	// Reverse the order too: a reader waiting on an already-started physical
	// drop must take its coverage snapshot after that drop commits.
	_, err = isolated.Exec(`CREATE TABLE usage_logs_202603 PARTITION OF usage_logs FOR VALUES FROM ('2026-03-01') TO ('2026-04-01'); INSERT INTO usage_logs VALUES('2026-03-01',7); UPDATE usage_dashboard_aggregation_watermark SET raw_retained_from='1970-01-01'`)
	require.NoError(t, err)
	tx, err := isolated.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, lockGroupUsageRollupState(ctx, tx))
	require.NoError(t, prepareDashboardRawRetention(ctx, tx, march.AddDate(0, 1, 0)))
	_, err = tx.ExecContext(ctx, `DROP TABLE usage_logs_202603`)
	require.NoError(t, err)
	waitingReader := make(chan error, 1)
	go func() {
		_, err := rawUsageReadSnapshot(ctx, repo, march, march.Add(time.Hour), func(snapshot *usageLogRepository) (float64, error) {
			var amount float64
			err := scanSingleRow(ctx, snapshot.sql, `SELECT COALESCE(SUM(actual_cost),0) FROM usage_logs`, nil, &amount)
			return amount, err
		})
		waitingReader <- err
	}()
	select {
	case err := <-waitingReader:
		t.Fatalf("reader passed an uncommitted physical drop: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-waitingReader, ErrUsageHistoryUnavailable)

}
