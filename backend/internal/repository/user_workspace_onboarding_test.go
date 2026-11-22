package repository

import (
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"os"
	"testing"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/workspacemember"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryDefaultWorkspaceOnboarding(t *testing.T) {
	for _, guard := range []bool{false, true} {
		for _, role := range []string{service.RoleAdmin, service.RoleUser} {
			name := role
			if guard {
				name += "-guard"
			}
			t.Run(name, func(t *testing.T) {
				repo, client := newUserEntRepo(t)
				var err error
				u := &service.User{Email: name + "@example.test", PasswordHash: "hash", Role: role, Status: service.StatusActive}
				if guard {
					err = repo.CreateWithEmailAliasGuard(t.Context(), u)
				} else {
					err = repo.Create(t.Context(), u)
				}
				require.NoError(t, err)
				member, err := client.WorkspaceMember.Query().Where(workspacemember.UserIDEQ(u.ID), workspacemember.WorkspaceIDEQ(1)).Only(t.Context())
				require.NoError(t, err)
				want := "member"
				if role == service.RoleAdmin {
					want = "admin"
				}
				require.Equal(t, want, string(member.Role))
				require.Equal(t, 1, client.WorkspaceMember.Query().CountX(t.Context()))
			})
		}
	}
}

func TestUserRepositoryWorkspaceOnboardingRollsBack(t *testing.T) {
	repo, client := newUserEntRepo(t)
	_, err := client.ExecContext(t.Context(), `CREATE TRIGGER reject_membership BEFORE INSERT ON workspace_members BEGIN SELECT RAISE(ABORT,'fixture rejection'); END`)
	require.NoError(t, err)
	u := &service.User{Email: "rollback@example.test", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.ErrorContains(t, repo.Create(t.Context(), u), "provision default workspace membership")
	require.Zero(t, client.User.Query().CountX(t.Context()))
}

func TestUserRepositoryWorkspaceOnboardingUsesOuterTransaction(t *testing.T) {
	repo, client := newUserEntRepo(t)
	tx, err := client.Tx(t.Context())
	require.NoError(t, err)
	u := &service.User{Email: "outer-tx@example.test", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(dbent.NewTxContext(t.Context(), tx), u))
	require.Equal(t, 1, tx.Client().WorkspaceMember.Query().CountX(t.Context()))
	require.NoError(t, tx.Rollback())
	require.Zero(t, client.User.Query().CountX(t.Context()))
	require.Zero(t, client.WorkspaceMember.Query().CountX(t.Context()))
}

func TestUserDefaultWorkspaceRoleLifecycle(t *testing.T) {
	repo, client := newUserEntRepo(t)
	_, err := client.ExecContext(t.Context(), `UPDATE workspaces SET is_default=false; INSERT INTO workspaces(id,name,is_default,created_at,updated_at) VALUES(9,'Restored default',true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = client.ExecContext(t.Context(), `ALTER TABLE workspaces ADD COLUMN scope_version INTEGER NOT NULL DEFAULT 1`)
	require.NoError(t, err)
	scopeVersion := func() int64 {
		t.Helper()
		rows, err := client.QueryContext(t.Context(), `SELECT scope_version FROM workspaces WHERE id=9`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var version int64
		require.NoError(t, rows.Scan(&version))
		require.NoError(t, rows.Close())
		return version
	}
	// SQLite equivalent of migration 290's membership scope trigger.
	_, err = client.ExecContext(t.Context(), `CREATE TRIGGER member_epoch AFTER UPDATE ON workspace_members BEGIN UPDATE workspaces SET scope_version=scope_version+1 WHERE id=NEW.workspace_id; END`)
	require.NoError(t, err)
	u := &service.User{Email: "roles@example.test", PasswordHash: "hash", Role: service.RoleAdmin, Status: service.StatusActive}
	require.NoError(t, repo.Create(t.Context(), u))
	member, err := client.WorkspaceMember.Query().Only(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 9, member.WorkspaceID)
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		before := scopeVersion()
		u.Role = role
		require.NoError(t, repo.Update(t.Context(), u, service.UserUpdateFields{Role: true}))
		after := scopeVersion()
		require.Greater(t, after, before)
		m, err := client.WorkspaceMember.Query().Only(t.Context())
		require.NoError(t, err)
		want := "member"
		if role == service.RoleAdmin {
			want = "admin"
		}
		require.Equal(t, want, string(m.Role))
		// The identity evaluator's administrator predicate must now deny the demoted
		// user workspace admin actions (connections.edit, pools.assign, grants).
		rows, err := client.QueryContext(t.Context(), `SELECT (u.role='admin' OR m.role='admin') FROM users u JOIN workspace_members m ON m.user_id=u.id WHERE u.id=$1 AND m.workspace_id=9`, u.ID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var admin bool
		require.NoError(t, rows.Scan(&admin))
		require.NoError(t, rows.Close())
		require.Equal(t, role == service.RoleAdmin, admin)
		require.NoError(t, repo.Update(t.Context(), u, service.UserUpdateFields{Role: true}))
		require.Equal(t, after, scopeVersion())
	}
}

func TestUserRoleAndMembershipRollbackTogether(t *testing.T) {
	repo, client := newUserEntRepo(t)
	u := &service.User{Email: "atomic@example.test", PasswordHash: "hash", Role: service.RoleAdmin, Status: service.StatusActive}
	require.NoError(t, repo.Create(t.Context(), u))
	_, err := client.ExecContext(t.Context(), `CREATE TRIGGER reject_role BEFORE UPDATE ON workspace_members BEGIN SELECT RAISE(ABORT,'fixture rejection'); END`)
	require.NoError(t, err)
	u.Role = service.RoleUser
	require.ErrorContains(t, repo.Update(t.Context(), u, service.UserUpdateFields{Role: true}), "synchronize default workspace role")
	stored, err := client.User.Get(t.Context(), u.ID)
	require.NoError(t, err)
	require.Equal(t, service.RoleAdmin, stored.Role)
}

func TestMissingDefaultRollsBackSignup(t *testing.T) {
	repo, client := newUserEntRepo(t)
	_, err := client.ExecContext(t.Context(), "UPDATE workspaces SET is_default=false")
	require.NoError(t, err)
	u := &service.User{Email: "missing@example.test", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.ErrorContains(t, repo.Create(t.Context(), u), "provision default workspace membership")
	require.Zero(t, client.User.Query().CountX(t.Context()))
}

func TestUserRoleUpdateUsesOuterTransaction(t *testing.T) {
	repo, client := newUserEntRepo(t)
	other, err := client.Workspace.Create().SetName("Delegated").Save(t.Context())
	require.NoError(t, err)
	u := &service.User{Email: "outer-role@example.test", PasswordHash: "hash", Role: service.RoleAdmin, Status: service.StatusActive}
	require.NoError(t, repo.Create(t.Context(), u))
	_, err = client.WorkspaceMember.Create().SetWorkspaceID(other.ID).SetUserID(u.ID).SetRole(workspacemember.RoleAdmin).Save(t.Context())
	require.NoError(t, err)
	tx, err := client.Tx(t.Context())
	require.NoError(t, err)
	u.Role = service.RoleUser
	require.NoError(t, repo.Update(dbent.NewTxContext(t.Context(), tx), u, service.UserUpdateFields{Role: true}))
	member, err := tx.Client().WorkspaceMember.Query().Where(workspacemember.UserIDEQ(u.ID), workspacemember.WorkspaceIDEQ(1)).Only(t.Context())
	require.NoError(t, err)
	require.Equal(t, workspacemember.RoleMember, member.Role)
	delegated, err := tx.Client().WorkspaceMember.Query().Where(workspacemember.UserIDEQ(u.ID), workspacemember.WorkspaceIDEQ(other.ID)).Only(t.Context())
	require.NoError(t, err)
	require.Equal(t, workspacemember.RoleAdmin, delegated.Role)
	require.NoError(t, tx.Rollback())
	stored, err := client.User.Get(t.Context(), u.ID)
	require.NoError(t, err)
	require.Equal(t, service.RoleAdmin, stored.Role)
	member, err = client.WorkspaceMember.Query().Where(workspacemember.UserIDEQ(u.ID), workspacemember.WorkspaceIDEQ(1)).Only(t.Context())
	require.NoError(t, err)
	require.Equal(t, workspacemember.RoleAdmin, member.Role)
}

func TestUserRepositoryDefaultWorkspacePostgresAtomicity(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to disposable job_test")
	}
	db, err := testresources.OpenPostgres(t.Context(), dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, ApplyMigrations(t.Context(), db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := NewUserRepository(client, db)
	u := &service.User{Email: uuid.NewString() + "@atomic.test", PasswordHash: "fixture", Role: service.RoleAdmin, Status: service.StatusActive}
	require.NoError(t, repo.Create(t.Context(), u))
	defer func() {
		_, err := db.ExecContext(t.Context(), "DELETE FROM users WHERE id=$1", u.ID)
		require.NoError(t, err)
	}()
	const snapshot = `SELECT u.role,m.role,w.scope_version FROM users u JOIN workspace_members m ON m.user_id=u.id JOIN workspaces w ON w.id=m.workspace_id WHERE u.id=$1 AND w.is_default`
	var before, inside, after int64
	var global, local string
	require.NoError(t, db.QueryRowContext(t.Context(), snapshot, u.ID).Scan(&global, &local, &before))
	tx, err := client.Tx(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ctx := dbent.NewTxContext(t.Context(), tx)
	u.Role = service.RoleUser
	require.NoError(t, repo.Update(ctx, u, service.UserUpdateFields{Role: true}))
	rows, err := tx.Client().QueryContext(ctx, snapshot, u.ID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&global, &local, &inside))
	require.NoError(t, rows.Close())
	require.Equal(t, "user", global)
	require.Equal(t, "member", local)
	require.Greater(t, inside, before)
	require.NoError(t, tx.Rollback())
	require.NoError(t, db.QueryRowContext(t.Context(), snapshot, u.ID).Scan(&global, &local, &after))
	require.Equal(t, "admin", global)
	require.Equal(t, "admin", local)
	require.Equal(t, before, after)
}
