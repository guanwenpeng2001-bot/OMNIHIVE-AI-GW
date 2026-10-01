//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/migrate"
	"github.com/Wei-Shaw/sub2api/ent/proxygroup"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func proxyGroupTestPostgres(t *testing.T) (*sql.DB, *dbent.Client) {
	t.Helper()
	if os.Getenv("IP_GROUP_DEDICATED_TESTS") != "1" {
		t.Skip("requires dedicated job_test on 127.0.0.1:55439")
	}
	password := os.Getenv("IP_GROUP_TEST_PASSWORD")
	require.NotEmpty(t, password, "dedicated test password must be supplied by the test runner")
	connection := url.URL{Scheme: "postgres", Host: "127.0.0.1:55439", Path: "/job_test", User: url.UserPassword("codex_test_admin", password), RawQuery: "sslmode=disable&connect_timeout=5"}
	db, err := testresources.OpenPostgres(context.Background(), connection.String(), "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	migration, err := migrations.FS.ReadFile("280_proxy_groups.sql")
	require.NoError(t, err)
	// Only the additive migration under test is executed, never old migration backfills.
	for i := 0; i < 2; i++ {
		_, err = db.Exec(string(migration))
		require.NoError(t, err, "migration must be idempotent")
	}
	return db, client
}

func TestProxyGroupRepositoryDedicatedPostgres(t *testing.T) {
	db, client := proxyGroupTestPostgres(t)
	for _, table := range []string{migrate.ProxyGroupsTable.Name, migrate.ProxyGroupMembersTable.Name} {
		columns, err := db.Query("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1", table)
		require.NoError(t, err)
		actual := map[string]bool{}
		for columns.Next() {
			var name string
			require.NoError(t, columns.Scan(&name))
			actual[name] = true
		}
		require.NoError(t, columns.Err())
		require.NoError(t, columns.Close())
		expected := migrate.ProxyGroupsTable.Columns
		if table == migrate.ProxyGroupMembersTable.Name {
			expected = migrate.ProxyGroupMembersTable.Columns
		}
		require.Len(t, actual, len(expected))
		for _, column := range expected {
			require.True(t, actual[column.Name], "migration missing generated field %s.%s", table, column.Name)
		}
	}
	var nullable string
	var defaultValue sql.NullString
	require.NoError(t, db.QueryRow("SELECT is_nullable,column_default FROM information_schema.columns WHERE table_schema='public' AND table_name='accounts' AND column_name='proxy_group_id'").Scan(&nullable, &defaultValue))
	require.Equal(t, "YES", nullable)
	require.False(t, defaultValue.Valid, "new accounts have no default group binding")
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := dbent.NewTxContext(context.Background(), tx)
	c := tx.Client()
	workspace, err := c.Workspace.Create().SetName("ip-group-test-" + uuid.NewString()).Save(ctx)
	require.NoError(t, err)
	otherWorkspace, err := c.Workspace.Create().SetName("ip-group-other-" + uuid.NewString()).Save(ctx)
	require.NoError(t, err)
	ctx = domain.WithWorkspaceScope(ctx, workspace.ID)
	otherCtx := domain.WithWorkspaceScope(ctx, otherWorkspace.ID)
	repo := NewProxyGroupRepository(client)
	accounts := newAccountRepositoryWithSQL(client, db, nil)
	createProxy := func(ctx context.Context, name, status string, expired bool) *dbent.Proxy {
		b := c.Proxy.Create().SetName(name).SetHost("fixture.invalid").SetPort(8080).SetProtocol("http").SetStatus(status)
		if expired {
			b.SetExpiresAt(time.Now().Add(-time.Hour))
		}
		p, err := b.Save(ctx)
		require.NoError(t, err)
		return p
	}
	active := createProxy(ctx, "active", "active", false)
	inactive := createProxy(ctx, "inactive", "inactive", false)
	expired := createProxy(ctx, "expired", "active", true)
	foreign := createProxy(otherCtx, "foreign", "active", false)
	group := &service.ProxyGroup{Name: "test-group", Description: "description", Region: "region", TargetServices: []string{"ChatGPT", "Grok"}}
	require.NoError(t, repo.Create(ctx, group))
	require.Equal(t, workspace.ID, group.WorkspaceID)
	require.Equal(t, 86400, group.BindingTTLSeconds)
	require.Equal(t, "active", group.Status)
	require.NoError(t, repo.ReplaceMembers(ctx, group.ID, []int64{active.ID, inactive.ID, expired.ID, active.ID}))
	detail, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.Len(t, detail.Members, 3)
	require.Equal(t, group.Description, detail.Description)
	require.Equal(t, group.TargetServices, detail.TargetServices)
	for _, m := range detail.Members {
		require.Equal(t, workspace.ID, m.WorkspaceID)
		require.Equal(t, workspace.ID, m.Proxy.WorkspaceID)
	}
	require.NotNil(t, detail.Members[2].Proxy.ExpiresAt)

	t.Run("scope_and_atomic_member_validation", func(t *testing.T) {
		_, err := repo.GetByID(otherCtx, group.ID)
		require.ErrorIs(t, err, service.ErrProxyGroupNotFound)
		require.ErrorIs(t, repo.ReplaceMembers(ctx, group.ID, []int64{foreign.ID}), service.ErrProxyNotFound)
		require.ErrorIs(t, repo.ReplaceMembers(otherCtx, group.ID, nil), service.ErrProxyGroupNotFound)
		members, err := repo.ListMembers(ctx, group.ID)
		require.NoError(t, err)
		require.Len(t, members, 3)
		require.ErrorIs(t, repo.Delete(otherCtx, group.ID), service.ErrProxyGroupNotFound)
		group.FallbackMode = "proxy"
		group.FallbackProxyID = &foreign.ID
		require.ErrorIs(t, repo.Update(ctx, group), service.ErrProxyNotFound)
		group.FallbackProxyID = &active.ID
		require.NoError(t, repo.Update(ctx, group))
		group.FallbackMode = "none"
		group.FallbackProxyID = nil
		require.NoError(t, repo.Update(ctx, group))
		detail, err := repo.GetByID(ctx, group.ID)
		require.NoError(t, err)
		require.Nil(t, detail.FallbackProxyID)
	})

	t.Run("target_service_filter_before_pagination", func(t *testing.T) {
		second := &service.ProxyGroup{Name: "test-second", TargetServices: []string{"Gemini"}}
		require.NoError(t, repo.Create(ctx, second))
		third := &service.ProxyGroup{Name: "test-third", TargetServices: []string{"ChatGPT"}, Status: "inactive"}
		require.NoError(t, repo.Create(ctx, third))
		rows, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 1}, "", "ChatGPT", "test-")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.EqualValues(t, 2, page.Total)
		rows, page, err = repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 1}, "active", "ChatGPT", "test-")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.EqualValues(t, 1, page.Total)
		_, page, err = repo.List(otherCtx, pagination.DefaultPagination(), "", "", "")
		require.NoError(t, err)
		require.Zero(t, page.Total)
	})

	t.Run("account_logical_mapping_create_update_copy_and_counts", func(t *testing.T) {
		a := &service.Account{Name: "group-account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, ProxyGroupID: &group.ID, Credentials: map[string]any{"api_key": "fixture"}}
		require.NoError(t, accounts.Create(ctx, a))
		require.Nil(t, a.ProxyID)
		got, err := accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, &group.ID, got.ProxyGroupID)
		require.Nil(t, got.ProxyID)
		require.NotNil(t, got.ProxyGroup)
		require.Len(t, got.ProxyGroup.Members, 3)
		batch, err := accounts.GetByIDs(ctx, []int64{a.ID})
		require.NoError(t, err)
		require.Len(t, batch, 1)
		require.NotNil(t, batch[0].ProxyGroup)
		copy := *got
		copy.ID = 0
		copy.Name = "copy-account"
		require.NoError(t, accounts.CreateWithAccountGroups(ctx, &copy, nil))
		require.NoError(t, c.Account.UpdateOneID(copy.ID).SetParentAccountID(a.ID).SetQuotaDimension(account.QuotaDimensionSpark).Exec(ctx))
		n, err := repo.CountAccountsByGroupID(ctx, group.ID)
		require.NoError(t, err)
		require.EqualValues(t, 2, n)
		summaries, err := repo.ListAccountSummariesByGroupID(ctx, group.ID)
		require.NoError(t, err)
		require.Len(t, summaries, 2)
		require.Nil(t, summaries[0].ParentAccountID)
		require.Equal(t, &a.ID, summaries[1].ParentAccountID)
		require.ErrorIs(t, repo.Delete(ctx, group.ID), service.ErrProxyGroupInUse)
		got.ProxyGroupID = nil
		got.ProxyGroup = nil
		got.ProxyID = &active.ID
		require.NoError(t, accounts.Update(ctx, got))
		stored, err := accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.Nil(t, stored.ProxyGroupID)
		require.Nil(t, stored.ProxyGroup)
		require.Equal(t, &active.ID, stored.ProxyID)
		stored.ProxyGroupID = &group.ID
		stored.ProxyID = nil
		require.NoError(t, accounts.Update(ctx, stored))
		stored, err = accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, &group.ID, stored.ProxyGroupID)
		require.Nil(t, stored.ProxyID)
		bad := *a
		bad.ID = 0
		bad.Name = "cross-workspace"
		require.ErrorIs(t, accounts.Create(otherCtx, &bad), service.ErrProxyGroupNotFound)
		require.NoError(t, c.Account.UpdateOneID(copy.ID).SetDeletedAt(time.Now()).Exec(ctx))
		n, err = repo.CountAccountsByGroupID(ctx, group.ID)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		require.NoError(t, c.Account.UpdateOneID(a.ID).ClearProxyGroupID().Exec(ctx))
	})

	t.Run("soft_delete_members_and_group", func(t *testing.T) {
		require.NoError(t, c.Proxy.UpdateOneID(inactive.ID).SetDeletedAt(time.Now()).Exec(ctx))
		members, err := repo.ListMembers(ctx, group.ID)
		require.NoError(t, err)
		require.Len(t, members, 2)
		require.ErrorIs(t, repo.ReplaceMembers(ctx, group.ID, []int64{inactive.ID}), service.ErrProxyNotFound)
		require.NoError(t, repo.Delete(ctx, group.ID))
		_, err = repo.GetByID(ctx, group.ID)
		require.ErrorIs(t, err, service.ErrProxyGroupNotFound)
		row, err := c.ProxyGroup.Query().Where(proxygroup.IDEQ(group.ID)).Only(mixins.SkipSoftDelete(ctx))
		require.NoError(t, err)
		require.NotNil(t, row.DeletedAt)
		require.ErrorIs(t, repo.ReplaceMembers(ctx, group.ID, nil), service.ErrProxyGroupNotFound)
		a := &service.Account{Name: "deleted-group", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, ProxyGroupID: &group.ID}
		require.ErrorIs(t, accounts.Create(ctx, a), service.ErrProxyGroupNotFound)
	})

	t.Run("no_implicit_binding", func(t *testing.T) {
		a := &service.Account{Name: "unselected", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive}
		require.NoError(t, accounts.Create(ctx, a))
		row, err := c.Account.Query().Where(account.IDEQ(a.ID)).Only(ctx)
		require.NoError(t, err)
		require.Nil(t, row.ProxyID)
		require.Nil(t, row.ProxyGroupID)
	})
}

