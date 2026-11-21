//go:build integration

package audit_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/audit"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestScopedAudit(t *testing.T) {
	ctx := t.Context()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires a disposable job_test PostgreSQL database")
	}
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	id := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&n))
		return n
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	workspace := func() (int64, int64) {
		w := id("INSERT INTO workspaces(name) VALUES('audit fixture') RETURNING id")
		return w, id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	}
	user := func() int64 {
		return id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	}
	w, p := workspace()
	w2, p2 := workspace()
	p3 := id("INSERT INTO projects(workspace_id,name) VALUES($1,'another') RETURNING id", w)
	actor, reader, finance := user(), user(), user()
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES ($1,$3,'admin'),($2,$3,'admin'),($1,$4,'member'),($1,$5,'member')", w, w2, actor, reader, finance)
	exec("INSERT INTO project_grants(workspace_id,project_id,user_id,action) VALUES($1,$2,$3,'audit.read'),($1,$2,$3,'audit.export'),($1,$2,$4,'billing.ledger.read')", w, p, reader, finance)
	access := postgres.New(db)
	store := domain.New(db, access)
	cfg := &config.Config{}
	cfg.JWT.Secret = uuid.NewString()
	h := control.New(store, domain.NewObserver(), cfg)
	engine := gin.New()
	session := func(c *gin.Context) {
		a, _ := strconv.ParseInt(c.GetHeader("Fixture-Actor"), 10, 64)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: a})
		c.Next()
	}
	v2 := engine.Group("/api/v2")
	control.Register(v2, h, session)
	identityhttp.Register(v2, identityhttp.New(access, identity.Operations{Readers: []identity.OperationReader{store}}, cfg), session)
	call := func(actor int64, method, path, body string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Fixture-Actor", strconv.FormatInt(actor, 10))
		r.Header.Set("Idempotency-Key", "audit-retry")
		out := httptest.NewRecorder()
		engine.ServeHTTP(out, r)
		require.Equal(t, status, out.Code, out.Body.String())
		require.Equal(t, "private, no-store", out.Header().Get("Cache-Control"))
		require.NotContains(t, out.Body.String(), "prompt-canary")
		var value map[string]any
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &value))
		if data, ok := value["data"].(map[string]any); ok {
			return data
		}
		return value
	}
	base := func(w, p int64) string { return fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/audit", w, p) }
	path := base(w, p)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	insert := func(at time.Time) int64 {
		key := uuid.NewString()
		exec(`INSERT INTO request_metadata_exports(id,workspace_id,project_id,actor_id,request_id,key_hash,sections,created_at) VALUES($1,$2,$3,$4,'prompt-canary',$5,ARRAY['billing'],$6)`, key, w, p, reader, make([]byte, 32), at)
		return id("SELECT id FROM scoped_audit_events WHERE resource_id=$1", key)
	}
	first := insert(at)
	exec("DELETE FROM request_metadata_exports WHERE workspace_id=$1 AND project_id=$2", w, p)
	insert(at.Add(time.Second))
	exec(`INSERT INTO audit_logs(actor_user_id,actor_email,actor_role,auth_method,action,method,path,request_id,client_ip,request_body,status_code,extra,project_id,resource_id)
 VALUES($1,'reader@example.test','member','jwt','connections.update','PUT','/api/v2/workspaces/:workspace_id/projects/:project_id/connections/:id','req-management-1','192.0.2.1','{{SECRET_gqwhgnky}}',200,'{"password":"{{SECRET_xdkmhgtp}}"}',$2,$3)`, reader, p, "connection-7")
	require.NoError(t, store.Capture(ctx, domain.Operation{Workspace: w, Project: p, Actor: reader, Action: "connections.update", Route: "/api/v2/workspaces/:workspace_id/projects/:project_id/connections/:id", RequestID: "req-management-1", ResourceID: "7", Method: "PUT", ActorEmail: "reader@example.test", ActorRole: "member", Status: 200, At: at.Add(2 * time.Second), Changes: json.RawMessage(`[{"field":"name","operation":"submitted","before":null,"after":"renamed"},{"field":"api_key","operation":"submitted","before":null,"after":"changed"}]`), Reason: "credential rotation"}))
	managementEvent := id("SELECT id FROM scoped_audit_events WHERE workspace_id=$1 AND project_id=$2 AND request_id='req-management-1'", w, p)
	var changes string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT changes::text FROM scoped_audit_events WHERE id=$1", managementEvent).Scan(&changes))
	require.NotContains(t, changes, "{{SECRET_xdkmhgtp}}")
	require.Contains(t, changes, `"after": "renamed"`)
	require.Zero(t, id("SELECT count(*) FROM scoped_audit_events WHERE project_id=$1", p3))
	event := fmt.Sprintf("/events/%d", first)
	call(finance, "GET", path+event, "", 403)
	for _, other := range []string{base(w2, p2), base(w, p3)} {
		call(actor, "GET", other+event, "", 404)
		call(actor, "POST", other+event+"/exports", "", 404)
	}
	read := call(actor, "GET", path+event, "", 200)
	require.Equal(t, "request_export", read["resource"])
	query := "?resource=request_export&action=requests.export&limit=1&from=" + url.QueryEscape(at.Format(time.RFC3339)) + "&to=" + url.QueryEscape(at.Add(time.Second).Format(time.RFC3339))
	list := call(reader, "GET", path+"/events"+query, "", 200)
	require.Len(t, list["items"], 1)
	require.Nil(t, list["next_cursor"])
	filtered := call(reader, "GET", path+"/events?resource_id="+read["resource_id"].(string), "", 200)
	require.Len(t, filtered["items"], 1)
	direct, directErr := store.Detail(ctx, domain.Scope{Actor: reader, Workspace: w, Project: p}, managementEvent)
	require.NoError(t, directErr)
	require.Equal(t, "req-management-1", direct.RequestID)
	managed := call(reader, "GET", fmt.Sprintf("%s/events/%d", path, managementEvent), "", 200)
	require.Equal(t, "req-management-1", managed["request_id"])
	require.Equal(t, "connections.update", managed["action"])
	require.Equal(t, "credential rotation", managed["reason"])
	require.Equal(t, "7", managed["resource_id"])
	require.NotContains(t, managed, "request_body")
	require.Equal(t, "reader@example.test", managed["actor_email"])
	pageExport := call(reader, "GET", path+"/export?limit=1", "", 200)
	require.Len(t, pageExport["items"], 1)
	require.NotNil(t, pageExport["next_cursor"])
	call(finance, "GET", path+"/export", "", 403)
	list = call(reader, "GET", path+"/events?limit=1", "", 200)
	require.Equal(t, strconv.FormatInt(managementEvent, 10), list["items"].([]any)[0].(map[string]any)["audit_event_id"])
	token := url.QueryEscape(list["next_cursor"].(string))
	call(reader, "GET", path+"/events?limit=1&cursor="+token, "", 200)
	call(actor, "GET", base(w2, p2)+"/events?cursor="+token, "", 422)
	adminPage := call(actor, "GET", path+"/events?limit=1", "", 200)
	adminCursor := url.QueryEscape(adminPage["next_cursor"].(string))
	call(actor, "GET", base(w2, p2)+"/events?cursor="+adminCursor, "", 422)
	call(reader, "GET", path+"/events?resource=artifact&cursor="+token, "", 422)
	op := call(reader, "POST", path+event+"/exports", "", 202)
	again := call(reader, "POST", path+event+"/exports", "", 202)
	require.Equal(t, op, again)
	download := op["result"].(map[string]any)["download_url"].(string)
	status := op["status_url"].(string)
	call(reader, "GET", status, "", 200)
	call(reader, "GET", download, "", 200)
	call(actor, "GET", download, "", 404)
	call(actor, "GET", strings.Replace(download, path, base(w2, p2), 1), "", 404)
	exec("DELETE FROM project_grants WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 AND action='audit.export'", w, p, reader)
	call(reader, "GET", path+"/events?cursor="+token, "", 422)
	call(reader, "GET", path+"/export", "", 403)
	call(reader, "GET", status, "", 403)
	call(reader, "GET", download, "", 403)
	exec("DELETE FROM project_grants WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3", w, p, reader)
	call(reader, "GET", path+"/events?cursor="+token, "", 403)
	call(reader, "GET", path+event, "", 403)
	body := `{"event_days":2,"trace_days":1,"content_hours":1}`
	call(actor, "PUT", path+"/retention", body, 200)
	call(actor, "PUT", path+"/retention", body, 200)
	require.EqualValues(t, 1, id("SELECT count(*) FROM scoped_audit_events WHERE workspace_id=$1 AND action='audit.retention.set'", w))
	call(finance, "PUT", path+"/retention", body, 403)
	// Input custody is independent of the task/receipt history. A new task
	// inherits the shorter policy and expiry removes only its encrypted retry input.
	taskID := "task_" + uuid.NewString()
	exec(`INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance,replay_input,created_at,input_until)
 VALUES($1,$2,$3,$4,$4,$4,0,'gateway',$1,'fixture','failed','not_sent','encrypted-fixture',now()-interval '2 hours',now()+interval '1 day')`, taskID, w, p, actor)
	require.EqualValues(t, 1, id("SELECT count(*) FROM image_tasks WHERE id=$1 AND input_until<now()", taskID))
	old := at.Add(-5 * 24 * time.Hour)
	oldRow := id("INSERT INTO scoped_audit_events(workspace_id,project_id,resource,action,effect,at) VALUES($1,$2,'project','scope.read','observed',$3) RETURNING id", w, p, old)
	held := id("INSERT INTO scoped_audit_events(workspace_id,project_id,resource,action,effect,at,evidence_until) VALUES($1,$2,'billing_command','billing.adjust','committed',$3,'infinity') RETURNING id", w, p, old)
	_, err = db.ExecContext(ctx, "DELETE FROM scoped_audit_events WHERE id=$1", held)
	require.Error(t, err)
	n, err := store.Purge(ctx, w, p)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	n, err = store.Purge(ctx, w, p)
	require.NoError(t, err)
	require.Zero(t, n)
	require.EqualValues(t, 1, id("SELECT count(*) FROM image_tasks WHERE id=$1 AND replay_input IS NULL", taskID))
	require.EqualValues(t, 0, id("SELECT count(*) FROM scoped_audit_events WHERE id=$1", oldRow))
	require.EqualValues(t, 1, id("SELECT count(*) FROM scoped_audit_events WHERE id=$1", held))
	require.EqualValues(t, 1, id("SELECT count(*) FROM scoped_audit_events WHERE workspace_id=$1 AND action='audit.retention.purge' AND affected=2", w))
}
