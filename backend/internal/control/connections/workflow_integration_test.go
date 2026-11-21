//go:build integration

package connections_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/connections"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestConnectionWorkflow(t *testing.T) {
	ctx := t.Context()
	db, ent, _ := testutil.NewCapabilityPostgres(t)
	aliceKey, bobKey, rotateKey := uuid.NewString(), uuid.NewString(), uuid.NewString()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var redirect atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if redirect.Load() {
			http.Redirect(w, r, "/forbidden", 302)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == rotateKey {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
		}
		if token == "" {
			w.WriteHeader(401)
			return
		}
		model := "model-a"
		if token == bobKey {
			model = "model-b"
		}
		fmt.Fprintf(w, `{"data":[{"id":%q}]}`, model)
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	t.Setenv("SSL_CERT_FILE", ca)
	cfg := &config.Config{}
	cfg.JWT.Secret = uuid.NewString()
	cfg.Totp = config.TotpConfig{EncryptionKey: strings.Repeat("1f", 32), EncryptionKeyConfigured: true}
	cfg.Security.URLAllowlist = config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"127.0.0.1"}, AllowPrivateHosts: true}
	enc, e := repository.NewAESEncryptor(cfg)
	require.NoError(t, e)
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NoError(t, testresources.ValidateRedis(addr))
	cache := repository.NewSchedulerCache(testresources.RedisClient(t, &redis.Options{Addr: addr}))
	accounts := repository.ProvideAccountRepositoryWithSecrets(ent, db, cache, enc, nil)
	probe := service.NewAccountTestService(accounts, nil, nil, nil, nil, nil, cfg, nil)
	identityStore := postgres.New(db)
	store := connections.New(db, identityStore, enc, probe, cfg)
	h := control.New(store)
	id := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}
	member := func() int64 {
		return id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	}
	admin, reader := member(), member()
	w := id("INSERT INTO workspaces(name) VALUES('BE04') RETURNING id")
	other := id("INSERT INTO workspaces(name) VALUES('BE04 other') RETURNING id")
	_, e = db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member'),($4,$2,'admin')", w, admin, reader, other)
	require.NoError(t, e)
	var actor atomic.Int64
	actor.Store(admin)
	router := gin.New()
	session := func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor.Load()})
	}
	v2 := router.Group("/api/v2", session)
	control.RegisterAuthorization(router.Group("/api/v1", session), h)
	control.Register(v2, h)
	identityhttp.Register(v2, identityhttp.New(identityStore, identity.Operations{Readers: []identity.OperationReader{store}}, cfg))
	base := fmt.Sprintf("/api/v2/workspaces/%d", w)
	cpath := base + "/connections"
	type obj = map[string]any
	browser := strings.Repeat("ab", 32)
	do := func(method, path string, b any, v, key string) (int, obj) {
		raw, _ := json.Marshal(b)
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		request.AddCookie(&http.Cookie{Name: "account_authorization_browser", Value: browser})
		if v != "" {
			request.Header.Set("If-Match", strconv.Quote(v))
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, request)
		var out obj
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	call := func(status int, method, path string, b any, v string) obj {
		t.Helper()
		code, out := do(method, path, b, v, uuid.NewString())
		require.Equal(t, status, code, "%s %s, reason=%v", method, path, out["reason"])
		if status >= 400 {
			return out
		}
		return out["data"].(obj)
	}
	oauth := call(201, "POST", cpath, obj{"name": "oauth", "schema_version": "1", "provider": "openai", "auth_type": "oauth"}, "")
	binding := sha256.Sum256([]byte(domain.WorkspaceActorScope(domain.WithWorkspaceScope(ctx, w), "admin:"+strconv.FormatInt(admin, 10)) + ":" + browser))
	pending := service.NewAuthPendingIdentityService(ent)
	draft, err := pending.IssueAccountCredentialDraft(ctx, hex.EncodeToString(binding[:]), "openai", "oauth", map[string]any{"access_token": uuid.NewString(), "expires_at": "1795044066"})
	require.NoError(t, err)
	bridge := fmt.Sprintf("/api/v1/workspaces/%d/connections/%s/authorization", w, oauth["id"])
	ticket := obj{"credential_draft": draft.Reference, "revision": draft.Revision}
	browser = strings.Repeat("cd", 32)
	call(404, "PUT", bridge, ticket, "1")
	browser = strings.Repeat("ab", 32)
	attached := call(200, "PUT", bridge, ticket, "1")
	require.Equal(t, "1", attached["credential"].(obj)["version"])
	call(404, "PUT", bridge, ticket, attached["version"].(string))
	_, _, err = pending.ResolveAccountCredentialDraft(ctx, draft.Reference, hex.EncodeToString(binding[:]))
	require.Error(t, err)
	verify := func(path, version, credential string) obj {
		t.Helper()
		op := call(202, "POST", path+"/verify", obj{"checks": []string{"network", "authorization", "discover_models"}, "credential_version": credential}, version)
		return call(200, "GET", op["status_url"].(string), nil, "")
	}
	pool := call(201, "POST", base+"/pools", obj{"name": "Fixture pool", "max_concurrency": 4, "connection_ids": []string{}}, "")["id"].(string)
	poolPath := base + "/pools/" + pool
	updatedPool := call(200, "PUT", poolPath, obj{"name": "Renamed pool", "max_concurrency": 5}, "")
	require.Equal(t, "Renamed pool", updatedPool["name"])
	require.Equal(t, float64(5), updatedPool["max_concurrency"])
	require.Equal(t, "2", updatedPool["version"])
	updatedPool = call(200, "PATCH", poolPath, obj{"max_concurrency": 6}, "")
	require.Equal(t, "Renamed pool", updatedPool["name"])
	require.Equal(t, float64(6), updatedPool["max_concurrency"])

	temporaryPool := call(201, "POST", base+"/pools", obj{"name": "Temporary pool", "max_concurrency": 1}, "")["id"].(string)
	draftPath := cpath + "/" + oauth["id"].(string)
	draftWithPool := call(200, "PATCH", draftPath, obj{"pool_ids": []string{temporaryPool}}, attached["version"].(string))
	require.Equal(t, []any{temporaryPool}, draftWithPool["pool_ids"])
	temporaryPoolPath := base + "/pools/" + temporaryPool
	code, _ := do("DELETE", temporaryPoolPath, nil, "", "")
	require.Equal(t, http.StatusNoContent, code)
	require.Empty(t, call(200, "GET", draftPath, nil, "")["pool_ids"])
	call(404, "PATCH", temporaryPoolPath, obj{"name": "Removed"}, "")
	setup := func(provider, key, model string) (string, string, string) {
		t.Helper()
		spec := obj{"provider": provider, "schema_version": "1", "auth_type": "api_key", "name": "fixture", "endpoint": server.URL}
		token := uuid.NewString()
		status, out := do("POST", cpath, spec, "", token)
		require.Equal(t, 201, status, "%v", out["reason"])
		c := out["data"].(obj)
		status, out = do("POST", cpath, spec, "", token)
		require.Equal(t, 201, status)
		require.Equal(t, c["id"], out["data"].(obj)["id"])
		path := cpath + "/" + c["id"].(string)
		r := call(200, "PUT", path+"/credential", obj{"operation": "set", "type": "api_key", "secret": key}, "1")
		ver := r["version"].(string)
		require.Equal(t, "1", r["credential"].(obj)["version"])
		keep := call(200, "PUT", path+"/credential", obj{"operation": "keep"}, ver)
		require.Equal(t, ver, keep["version"])
		redirect.Store(true)
		op := verify(path, ver, "1")
		redirect.Store(false)
		require.Equal(t, "failed", op["state"])
		op = verify(path, ver, "1")
		require.Equal(t, "succeeded", op["state"], "%v", op["result"])
		verification := op["result"].(obj)["verification_id"]
		p := call(200, "POST", path+"/preview", obj{"model_ids": []string{model}, "pool_ids": []string{pool}, "limits": obj{"concurrency": 2}}, ver)
		require.Empty(t, p["blocked"])
		c = call(200, "POST", path+"/activate", obj{"preview_id": p["preview_id"], "verification_id": verification, "expected_credential_version": "1"}, ver)
		require.Equal(t, "active", c["state"])
		return path, c["id"].(string), c["version"].(string)
	}
	schemas := call(200, "GET", base+"/provider-schemas?provider=openai", nil, "")
	require.Len(t, schemas["items"], 1)
	a, aid, av := setup("openai", aliceKey, "model-a")
	b, bid, bv := setup("mistral", bobKey, "model-b")
	require.Equal(t, "POOL_IN_USE", call(409, "DELETE", poolPath, nil, "")["reason"])
	both := call(200, "POST", cpath+"/bulk-previews", obj{"ids": []string{aid, bid}, "patch": obj{"limits": obj{"concurrency": 5}}}, "")
	bv = call(200, "PATCH", b, obj{"name": "reviewed name"}, bv)["version"].(string)
	partial := call(202, "POST", cpath+"/bulk-apply", obj{"preview_id": both["preview_id"], "selected_ids": []string{aid, bid}}, "")["result"].(obj)["items"].([]any)
	require.Equal(t, "applied", partial[0].(obj)["state"])
	require.Equal(t, "conflict", partial[1].(obj)["state"])
	av = partial[0].(obj)["version"].(string)
	check := verify(b, bv, "1")
	review := call(200, "POST", b+"/preview", obj{}, bv)
	_, e = db.ExecContext(ctx, "UPDATE connection_verifications SET expires_at=now()-interval '1 second' WHERE id=$1", check["result"].(obj)["verification_id"])
	require.NoError(t, e)
	call(409, "POST", b+"/activate", obj{"preview_id": review["preview_id"], "verification_id": check["result"].(obj)["verification_id"], "expected_credential_version": "1"}, bv)
	check = verify(b, bv, "1")
	review = call(200, "POST", b+"/preview", obj{}, bv)
	bv = call(200, "POST", b+"/activate", obj{"preview_id": review["preview_id"], "verification_id": check["result"].(obj)["verification_id"], "expected_credential_version": "1"}, bv)["version"].(string)
	_, e = db.ExecContext(ctx, "UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2", w, admin)
	require.NoError(t, e)
	hidden := call(200, "GET", check["status_url"].(string), nil, "")
	require.Nil(t, hidden["result"])
	require.Nil(t, hidden["resource_id"])
	_, e = db.ExecContext(ctx, "UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2", w, admin)
	require.NoError(t, e)
	activeID, _ := strconv.ParseInt(aid, 10, 64)
	scoped := domain.WithWorkspaceScope(ctx, w)
	account, e := accounts.GetByID(scoped, activeID)
	require.NoError(t, e)
	require.Equal(t, aliceKey, account.GetCredential("api_key"))
	require.NoError(t, cache.SetAccount(ctx, account))
	cached, e := cache.GetAccount(ctx, activeID)
	require.NoError(t, e)
	require.Empty(t, cached.GetCredential("api_key"))
	var storage string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT row_to_json(c)::text||row_to_json(v)::text||row_to_json(h)::text||row_to_json(a)::text FROM provider_connections c JOIN accounts a ON a.id=c.account_id JOIN connection_verifications v ON v.id=c.verified_id JOIN connection_credential_versions h ON h.account_id=c.account_id WHERE c.account_id=$1`, activeID).Scan(&storage))
	require.NotContains(t, storage, aliceKey)
	info := call(200, "POST", cpath+"/bulk-previews", obj{"ids": []string{aid, bid}, "patch": obj{"model_ids": []string{"model-a"}}}, "")
	items := info["items"].([]any)
	require.Equal(t, "applicable", items[0].(obj)["state"])
	require.Equal(t, "INCOMPATIBLE_PROVIDER", items[1].(obj)["reason"])
	op := call(202, "POST", cpath+"/bulk-apply", obj{"preview_id": info["preview_id"], "selected_ids": []string{aid, bid}}, "")
	require.Contains(t, fmt.Sprint(op["result"]), "applied")
	av = call(200, "GET", a, nil, "")["version"].(string)
	otherBase := fmt.Sprintf("/api/v2/workspaces/%d", other)
	call(404, "GET", otherBase+"/connections/"+aid, nil, "")
	call(404, "GET", otherBase+"/operations/"+op["operation_id"].(string), nil, "")
	foreignPool := call(201, "POST", otherBase+"/pools", obj{"name": "other", "max_concurrency": 1}, "")["id"].(string)
	call(404, "PATCH", a, obj{"pool_ids": []string{foreignPool}}, av)
	actor.Store(reader)
	call(403, "GET", a, nil, "")
	call(404, "GET", op["status_url"].(string), nil, "")
	actor.Store(admin)
	p := call(200, "POST", b+"/preview", obj{}, bv)
	old := call(200, "GET", b, nil, "")["last_verification"].(obj)["verification_id"]
	bv = call(200, "PATCH", b, obj{"name": "changed"}, bv)["version"].(string)
	call(409, "POST", b+"/activate", obj{"preview_id": p["preview_id"], "verification_id": old, "expected_credential_version": "1"}, bv)
	// A legacy refresh commits while the proposed replacement is still on the wire.
	done := make(chan int, 1)
	go func() {
		code, _ := do("POST", a+"/rotations", obj{"expected_credential_version": "1", "replacement": obj{"type": "api_key", "secret": rotateKey}}, av, uuid.NewString())
		done <- code
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("rotation did not reach the upstream")
	}
	account.Credentials["api_key"] = uuid.NewString()
	e = accounts.(interface {
		UpdateCredentialsCAS(context.Context, *service.Account, map[string]any) error
	}).UpdateCredentialsCAS(scoped, account, account.Credentials)
	close(release)
	require.NoError(t, e)
	require.Contains(t, []int{409, 412}, <-done)
	c := call(200, "GET", a, nil, "")
	av = c["version"].(string)
	require.Equal(t, "2", c["credential"].(obj)["version"])
	op = call(202, "POST", a+"/rotations", obj{"expected_credential_version": "2", "replacement": obj{"type": "api_key", "secret": aliceKey}}, av)
	require.Equal(t, "succeeded", op["state"])
	av = call(200, "GET", a, nil, "")["version"].(string)
	call(200, "PUT", a+"/credential", obj{"operation": "clear"}, av)
	_, e = accounts.GetByID(scoped, activeID)
	require.ErrorIs(t, e, service.ErrCredentialSecretRevoked)
	require.Equal(t, int64(4), id("SELECT count(*) FROM connection_credential_versions WHERE account_id=$1 AND revoked_at IS NOT NULL", activeID))
	require.Positive(t, id("SELECT count(*) FROM connection_events WHERE account_id=$1 AND type='credential.rotated'", activeID))
}