func TestProxyGroupRepositoryConcurrentReplacement(t *testing.T) {
	db, client := proxyGroupTestPostgres(t)
	ctx := context.Background()
	workspace, err := client.Workspace.Create().SetName("ip-group-concurrency-" + uuid.NewString()).Save(ctx)
	require.NoError(t, err)
	ctx = domain.WithWorkspaceScope(ctx, workspace.ID)
	// Every cleanup predicate is confined to this invocation's newly created workspace.
	defer func() {
		for _, query := range []string{"DELETE FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE workspace_id=$1)", "DELETE FROM accounts WHERE workspace_id=$1", "DELETE FROM proxy_group_members WHERE workspace_id=$1", "DELETE FROM proxy_groups WHERE workspace_id=$1", "DELETE FROM proxies WHERE workspace_id=$1", "DELETE FROM workspaces WHERE id=$1"} {
			_, err := db.Exec(query, workspace.ID)
			require.NoError(t, err)
		}
	}()
	repo := NewProxyGroupRepository(client)
	group := &service.ProxyGroup{Name: "concurrent"}
	require.NoError(t, repo.Create(ctx, group))
	ids := make([]int64, 4)
	for i := range ids {
		p, err := client.Proxy.Create().SetName("fixture").SetHost("fixture.invalid").SetPort(8080).SetProtocol("http").Save(ctx)
		require.NoError(t, err)
		ids[i] = p.ID
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, set := range [][]int64{ids[:2], ids[2:]} {
		wg.Add(1)
		go func(values []int64) { defer wg.Done(); errs <- repo.ReplaceMembers(ctx, group.ID, values) }(set)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	members, err := repo.ListMembers(ctx, group.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	actual := []int64{members[0].ProxyID, members[1].ProxyID}
	require.True(t, (actual[0] == ids[0] && actual[1] == ids[1]) || (actual[0] == ids[2] && actual[1] == ids[3]), "concurrent replacements must never leave a mixed member set")

	// A caller-owned transaction can roll back both configuration and membership.
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(ctx, tx)
	group.Name = "rolled-back"
	require.NoError(t, repo.Update(txCtx, group))
	require.NoError(t, repo.ReplaceMembers(txCtx, group.ID, nil))
	require.NoError(t, tx.Rollback())
	got, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, "concurrent", got.Name)
	require.Len(t, got.Members, 2)

	// An account attachment holds the group lock until its transaction commits.
	accounts := newAccountRepositoryWithSQL(client, db, nil)
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	a := &service.Account{Name: "concurrent-binding", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, ProxyGroupID: &group.ID}
	require.NoError(t, accounts.Create(dbent.NewTxContext(ctx, tx), a))
	deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deleted := make(chan error, 1)
	go func() { deleted <- repo.Delete(deleteCtx, group.ID) }()
	select {
	case err := <-deleted:
		t.Fatalf("delete escaped attachment lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-deleted, service.ErrProxyGroupInUse)
	// Standalone create also takes its own transaction and preserves logical fields.
	b := *a
	b.ID = 0
	b.Name = "standalone-binding"
	require.NoError(t, accounts.Create(ctx, &b))
	stored, err := accounts.GetByID(ctx, b.ID)
	require.NoError(t, err)
	require.Equal(t, &group.ID, stored.ProxyGroupID)
	require.Nil(t, stored.ProxyID)
}
