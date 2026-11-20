//go:build integration && !windows

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// Execute the production entry in a child process, including flag parsing,
// config, Wire, socket binding, signals, migrations and infrastructure cleanup.
func TestRuntimeProcessEntry(t *testing.T) {
	if os.Getenv("RUNTIME_MODE_CHILD") != "1" {
		return
	}
	os.Args = []string{"omnihive"}
	if m := os.Getenv("RUNTIME_MODE_FLAG"); m != "" {
		os.Args = append(os.Args, "-mode", m)
	}
	flag.CommandLine = flag.NewFlagSet("omnihive", flag.ExitOnError)
	main()
}

type childServer struct {
	base string
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	log  string
}

func startChild(t *testing.T, mode, source string, dsn *url.URL, redisPort, pricing, startupError string, overrides ...string) *childServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	roleConfig := "all"
	roleEnv := ""
	roleFlag := ""
	switch source {
	case "yaml":
		roleConfig = mode
	case "env":
		roleEnv = mode
	case "flag":
		roleEnv = "control"
		roleFlag = mode
	case "default":
	default:
		t.Fatal(source)
	}
	contents := ""
	if source != "default" {
		contents = fmt.Sprintf("runtime:\n  mode: %s\n", roleConfig)
	}
	require.NoError(t, os.WriteFile(cfg, []byte(contents), 0600))
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run=^TestRuntimeProcessEntry$")
	cmd.Dir = dir
	password, _ := dsn.User.Password()
	cmd.Env = append(os.Environ(), "RUNTIME_MODE_CHILD=1", "RUNTIME_MODE_FLAG="+roleFlag, "OMNIHIVE_RUNTIME_MODE="+roleEnv,
		"CONFIG_FILE="+cfg, "OMNIHIVE_DATA_DIR="+dir, "SKIP_SETUP=true", "SERVER_HOST=127.0.0.1", "SERVER_PORT="+strconv.Itoa(port), "DATABASE_HOST="+dsn.Hostname(), "DATABASE_PORT="+dsn.Port(), "DATABASE_USER="+dsn.User.Username(), "DATABASE_PASSWORD="+password, "DATABASE_DBNAME=job_test", "DATABASE_SSLMODE=disable", "REDIS_HOST=127.0.0.1", "REDIS_PORT="+redisPort, "LOG_OUTPUT_TO_FILE=false", "PRICING_DATA_DIR="+pricing, "PRICING_HASH_URL=", "PRICING_REMOTE_URL=https://127.0.0.1:1/prices", "PLUGINS_ROOT_DIR="+dir)
	cmd.Env = append(cmd.Env, "OMNIHIVE_RESTORE_JOURNAL_DSN=", "OMNIHIVE_RESTORE_ISOLATED_DSN=")
	cmd.Env = append(cmd.Env, overrides...)
	p := &childServer{cmd: cmd, base: "http://" + addr, done: make(chan struct{}), log: filepath.Join(dir, "server.log")}
	f, err := os.Create(p.log)
	require.NoError(t, err)
	cmd.Stdout = f
	cmd.Stderr = f
	err = cmd.Start()
	f.Close()
	require.NoError(t, err)
	go func() { p.err = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
			_ = cmd.Process.Kill()
			<-p.done
		}
	})
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-p.done:
			if startupError == "" {
				p.fail(t, "startup exited")
			}
			require.Error(t, p.err)
			raw, err := os.ReadFile(p.log)
			require.NoError(t, err)
			require.Contains(t, string(raw), startupError)
			return p
		case <-deadline.C:
			p.fail(t, "readiness deadline")
		case <-time.After(100 * time.Millisecond):
			client := http.Client{Timeout: 2 * time.Second}
			res, e := client.Get(p.base + "/ready")
			if e != nil {
				continue
			}
			res.Body.Close()
			if res.StatusCode == 200 {
				require.Empty(t, startupError, "an unavailable required dependency became ready")
				require.Equal(t, mode, res.Header.Get("X-OmniHive-Mode"))
				if mode != "gateway" {
					require.Contains(t, res.Header.Get("X-OmniHive-Degraded"), "restore")
				}
				return p
			}
		}
	}
}
func (p *childServer) fail(t *testing.T, reason string) {
	t.Helper()
	raw, _ := os.ReadFile(p.log)
	if len(raw) > 2500 {
		raw = raw[len(raw)-2500:]
	}
	t.Fatalf("%s\n%s", reason, raw)
}
func (p *childServer) stop(t *testing.T, sig os.Signal) {
	t.Helper()
	require.NoError(t, p.cmd.Process.Signal(sig))
	select {
	case <-p.done:
		if sig != os.Kill && p.err != nil {
			p.fail(t, "shutdown exited")
		}
	case <-time.After(25 * time.Second):
		p.fail(t, "shutdown deadline")
	}
}
func (p *childServer) request(t *testing.T, status int, method, path, token, body string) string {
	t.Helper()
	req, err := http.NewRequest(method, p.base+path, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	require.NoError(t, err)
	if res.StatusCode != status {
		t.Fatalf("%s %s returned %d, expected %d (%s)", method, path, res.StatusCode, status, raw)
	}
	return string(raw)
}

func TestRuntimeSplitProcesses(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	dsn, err := url.Parse(os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	_, redisPort, err := net.SplitHostPort(os.Getenv("AFFINITY_REDIS_TEST_ADDR"))
	require.NoError(t, err)
	pricing := t.TempDir()
	raw, err := os.ReadFile("../../resources/model-pricing/model_prices_and_context_window.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pricing, "model_prices_and_context_window.json"), raw, 0600))
	for _, tc := range []struct{ name, message, override string }{
		{"postgres", "required PostgreSQL dependency unavailable", "DATABASE_PORT=1"},
		{"redis", "required Redis dependency unavailable", "REDIS_PORT=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			startChild(t, "worker", "flag", dsn, redisPort, pricing, tc.message, tc.override)
		})
	}
	start := func(mode, source string) *childServer {
		return startChild(t, mode, source, dsn, redisPort, pricing, "")
	}
	all := start("all", "default")
	groupName := "runtime-mode-" + uuid.NewString()
	email := groupName + "@example.test"
	password := uuid.NewString()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	var user, group, key, account int64
	require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash,role,balance) VALUES ($1,$2,'admin',100) RETURNING id", email, string(hash)).Scan(&user))
	require.NoError(t, db.QueryRow("INSERT INTO groups(name,platform) VALUES ($1,'openai') RETURNING id", groupName).Scan(&group))
	secret := uuid.NewString()
	require.NoError(t, db.QueryRow("INSERT INTO api_keys(user_id,group_id,name) VALUES($1,$2,'runtime-mode-key') RETURNING id", user, group).Scan(&key))
	_, err = db.Exec("INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest,display_suffix,status) VALUES(1,$1,$2,'test','current')", key, service.APIKeyCredentialDigest(secret))
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("INSERT INTO accounts(name,platform,type) VALUES($1,'openai','apikey') RETURNING id", groupName).Scan(&account))
	_, err = db.Exec("INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES ($1,$2,now(),now()+interval '1 day')", user, group)
	require.NoError(t, err)
	batchID := "batch-" + uuid.NewString()
	_, err = db.Exec("INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,provider,model,status,item_count) VALUES ($1,$2,$3,'gemini','runtime-model','completed',1)", batchID, user, key)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO sub2api_plugin_installations(plugin_key,name,version,artifact_path,install_path,binary_path,binary_sha256,manifest) VALUES ($1::text,$1::text,'1.0','unused','unused','unused',repeat('0',64),jsonb_build_object('schema_version',1,'id',$1::text,'capabilities',jsonb_build_array()))", groupName)
	require.NoError(t, err)
	payload, _ := json.Marshal(map[string]string{"email": email, "password": password})
	response := all.request(t, 200, "POST", "/api/v1/auth/login", "", string(payload))
	var auth struct {
		Data struct {
			AccessToken string `json:"access_token"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(response), &auth))
	require.NotEmpty(t, auth.Data.AccessToken)
	token := auth.Data.AccessToken
	retained := func(control, gateway *childServer) {
		require.Contains(t, control.request(t, 200, "GET", "/api/v1/user/profile", token, ""), email)
		require.Contains(t, control.request(t, 200, "GET", "/api/v1/subscriptions", token, ""), groupName)
		require.Contains(t, control.request(t, 200, "GET", "/api/v2/admin/extensions", token, ""), groupName)
		require.Contains(t, gateway.request(t, 200, "GET", "/v1/images/batches", secret, ""), batchID)
		for _, path := range []string{"/api/v1/user/onboarding", "/api/v1/user/mcp/grants", "/api/v1/user/audit-logs", "/api/v1/user/routing-policies"} {
			control.request(t, 401, "GET", path, "", "")
			control.request(t, 200, "GET", path, token, "")
		}
		for _, path := range []string{"/api/v1/public/rankings", "/api/v1/model-plaza/rankings"} {
			control.request(t, 503, "GET", path, "", "")
		}
	}
	retained(all, all)
	all.stop(t, syscall.SIGTERM)
	// A rolling restart must preserve another process's unexpired concurrency slot.
	cache := redis.NewClient(&redis.Options{Addr: os.Getenv("AFFINITY_REDIS_TEST_ADDR")})
	t.Cleanup(func() { _ = cache.Close() })
	slots := repository.NewConcurrencyCache(cache, 10, 30)
	ctx := t.Context()
	held, err := slots.AcquireAccountSlot(ctx, account, 1, groupName)
	require.NoError(t, err)
	require.True(t, held)
	defer slots.ReleaseAccountSlot(ctx, account, groupName)
	control := start("control", "yaml")
	gateway := start("gateway", "env")
	retained(control, gateway)
	control.request(t, 404, "GET", "/v1/images/batches", secret, "")
	gateway.request(t, 404, "GET", "/api/v1/subscriptions", token, "")
	// Publish a second key in control and use its one-time credential on gateway.
	var issued struct {
		Data struct {
			Secret string
			APIKey struct{ ID int64 } `json:"api_key"`
		}
	}
	body := control.request(t, 200, "POST", "/api/v1/keys", token, fmt.Sprintf(`{"name":"published","group_id":%d}`, group))
	require.NoError(t, json.Unmarshal([]byte(body), &issued))
	require.NotEmpty(t, issued.Data.Secret)
	gateway.request(t, 200, "GET", "/v1/images/batches", issued.Data.Secret, "")
	control.request(t, 200, "DELETE", fmt.Sprintf("/api/v1/keys/%d", issued.Data.APIKey.ID), token, "")
	gateway.request(t, 401, "GET", "/v1/images/batches", issued.Data.Secret, "")

	taskID := "runtime-mode-" + uuid.NewString()
	_, err = db.Exec(`INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance,generation,lease_owner,lease_until,replay_input,input_until)
		SELECT $1,workspace_id,id,$2,$2,$2,$3,'gateway',$1,'model','running','unknown',1,'lost',now()-interval '1 second','expired',now()-interval '1 second'
		FROM projects WHERE workspace_id=1 AND is_default`, taskID, user, key)
	require.NoError(t, err)
	requestID := queueReceipt(t, db, user, key, account)
	// Hold the receipt row so process death occurs inside real financial recovery.
	lock, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer lock.Rollback()
	var lockerPID int
	require.NoError(t, lock.QueryRow("SELECT pg_backend_pid() FROM usage_settlement_receipts WHERE request_id=$1 FOR UPDATE", requestID).Scan(&lockerPID))
	awaitBlocked := func() {
		t.Helper()
		require.Eventually(t, func() bool {
			var blocked bool
			err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%usage_settlement_receipts%')`, lockerPID).Scan(&blocked)
			return err == nil && blocked
		}, 25*time.Second, 100*time.Millisecond)
	}
	var state string
	check := func() string {
		t.Helper()
		require.NoError(t, db.QueryRow("SELECT state FROM usage_settlement_receipts WHERE request_id=$1", requestID).Scan(&state))
		return state
	}
	require.Equal(t, "pending", check())
	worker := start("worker", "flag")
	awaitBlocked()
	worker.request(t, 404, "GET", "/v1/models", secret, "")
	worker.stop(t, os.Kill)
	require.Equal(t, "pending", check())
	restarted := start("worker", "yaml")
	peer := start("worker", "env")
	peer.request(t, 404, "GET", "/api/v1/user/profile", token, "")
	awaitBlocked()
	require.NoError(t, lock.Rollback())
	require.Eventually(t, func() bool { return check() == "settled" }, 25*time.Second, 100*time.Millisecond)
	var taskState string
	var generation int
	var expired bool
	require.NoError(t, db.QueryRow(`SELECT state,generation,replay_input IS NULL FROM image_tasks WHERE id=$1`, taskID).Scan(&taskState, &generation, &expired))
	require.Equal(t, "unknown", taskState)
	require.Equal(t, 2, generation)
	require.True(t, expired)
	active, err := slots.GetAccountConcurrency(ctx, account)
	require.NoError(t, err)
	require.Equal(t, 1, active)
	var balance float64
	var count int
	require.NoError(t, db.QueryRow("SELECT balance FROM users WHERE id=$1", user).Scan(&balance))
	require.Equal(t, 99.0, balance)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", requestID, key).Scan(&count))
	require.Equal(t, 1, count)
	control.request(t, 200, "DELETE", fmt.Sprintf("/api/v1/keys/%d", key), token, "")
	gateway.request(t, 401, "GET", "/v1/images/batches", secret, "")
	for _, p := range []*childServer{restarted, peer, gateway, control} {
		p.stop(t, syscall.SIGTERM)
	}
}

