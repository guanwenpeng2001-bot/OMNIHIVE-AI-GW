//go:build integration

package identity_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestExactGrantsAndMembershipRequests(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	store := identitydb.New(db)
	id := func(sql string, args ...any) int64 {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, sql, args...).Scan(&id))
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, sql, args...)
		require.NoError(t, err)
	}
	user := func(role string) int64 {
		return id("INSERT INTO users(email,role,password_hash) VALUES($1,$2,'fixture') RETURNING id", uuid.NewString()+"@example.test", role)
	}
	admin, member, platform := user("user"), user("user"), user("admin")
	w := id("SELECT resolve_default_workspace_id()")
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member')", w, admin, member)
	shared := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	project := func() int64 {
		return id("INSERT INTO projects(workspace_id,name) VALUES($1,$2) RETURNING id", w, uuid.NewString())
	}
	x, y := project(), project()
	a, err := store.WorkspaceAccess(ctx, admin, w)
	require.NoError(t, err)
	require.True(t, a.EffectiveAdmin)
	require.Equal(t, "delegated", *a.RoleSource)
	require.Nil(t, a.ProjectID)
	require.NotContains(t, *a.GrantableActions, "ops.restore.execute")
	a, err = store.WorkspaceAccess(ctx, platform, w)
	require.NoError(t, err)
	require.True(t, a.EffectiveAdmin)
	require.Nil(t, a.Role)
	pa, err := store.ProjectAccess(ctx, member, w, shared)
	require.NoError(t, err)
	require.Equal(t, []string{"models.read"}, pa.IssuableKeyActions)
	rows := []domain.ProjectGrant{{ProjectID: x, Actions: []string{"text.chat"}}, {ProjectID: y, Actions: []string{"keys.read"}}}
	v, err := store.SetExactGrants(ctx, admin, w, member, 1, rows)
	require.NoError(t, err)
	same, err := store.SetExactGrants(ctx, admin, w, member, v.Version, rows)
	require.NoError(t, err)
	require.Equal(t, v, same)
	g, err := store.MemberGrants(ctx, admin, w, member)
	require.NoError(t, err)
	require.Equal(t, rows, g.ProjectGrants)
	_, err = store.SetGrants(ctx, admin, w, member, v.Version, []int64{x, y}, g.Actions)
	var f *fault.Fault
	require.ErrorAs(t, err, &f)
	require.Equal(t, "EXACT_GRANTS_REQUIRED", f.Reason)
	for _, bad := range [][]domain.ProjectGrant{{rows[0], rows[0]}, {{ProjectID: shared, Actions: []string{"keys.create"}}}, {{ProjectID: x, Actions: []string{"ops.restore.execute"}}}} {
		_, err = store.SetExactGrants(ctx, admin, w, member, v.Version, bad)
		require.Error(t, err)
	}
	// Historical rows cannot convey a platform capability to a project member.
	exec("INSERT INTO project_grants(workspace_id,user_id,project_id,action) VALUES($1,$2,$3,'ops.restore.execute')", w, member, x)
	_, err = store.Evaluate(ctx, member, w, x, "ops.restore.execute")
	require.ErrorIs(t, err, fault.Forbidden)
	exec("DELETE FROM project_grants WHERE workspace_id=$1 AND user_id=$2 AND project_id=$3 AND action='ops.restore.execute'", w, member, x)
	in := domain.RequestAccess{Note: "  use image tools  "}
	r, err := store.RequestAccess(ctx, member, w, in, "create")
	require.NoError(t, err)
	require.Equal(t, "use image tools", r.Note)
	replay, err := store.RequestAccess(ctx, member, w, in, "create")
	require.NoError(t, err)
	require.Equal(t, r.ID, replay.ID)
	_, err = store.RequestAccess(ctx, member, w, domain.RequestAccess{Note: "different"}, "create")
	require.ErrorIs(t, err, fault.Conflict)
	dec := domain.DecideAccess{Decision: "approve", ProjectID: y, AddActions: []string{"text.chat"}, MemberVersion: 1}
	_, err = store.DecideAccess(ctx, admin, w, r.ID, 1, dec, "request")
	require.ErrorAs(t, err, &f)
	require.Equal(t, 412, f.Status)
	dec.MemberVersion = v.Version
	result, err := store.DecideAccess(ctx, admin, w, r.ID, 1, dec, "request")
	require.NoError(t, err)
	require.Equal(t, "approved", result.State)
	again, err := store.DecideAccess(ctx, admin, w, r.ID, 1, dec, "request")
	require.NoError(t, err)
	requireSameAccessDecision(t, result, again)
	g, err = store.MemberGrants(ctx, admin, w, member)
	require.NoError(t, err)
	rows[1].Actions = []string{"keys.read", "text.chat"}
	require.Equal(t, rows, g.ProjectGrants)
	require.EqualValues(t, 1, id("SELECT count(*) FROM audit_logs WHERE action='project.access_request.decision' AND actor_user_id=$1 AND NOT extra ? 'note'", admin))
	current, err := store.Project(ctx, admin, w, x)
	require.NoError(t, err)
	require.Equal(t, "active", current.State)
	archived, err := store.ChangeProject(ctx, admin, w, x, current.Version, "", true)
	require.NoError(t, err)
	require.Equal(t, "archived", archived.State)
	_, err = store.SetExactGrants(ctx, admin, w, member, g.Version, []domain.ProjectGrant{})
	require.NoError(t, err)
	require.EqualValues(t, 1, id("SELECT count(*) FROM project_grants WHERE user_id=$1 AND project_id=$2", member, x))
	page := domain.Page{AsOf: time.Now().Add(time.Minute), Limit: 10}
	mine, err := store.AccessRequests(ctx, member, w, "mine", page)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Nil(t, mine[0].ApprovedProjectID)
	_, err = store.AccessRequests(ctx, member, w, "review", page)
	require.ErrorIs(t, err, fault.Forbidden)
	pending, err := store.RequestAccess(ctx, member, w, in, "second")
	require.NoError(t, err)
	declined, err := store.DecideAccess(ctx, admin, w, pending.ID, 1, domain.DecideAccess{Decision: "decline"}, "decision")
	require.NoError(t, err)
	require.Equal(t, "declined", declined.State)
	old, err := store.RequestAccess(ctx, member, w, in, "third")
	require.NoError(t, err)
	exec("DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2", w, member)
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')", w, member)
	_, err = store.DecideAccess(ctx, admin, w, old.ID, 1, dec, "new")
	require.ErrorIs(t, err, fault.NotFound)
	mine, err = store.AccessRequests(ctx, member, w, "mine", page)
	require.NoError(t, err)
	require.Empty(t, mine)
}

