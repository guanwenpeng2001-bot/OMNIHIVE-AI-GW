//go:build integration

package restore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	ctl "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/restore"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-09"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore/testfixture"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type storage struct {
	service.BackupObjectStore
	mu    sync.Mutex
	files map[string][]byte
}

func (s *storage) Upload(_ context.Context, k string, r io.Reader, _ string) (int64, error) {
	b, e := io.ReadAll(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[k] = b
	return int64(len(b)), e
}
func (s *storage) UploadFile(ctx context.Context, k, path, kind string) (int64, error) {
	f, e := os.Open(path)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	return s.Upload(ctx, k, f, kind)
}
func (s *storage) Download(_ context.Context, k string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[k]
	if !ok {
		return nil, errors.New("fixture object missing")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func TestRestoreHTTPAndLegacy(t *testing.T) {
	ctx := t.Context()
	if os.Getenv("BE09_TEST_ISOLATED_DSN") == "" {
		t.Skip("requires independent restore test cluster")
	}
	require.NoError(t, testresources.ValidatePostgres(os.Getenv("UPSTREAM_JOB_TEST_DSN"), "job_test"))
	prod, pcfg := testfixture.Database(t, "UPSTREAM_JOB_TEST_DSN", "job_test")
	_, jcfg := testfixture.Database(t, "BE09_TEST_JOURNAL_DSN", "be09_journal_test")
	_, icfg := testfixture.Database(t, "BE09_TEST_ISOLATED_DSN", "be09_rehearsal_test")
	for env, db := range map[string]string{"JOURNAL": jcfg.Database.DBName, "ISOLATED": icfg.Database.DBName} {
		u, e := url.Parse(os.Getenv("BE09_TEST_" + env + "_DSN"))
		require.NoError(t, e)
		u.Path = "/" + db
		t.Setenv("OMNIHIVE_RESTORE_"+env+"_DSN", u.String())
	}
	require.NoError(t, repository.ApplyMigrations(ctx, prod))
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, prod)))
	settings := repository.NewSettingRepository(client)
	users := repository.NewUserRepository(client, prod)
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NoError(t, testresources.ValidateRedis(addr))
	cache := repository.NewTotpCache(testresources.RedisClient(t, &redis.Options{Addr: addr}))
	totp := service.NewTotpService(users, nil, cache, nil, nil, nil)
	pcfg.Totp.EncryptionKey = strings.Repeat("ad", 32)
	pcfg.Totp.EncryptionKeyConfigured = true
	enc, e := repository.NewAESEncryptor(pcfg)
	require.NoError(t, e)
	objects := &storage{files: map[string][]byte{}}
	newBackup := func() *service.BackupService {
		return service.NewBackupService(settings, pcfg, enc, func(context.Context, *service.BackupS3Config) (service.BackupObjectStore, error) { return objects, nil }, repository.NewPgDumper(pcfg, prod))
	}
	backup := newBackup()
	_, e = backup.UpdateS3Config(ctx, service.BackupS3Config{Bucket: "fixture", AccessKeyID: "fixture", SecretAccessKey: "fixture"})
	require.NoError(t, e)
	var admin, member int64
	require.NoError(t, prod.QueryRow("INSERT INTO users(email,password_hash,role,totp_enabled) VALUES ('a@example.test','fixture','admin',true) RETURNING id").Scan(&admin))
	require.NoError(t, prod.QueryRow("INSERT INTO users(email,password_hash) VALUES ('m@example.test','fixture') RETURNING id").Scan(&member))
	_, e = prod.Exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(1,$1,'admin')", member)
	require.NoError(t, e)
	b, e := backup.StartBackup(ctx, "manual", 0)
	require.NoError(t, e)
	require.Eventually(t, func() bool { r, e := backup.GetBackupRecord(ctx, b.ID); return e == nil && r.Status == "completed" }, time.Minute, 100*time.Millisecond)
	rt, e := legacy.New(prod, pcfg, backup, postgres.New(prod), totp, service.NewUserService(users, settings, nil, nil))
	require.NoError(t, e)
	defer rt.Close()
	engine := gin.New()
	var actor int64
	session := uuid.NewString()
	ctl.Register(engine.Group("/api/v2"), ctl.New(rt), func(c *gin.Context) {
		if actor > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
			c.Set(middleware.ContextKeySessionID, session)
		}
	})
	request := func(method, path string, body any, key string, status int) map[string]any {
		t.Helper()
		data, e := json.Marshal(body)
		require.NoError(t, e)
		req := httptest.NewRequest(method, "/api/v2/admin/restores"+path, bytes.NewReader(data))
		req.Header.Set("Idempotency-Key", key)
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		require.Equal(t, status, res.Code, res.Body.String())
		require.Equal(t, "private, no-store", res.Header().Get("Cache-Control"))
		require.NotEmpty(t, res.Header().Get("X-Request-ID"))
		var out map[string]any
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &out))
		return out
	}
	request("GET", "/rs_missing", nil, "", 401)
	actor = member
	request("POST", "/previews", map[string]any{"backup_id": b.ID, "target": "isolated"}, "preview", 403)
	actor = admin
	p := request("POST", "/previews", map[string]any{"backup_id": b.ID, "target": "isolated"}, "p", 201)["data"].(map[string]any)
	cmd := map[string]any{"preview_id": p["id"], "target": "isolated", "rehearsal_id": nil}
	op := request("POST", "", cmd, "q", 202)["data"].(map[string]any)
	require.Equal(t, op, request("POST", "", cmd, "q", 202)["data"])
	worked, e := rt.Work(ctx)
	require.NoError(t, e)
	require.True(t, worked)
	got := request("GET", "/"+op["id"].(string), nil, "", 200)["data"].(map[string]any)
	require.Equal(t, "succeeded", got["state"], got)
	actor = member
	request("GET", "/"+op["id"].(string), nil, "", 403)
	actor = admin
	p = request("POST", "/previews", map[string]any{"backup_id": b.ID, "target": "production"}, "p2", 201)["data"].(map[string]any)
	cmd = map[string]any{"preview_id": p["id"], "target": "production", "rehearsal_id": op["id"]}
	denied := request("POST", "", cmd, "prod", 403)
	require.Equal(t, "STEP_UP_REQUIRED", denied["reason"])
	require.NoError(t, cache.SetStepUpGrant(ctx, admin, session, service.StepUpGrantTTL))
	expiredGrant := request("POST", "", cmd, "expired-grant", 202)["data"].(map[string]any)
	require.NoError(t, cache.SetStepUpGrant(ctx, admin, session, time.Second))
	require.Eventually(t, func() bool { ok, e := cache.HasStepUpGrant(ctx, admin, session); return e == nil && !ok }, 5*time.Second, 100*time.Millisecond)
	worked, e = rt.Work(ctx)
	require.True(t, worked)
	require.NoError(t, e)
	refused := request("GET", "/"+expiredGrant["id"].(string), nil, "", 200)["data"].(map[string]any)
	require.Equal(t, "failed", refused["state"])
	require.Equal(t, "STEP_UP_REQUIRED", refused["report"].(map[string]any)["reason"])
	require.NoError(t, cache.SetStepUpGrant(ctx, admin, session, service.StepUpGrantTTL))
	_, e = backup.StartAuthorizedRestore(ctx, admin, b.ID, "legacy", "wrong-session")
	require.Error(t, e)
	accepted, e := backup.StartAuthorizedRestore(ctx, admin, b.ID, "legacy", session)
	require.NoError(t, e)
	require.Equal(t, "running", accepted.RestoreStatus)
	again, e := backup.StartAuthorizedRestore(ctx, admin, b.ID, "legacy", session)
	require.NoError(t, e)
	require.Equal(t, accepted.RestoreOperationID, again.RestoreOperationID)
	stop := rt.StartWorker(ctx)
	defer stop()
	require.Eventually(t, func() bool {
		r, e := backup.GetBackupRecord(ctx, b.ID)
		return e == nil && r.RestoreStatus == "completed"
	}, 2*time.Minute, 200*time.Millisecond)
	r, e := backup.GetBackupRecord(ctx, b.ID)
	require.NoError(t, e)
	require.Equal(t, accepted.RestoreOperationID, r.RestoreOperationID)
	require.Equal(t, "succeeded", request("GET", "/"+r.RestoreOperationID, nil, "", 200)["data"].(map[string]any)["state"])
	require.Equal(t, "completed", r.Status)
	restarted := newBackup()
	restarted.Start()
	defer restarted.Stop()
	after, e := legacy.New(prod, pcfg, restarted, postgres.New(prod), totp, service.NewUserService(users, settings, nil, nil))
	require.NoError(t, e)
	defer after.Close()
	complete, e := restarted.GetBackupRecord(ctx, b.ID)
	require.NoError(t, e)
	require.Equal(t, "completed", complete.Status)
	raw, e := restarted.OpenRestoreArchive(ctx, complete)
	require.NoError(t, e)
	_, e = io.Copy(io.Discard, raw)
	require.NoError(t, e)
	require.NoError(t, raw.Close())
	var c int
	require.NoError(t, rt.Journal.QueryRow("SELECT count(*) FROM restore_journal.legacy_requests WHERE actor_id=$1 AND state='submitted'", admin).Scan(&c))
	require.Equal(t, 1, c)
	_, e = prod.Exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(1,$1,'member') ON CONFLICT DO NOTHING", admin)
	require.NoError(t, e)
	_, e = prod.Exec("UPDATE users SET role='user' WHERE id=$1", admin)
	require.NoError(t, e)
	visible, e := rt.ReadOperation(ctx, 1, admin, r.RestoreOperationID)
	require.NoError(t, e)
	require.Nil(t, visible.Result)
	require.Equal(t, "succeeded", visible.State)
	request("GET", "/"+r.RestoreOperationID, nil, "", 403)
}
