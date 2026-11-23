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

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSettingAuthorityMigrationPreservesExplicitValues(t *testing.T) {
	testresources.RequireDisposableDatabase(t, "BE6_SETTING_MIGRATION_DSN")
	dsn := os.Getenv("BE6_SETTING_MIGRATION_DSN")
	if dsn == "" {
		t.Skip("set BE6_SETTING_MIGRATION_DSN to run the isolated migration test")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	databaseName := fmt.Sprintf("omnihive_be6_%d", time.Now().UnixNano())
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
	_, err = database.ExecContext(ctx, `CREATE TABLE users (id BIGSERIAL PRIMARY KEY); CREATE TABLE settings (id BIGSERIAL PRIMARY KEY, key VARCHAR(100) NOT NULL UNIQUE, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES ('default_balance','0'),('default_concurrency','0'),('model_plaza_enabled','true'),('model_plaza_description',''),('registration_enabled','false')`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("341_setting_authority_and_defaults.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = database.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	for key, expected := range map[string]string{
		"default_balance": "0", "default_concurrency": "0", "model_plaza_enabled": "true",
		"model_plaza_description": "", "registration_enabled": "false",
	} {
		var actual string
		require.NoError(t, database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&actual))
		require.Equal(t, expected, actual)
	}
	var plazaSeed string
	require.NoError(t, database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='model_plaza_require_auth'`).Scan(&plazaSeed))
	require.Equal(t, "false", plazaSeed)
}
