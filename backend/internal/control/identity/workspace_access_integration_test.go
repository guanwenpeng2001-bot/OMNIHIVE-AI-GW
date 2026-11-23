//go:build integration

package identity_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceAccessHTTP(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	id := func(q string, args ...any) int64 {
		t.Helper()
		var v int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&v))
		return v
	}
	user := func(role string) int64 {
		return id("INSERT INTO users(email,role,password_hash) VALUES($1,$2,'fixture') RETURNING id", uuid.NewString()+"@access.test", role)
	}
	admin, member := user("admin"), user("user")
	w := id("SELECT resolve_default_workspace_id()")
	_, err := db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role,inherits_global_role) VALUES($1,$2,'member',true)", w, member)
	require.NoError(t, err)
	p := id("INSERT INTO projects(workspace_id,name) VALUES($1,'team') RETURNING id", w)
	v1 := repository.NewWorkspaceRepository(db)
	role, err := v1.Role(ctx, w, admin, false)
	require.NoError(t, err)
	require.Equal(t, "admin", role)
	list, err := v1.List(ctx, member, true)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "member", list[0].Role)
	require.True(t, list[0].IsDefault)
	actor := member
	engine := gin.New()
	control.Register(engine.Group("/api/v2"), control.New(identitydb.New(db), domain.Operations{}, &config.Config{}), func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor}) })
	call := func(method, path, body, header, value string, status int) (map[string]any, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if header != "" {
			req.Header.Set(header, value)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, rec.Body.String())
		var env map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		data, _ := env["data"].(map[string]any)
		return data, rec.Header().Get("ETag")
	}
	wp := fmt.Sprintf("/api/v2/workspaces/%d", w)
	grants := fmt.Sprintf("%s/members/%d/grants", wp, member)
	data, _ := call("GET", wp+"/access", "", "", "", 200)
	require.Contains(t, data, "project_id")
	require.Nil(t, data["project_id"])
	require.Equal(t, "inherited", data["role_source"])
	require.NotContains(t, data, "grantable_actions")
	data, _ = call("GET", "/api/v2/me/scopes", "", "", "", 200)
	row := data["items"].([]any)[0].(map[string]any)
	require.Equal(t, true, row["is_default"])
	require.Equal(t, true, row["projects"].([]any)[0].(map[string]any)["is_default"])
	data, etag := call("POST", wp+"/access-requests", `{"note":"join the team"}`, "Idempotency-Key", "one", 201)
	require.Equal(t, `"1"`, etag)
	decision := wp + "/access-requests/" + data["id"].(string) + "/decision"
	call("GET", wp+"/access-requests?view=review", "", "", "", 403)
	actor = admin
	_, memberTag := call("GET", grants, "", "", "", 200)
	body := fmt.Sprintf(`{"decision":"approve","project_id":"%d","add_actions":["keys.read"],"member_version":%s}`, p, memberTag)
	data, etag = call("POST", decision, body, "If-Match", etag, 200)
	require.Equal(t, "approved", data["state"])
	require.Equal(t, `"2"`, etag)
	data, memberTag = call("GET", grants, "", "", "", 200)
	matrix, err := json.Marshal(map[string]any{"project_grants": data["project_grants"]})
	require.NoError(t, err)
	_, unchanged := call("PUT", grants, string(matrix), "If-Match", memberTag, 200)
	require.Equal(t, memberTag, unchanged)
	actor = member
	data, _ = call("GET", wp+"/access-requests?view=mine", "", "", "", 200)
	require.Equal(t, fmt.Sprint(p), data["items"].([]any)[0].(map[string]any)["approved_project_id"])
	// Both administrators reviewed the same member version. Only one command may commit.
	store := identitydb.New(db)
	pending, err := store.RequestAccess(ctx, member, w, domain.RequestAccess{Note: "more use"}, "race")
	require.NoError(t, err)
	g, err := store.MemberGrants(ctx, admin, w, member)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := store.DecideAccess(ctx, admin, w, pending.ID, 1, domain.DecideAccess{Decision: "approve", ProjectID: p, MemberVersion: g.Version, AddActions: []string{"keys.create"}}, "race")
		results <- err
	}()
	go func() {
		<-start
		_, err := store.SetExactGrants(ctx, admin, w, member, g.Version, []domain.ProjectGrant{{ProjectID: p, Actions: []string{"text.chat"}}})
		results <- err
	}()
	close(start)
	successes, stale := 0, 0
	for range 2 {
		err := <-results
		var conflict *fault.Fault
		switch {
		case err == nil:
			successes++
		case errors.As(err, &conflict) && conflict.Status == 412:
			stale++
		default:
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, stale)
	current, err := store.MemberGrants(ctx, admin, w, member)
	require.NoError(t, err)
	require.Equal(t, g.Version+1, current.Version)
}
