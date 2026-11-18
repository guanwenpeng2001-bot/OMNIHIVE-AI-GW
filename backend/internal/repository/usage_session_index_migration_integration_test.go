//go:build integration

package repository

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestUsageSessionIndexMigrationSupportsPartitionTrees(t *testing.T) {
	for _, layout := range []string{"ordinary", "partitioned", "nested", "empty_parent", "incomplete_parent", "invalid_leaf", "unready_leaf"} {
		t.Run(layout, func(t *testing.T) {
			database := apiKeyCredentialMigrationDatabase(t)
			definition := `CREATE TABLE usage_logs (session_id varchar(256), created_at timestamptz NOT NULL)`
			if layout != "ordinary" {
				definition += ` PARTITION BY RANGE (created_at)`
			}
			_, err := database.Exec(definition)
			require.NoError(t, err)
			if layout != "ordinary" && layout != "empty_parent" {
				partition := `CREATE TABLE usage_logs_history PARTITION OF usage_logs DEFAULT`
				if layout == "nested" {
					partition += ` PARTITION BY HASH (session_id)`
				}
				_, err = database.Exec(partition)
				require.NoError(t, err)
				if layout == "nested" {
					_, err = database.Exec(`CREATE TABLE usage_logs_history_leaf PARTITION OF usage_logs_history FOR VALUES WITH (MODULUS 1, REMAINDER 0)`)
					require.NoError(t, err)
				}
			}
			if layout != "empty_parent" {
				_, err = database.Exec(`INSERT INTO usage_logs VALUES ('session-existing', NOW()), (NULL, NOW())`)
				require.NoError(t, err)
			}
			if layout == "incomplete_parent" {
				_, err = database.Exec(`CREATE INDEX idx_usage_logs_session_id ON ONLY usage_logs (session_id) WHERE session_id IS NOT NULL`)
				require.NoError(t, err)
			}
			if layout == "invalid_leaf" || layout == "unready_leaf" {
				_, err = database.Exec(`CREATE INDEX idx_usage_logs_session_id ON usage_logs (session_id) WHERE session_id IS NOT NULL;
					UPDATE pg_index SET indisvalid = FALSE WHERE indrelid = 'usage_logs_history'::regclass;
					UPDATE pg_index SET indisvalid = FALSE WHERE indexrelid = 'idx_usage_logs_session_id'::regclass`)
				require.NoError(t, err)
				if layout == "unready_leaf" {
					_, err = database.Exec(`UPDATE pg_index SET indisready = FALSE WHERE indrelid = 'usage_logs_history'::regclass`)
					require.NoError(t, err)
				}
			}
			body, err := migrations.FS.ReadFile(usageLogsSessionIDIndexMigration)
			require.NoError(t, err)
			migrationFiles := fstest.MapFS{usageLogsSessionIDIndexMigration: &fstest.MapFile{Data: body}}
			require.NoError(t, applyMigrationsFS(context.Background(), database, migrationFiles))
			var valid, ready bool
			require.NoError(t, database.QueryRow(`SELECT indisvalid, indisready FROM pg_index WHERE indexrelid = 'idx_usage_logs_session_id'::regclass`).Scan(&valid, &ready))
			require.True(t, valid)
			require.True(t, ready)
			var missing int
			require.NoError(t, database.QueryRow(`WITH RECURSIVE relations AS (
				SELECT 'usage_logs'::regclass::oid AS relation_id
				UNION ALL SELECT inheritance.inhrelid FROM pg_inherits inheritance JOIN relations ON inheritance.inhparent = relations.relation_id
			) SELECT count(*) FROM relations WHERE NOT EXISTS (
				SELECT 1 FROM pg_index WHERE indrelid = relation_id AND indisvalid AND indisready
				AND pg_get_indexdef(indexrelid, 1, true) = 'session_id'
				AND pg_get_expr(indpred, indrelid) = '(session_id IS NOT NULL)'
			)`).Scan(&missing))
			require.Zero(t, missing, "every parent and leaf must have the complete session index")
			require.NoError(t, applyMigrationsFS(context.Background(), database, migrationFiles), "completed migrations must be idempotent")
			_, err = database.Exec(`DELETE FROM schema_migrations WHERE filename = $1`, usageLogsSessionIDIndexMigration)
			require.NoError(t, err)
			require.NoError(t, applyMigrationsFS(context.Background(), database, migrationFiles), "retry after index completion but before ledger commit must succeed")
		})
	}
}

func TestUsageSessionIndexMigrationRejectsWrongExistingDefinition(t *testing.T) {
	for _, partitioned := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "partitioned"}[partitioned], func(t *testing.T) {
			database := apiKeyCredentialMigrationDatabase(t)
			definition := `CREATE TABLE usage_logs (session_id varchar(256), created_at timestamptz NOT NULL)`
			if partitioned {
				definition += ` PARTITION BY RANGE (created_at)`
			}
			_, err := database.Exec(definition)
			require.NoError(t, err)
			_, err = database.Exec(`CREATE INDEX idx_usage_logs_session_id ON usage_logs (created_at)`)
			require.NoError(t, err)
			body, err := migrations.FS.ReadFile(usageLogsSessionIDIndexMigration)
			require.NoError(t, err)
			migrationFiles := fstest.MapFS{usageLogsSessionIDIndexMigration: &fstest.MapFile{Data: body}}
			err = applyMigrationsFS(context.Background(), database, migrationFiles)
			require.ErrorContains(t, err, "does not match")
			var recorded int
			require.NoError(t, database.QueryRow(`SELECT count(*) FROM schema_migrations WHERE filename = $1`, usageLogsSessionIDIndexMigration).Scan(&recorded))
			require.Zero(t, recorded)
		})
	}
}
