//go:build integration

package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Configure only a caller-owned disposable PostgreSQL container/database.
// OpenSchema additionally isolates each invocation and cleans up that schema.
func TestCopilotNousIdentityMigrationPostgres(test *testing.T) {
	dsn := os.Getenv("COMPATIBLE_PROVIDER_TEST_POSTGRES_DSN")
	if dsn == "" {
		test.Skip("COMPATIBLE_PROVIDER_TEST_POSTGRES_DSN must point to a task-owned disposable PostgreSQL instance")
	}
	require.NoError(test, testresources.ValidatePostgres(dsn, "job_test"))
	database := testresources.OpenSchema(test, dsn)
	ctx := context.Background()
	// Minimal fixtures reproduce the affected columns; unrelated application
	// tables, foreign keys and the full historical chain are outside this test.
	_, err := database.ExecContext(ctx, `
 CREATE TABLE user_platform_quotas(platform varchar(32) NOT NULL, marker text NOT NULL);
 CREATE TABLE composite_model_routes(target_platform varchar(50) NOT NULL, marker text NOT NULL);
 CREATE TABLE channel_monitors(provider varchar(20) NOT NULL, marker text NOT NULL);
 CREATE TABLE channel_monitor_request_templates(provider varchar(20) NOT NULL, marker text NOT NULL);`)
	require.NoError(test, err)
	prior := compatibleIdentitySQL(test, compatibleIdentityMigration)
	_, err = database.ExecContext(ctx, prior)
	require.NoError(test, err)
	for _, constraint := range compatibleIdentityConstraints {
		for _, id := range compatibleIdentityValues(test, prior, constraint.name) {
			_, err = database.ExecContext(ctx, "INSERT INTO "+constraint.table+" VALUES($1,$2)", id, "prior:"+id)
			require.NoError(test, err)
		}
	}
	body := compatibleIdentitySQL(test, "284_copilot_nous_identities.sql")
	for repeat := 0; repeat < 2; repeat++ {
		// The production migrator executes ordinary migration files transactionally.
		transaction, err := database.BeginTx(ctx, nil)
		require.NoError(test, err)
		_, err = transaction.ExecContext(ctx, body)
		if err != nil {
			_ = transaction.Rollback()
		}
		require.NoError(test, err)
		require.NoError(test, transaction.Commit())
		for _, constraint := range compatibleIdentityConstraints {
			test.Run(constraint.table+[]string{"/first", "/repeat"}[repeat], func(test *testing.T) {
				before := compatibleIdentityValues(test, prior, constraint.name)
				for _, id := range before {
					var marker string
					require.NoError(test, database.QueryRowContext(ctx, "SELECT marker FROM "+constraint.table+" WHERE "+constraint.column+"=$1", id).Scan(&marker))
					require.Equal(test, "prior:"+id, marker)
				}
				for _, id := range strings.Fields("github_copilot nous_research") {
					if repeat == 0 {
						_, err := database.ExecContext(ctx, "INSERT INTO "+constraint.table+" VALUES($1,$2)", id, "added:"+id)
						require.NoError(test, err, id)
					}
					var marker string
					require.NoError(test, database.QueryRowContext(ctx, "SELECT marker FROM "+constraint.table+" WHERE "+constraint.column+"=$1", id).Scan(&marker))
					require.Equal(test, "added:"+id, marker)
				}
				for _, id := range []string{"unknown", "composite", "relaycode", "etok", "A6API", ""} {
					_, err := database.ExecContext(ctx, "INSERT INTO "+constraint.table+" VALUES($1,'rejected')", id)
					var postgresError *pq.Error
					require.ErrorAs(test, err, &postgresError, id)
					require.Equal(test, pq.ErrorCode("23514"), postgresError.Code)
					require.Equal(test, constraint.name, postgresError.Constraint)
				}
				var count int
				require.NoError(test, database.QueryRowContext(ctx, "SELECT count(*) FROM "+constraint.table).Scan(&count))
				require.Equal(test, len(before)+2, count)
				var definition string
				require.NoError(test, database.QueryRowContext(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1 AND connamespace=current_schema()::regnamespace", constraint.name).Scan(&definition))
				// PostgreSQL rewrites IN into ANY; compare the persisted finite values.
				for _, id := range append(before, strings.Fields("github_copilot nous_research")...) {
					require.Contains(test, definition, "'"+id+"'")
				}
			})
		}
	}
}