func TestAccessDecisionReplayReceipt(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	store := identitydb.New(db)
	id := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&value))
		return value
	}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	for _, scenario := range []string{"demoted reviewer", "archived project", "changed target grants", "no-op approval", "decline", "missing receipt"} {
		t.Run(scenario, func(t *testing.T) {
			user := func() int64 {
				return id("INSERT INTO users(email,role,password_hash) VALUES($1,'user','fixture') RETURNING id", uuid.NewString()+"@example.test")
			}
			admin, member := user(), user()
			w := id("INSERT INTO workspaces(name) VALUES($1) RETURNING id", uuid.NewString())
			exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member')", w, admin, member)
			p := id("INSERT INTO projects(workspace_id,name) VALUES($1,'team') RETURNING id", w)
			in := domain.RequestAccess{RequestedProjectID: &p, Note: "reviewed access"}
			// The requester can initially see the requested project.
			v, err := store.SetExactGrants(ctx, admin, w, member, 1, []domain.ProjectGrant{{ProjectID: p, Actions: []string{"models.read"}}})
			require.NoError(t, err)
			if scenario == "no-op approval" {
				v, err = store.SetExactGrants(ctx, admin, w, member, v.Version, []domain.ProjectGrant{{ProjectID: p, Actions: []string{"models.read", "keys.read"}}})
				require.NoError(t, err)
			}
			r, err := store.RequestAccess(ctx, member, w, in, "create")
			require.NoError(t, err)
			decision := domain.DecideAccess{Decision: "approve", ProjectID: p, AddActions: []string{"keys.read"}, MemberVersion: v.Version}
			if scenario == "decline" {
				decision = domain.DecideAccess{Decision: "decline"}
			}
			result, err := store.DecideAccess(ctx, admin, w, r.ID, r.Version, decision, "first")
			require.NoError(t, err)
			switch scenario {
			case "demoted reviewer":
				exec("UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2", w, admin)
			case "archived project":
				_, err = store.ChangeProject(ctx, admin, w, p, 1, "", true)
				require.NoError(t, err)
			case "changed target grants":
				g, err := store.MemberGrants(ctx, admin, w, member)
				require.NoError(t, err)
				_, err = store.SetExactGrants(ctx, admin, w, member, g.Version, []domain.ProjectGrant{})
				require.NoError(t, err)
			case "missing receipt":
				exec("UPDATE project_access_requests SET decision_receipt=NULL WHERE id=$1", r.ID)
			}
			version := id("SELECT grant_version FROM workspace_members WHERE workspace_id=$1 AND user_id=$2", w, member)
			grants := id("SELECT count(*) FROM project_grants WHERE workspace_id=$1 AND user_id=$2", w, member)
			for _, expected := range []int64{r.Version, result.Version} {
				again, err := store.DecideAccess(ctx, admin, w, r.ID, expected, decision, "replay")
				switch scenario {
				case "demoted reviewer":
					require.ErrorIs(t, err, fault.Forbidden)
				case "missing receipt":
					require.ErrorIs(t, err, fault.Conflict)
				default:
					require.NoError(t, err)
					requireSameAccessDecision(t, result, again)
				}
			}
			require.Equal(t, version, id("SELECT grant_version FROM workspace_members WHERE workspace_id=$1 AND user_id=$2", w, member))
			require.Equal(t, grants, id("SELECT count(*) FROM project_grants WHERE workspace_id=$1 AND user_id=$2", w, member))
			require.EqualValues(t, 1, id("SELECT count(*) FROM audit_logs WHERE action='project.access_request.decision' AND (extra->>'access_request_id')::bigint=$1", result.ID))
			if scenario == "archived project" {
				mine, err := store.AccessRequests(ctx, member, w, "mine", domain.Page{AsOf: time.Now().Add(time.Minute), Limit: 10})
				require.NoError(t, err)
				require.Len(t, mine, 1)
				require.Nil(t, mine[0].RequestedProjectID)
				require.Nil(t, mine[0].ApprovedProjectID)
			}
		})
	}
}

func requireSameAccessDecision(t *testing.T, want, got domain.AccessRequest) {
	t.Helper()
	// PostgreSQL and the JSON receipt can decode the same instant with
	// different location objects.
	require.True(t, want.CreatedAt.Equal(got.CreatedAt), "created_at changed on replay")
	got.CreatedAt = want.CreatedAt
	if want.DecidedAt != nil && got.DecidedAt != nil {
		require.True(t, want.DecidedAt.Equal(*got.DecidedAt), "decided_at changed on replay")
		got.DecidedAt = want.DecidedAt
	}
	require.Equal(t, want, got)
}
