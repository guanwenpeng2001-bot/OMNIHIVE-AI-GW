//go:build unit || integration

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

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
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
	_, err = db.ExecContext(ctx, "UPDATE workspaces SET is_default=false")
	require.NoError(t, err)
	created, _, err := createAdminUser(cfg)
	require.ErrorContains(t, err, "provision admin workspace")
	require.False(t, created)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count))
	require.Zero(t, count)
	var workspace int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO workspaces(name,is_default) VALUES('Restored',true) RETURNING id").Scan(&workspace))
	require.NotEqualValues(t, 1, workspace)
	created, _, err = createAdminUser(cfg)
	require.NoError(t, err)
	require.True(t, created)
	var role string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT m.role FROM users u JOIN workspace_members m ON m.user_id=u.id WHERE u.email=$1 AND m.workspace_id=$2`, cfg.Admin.Email, workspace).Scan(&role))
	require.Equal(t, "admin", role)
	created, _, err = createAdminUser(cfg)
	require.NoError(t, err)
	require.False(t, created)
	var members int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM workspace_members").Scan(&members))
	require.Equal(t, 1, members)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := repository.NewUserRepository(client, db)
	admin, err := repo.GetByEmail(ctx, cfg.Admin.Email)
	require.NoError(t, err)
	var project int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM projects WHERE workspace_id=$1 AND is_default", workspace).Scan(&project))
	access := identitydb.New(db)
	before, err := access.Evaluate(ctx, admin.ID, workspace, project, "connections.edit")
	require.NoError(t, err)
	admin.Role = service.RoleUser
	require.NoError(t, repo.Update(ctx, admin, service.UserUpdateFields{Role: true}))
	_, err = access.Evaluate(ctx, admin.ID, workspace, project, "connections.edit")
	require.ErrorIs(t, err, fault.Forbidden)
	after, err := access.Evaluate(ctx, admin.ID, workspace, project, "models.read")
	require.NoError(t, err)
	require.Greater(t, after.Version, before.Version)
}
