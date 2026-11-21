//go:build integration

package identity_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
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
