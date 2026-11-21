package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceMigrationAndRepositoryIsolation(t *testing.T) {
	dsn := os.Getenv("WORKSPACE_TEST_DSN")
	if dsn == "" {
		t.Skip("WORKSPACE_TEST_DSN must point to disposable PostgreSQL")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, database, migrations.FS))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := NewWorkspaceRepository(database)
	fixture := uniqueName("workspace-isolation")
	workspace, err := repository.Create(ctx, fixture)
	require.NoError(t, err)
	defer func() {
		for _, table := range []string{"accounts", "groups", "proxies"} {
			_, err := database.ExecContext(ctx, "DELETE FROM "+table+" WHERE name=$1", fixture)
			require.NoError(t, err)
		}
		_, err := database.ExecContext(ctx, "DELETE FROM workspaces WHERE id=$1", workspace.ID)
		require.NoError(t, err)
	}()
	scoped := domain.WithWorkspaceScope(ctx, workspace.ID)
	other := domain.WithWorkspaceScope(ctx, domain.DefaultWorkspaceID)
	accountRepo := NewAccountRepository(client, database, nil)
	groupRepo := NewGroupRepository(client, database)
	proxyRepo := NewProxyRepository(client, database)
	for _, resourceContext := range []context.Context{other, scoped} {
		account := &service.Account{Name: fixture, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Concurrency: 1}
		require.NoError(t, accountRepo.Create(resourceContext, account))
		group := &service.Group{Name: fixture, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
		require.NoError(t, groupRepo.Create(resourceContext, group))
		proxy := &service.Proxy{Name: fixture, Protocol: "http", Host: "127.0.0.1", Port: 8081, Status: service.StatusActive}
		require.NoError(t, proxyRepo.Create(resourceContext, proxy))
	}
	accounts, page, err := accountRepo.List(scoped, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, accounts, 1)
	require.Equal(t, workspace.ID, accounts[0].WorkspaceID)
	_, err = accountRepo.GetByID(other, accounts[0].ID)
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	_, err = accountRepo.GetByID(other, 99999999)
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	// Mutating through a foreign scope cannot affect an existing resource.
	require.Error(t, accountRepo.Delete(other, accounts[0].ID))
	_, err = accountRepo.GetByID(scoped, accounts[0].ID)
	require.NoError(t, err)
	// SQL migration is safe to run again after explicit ownership exists.
	migration, err := migrations.FS.ReadFile("267_add_workspace_resource_scope.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = database.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	var owner int64
	require.NoError(t, database.QueryRowContext(ctx, "SELECT workspace_id FROM accounts WHERE id=$1", accounts[0].ID).Scan(&owner))
	require.Equal(t, workspace.ID, owner)
	var defaultCount int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM workspaces WHERE is_default").Scan(&defaultCount))
	require.Equal(t, 1, defaultCount)
	var foreignGroup int64
	require.NoError(t, database.QueryRowContext(ctx, "SELECT id FROM groups WHERE workspace_id=$1 AND name=$2", domain.DefaultWorkspaceID, fixture).Scan(&foreignGroup))
	_, err = database.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)", accounts[0].ID, foreignGroup)
	require.Error(t, err, "cross-workspace references must be rejected even outside HTTP")
	_, err = database.ExecContext(ctx, "UPDATE accounts SET workspace_id=1 WHERE id=$1", accounts[0].ID)
	require.Error(t, err, "ownership cannot be reassigned by a stale writer")
}

