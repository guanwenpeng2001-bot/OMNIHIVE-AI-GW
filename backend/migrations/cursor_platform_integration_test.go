//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This test only creates and deletes its own throwaway database; it never
// applies migrations against the database named in the administrative DSN.
func TestCursorPlatformMigrationFreshAndUpgrade(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "CURSOR_TEST_POSTGRES_DSN")
	dsn := os.Getenv("CURSOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set CURSOR_TEST_POSTGRES_DSN to run PostgreSQL integration")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := fmt.Sprintf("sub2api_cursor_test_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, `CREATE DATABASE "`+databaseName+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.ExecContext(context.Background(), `DROP DATABASE "`+databaseName+`" WITH (FORCE)`)
		require.NoError(t, err)
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + databaseName
	database, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	defer database.Close()
	require.NoError(t, repository.ApplyMigrations(ctx, database))
	previous, err := migrations.FS.ReadFile("238_opencode_go_platform.sql")
	require.NoError(t, err)
	current, err := migrations.FS.ReadFile("239_cursor_platform.sql")
	require.NoError(t, err)
	// Simulate the previous CHECK set and upgrade it twice, then run the normal
	// migration runner again to verify ledger-based idempotence as well.
	_, err = database.ExecContext(ctx, string(previous))
	require.NoError(t, err)
	for range 2 {
		_, err = database.ExecContext(ctx, string(current))
		require.NoError(t, err)
	}
	require.NoError(t, repository.ApplyMigrations(ctx, database))
	for _, constraint := range []string{"user_platform_quotas_platform_check", "composite_model_routes_target_platform_check", "channel_monitors_provider_check", "channel_monitor_request_templates_provider_check"} {
		var definition string
		require.NoError(t, database.QueryRowContext(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1", constraint).Scan(&definition))
		require.Contains(t, definition, "cursor")
		require.Contains(t, definition, "opencode_go")
		require.Contains(t, definition, "minimax")
	}
}
