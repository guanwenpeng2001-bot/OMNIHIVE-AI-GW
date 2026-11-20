//go:build integration

package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	workspace "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type reader struct{ workspace, project, owner int64 }

func (r reader) ReadOperation(_ context.Context, w, actor int64, id string) (domain.Operation, error) {
	if w != r.workspace || id != "export1" {
		return domain.Operation{}, fault.NotFound
	}
	return domain.Operation{ID: id, State: "succeeded", ProjectID: r.project, OwnerID: r.owner, DelegatedAction: "requests.export"}, nil
}

func TestProjectIdentity(t *testing.T) {
	ctx := t.Context()
	db, ent, _ := testutil.NewCapabilityPostgres(t)
	store := postgres.New(db)
	id := func(q string, args ...any) int64 {
		t.Helper()
		var v int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&v))
		return v
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	newUser := func() int64 {
		return id("INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',1) RETURNING id", uuid.NewString()+"@example.test")
	}
	admin, member, outsider := newUser(), newUser(), newUser()
	w := id("INSERT INTO workspaces(name) VALUES('identity fixture') RETURNING id")
	other := id("INSERT INTO workspaces(name) VALUES('other') RETURNING id")
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member'),($4,$5,'admin')", w, admin, member, other, outsider)
	def := id("SELECT identity_default_project($1)", w)
	p, err := store.CreateProject(ctx, admin, w, "Experiment", "project-1")
	require.NoError(t, err)
	replay, err := store.CreateProject(ctx, admin, w, "Experiment", "project-1")
	require.NoError(t, err)
	require.Equal(t, p, replay)
	_, err = store.CreateProject(ctx, admin, w, "Different", "project-1")
	require.ErrorIs(t, err, fault.Conflict)
	_, err = store.Evaluate(ctx, member, w, p.ID, "")
	require.ErrorIs(t, err, fault.NotFound)
	_, err = store.Evaluate(ctx, member, w, def, "keys.create")
	require.ErrorIs(t, err, fault.Forbidden)
	_, err = store.SetGrants(ctx, admin, w, member, 1, []int64{p.ID}, []string{"keys.create", "keys.read", "text.chat", "usage.read"})
	require.NoError(t, err)
	_, err = store.ChangeProject(ctx, admin, w, p.ID, 100, "Changed", false)
	var stale *fault.Fault
	require.ErrorAs(t, err, &stale)
	require.Equal(t, 412, stale.Status)
	args := domain.CreateKey{Name: "Search", ModelAccess: domain.ModelAccess{Mode: "allow_list", IDs: []string{"m1"}}, Actions: []string{"text.chat", "usage.read"}}
	key, err := store.CreateKey(ctx, member, w, p.ID, args, "key-1")
	require.NoError(t, err)
	require.NotNil(t, key.Credential)
	again, err := store.CreateKey(ctx, member, w, p.ID, args, "key-1")
	require.NoError(t, err)
	require.Nil(t, again.Credential)
	require.Equal(t, key.ID, again.ID)
	require.Zero(t, id("SELECT count(*) FROM identity_create_receipts WHERE result::text LIKE $1", "%"+*key.Credential+"%"))
	keys, err := store.Keys(ctx, member, w, p.ID, domain.Page{Limit: 25, AsOf: time.Now()})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Nil(t, keys[0].Credential)
	require.NoError(t, store.RequireResource(ctx, member, w, p.ID, "keys.read", "api_keys", key.ID))
	require.ErrorIs(t, store.RequireResource(ctx, admin, w, def, "keys.read", "api_keys", key.ID), fault.NotFound)
	_, err = db.ExecContext(ctx, "INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('fixture',1,$1,$2)", other, p.ID)
	require.Error(t, err)
	_, err = db.ExecContext(ctx, "UPDATE project_resources SET workspace_id=$1, project_id=identity_default_project($1) WHERE kind='api_keys' AND resource_id=$2", other, key.ID)
	require.Error(t, err)
	actor := member
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.JWT.Secret = uuid.NewString()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v2 := engine.Group("/api/v2", func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor}) })
	h := control.New(store, domain.Operations{Readers: []domain.OperationReader{reader{w, p.ID, admin}}}, cfg)
	control.Register(v2, h)
	call := func(router *gin.Engine, method, path, body, header, secret string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set(header, secret)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var data map[string]any
		if rec.Body.Len() > 0 {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &data))
		}
		if router == engine {
			require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
			require.NotEmpty(t, rec.Header().Get("X-Request-ID"))
		}
		return rec.Code, data
	}
	wp := fmt.Sprintf("/api/v2/workspaces/%d", w)
	pp := wp + "/projects/" + strconv.FormatInt(p.ID, 10)
	code, b := call(engine, "GET", pp+"/access", "", "", "")
	require.Equal(t, 200, code)
	require.Equal(t, strconv.FormatInt(member, 10), b["data"].(map[string]any)["actor_id"])
	code, _ = call(engine, "GET", wp+"/operations/export1", "", "", "")
	require.Equal(t, 404, code)
	actor = admin
	code, _ = call(engine, "GET", wp+"/operations/export1", "", "", "")
	require.Equal(t, 200, code)
	code, b = call(engine, "GET", wp+"/projects?limit=1", "", "", "")
	require.Equal(t, 200, code)
	cursor := b["data"].(map[string]any)["next_cursor"].(string)
	actor = outsider
	code, _ = call(engine, "GET", pp+"/keys", "", "", "")
	require.Equal(t, 404, code)
	code, _ = call(engine, "GET", fmt.Sprintf("/api/v2/workspaces/%d/projects?cursor=%s", other, cursor), "", "", "")
	require.Equal(t, 422, code)
	actor = member
	code, _ = call(engine, "POST", wp+"/projects", `{"name":"Forbidden"}`, "Idempotency-Key", "denied")
	require.Equal(t, 403, code)

	actor = admin
	code, b = call(engine, "POST", wp+"/projects", `{"name":"Archive me"}`, "Idempotency-Key", "archive")
	require.Equal(t, 201, code)
	archived := wp + "/projects/" + b["data"].(map[string]any)["id"].(string)
	code, _ = call(engine, "PATCH", archived, `{"name":"Rename me"}`, "If-Match", `"1"`)
	require.Equal(t, 200, code)
	code, b = call(engine, "DELETE", archived, "", "If-Match", `"1"`)
	require.Equal(t, 412, code)
	require.Equal(t, "2", b["metadata"].(map[string]any)["current_version"])
	code, _ = call(engine, "DELETE", archived, "", "If-Match", `"2"`)
	require.Equal(t, 200, code)
	code, _ = call(engine, "GET", archived, "", "", "")
	require.Equal(t, 404, code)
	actor = member
	code, b = call(engine, "GET", "/api/v2/me/scopes", "", "", "")
	require.Equal(t, 200, code)
	require.Len(t, b["data"].(map[string]any)["items"], 1)
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if addr == "" {
		t.Fatal("requires AFFINITY_REDIS_TEST_ADDR")
	}
	require.NoError(t, testresources.ValidateRedis(addr))
	rc := testresources.RedisClient(t, &redis.Options{Addr: addr})
	cfg.APIKeyAuth.L2TTLSeconds = 600
	repo := repository.NewAPIKeyRepository(ent, db)
	userKey := &service.APIKey{UserID: member, Name: "legacy user key", Key: uuid.NewString(), Status: service.StatusActive}
	require.NoError(t, repo.Create(workspace.WithWorkspaceScope(ctx, w), userKey))
	require.Equal(t, def, id("SELECT project_id FROM project_resources WHERE workspace_id=$1 AND kind='api_keys' AND resource_id=$2", w, userKey.ID))
	userScope, err := store.LegacyScope(ctx, w, userKey.ID, member, service.APIKeyCredentialDigest(userKey.Key))
	require.NoError(t, err)
	require.Equal(t, def, userScope.ProjectID)
	serviceA := service.NewAPIKeyService(repo, nil, nil, nil, nil, repository.NewAPIKeyCache(rc), cfg)
	serviceB := service.NewAPIKeyService(repo, nil, nil, nil, nil, repository.NewAPIKeyCache(rc), cfg)
	gateway := func(s *service.APIKeyService, scoped bool) *gin.Engine {
		r := gin.New()
		if scoped {
			r.Use(middleware.InstallProjectScopes(store, cfg))
		}
		check := func(c *gin.Context) {
			if scoped {
				s, ok := scope.From(c.Request.Context())
				require.True(t, ok)
				require.Equal(t, w, s.WorkspaceID)
			}
			c.Status(200)
		}
		ordinary := gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(s, nil, cfg))
		r.POST("/v1/chat/completions", ordinary, check)
		r.GET("/v1/usage", ordinary, check)
		r.POST("/v1beta/models/*modelAction", middleware.APIKeyAuthWithSubscriptionGoogle(s, nil, cfg), check)
		return r
	}
	old, newGateway := gateway(serviceA, false), gateway(serviceB, true)
	code, _ = call(old, "POST", "/v1/chat/completions", `{"model":"m1"}`, "x-api-key", *key.Credential)
	require.Equal(t, 401, code)
	code, b = call(newGateway, "POST", "/v1/chat/completions", `{"model":"m1"}`, "x-api-key", *key.Credential)
	require.Equal(t, 200, code, "%v", b)
	code, _ = call(newGateway, "POST", "/v1/chat/completions", `{"model":"m1","MODEL":"m2"}`, "x-api-key", *key.Credential)
	require.Equal(t, 403, code)
	code, _ = call(newGateway, "POST", "/v1beta/models/m1:generateContent?key="+*key.Credential, "{}", "", "")
	require.Equal(t, 200, code)
	code, _ = call(newGateway, "POST", "/v1beta/models/m1:embedContent?key="+*key.Credential, "{}", "", "")
	require.Equal(t, 403, code)
	_, err = db.ExecContext(ctx, "INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest,status) VALUES($1,$2,$3,'current')", w, key.ID, uuid.NewString())
	require.Error(t, err)
	exec("UPDATE users SET balance=0 WHERE id=$1", member)
	code, _ = call(newGateway, "GET", "/v1/usage", "", "x-api-key", *key.Credential)
	require.Equal(t, 200, code)
	code, _ = call(newGateway, "POST", "/v1/chat/completions", `{"model":"m1"}`, "x-api-key", *key.Credential)
	require.Equal(t, 429, code)
	exec("UPDATE users SET balance=1 WHERE id=$1", member)
	actor = admin
	grantsPath := fmt.Sprintf("%s/members/%d/grants", wp, member)
	code, b = call(engine, "GET", wp+"/members", "", "", "")
	require.Equal(t, 200, code)
	require.Len(t, b["data"].(map[string]any)["items"], 2)
	code, b = call(engine, "GET", grantsPath, "", "", "")
	require.Equal(t, 200, code)
	current := b["data"].(map[string]any)
	require.Equal(t, []any{strconv.FormatInt(p.ID, 10)}, current["project_ids"])
	require.Contains(t, current["actions"], "keys.read")
	code, _ = call(engine, "PUT", grantsPath, fmt.Sprintf(`{"project_ids":["%d"],"actions":["keys.read","requests.export"]}`, p.ID), "If-Match", strconv.Quote(current["version"].(string)))
	require.Equal(t, 200, code)
	actor = member
	code, _ = call(engine, "GET", grantsPath, "", "", "")
	require.Equal(t, 403, code)
	code, _ = call(engine, "GET", wp+"/operations/export1", "", "", "")
	require.Equal(t, 200, code)
	code, _ = call(newGateway, "POST", "/v1/chat/completions", `{"model":"m1"}`, "x-api-key", *key.Credential)
	require.Equal(t, 403, code)
	exec("DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2", w, member)
	code, _ = call(engine, "GET", pp+"/access", "", "", "")
	require.Equal(t, 404, code)
	code, _ = call(newGateway, "POST", "/v1/chat/completions", `{"model":"m1"}`, "x-api-key", *key.Credential)
	require.Equal(t, 401, code)
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')", w, member)
	_, err = store.SetGrants(ctx, admin, w, member, 1, []int64{p.ID}, []string{"text.chat"})
	require.NoError(t, err)
	code, _ = call(newGateway, "POST", "/v1/chat/completions", `{"model":"m1"}`, "x-api-key", *key.Credential)
	require.Equal(t, 401, code)
	legacy := id("INSERT INTO api_keys(workspace_id,user_id,name) VALUES($1,$2,'legacy') RETURNING id", w, member)
	secret := "fixture-" + uuid.NewString()
	digest := sha256.Sum256([]byte(secret))
	exec("INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest) VALUES($1,$2,$3)", w, legacy, hex.EncodeToString(digest[:]))
	code, _ = call(old, "GET", "/v1/usage", "", "x-api-key", secret)
	require.Equal(t, 200, code)
	code, _ = call(newGateway, "GET", "/v1/usage", "", "x-api-key", secret)
	require.Equal(t, 200, code)
	exec("UPDATE api_key_credentials SET status='revoked',revoked_at=now() WHERE api_key_id=$1", legacy)
	cached, err := repository.NewAPIKeyCache(rc).GetAuthCache(ctx, hex.EncodeToString(digest[:]))
	require.NoError(t, err)
	require.NotNil(t, cached.Snapshot)
	code, _ = call(newGateway, "GET", "/v1/usage", "", "x-api-key", secret)
	require.Equal(t, 401, code)

	bad := id("INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,provider,model,item_count) VALUES($1,$2,$3,'test','m1',0) RETURNING id", uuid.NewString(), outsider, legacy)
	good := id("INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,provider,model,item_count) VALUES($1,$2,$3,'test','m1',0) RETURNING id", uuid.NewString(), member, legacy)
	for id("SELECT identity_backfill_batch('batch_image_jobs',1)") > 0 {
	}
	require.Equal(t, def, id("SELECT project_id FROM project_resources WHERE kind='batch_image_jobs' AND resource_id=$1", good))
	require.EqualValues(t, 1, id("SELECT count(*) FROM identity_scope_quarantine WHERE kind='batch_image_jobs' AND resource_id=$1", bad))
	require.ErrorIs(t, store.RequireResource(ctx, admin, w, def, "tasks.read", "batch_image_jobs", bad), fault.NotFound)
	require.NoError(t, store.RequireResource(ctx, admin, w, def, "tasks.read", "batch_image_jobs", good))
	require.Zero(t, id("SELECT identity_backfill_batch('batch_image_jobs',1)"))
	exec("DELETE FROM project_resources WHERE kind='batch_image_jobs' AND resource_id=$1", good)
	require.Positive(t, id("SELECT identity_backfill_batch('batch_image_jobs',1)"))
	require.Equal(t, def, id("SELECT project_id FROM project_resources WHERE kind='batch_image_jobs' AND resource_id=$1", good))
	require.Equal(t, member, id("SELECT user_id FROM api_keys WHERE id=$1", legacy))
}

