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
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// Execute the production entry in a child process, including flag parsing,
// config, Wire, socket binding, signals, migrations and infrastructure cleanup.
func TestRuntimeProcessEntry(t *testing.T) {
	if os.Getenv("BE22_CHILD") != "1" {
		return
	}
	os.Args = []string{"omnihive"}
	if m := os.Getenv("BE22_FLAG"); m != "" {
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

func startChild(t *testing.T, mode, source string, dsn *url.URL, redisPort, pricing string) *childServer {
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
		roleFlag = mode
	case "default":
	default:
		t.Fatal(source)
	}
	require.NoError(t, os.WriteFile(cfg, []byte(fmt.Sprintf("runtime:\n  mode: %s\n", roleConfig)), 0600))
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run=^TestRuntimeProcessEntry$")
	cmd.Dir = dir
	password, _ := dsn.User.Password()
	cmd.Env = append(os.Environ(), "BE22_CHILD=1", "BE22_FLAG="+roleFlag, "OMNIHIVE_RUNTIME_MODE="+roleEnv,
		"CONFIG_FILE="+cfg, "OMNIHIVE_DATA_DIR="+dir, "SKIP_SETUP=true", "SERVER_HOST=127.0.0.1", "SERVER_PORT="+strconv.Itoa(port), "DATABASE_HOST="+dsn.Hostname(), "DATABASE_PORT="+dsn.Port(), "DATABASE_USER="+dsn.User.Username(), "DATABASE_PASSWORD="+password, "DATABASE_DBNAME=job_test", "DATABASE_SSLMODE=disable", "REDIS_HOST=127.0.0.1", "REDIS_PORT="+redisPort, "LOG_OUTPUT_TO_FILE=false", "PRICING_DATA_DIR="+pricing, "PRICING_HASH_URL=", "PRICING_REMOTE_URL=https://127.0.0.1:1/prices", "PLUGINS_ROOT_DIR="+dir)
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
			p.fail(t, "startup exited")
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
				require.Equal(t, mode, res.Header.Get("X-OmniHive-Mode"))
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
	t.Fatalf("%s (%v)\n%s", reason, p.err, raw)
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
	start := func(mode, source string) *childServer { return startChild(t, mode, source, dsn, redisPort, pricing) }
	all := start("all", "default")
	groupName := "be22-" + uuid.NewString()
	email := groupName + "@example.test"
	password := uuid.NewString()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	var user, group, key, account int64
	require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash,role,balance) VALUES ($1,$2,'admin',100) RETURNING id", email, string(hash)).Scan(&user))
	require.NoError(t, db.QueryRow("INSERT INTO groups(name,platform) VALUES ($1,'openai') RETURNING id", groupName).Scan(&group))
	secret := uuid.NewString()
	require.NoError(t, db.QueryRow("INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,$3,'BE-22') RETURNING id", user, group, secret).Scan(&key))
	require.NoError(t, db.QueryRow("INSERT INTO accounts(name,platform,type) VALUES($1,'openai','apikey') RETURNING id", groupName).Scan(&account))
	_, err = db.Exec("INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES ($1,$2,now(),now()+interval '1 day')", user, group)
	require.NoError(t, err)
	batchID := "batch-" + uuid.NewString()
	_, err = db.Exec("INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,provider,model,status,item_count) VALUES ($1,$2,$3,'gemini','runtime-model','completed',1)", batchID, user, key)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO sub2api_plugin_installations(plugin_key,name,version,artifact_path,install_path,binary_path,binary_sha256) VALUES ($1,$1,'1.0','unused','unused','unused',repeat('0',64))", groupName)
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
		for _, path := range []string{"/api/v1/public/rankings", "/api/v1/user/mcp/grants", "/api/v1/user/audit-logs"} {
			control.request(t, 404, "GET", path, token, "")
		}
	}
	retained(all, all)
	all.stop(t, syscall.SIGTERM)
	control := start("control", "yaml")
	gateway := start("gateway", "env")
	retained(control, gateway)
	control.request(t, 404, "GET", "/v1/images/batches", secret, "")
	gateway.request(t, 404, "GET", "/api/v1/subscriptions", token, "")
	requestID := queueReceipt(t, db, user, key, account)
	var state string
	check := func() string {
		t.Helper()
		require.NoError(t, db.QueryRow("SELECT state FROM usage_settlement_receipts WHERE request_id=$1", requestID).Scan(&state))
		return state
	}
	require.Equal(t, "pending", check())
	worker := start("worker", "flag")
	peer := start("worker", "env")
	worker.request(t, 404, "GET", "/v1/models", secret, "")
	peer.request(t, 404, "GET", "/api/v1/user/profile", token, "")
	worker.stop(t, os.Kill)
	restarted := start("worker", "yaml")
	require.Eventually(t, func() bool { return check() == "settled" }, 25*time.Second, 100*time.Millisecond)
	var balance float64
	var count int
	require.NoError(t, db.QueryRow("SELECT balance FROM users WHERE id=$1", user).Scan(&balance))
	require.Equal(t, 99.0, balance)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", requestID, key).Scan(&count))
	require.Equal(t, 1, count)
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
