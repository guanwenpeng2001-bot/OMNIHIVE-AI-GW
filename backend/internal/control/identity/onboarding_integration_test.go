//go:build integration

package identity_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/workspacemember"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestDefaultWorkspaceOnboardingAccess(t *testing.T) {
	db, client, _ := testutil.NewCapabilityPostgres(t)
	repo := repository.NewUserRepository(client, db)
	access := identitydb.New(db)
	var project int64
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT id FROM projects WHERE workspace_id=1 AND is_default").Scan(&project))
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			user := &service.User{Email: uuid.NewString() + "@onboarding.test", PasswordHash: "fixture", Role: role, Status: service.StatusActive}
			require.NoError(t, repo.CreateWithEmailAliasGuard(t.Context(), user))
			var memberRole string
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT role FROM workspace_members WHERE workspace_id=1 AND user_id=$1", user.ID).Scan(&memberRole))
			want := "member"
			if role == service.RoleAdmin {
				want = "admin"
			}
			require.Equal(t, want, memberRole)
			a, err := access.Evaluate(t.Context(), user.ID, 1, project, "")
			require.NoError(t, err)
			var grants int
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM project_grants WHERE user_id=$1", user.ID).Scan(&grants))
			require.Zero(t, grants)
			if role == service.RoleUser {
				require.Equal(t, []string{"models.read", "projects.read"}, a.Actions)
				for _, action := range []string{"keys.read", "keys.create", "playground.read", "playground.run", "billing.read", "billing.ledger.read", "requests.read", "tasks.read", "tasks.create", "credentials.byok.manage", "connections.read", "connections.edit", "pools.assign", "routing.read", "billing.adjust", "billing.reconcile.read", "members.grants.write", "ops.restore.execute", "projects.edit", "projects.delete"} {
					_, err = access.Evaluate(t.Context(), user.ID, 1, project, action)
					require.ErrorIs(t, err, fault.Forbidden, action)
				}
				workspaces, err := access.Workspaces(t.Context(), user.ID, domain.Page{Limit: 100, AsOf: time.Now().Add(time.Minute)})
				require.NoError(t, err)
				require.Len(t, workspaces, 1)
				require.Equal(t, int64(1), workspaces[0].ID)
				projects, err := access.Projects(t.Context(), user.ID, 1, domain.Page{Limit: 100, AsOf: time.Now().Add(time.Minute)})
				require.NoError(t, err)
				require.Len(t, projects, 1)
				require.Equal(t, project, projects[0].ID)
			} else {
				require.NoError(t, a.Require("connections.edit"))
				require.NoError(t, a.Require("billing.read"))
				before := a.Version
				user.Role = service.RoleUser
				require.NoError(t, repo.Update(t.Context(), user, service.UserUpdateFields{Role: true}))
				for _, action := range []string{"connections.edit", "pools.assign", "members.grants.write", "billing.adjust"} {
					_, err = access.Evaluate(t.Context(), user.ID, 1, project, action)
					require.ErrorIs(t, err, fault.Forbidden, action)
				}
				demoted, err := access.Evaluate(t.Context(), user.ID, 1, project, "models.read")
				require.NoError(t, err)
				require.Greater(t, demoted.Version, before)
				user.Role = service.RoleAdmin
				require.NoError(t, repo.Update(t.Context(), user, service.UserUpdateFields{Role: true}))
				_, err = access.Evaluate(t.Context(), user.ID, 1, project, "connections.edit")
				require.NoError(t, err)
				// Explicitly delegating the same role takes ownership of the grant.
				require.NoError(t, repository.NewWorkspaceRepository(db).SetMember(t.Context(), 1, user.ID, "admin"))
				user.Role = service.RoleUser
				require.NoError(t, repo.Update(t.Context(), user, service.UserUpdateFields{Role: true}))
				_, err = access.Evaluate(t.Context(), user.ID, 1, project, "connections.edit")
				require.NoError(t, err)
			}
		})
	}
	// Application provisioning must not become a trigger that invalidates outsider fixtures.
	var outsider int64
	require.NoError(t, db.QueryRowContext(t.Context(), "INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@outsider.test").Scan(&outsider))
	_, err := access.Evaluate(t.Context(), outsider, 1, project, "projects.read")
	require.ErrorIs(t, err, fault.NotFound)
}