func TestConcurrentDefaultProject(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	// Removing only the freshly seeded row simulates a workspace awaiting its first lazy default.
	var w int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO workspaces(name) VALUES('race') RETURNING id").Scan(&w))
	_, err := db.ExecContext(ctx, "DELETE FROM projects WHERE workspace_id=$1", w)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan int64, 16)
	errs := make(chan error, 16)
	start := make(chan struct{})
	for range 16 {
		wg.Go(func() {
			<-start
			var p int64
			err := db.QueryRowContext(ctx, "SELECT identity_default_project($1)", w).Scan(&p)
			results <- p
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var expected int64
	for p := range results {
		if expected == 0 {
			expected = p
		}
		require.Equal(t, expected, p)
	}
}

func TestProjectKeyGroupBinding(t *testing.T) {
	db, ent, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	id := func(query string, args ...any) int64 {
		t.Helper()
		var v int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&v))
		return v
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	user := id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	w := id("INSERT INTO workspaces(name) VALUES('binding test') RETURNING id")
	p := id("INSERT INTO projects(name,workspace_id) VALUES('custom',$1) RETURNING id", w)
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", w, user)
	group := id("INSERT INTO groups(workspace_id,name,platform,is_exclusive) VALUES($1,$2,'openai',true) RETURNING id", w, uuid.NewString())
	store := postgres.New(db)
	in := domain.CreateKey{Name: "scoped", Actions: []string{"text.chat"}, GroupID: &group, ModelAccess: domain.ModelAccess{Mode: "inherit"}}
	_, err := store.CreateKey(ctx, user, w, p, in, "wrong-project")
	require.ErrorIs(t, err, fault.NotFound)
	exec("INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('groups',$1,$2,$3)", group, w, p)
	_, err = store.CreateKey(ctx, user, w, p, in, "exclusive")
	require.ErrorIs(t, err, fault.NotFound)
	auto := in
	auto.GroupID = nil
	withoutGroup, err := store.CreateKey(ctx, user, w, p, auto, "auto-no-permission")
	require.NoError(t, err)
	require.Equal(t, int64(1), id("SELECT count(*) FROM api_keys WHERE id=$1 AND group_id IS NULL", withoutGroup.ID))
	exec("INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)", user, group)
	key, err := store.CreateKey(ctx, user, w, p, in, "permitted")
	require.NoError(t, err)
	require.Equal(t, group, id("SELECT group_id FROM api_keys WHERE id=$1", key.ID))

	standard := id("INSERT INTO groups(workspace_id,name,platform,is_exclusive) VALUES($1,$2,'openai',false) RETURNING id", w, uuid.NewString())
	exec("INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('groups',$1,$2,$3)", standard, w, p)
	key, err = store.CreateKey(ctx, user, w, p, auto, "auto-standard")
	require.NoError(t, err)
	require.Equal(t, standard, id("SELECT group_id FROM api_keys WHERE id=$1", key.ID))
	account := id("INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable) VALUES($1,'available','openai','apikey','active',true) RETURNING id", w)
	exec("INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)", account, group)
	key, err = store.CreateKey(ctx, user, w, p, auto, "auto-with-account")
	require.NoError(t, err)
	require.Equal(t, group, id("SELECT group_id FROM api_keys WHERE id=$1", key.ID))

	repo := repository.NewAPIKeyRepository(ent, db).(interface {
		CreateInProject(context.Context, *service.APIKey, int64) error
	})
	userKey := &service.APIKey{UserID: user, Name: "user key", GroupID: &group, Key: uuid.NewString(), Status: service.StatusActive}
	require.NoError(t, repo.CreateInProject(workspace.WithWorkspaceScope(ctx, w), userKey, p))
	require.Equal(t, p, id("SELECT project_id FROM project_resources WHERE kind='api_keys' AND resource_id=$1", userKey.ID))
	digest := service.APIKeyCredentialDigest(userKey.Key)
	resolved, err := store.LegacyScope(ctx, w, userKey.ID, user, digest)
	require.NoError(t, err)
	require.Equal(t, p, resolved.ProjectID)
	exec("DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2", w, user)
	_, err = store.LegacyScope(ctx, w, userKey.ID, user, digest)
	require.ErrorIs(t, err, fault.NotFound)
}
