//go:build integration

package setup

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestInitialAdminDefaultWorkspaceOnboarding(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to disposable job_test")
	}
	require.NoError(t, testresources.ValidatePostgres(dsn, "job_test"))
	adminDB, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, adminDB.Close()) })
	name := "job_test_t2_setup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = adminDB.ExecContext(t.Context(), `CREATE DATABASE "`+name+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := adminDB.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		require.NoError(t, err)
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	// Cleanup closes the test connection before dropping its invocation-owned database.
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	password, _ := parsed.User.Password()
	cfg := &SetupConfig{Database: DatabaseConfig{Host: parsed.Hostname(), Port: port, User: parsed.User.Username(), Password: password, DBName: name, SSLMode: "disable"}, Admin: AdminConfig{Email: "admin@onboarding.test", Password: "test-admin-onboarding-password"}}
	created, _, err := createAdminUser(cfg)
	require.NoError(t, err)
	require.True(t, created)
	var role string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT m.role FROM users u JOIN workspace_members m ON m.user_id=u.id WHERE u.email=$1 AND m.workspace_id=1`, cfg.Admin.Email).Scan(&role))
	require.Equal(t, "admin", role)
	created, _, err = createAdminUser(cfg)
	require.NoError(t, err)
	require.False(t, created)
	var members int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM workspace_members").Scan(&members))
	require.Equal(t, 1, members)
}
