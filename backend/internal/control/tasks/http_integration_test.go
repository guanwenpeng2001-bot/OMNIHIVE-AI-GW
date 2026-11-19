//go:build integration

package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type sessionFixture struct {
	grants  []string
	version int64
}

func (f *sessionFixture) Evaluate(_ context.Context, actor, w, p int64, action string) (identity.Access, error) {
	a := identity.Access{WorkspaceID: w, ProjectID: p, ActorID: actor, Version: f.version, Actions: f.grants}
	return a, a.Require(action)
}

type imageContent struct{}

func (imageContent) Open(context.Context, domain.ArtifactContent) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("ok")), nil
}
func TestImageTaskCenterDownloadHTTP(t *testing.T) {
	ctx := t.Context()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	db, e := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, e)
	defer db.Close()
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	id := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&n))
		return n
	}
	w := id("INSERT INTO workspaces(name) VALUES('task HTTP') RETURNING id")
	p := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	task := uuid.NewString()
	_, e = db.ExecContext(ctx, `INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance) VALUES ($1,$2,$3,1,1,1,1,'gateway',$1,'fixture','succeeded','accepted')`, task, w, p)
	require.NoError(t, e)
	_, e = db.ExecContext(ctx, `INSERT INTO image_task_artifacts(task_id,id,item_id,locator,content_type,size_bytes,expires_at) VALUES($1,'a','1','private/key','image/png',2,now()+interval '1 hour')`, task)
	require.NoError(t, e)
	cfg := &config.Config{}
	cfg.JWT.Secret = "task-test-signing-key"
	access := &sessionFixture{[]string{"tasks.read", "tasks.artifacts.read", "tasks.artifacts.revoke"}, 5}
	store := domain.New(db, nil, nil, imageContent{}, nil)
	server := gin.New()
	actor := int64(1)
	Register(server.Group("/api/v2"), New(store, access, cfg), func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
		c.Next()
	})
	req := func(method, path, ticket string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString("{}"))
		r.Header.Set("X-Artifact-Ticket", ticket)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, r)
		require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		return rec
	}
	prefix := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/tasks", w, p)
	base := prefix + "/" + task + "/artifacts/a"
	list := req("GET", prefix+"?kind=image&state=succeeded", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), task)
	ticket := func() string {
		r := req("POST", base+"/download", "")
		require.Equal(t, 200, r.Code, r.Body.String())
		var j struct {
			Data struct {
				Ticket string
				URL    string
			}
		}
		require.NoError(t, json.Unmarshal(r.Body.Bytes(), &j))
		require.Equal(t, base+"/content", j.Data.URL)
		return j.Data.Ticket
	}
	tk := ticket()
	r := req("GET", base+"/content", tk)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Equal(t, "ok", r.Body.String())
	require.NotEmpty(t, r.Header().Get("Content-Disposition"))
	actor = 2
	require.Equal(t, 404, req("GET", base+"/content", tk).Code)
	actor = 1
	require.Equal(t, 404, req("GET", strings.Replace(base, fmt.Sprintf("/projects/%d/", p), fmt.Sprintf("/projects/%d/", p+10000), 1)+"/content", tk).Code)
	access.version++
	require.Equal(t, 404, req("GET", base+"/content", tk).Code)
	tk = ticket()
	access.grants = []string{"tasks.read"}
	require.Equal(t, 403, req("GET", base+"/content", tk).Code)
	access.grants = []string{"tasks.artifacts.read", "tasks.artifacts.revoke"}
	require.Equal(t, 200, req("DELETE", base, "").Code)
	require.Equal(t, 410, req("GET", base+"/content", tk).Code)
}
