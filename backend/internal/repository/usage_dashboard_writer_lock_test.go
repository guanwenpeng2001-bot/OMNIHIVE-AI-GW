//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestUsageDashboardWriterLocks(t *testing.T) {
	dsn := os.Getenv("USAGE_STATS_TEST_DSN")
	if dsn == "" {
		t.Skip("set USAGE_STATS_TEST_DSN to a disposable stats_test database")
	}
	for _, layout := range []string{"ordinary", "partition_parent", "partition_direct"} {
		t.Run(layout, func(t *testing.T) {
			database, err := sql.Open("postgres", dsn)
			require.NoError(t, err)
			t.Cleanup(func() { _ = database.Close() })
			var databaseName string
			require.NoError(t, database.QueryRow(`SELECT current_database()`).Scan(&databaseName))
			require.Equal(t, "stats_test", databaseName)
			_, err = database.Exec(`CREATE SCHEMA stats_writer_locks`)
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = database.Exec(`DROP SCHEMA stats_writer_locks CASCADE`) })
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			isolatedDSN := dsn + " search_path=stats_writer_locks"
			if parsed.Scheme == "postgres" || parsed.Scheme == "postgresql" {
				query := parsed.Query()
				query.Set("search_path", "stats_writer_locks")
				parsed.RawQuery = query.Encode()
				isolatedDSN = parsed.String()
			}
			isolated, err := sql.Open("postgres", isolatedDSN)
			require.NoError(t, err)
			t.Cleanup(func() { _ = isolated.Close() })
			execute := func(query string) {
				t.Helper()
				_, executeErr := isolated.Exec(query)
				require.NoError(t, executeErr)
			}
			usageTable := `CREATE TABLE usage_logs(id bigserial, group_id bigint, actual_cost numeric DEFAULT 1, created_at timestamptz NOT NULL)`
			if layout != "ordinary" {
				usageTable += ` PARTITION BY RANGE(created_at); CREATE TABLE usage_logs_default PARTITION OF usage_logs DEFAULT`
			}
			execute(usageTable)
			execute(`CREATE TABLE usage_dashboard_aggregation_watermark(id smallint PRIMARY KEY, last_aggregated_at timestamptz, updated_at timestamptz);
INSERT INTO usage_dashboard_aggregation_watermark VALUES(1,'1970-01-01',NOW())`)
			for _, migrationName := range []string{"222_group_usage_daily_rollups.sql", "223_group_usage_rollup_timezone.sql", "258_usage_dashboard_coverage.sql", "279_usage_history_origin.sql"} {
				migrationSQL, readErr := migrations.FS.ReadFile(migrationName)
				require.NoError(t, readErr)
				execute(string(migrationSQL))
			}
			tableName := "usage_logs"
			if layout == "partition_direct" {
				tableName = "usage_logs_default"
			}
			insert := `INSERT INTO ` + tableName + `(group_id,created_at) VALUES(NULL,'2026-03-02 12:00Z'),(7,'2026-03-02 13:00Z')`
			reset := func() {
				execute(`UPDATE usage_group_rollup_state SET closed_before='2026-03-02'; UPDATE usage_dashboard_aggregation_watermark SET last_aggregated_at='2026-03-02',dirty_from=NULL`)
			}
			begin := func(t *testing.T) *sql.Tx {
				t.Helper()
				transaction, beginErr := isolated.BeginTx(context.Background(), nil)
				require.NoError(t, beginErr)
				t.Cleanup(func() { _ = transaction.Rollback() })
				_, beginErr = transaction.Exec(`SET LOCAL statement_timeout='2s'; SET LOCAL TIME ZONE 'UTC'`)
				require.NoError(t, beginErr)
				return transaction
			}
			t.Run("independent_batches_do_not_wait", func(t *testing.T) {
				reset()
				firstWriter := begin(t)
				_, err := firstWriter.Exec(insert)
				require.NoError(t, err)
				secondWriter := begin(t)
				_, err = secondWriter.Exec(insert)
				require.NoError(t, err, "an unrelated batch must finish while the first transaction remains open")
				require.NoError(t, secondWriter.Commit())
				require.NoError(t, firstWriter.Commit())
				var dirty sql.NullTime
				require.NoError(t, isolated.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.False(t, dirty.Valid)
			})
			t.Run("publisher_waits_for_writer", func(t *testing.T) {
				reset()
				writer := begin(t)
				_, err := writer.Exec(insert)
				require.NoError(t, err)
				publisher := begin(t)
				_, err = publisher.Exec(`SET LOCAL statement_timeout='200ms'; SELECT id FROM usage_group_rollup_state WHERE id=1 FOR UPDATE`)
				require.ErrorContains(t, err, "statement timeout")
				require.NoError(t, publisher.Rollback())
				require.NoError(t, writer.Rollback())
			})
			t.Run("concurrent_batch_load", func(t *testing.T) {
				reset()
				const writerCount = 16
				transactions := make([]*sql.Tx, writerCount)
				for index := range transactions {
					transactions[index] = begin(t)
				}
				finished := make(chan error, writerCount)
				for _, transaction := range transactions {
					go func() {
						_, insertErr := transaction.Exec(`INSERT INTO ` + tableName + `(group_id,created_at) SELECT CASE WHEN ordinal%2=0 THEN 7 END,'2026-03-02 12:00Z'::timestamptz FROM generate_series(1,128) ordinal`)
						finished <- insertErr
					}()
				}
				// All 2048 inserts must finish before any writer releases its locks.
				for range writerCount {
					require.NoError(t, <-finished)
				}
				for _, transaction := range transactions {
					require.NoError(t, transaction.Commit())
				}
			})
			t.Run("waiting_writer_invalidates_new_publication", func(t *testing.T) {
				reset()
				publisher := begin(t)
				require.NoError(t, lockGroupUsageRollupState(context.Background(), publisher))
				_, err := publisher.Exec(`SELECT id FROM usage_dashboard_aggregation_watermark WHERE id=1 FOR UPDATE; UPDATE usage_dashboard_aggregation_watermark SET last_aggregated_at='2026-03-03'`)
				require.NoError(t, err)
				writer := begin(t)
				finished := make(chan error, 1)
				go func() { _, insertErr := writer.Exec(insert); finished <- insertErr }()
				select {
				case err := <-finished:
					t.Fatalf("writer passed uncommitted publication: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				require.NoError(t, publisher.Commit())
				require.NoError(t, <-finished)
				require.NoError(t, writer.Commit())
				var dirty time.Time
				require.NoError(t, isolated.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.Equal(t, time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC), dirty.UTC())
			})
			t.Run("historical_correction_with_open_normal_writer", func(t *testing.T) {
				reset()
				writer := begin(t)
				_, err := writer.Exec(insert)
				require.NoError(t, err)
				correction := begin(t)
				_, err = correction.Exec(`UPDATE ` + tableName + ` SET created_at='2026-03-01 12:00Z',actual_cost=3 WHERE id=(SELECT MIN(id) FROM ` + tableName + ` WHERE group_id=7)`)
				require.NoError(t, err, "correction lock upgrades must remain compatible with a normal writer")
				require.NoError(t, correction.Commit())
				require.NoError(t, writer.Commit())
				var dirty time.Time
				require.NoError(t, isolated.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.Equal(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), dirty.UTC())
				var groupClosed string
				require.NoError(t, isolated.QueryRow(`SELECT closed_before::text FROM usage_group_rollup_state`).Scan(&groupClosed))
				require.Equal(t, "2026-03-01", groupClosed)
			})
			t.Run("corrections_upgrade_without_deadlock", func(t *testing.T) {
				reset()
				firstWriter := begin(t)
				secondWriter := begin(t)
				_, err := firstWriter.Exec(insert)
				require.NoError(t, err)
				_, err = secondWriter.Exec(insert)
				require.NoError(t, err)
				_, err = firstWriter.Exec(`INSERT INTO ` + tableName + `(group_id,created_at) VALUES(7,'2026-03-01 12:00Z')`)
				require.NoError(t, err)
				finished := make(chan error, 1)
				go func() {
					_, insertErr := secondWriter.Exec(`INSERT INTO ` + tableName + `(group_id,created_at) VALUES(7,'2026-02-28 12:00Z')`)
					finished <- insertErr
				}()
				select {
				case err := <-finished:
					t.Fatalf("correction passed uncommitted watermark change: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				require.NoError(t, firstWriter.Commit())
				require.NoError(t, <-finished)
				require.NoError(t, secondWriter.Commit())
				var dirty time.Time
				require.NoError(t, isolated.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.Equal(t, time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC), dirty.UTC())
			})
			t.Run("origin_revocations_serialize_before_first_publication", func(test *testing.T) {
				execute(`UPDATE usage_group_rollup_state SET closed_before='1970-01-01'; UPDATE usage_dashboard_aggregation_watermark SET last_aggregated_at='1970-01-01',dirty_from=NULL,history_known_empty_before='2026-03-02'`)
				firstWriter := begin(test)
				secondWriter := begin(test)
				_, err := firstWriter.Exec(insert)
				require.NoError(test, err)
				_, err = secondWriter.Exec(insert)
				require.NoError(test, err)
				_, err = firstWriter.Exec(`INSERT INTO ` + tableName + `(group_id,created_at) VALUES(NULL,'2026-03-01')`)
				require.NoError(test, err, "revocation coexists with an open normal writer")
				finished := make(chan error, 1)
				go func() {
					_, err := secondWriter.Exec(`INSERT INTO ` + tableName + `(group_id,created_at) VALUES(NULL,'2026-02-28')`)
					finished <- err
				}()
				require.NoError(test, firstWriter.Commit())
				require.NoError(test, <-finished)
				require.NoError(test, secondWriter.Commit())
				var origin, dirty sql.NullTime
				require.NoError(test, isolated.QueryRow(`SELECT history_known_empty_before,dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&origin, &dirty))
				require.False(test, origin.Valid)
				require.False(test, dirty.Valid)
			})
			t.Run("delete_rollback_and_verified_retention", func(t *testing.T) {
				reset()
				correction := begin(t)
				_, err := correction.Exec(`DELETE FROM ` + tableName + ` WHERE created_at<'2026-03-02'`)
				require.NoError(t, err)
				var dirty sql.NullTime
				require.NoError(t, correction.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.True(t, dirty.Valid)
				require.NoError(t, correction.Rollback())
				require.NoError(t, isolated.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.False(t, dirty.Valid)
				retention := begin(t)
				require.NoError(t, lockGroupUsageRollupState(context.Background(), retention))
				_, err = retention.Exec(`SELECT id FROM usage_dashboard_aggregation_watermark WHERE id=1 FOR UPDATE; SET LOCAL sub2api.usage_retention='on'; DELETE FROM ` + tableName + ` WHERE created_at<'2026-03-02'`)
				require.NoError(t, err)
				require.NoError(t, retention.QueryRow(`SELECT dirty_from FROM usage_dashboard_aggregation_watermark`).Scan(&dirty))
				require.False(t, dirty.Valid)
				waitingWriter := begin(t)
				_, err = waitingWriter.Exec(`SET LOCAL statement_timeout='200ms'; ` + insert)
				require.ErrorContains(t, err, "statement timeout")
				require.NoError(t, waitingWriter.Rollback())
				require.NoError(t, retention.Rollback())
			})
		})
	}
}