func TestDefaultWorkspaceMembershipBackfill(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	create := func(role, status string, deleted bool) int64 {
		t.Helper()
		var id int64
		require.NoError(t, tx.QueryRowContext(t.Context(), `INSERT INTO users(email,password_hash,role,status,deleted_at) VALUES($1,'fixture',$2,$3,CASE WHEN $4 THEN now() END) RETURNING id`, uuid.NewString()+"@backfill.test", role, status, deleted).Scan(&id))
		return id
	}
	member := create("user", "active", false)
	admin := create("admin", "active", false)
	existingAdmin := create("admin", "active", false)
	existingMember := create("user", "active", false)
	inactive := create("user", "disabled", false)
	deleted := create("user", "active", true)
	_, err = tx.ExecContext(t.Context(), "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(1,$1,'member'),(1,$2,'admin')", existingAdmin, existingMember)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("334_backfill_default_workspace_members.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), string(migration))
	require.NoError(t, err)
	for id, want := range map[int64]string{member: "member", admin: "admin", existingAdmin: "admin", existingMember: "admin"} {
		var got string
		require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT role FROM workspace_members WHERE workspace_id=1 AND user_id=$1", id).Scan(&got))
		require.Equal(t, want, got, fmt.Sprint(id))
	}
	for _, id := range []int64{inactive, deleted} {
		var count int
		require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT count(*) FROM workspace_members WHERE user_id=$1", id).Scan(&count))
		require.Zero(t, count)
	}
	var before, after int64
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT scope_version FROM workspaces WHERE id=1").Scan(&before))
	_, err = tx.ExecContext(t.Context(), string(migration))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT scope_version FROM workspaces WHERE id=1").Scan(&after))
	require.Equal(t, before, after)
	var grants int
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT count(*) FROM project_grants WHERE user_id IN ($1,$2,$3,$4)", member, admin, existingAdmin, existingMember).Scan(&grants))
	require.Zero(t, grants)
}

func TestHistoricalDefaultRoleSourceRepair(t *testing.T) {
	db, client, _ := testutil.NewCapabilityPostgres(t)
	tx, err := client.Tx(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	ctx := dbent.NewTxContext(t.Context(), tx)
	store := tx.Client()
	repo := repository.NewUserRepository(client, db)
	migration, err := migrations.FS.ReadFile("337_default_workspace_role_source.sql")
	require.NoError(t, err)
	// Make fixtures indistinguishable from rows touched by 334. Its migration
	// runner wrote its receipt with the same transaction timestamp as the upsert.
	users := make([]*service.User, 0, 3)
	for _, role := range []string{"admin", "user", "admin"} {
		u := &service.User{Email: uuid.NewString() + "@legacy.test", Role: role, Status: service.StatusActive, PasswordHash: "fixture"}
		require.NoError(t, repo.Create(ctx, u))
		users = append(users, u)
	}
	_, err = store.ExecContext(ctx, `UPDATE workspace_members SET inherits_global_role=false,role='admin',
 updated_at=(SELECT applied_at FROM schema_migrations WHERE filename='334_backfill_default_workspace_members.sql') WHERE user_id IN ($1,$2)`, users[0].ID, users[1].ID)
	require.NoError(t, err)
	_, err = store.ExecContext(ctx, `UPDATE workspace_members SET inherits_global_role=false,updated_at=clock_timestamp() WHERE user_id=$1`, users[2].ID)
	require.NoError(t, err)
	for range 2 {
		_, err = store.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	for i, u := range users {
		u.Role = service.RoleUser
		require.NoError(t, repo.Update(ctx, u, service.UserUpdateFields{Role: true}))
		member, err := store.WorkspaceMember.Query().Where(workspacemember.UserIDEQ(u.ID)).Only(ctx)
		require.NoError(t, err)
		if i == 2 {
			require.EqualValues(t, "admin", member.Role)
		} else {
			require.EqualValues(t, "member", member.Role)
		}
	}
	// A restored non-1 default gets the missing backfill as well.
	_, err = store.ExecContext(ctx, "UPDATE workspaces SET is_default=false WHERE is_default")
	require.NoError(t, err)
	restored, err := store.Workspace.Create().SetName("Restored").SetIsDefault(true).Save(ctx)
	require.NoError(t, err)
	_, err = store.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.Equal(t, 1, store.WorkspaceMember.Query().Where(workspacemember.WorkspaceIDEQ(restored.ID), workspacemember.UserIDEQ(users[0].ID)).CountX(ctx))
}