func TestWorkspaceLegacyBackfillBatchesAreIdempotent(t *testing.T) {
	dsn := os.Getenv("WORKSPACE_TEST_DSN")
	if dsn == "" {
		t.Skip("WORKSPACE_TEST_DSN must point to disposable PostgreSQL")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	transaction, err := database.Begin()
	require.NoError(t, err)
	defer func() { _ = transaction.Rollback() }()
	_, err = transaction.Exec(`CREATE SCHEMA workspace_backfill_contract;
        SET LOCAL search_path TO workspace_backfill_contract;
        CREATE TABLE users(id BIGINT PRIMARY KEY);
        CREATE TABLE accounts(id BIGINT PRIMARY KEY,proxy_id BIGINT,proxy_fallback_origin_id BIGINT,parent_account_id BIGINT);
        CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,deleted_at TIMESTAMPTZ,fallback_group_id BIGINT,fallback_group_id_on_invalid_request BIGINT,model_routing JSONB);
        CREATE TABLE channels(id BIGINT PRIMARY KEY,name TEXT);
        CREATE TABLE proxies(id BIGINT PRIMARY KEY,backup_proxy_id BIGINT);
        CREATE TABLE channel_monitors(id BIGINT PRIMARY KEY,account_id BIGINT,template_id BIGINT);
        CREATE TABLE channel_monitor_request_templates(id BIGINT PRIMARY KEY,provider TEXT,name TEXT);
        CREATE TABLE api_keys(id BIGINT PRIMARY KEY,group_id BIGINT);
        CREATE TABLE account_groups(account_id BIGINT,group_id BIGINT);
        CREATE TABLE channel_groups(channel_id BIGINT,group_id BIGINT);
        INSERT INTO accounts(id) SELECT generate_series(1,2005);
        INSERT INTO groups(id,name) VALUES(1,'legacy');
        INSERT INTO channels(id,name) VALUES(1,'legacy');
        INSERT INTO proxies(id) VALUES(1);
        INSERT INTO channel_monitors(id) VALUES(1);
        INSERT INTO channel_monitor_request_templates(id,provider,name) VALUES(1,'openai','legacy');
        INSERT INTO api_keys(id) VALUES(1);`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("267_add_workspace_resource_scope.sql")
	require.NoError(t, err)
	_, err = transaction.Exec(string(migration))
	require.NoError(t, err)
	var backfilled int
	require.NoError(t, transaction.QueryRow("SELECT count(*) FROM accounts WHERE workspace_id=1").Scan(&backfilled))
	require.Equal(t, 2005, backfilled, "legacy rows span three bounded update batches")
	for _, table := range []string{"groups", "channels", "proxies", "channel_monitors", "channel_monitor_request_templates", "api_keys"} {
		var workspaceID int64
		require.NoError(t, transaction.QueryRow("SELECT workspace_id FROM "+table+" WHERE id=1").Scan(&workspaceID))
		require.Equal(t, int64(1), workspaceID, table)
	}
	_, err = transaction.Exec(`INSERT INTO workspaces(id,name) VALUES(2,'New domain');
        INSERT INTO accounts(id,workspace_id) VALUES(3000,2);
        INSERT INTO accounts(id) VALUES(3001);`)
	require.NoError(t, err)
	for range 2 {
		_, err = transaction.Exec(string(migration))
		require.NoError(t, err)
	}
	var workspaceID int64
	require.NoError(t, transaction.QueryRow("SELECT workspace_id FROM accounts WHERE id=3000").Scan(&workspaceID))
	require.Equal(t, int64(2), workspaceID)
	require.NoError(t, transaction.QueryRow("SELECT workspace_id FROM accounts WHERE id=3001").Scan(&workspaceID))
	require.Equal(t, int64(1), workspaceID, "legacy writers retain the default domain during rollout")
}

func TestWorkspaceMemberRateReadsAndClearsShareTheSameScope(t *testing.T) {
	dsn := os.Getenv("WORKSPACE_TEST_DSN")
	if dsn == "" {
		t.Skip("WORKSPACE_TEST_DSN must point to disposable PostgreSQL")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	transaction, err := database.Begin()
	require.NoError(t, err)
	defer func() { _ = transaction.Rollback() }()
	_, err = transaction.Exec(`CREATE SCHEMA workspace_member_rates_contract;
        SET LOCAL search_path TO workspace_member_rates_contract;
        CREATE TABLE users(id BIGINT PRIMARY KEY,username TEXT,email TEXT,notes TEXT,status TEXT,deleted_at TIMESTAMPTZ);
        CREATE TABLE workspace_members(workspace_id BIGINT,user_id BIGINT);
        CREATE TABLE user_group_rate_multipliers(user_id BIGINT,group_id BIGINT,rate_multiplier NUMERIC,rpm_override INTEGER,updated_at TIMESTAMPTZ);
        INSERT INTO users VALUES(21,'member','member@example.invalid','','active',NULL),(31,'outside','outside@example.invalid','','active',NULL);
        INSERT INTO workspace_members VALUES(2,21);
        INSERT INTO user_group_rate_multipliers VALUES(21,5,0.5,10,NOW()),(31,5,0.7,20,NOW());`)
	require.NoError(t, err)
	repository := &userGroupRateRepository{sql: transaction}
	ctx := domain.WithWorkspaceScope(context.Background(), 2)
	entries, err := repository.GetByGroupID(ctx, 5)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, int64(21), entries[0].UserID)
	require.NoError(t, repository.SyncGroupRateMultipliers(ctx, 5, nil))
	require.NoError(t, repository.ClearGroupRPMOverrides(ctx, 5))
	require.NoError(t, repository.DeleteByGroupID(ctx, 5))
	var remaining int
	require.NoError(t, transaction.QueryRow("SELECT count(*) FROM user_group_rate_multipliers WHERE user_id=31 AND rate_multiplier=0.7 AND rpm_override=20").Scan(&remaining))
	require.Equal(t, 1, remaining, "a workspace clear must not change an unreadable member's configuration")
}
