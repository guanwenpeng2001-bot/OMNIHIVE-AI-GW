//go:build integration

package identity_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	workspace "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestKeyPolicyAndProjectGrantConvergence(t *testing.T) {
	ctx := t.Context()
	db, _, _ := testutil.NewCapabilityPostgres(t)
	store := postgres.New(db)
	id := func(q string, args ...any) int64 {
		t.Helper()
		var value int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&value))
		return value
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	newUser := func() int64 {
		return id(`INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',1) RETURNING id`, uuid.NewString()+"@example.test")
	}
	admin, member := newUser(), newUser()
	w := id(`INSERT INTO workspaces(name) VALUES('policy convergence') RETURNING id`)
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member')`, w, admin, member)
	def := id(`SELECT identity_default_project($1)`, w)
	require.Zero(t, id(`SELECT count(*) FROM project_grants WHERE workspace_id=$1 AND user_id=$2`, w, member))
	_, err := store.Evaluate(ctx, member, w, def, "playground.run")
	require.ErrorIs(t, err, fault.Forbidden)
	p, err := store.CreateProject(ctx, admin, w, "Calls", "calls")
	require.NoError(t, err)
	p2, err := store.CreateProject(ctx, admin, w, "Reports", "reports")
	require.NoError(t, err)
	g, err := store.SetProjectGrants(ctx, admin, w, member, p.ID, 1, []string{"keys.create", "keys.read", "text.chat", "models.read"})
	require.NoError(t, err)
	g, err = store.SetProjectGrants(ctx, admin, w, member, p2.ID, g.Version, []string{"usage.read"})
	require.NoError(t, err)
	read, err := store.MemberGrants(ctx, admin, w, member)
	require.NoError(t, err)
	require.Len(t, read.ProjectGrants, 2)
	require.Equal(t, p.ID, read.ProjectGrants[0].ProjectID)
	require.Equal(t, []string{"usage.read"}, read.ProjectGrants[1].Actions)
	_, err = store.SetGrants(ctx, admin, w, member, g.Version, []int64{p.ID, p2.ID}, read.Actions)
	var typed *fault.Fault
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "GRANT_REPRESENTATION_LOSS", typed.Reason)
	_, err = store.SetProjectGrants(ctx, admin, w, member, p.ID, g.Version-1, []string{})
	require.ErrorAs(t, err, &typed)
	require.Equal(t, 412, typed.Status)
	require.EqualValues(t, 1, id(`SELECT count(*) FROM project_grants WHERE workspace_id=$1 AND user_id=$2 AND project_id=$3`, w, member, p2.ID))
	group := id(`INSERT INTO groups(workspace_id,name,platform) VALUES($1,'models','openai') RETURNING id`, w)
	exec(`INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('groups',$1,$2,$3)`, group, w, p.ID)
	k, err := store.CreateKey(ctx, member, w, p.ID, identity.CreateKey{Name: "converged", GroupID: &group, Actions: []string{"text.chat", "models.read"}, ModelAccess: identity.ModelAccess{Mode: "allow_list", IDs: []string{"a", "b"}}}, "key")
	require.NoError(t, err)
	require.NotNil(t, k.Credential)
	digest := sha256.Sum256([]byte(*k.Credential))
	secretDigest := hex.EncodeToString(digest[:])
	policy, err := store.ProjectCredential(ctx, secretDigest)
	require.NoError(t, err)
	require.True(t, policy.Allows("text.chat", []string{"a"}))
	require.True(t, policy.Allows("text.chat", []string{"b"}))
	require.EqualValues(t, 1, id(`SELECT count(*) FROM project_key_policies WHERE api_key_id=$1 AND model_mode='inherit' AND cardinality(models)=0`, k.ID))
	// Simulate a historical row whose two model lists disagree.
	exec(`UPDATE project_key_policies SET model_mode='allow_list',models=ARRAY['a'] WHERE api_key_id=$1`, k.ID)
	policy, err = store.ProjectCredential(ctx, secretDigest)
	require.NoError(t, err)
	require.Equal(t, "legacy_intersection", policy.ModelPolicySource)
	require.True(t, policy.Allows("text.chat", []string{"a"}))
	require.False(t, policy.Allows("text.chat", []string{"b"}))
	_, err = store.AuthorizeKeyUse(ctx, member, w, p.ID, k.ID, "text.chat", []string{"b"})
	require.ErrorIs(t, err, fault.Forbidden)
	// V1 no-op/clear must not silently erase the tighter historical boundary.
	rules := repository.NewModelAccessRepository(db)
	scoped := workspace.WithWorkspaceScope(ctx, w)
	rule, err := rules.Rule(scoped, "keys", k.ID, member)
	require.NoError(t, err)
	require.ErrorIs(t, rules.SaveRule(scoped, "keys", k.ID, member, *rule), service.ErrModelAccessExpansion)
	rule.Models = nil
	require.NoError(t, rules.SaveRule(scoped, "keys", k.ID, member, *rule))
	policy, err = store.ProjectCredential(ctx, secretDigest)
	require.NoError(t, err)
	require.Equal(t, "model_access_rules", policy.ModelPolicySource)
	require.Equal(t, []string{catalog.StableID("openai", "a")}, policy.Models)
	require.False(t, policy.Allows("text.chat", []string{"b"}))
	// Named sets and exclude use the same canonical resolver as preflight/execution.
	named, err := rules.SaveGroup(scoped, service.ModelAccessGroup{Name: "Named", Models: []string{"a", "b"}})
	require.NoError(t, err)
	rule, err = rules.Rule(scoped, "keys", k.ID, member)
	require.NoError(t, err)
	rule.AccessGroupID, rule.Models, rule.Exclude = &named.ID, nil, []string{"b"}
	require.NoError(t, rules.SaveRule(scoped, "keys", k.ID, member, *rule))
	_, err = store.AuthorizeKeyUse(ctx, member, w, p.ID, k.ID, "text.chat", []string{"a"})
	require.NoError(t, err)
	_, err = store.AuthorizeKeyUse(ctx, admin, w, p.ID, k.ID, "text.chat", []string{"b"})
	require.ErrorIs(t, err, fault.Forbidden)
	// Grant revocation changes an already-issued key, including act_as use.
	_, err = store.SetProjectGrants(ctx, admin, w, member, p.ID, g.Version, []string{"keys.read", "models.read"})
	require.NoError(t, err)
	_, err = store.AuthorizeKeyUse(ctx, admin, w, p.ID, k.ID, "text.chat", []string{"a"})
	require.ErrorIs(t, err, fault.Forbidden)
	exec(`UPDATE api_keys SET expires_at=now()-interval '1 second' WHERE id=$1`, k.ID)
	listed, err := store.Keys(ctx, admin, w, p.ID, identity.Page{Limit: 25, AsOf: time.Now()})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "expired", listed[0].Status)
	require.Nil(t, listed[0].Credential)
	_, err = store.AuthorizeKeyUse(ctx, admin, w, p.ID, k.ID, "models.read", nil)
	require.ErrorIs(t, err, fault.NotFound)
}
