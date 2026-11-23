//go:build integration

package verification

import (
	"bytes"
	"context"
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
	models "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/models"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	verification "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/verification"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitypg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type catalogSource func(context.Context, catalog.Perspective, catalog.Window, time.Time) (catalog.Input, error)

func (s catalogSource) Load(ctx context.Context, p catalog.Perspective, w catalog.Window, at time.Time) (catalog.Input, error) {
	return s(ctx, p, w, at)
}

func TestVerificationLifecycleHTTPPostgres(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	gin.SetMode(gin.TestMode)
	model := "be27-" + uuid.NewString()
	entry := catalog.StableID("openai", model)
	const secret = "fixture-catalog-token"
	var upstreamState, traffic atomic.Int64
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("catalog probe requested %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		traffic.Add(1)
		mode := upstreamState.Load()
		if mode == 1 || (mode == 0 && r.Header.Get("Authorization") != "Bearer "+secret) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":%q},{"id":%q}]}`, model, secret)
	}))
	t.Cleanup(fixture.Close)
	root := filepath.Join(t.TempDir(), "fixture-ca.pem")
	require.NoError(t, os.WriteFile(root, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Certificate().Raw}), 0600))
	t.Setenv("SSL_CERT_FILE", root)
	cfg := &config.Config{
		Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, AllowPrivateHosts: true, UpstreamHosts: []string{"127.0.0.1"}}},
	}
	cfg.Totp.EncryptionKey = strings.Repeat("42", 32)
	cfg.Totp.EncryptionKeyConfigured = true
	enc, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	id := func(query string, args ...any) int64 {
		t.Helper()
		var out int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&out))
		return out
	}
	admin := id(`INSERT INTO users(email,password_hash,role) VALUES($1,'fixture','admin') RETURNING id`, uuid.NewString()+"@example.test")
	member := id(`INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, uuid.NewString()+"@example.test")
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(1,$1,'admin')`, member)
	other := id(`INSERT INTO workspaces(name) VALUES ('BE-27 fixture') RETURNING id`)
	newConnection := func(workspace int64) int64 {
		account := id(`INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable)
			VALUES ($1,'BE-27 fixture','openai','apikey','active',false) RETURNING id`, workspace)
		ref := uuid.NewString()
		encrypted, err := service.EncryptVerifiedCredential(ctx, enc, service.CredentialSecret{
			WorkspaceID: workspace, AccountID: account, Reference: ref, Version: 1,
		}, map[string]any{"api_key": secret})
		require.NoError(t, err)
		exec(`INSERT INTO credential_secrets(reference,workspace_id,account_id,ciphertext) VALUES($1,$2,$3,$4)`, ref, workspace, account, encrypted)
		exec(`INSERT INTO provider_connections(workspace_id,account_id,provider,auth_type,name,endpoint,concurrency,state)
			VALUES($1,$2,'openai','api_key','fixture',$3,1,'active')`, workspace, account, fixture.URL)
		return account
	}
	account := newConnection(1)
	foreign := newConnection(other)
	previousID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Hour)
	sample, err := json.Marshal(map[string]any{
		"models": []string{model}, "verified_at": observed,
		"checks": []map[string]string{{"name": "discover_models", "state": "passed"}},
	})
	require.NoError(t, err)
	exec(`INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at)
		VALUES($1,1,$2,1,1,'',true,$3,$4)`, previousID, account, string(sample), observed.Add(24*time.Hour))
	exec(`UPDATE provider_connections SET verified_id=$1 WHERE account_id=$2`, previousID, account)

	boundaries := func() [3]int64 {
		return [3]int64{
			id("SELECT count(*) FROM project_grants"),
			id("SELECT count(*) FROM project_key_policies"),
			id("SELECT count(*) FROM channel_model_pricing"),
		}
	}
	baseline := boundaries()
	access := identitypg.New(db)
	store := verification.NewStore(db, access)
	p := verification.NewConnectionProber(db, enc, service.NewAccountTestService(nil, nil, nil, nil, nil, nil, cfg, nil))
	workerCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		verification.NewWorker(store, p).Run(workerCtx)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	router := gin.New()
	router.Use(middleware.DefaultWorkspaceResolution(service.NewWorkspaceService(repository.NewWorkspaceRepository(db))))
	v2 := router.Group("/api/v2")
	session := func(c *gin.Context) {
		who, _ := strconv.ParseInt(c.GetHeader("X-Actor"), 10, 64)
		if who > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: who})
		}
		c.Next()
	}
	Register(v2, New(store), session)
	identityhttp.Register(v2, identityhttp.New(access, identity.Operations{Readers: []identity.OperationReader{store}}, cfg), session)
	reader := catalog.NewReader(catalogSource(func(ctx context.Context, _ catalog.Perspective, _ catalog.Window, _ time.Time) (catalog.Input, error) {
		compat, err := verification.ReadPublished(ctx, db, []string{entry})
		return catalog.Input{
			Models:        []catalog.Candidate{{Provider: "openai", Model: model}},
			Compatibility: compat,
		}, err
	}), nil)
	models.Register(v2, models.New(reader, access, cfg), nil, nil)

	const adminCatalog = "/api/v2/admin/catalog"
	const targetRoute = adminCatalog + "/verification-targets"
	const verificationRoute = adminCatalog + "/verifications"
	call := func(method, route string, actor int64, key, tag string, body any) *httptest.ResponseRecorder {
		t.Helper()
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, route, bytes.NewReader(buf))
		r.Header.Set("X-Actor", strconv.FormatInt(actor, 10))
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("If-Match", tag)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	ok := func(w *httptest.ResponseRecorder, code int, out any) {
		t.Helper()
		require.Equal(t, code, w.Code, w.Body.String())
		if out != nil {
			payload := struct{ Data any }{Data: out}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		}
	}
	in := verification.CreateTarget{Provider: "openai", Model: model, ConnectionID: foreign,
		AdapterVersion: verification.OpenAIChatAdapterVersion, Action: "text.chat", Protocol: "openai_chat", AccountMode: "api_key"}
	ok(call(http.MethodPost, targetRoute, 0, "target", "", in), 401, nil)
	ok(call(http.MethodPost, targetRoute, member, "target", "", in), 403, nil)
	ok(call(http.MethodPost, targetRoute, admin, "foreign", "", in), 409, nil)
	in.ConnectionID = account
	var target verification.Target
	ok(call(http.MethodPost, targetRoute, admin, "target", "", in), 201, &target)
	require.Equal(t, entry, target.EntryID)
	path := targetRoute + "/" + target.ID
	var detail verification.TargetDetail
	detailResponse := call(http.MethodGet, path, admin, "", "", nil)
	ok(detailResponse, 200, &detail)
	require.Equal(t, versionTag(detail.ScheduleVersion), detailResponse.Header().Get("ETag"))
	require.NotNil(t, detail.LastSuccess)
	require.Equal(t, "connection_verification", detail.LastSuccess.Source)
	require.Nil(t, detail.LastSuccess.FixtureID)
	require.Nil(t, detail.Publication)

	publish := func(key, tag string, body verification.Publish, code int) verification.Publication {
		t.Helper()
		var out verification.Publication
		w := call(http.MethodPost, path+"/publications", admin, key, tag, body)
		if code == 201 {
			ok(w, code, &out)
		} else {
			ok(w, code, nil)
		}
		return out
	}
	public := func() catalog.Detail {
		t.Helper()
		w := call(http.MethodGet, "/api/v2/public/models/"+entry, 0, "", "", nil)
		var d catalog.Detail
		ok(w, 200, &d)
		require.NotContains(t, w.Body.String(), secret)
		require.NotContains(t, w.Body.String(), fixture.URL)
		require.NotContains(t, w.Body.String(), target.ID)
		require.NotContains(t, w.Body.String(), previousID)
		return d
	}
	require.Empty(t, public().Compatibility)
	exp := publish("experimental", `"0"`, verification.Publish{Maturity: "experimental", EvidenceID: detail.LastSuccess.ID}, 201)
	require.Equal(t, "experimental", exp.Maturity)
	require.Equal(t, "experimental", public().Compatibility[0].Maturity)
	publish("bad-stable", `"1"`, verification.Publish{Maturity: "stable", EvidenceID: detail.LastSuccess.ID}, 409)

	matrix := target.Matrix
	matrix.Protocol = ""
	run := func(key string) verification.Observation {
		t.Helper()
		var op, replay identity.Operation
		ok(call(http.MethodPost, verificationRoute, admin, key, "", matrix), 202, &op)
		ok(call(http.MethodPost, verificationRoute, admin, key, "", matrix), 202, &replay)
		require.Equal(t, op.ID, replay.ID)
		ok(call(http.MethodGet, op.StatusURL, member, "", "", nil), 404, nil)
		var terminal struct {
			State  string
			Result *verification.Observation
		}
		require.Eventually(t, func() bool {
			w := call(http.MethodGet, op.StatusURL, admin, "", "", nil)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &struct{ Data any }{Data: &terminal}))
			return terminal.Result != nil
		}, 10*time.Second, 50*time.Millisecond)
		require.Equal(t, target.Matrix, terminal.Result.Matrix)
		return *terminal.Result
	}
	good := run("first")
	require.Equal(t, "success", good.State)
	require.Equal(t, "fresh", good.Freshness)
	require.Equal(t, 1, good.SampleCount)
	require.NotNil(t, good.VerifiedAt)
	require.Equal(t, verification.OpenAIChatFixtureID, *good.FixtureID)
	require.Equal(t, int64(2), traffic.Load())
	mismatch := matrix
	mismatch.AdapterVersion = "ad_other"
	ok(call(http.MethodPost, verificationRoute, admin, "first", "", mismatch), 409, nil)
	stable := publish("good-stable", `"1"`, verification.Publish{Maturity: "stable", EvidenceID: good.ID}, 201)
	require.Equal(t, int64(2), stable.Version)
	publish("other-key-stale", `"1"`, verification.Publish{Maturity: "stable", EvidenceID: good.ID}, 412)
	replay := publish("good-stable", `"1"`, verification.Publish{Maturity: "stable", EvidenceID: good.ID}, 201)
	require.Equal(t, stable.Version, replay.Version)
	d := public()
	require.Equal(t, "stable", d.Compatibility[0].Maturity)
	require.Empty(t, d.Model.Actions)
	require.Equal(t, "requires_scope", d.Model.AccessState)
	require.Equal(t, "requires_scope", d.Model.RouteState)
	require.Equal(t, "unpriced", d.Model.PriceState)

	upstreamState.Store(1)
	rejected := run("reject")
	require.Equal(t, "failed", rejected.State)
	require.Equal(t, "CREDENTIAL_REJECTED", rejected.Reason)
	upstreamState.Store(2)
	inconclusive := run("public-upstream")
	require.Equal(t, "unknown", inconclusive.State)
	require.Equal(t, "unknown", inconclusive.Freshness)
	require.Nil(t, inconclusive.VerifiedAt)
	require.Equal(t, 0, inconclusive.SampleCount)
	ok(call(http.MethodGet, path, admin, "", "", nil), 200, &detail)
	require.Equal(t, inconclusive.ID, detail.LatestAttempt.ID)
	require.Equal(t, good.ID, detail.LastSuccess.ID)
	require.Equal(t, good.ID, public().Compatibility[0].Evidence.ID)
	publish("cannot-republish", `"2"`, verification.Publish{Maturity: "stable", EvidenceID: good.ID}, 409)

	exec(`UPDATE catalog_verification_evidence SET expires_at=observed_at+interval '1 millisecond' WHERE id=$1`,
		strings.TrimPrefix(good.ID, "ve_"))
	require.Equal(t, "stale", public().Compatibility[0].Evidence.Freshness)

	sunset := time.Now().UTC().Add(time.Hour)
	deprecation := publish("retire", `"2"`, verification.Publish{Maturity: "deprecated", Reason: "superseded", SunsetAt: &sunset}, 201)
	require.Equal(t, "superseded", deprecation.Deprecation.Reason)
	require.Equal(t, "deprecated", public().Compatibility[0].Maturity)
	publish("early-close", `"3"`, verification.Publish{Maturity: "closed"}, 409)
	to := int64(2)
	restored := publish("rollback", `"3"`, verification.Publish{RollbackVersion: &to}, 201)
	require.Equal(t, int64(4), restored.Version)
	require.Equal(t, "stable", restored.Maturity)
	require.Equal(t, good.ID, restored.Evidence.ID)
	require.Nil(t, public().Compatibility[0].Deprecation)
	to = 0
	publish("withdraw", `"4"`, verification.Publish{RollbackVersion: &to}, 201)
	require.Empty(t, public().Compatibility)
	require.Equal(t, baseline, boundaries())
}