func queueReceipt(t *testing.T, db *sql.DB, user, key, account int64) string {
	t.Helper()
	id := uuid.NewString()
	amount := 1.0
	snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: service.PricingStatePriced, Model: "runtime-model", Provider: "openai", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: service.QuantizeUsageBillingAmount(amount)})
	require.NoError(t, err)
	receipt, err := service.PrepareUsageSettlementReceipt(&service.UsageSettlementReceipt{Key: service.UsageSettlementKey{RequestID: id, UserID: user, APIKeyID: key}, Command: service.UsageBillingCommand{RequestID: id, APIKeyID: key, UserID: user, AccountID: account, AccountType: service.AccountTypeAPIKey, Model: "runtime-model", BalanceCost: amount, APIKeyQuotaCost: amount, APIKeyRateLimitCost: amount}, Usage: service.UsageLog{RequestID: id, UserID: user, APIKeyID: key, AccountID: account, Model: "runtime-model", TotalCost: amount, ActualCost: amount, RateMultiplier: 1, PricingState: service.PricingStatePriced, PricingSnapshot: string(bytes.TrimSpace(snapshot))}, ResolutionState: "resolved", CompletionState: "complete"})
	require.NoError(t, err)
	_, err = repository.NewUsageSettlementReceiptRepository(db).Persist(context.Background(), receipt)
	require.NoError(t, err)
	return id
}
