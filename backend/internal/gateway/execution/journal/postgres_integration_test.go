//go:build integration

package journal_test

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	compat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-06"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func database(t testing.TB) *sql.DB {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to a dedicated job_test")
	}
	db, err := testresources.OpenPostgres(t.Context(), dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, repository.ApplyMigrations(t.Context(), db))
	return db
}

func id(t testing.TB, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var v int64
	require.NoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&v))
	return v
}

func identity(t testing.TB, db *sql.DB) (scope.Authorized, string) {
	t.Helper()
	w := id(t, db, "INSERT INTO workspaces(name) VALUES('execution fixture') RETURNING id")
	u := id(t, db, "INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',1) RETURNING id", journal.NewID()+"@example.test")
	k := id(t, db, "INSERT INTO api_keys(workspace_id,user_id,name) VALUES($1,$2,'execution fixture') RETURNING id", w, u)
	digest := service.APIKeyCredentialDigest(journal.NewID())
	_, err := db.Exec("INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest) VALUES($1,$2,$3)", w, k, digest)
	require.NoError(t, err)
	a, err := postgres.New(db).LegacyScope(t.Context(), w, k, u, digest)
	require.NoError(t, err)
	return a, digest
}

func request() journal.Admission {
	return journal.Admission{RequestID: journal.NewID(), Kind: "request", Route: "/v1/responses", Model: "model", AttemptBudget: 2, Deadline: time.Now().Add(time.Minute), Versions: journal.Versions{Model: "catalog1", Price: "price1", PricingAt: time.Now()}}
}

const insertReceipt = `INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,
 usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state)
 VALUES($1,$2,$3,$4,'fixture',$5,'{}','free','','resolved','complete','pending') RETURNING id`

func receipt(t testing.TB, db *sql.DB, e journal.AdmittedExecution, account int64, observations string) error {
	t.Helper()
	var rid int64
	return db.QueryRow(insertReceipt, e.RequestID, e.Scope.KeyID, e.Scope.FundingUserID, account, observations).Scan(&rid)
}

func TestCrashRecovery(t *testing.T) {
	db := database(t)
	a, _ := identity(t, db)
	fixture, err := os.ReadFile("../../../../../contracts/protocol/execution/crash.json")
	require.NoError(t, err)
	var cases []struct {
		Stage                 string
		Dispatched, Candidate bool
	}
	require.NoError(t, json.Unmarshal(fixture, &cases))
	for _, tc := range cases {
		t.Run(tc.Stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			req := journal.NewID()
			received := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(received)
				if tc.Stage == "dispatching" {
					select {
					case <-r.Context().Done():
					case <-ctx.Done():
					}
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashProcess$")
			cmd.Env = append(os.Environ(), "BE06_STAGE="+tc.Stage, "BE06_UPSTREAM="+upstream.URL, "BE06_REQUEST="+req, "BE06_WORKSPACE="+strconv.FormatInt(a.WorkspaceID, 10), "BE06_KEY="+strconv.FormatInt(a.KeyID, 10), "BE06_OWNER="+strconv.FormatInt(a.KeyOwnerID, 10))
			pipe, err := cmd.StdoutPipe()
			require.NoError(t, err)
			cmd.Stderr = os.Stderr
			require.NoError(t, cmd.Start())
			scan := bufio.NewScanner(pipe)
			var ready bool
			for scan.Scan() {
				if scan.Text() == "ready" {
					ready = true
					break
				}
				t.Log(scan.Text())
			}
			require.True(t, ready)
			if tc.Dispatched {
				select {
				case <-received:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			} else {
				select {
				case <-received:
					t.Fatal("provider received unsent request")
				default:
				}
			}
			require.NoError(t, cmd.Process.Kill())
			require.Error(t, cmd.Wait())
			_, err = db.Exec("UPDATE gateway_executions SET deadline=now()-interval '1 second' WHERE request_id=$1", req)
			require.NoError(t, err)
			require.Equal(t, tc.Dispatched, id(t, db, "SELECT count(*) FROM gateway_execution_attempts WHERE request_id=$1 AND dispatched_at IS NOT NULL", req) == 1)
			require.Equal(t, tc.Candidate, id(t, db, "SELECT count(*) FROM gateway_execution_reconciliation WHERE request_id=$1", req) == 1)
			require.EqualValues(t, 1, id(t, db, "SELECT count(*) FROM gateway_execution_release_obligations WHERE request_id=$1", req))
		})
	}
}

func TestCrashProcess(t *testing.T) {
	stage := os.Getenv("BE06_STAGE")
	if stage == "" {
		t.Skip("subprocess only")
	}
	db := database(t)
	var a scope.Authorized
	a.WorkspaceID, _ = strconv.ParseInt(os.Getenv("BE06_WORKSPACE"), 10, 64)
	a.KeyID, _ = strconv.ParseInt(os.Getenv("BE06_KEY"), 10, 64)
	a.KeyOwnerID, _ = strconv.ParseInt(os.Getenv("BE06_OWNER"), 10, 64)
	a.ActorID, a.FundingUserID, a.Version = a.KeyOwnerID, a.KeyOwnerID, 1
	a.ProjectID = id(t, db, "SELECT identity_default_project($1)", a.WorkspaceID)
	ctx := scope.With(t.Context(), a)
	store := journal.New(db)
	in := request()
	in.RequestID = os.Getenv("BE06_REQUEST")
	e, replay, err := store.Admit(ctx, in)
	require.NoError(t, err)
	require.False(t, replay)
	_, err = db.Exec("INSERT INTO api_key_budget_reservations(api_key_id,user_id,request_id,expires_at) VALUES($1,$2,$3,now())", a.KeyID, a.FundingUserID, e.ReservationRef)
	require.NoError(t, err)
	if stage != "admitted" {
		sent, err := store.Dispatch(ctx, e, 1, 80)
		require.NoError(t, err)
		if stage == "dispatching" {
			fmt.Println("ready")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, os.Getenv("BE06_UPSTREAM"), nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
		sent.Acceptance, sent.Outcome = attempt.Accepted, "transport_response"
		require.NoError(t, store.Observe(ctx, sent))
		if stage == "receipt_committed" {
			require.NoError(t, receipt(t, db, e, 80, "{}"))
		}
	}
	fmt.Println("ready")
	time.Sleep(time.Minute)
}

func TestRegistrationAndUncertainReplacement(t *testing.T) {
	db := database(t)
	a, _ := identity(t, db)
	ctx := scope.With(t.Context(), a)
	s := journal.New(db)
	in := request()
	in.Registration, _ = journal.Register("submit", []byte(`{"a":1,"b":true}`))
	type result struct {
		e      journal.AdmittedExecution
		replay bool
		err    error
	}
	results := make(chan result, 6)
	for range cap(results) {
		go func(in journal.Admission) {
			in.RequestID = journal.NewID()
			e, replay, err := s.Admit(ctx, in)
			results <- result{e, replay, err}
		}(in)
	}
	var e journal.AdmittedExecution
	first := 0
	for range cap(results) {
		row := <-results
		require.NoError(t, row.err)
		if !row.replay {
			first++
		}
		if e.RequestID != "" {
			require.Equal(t, e.RequestID, row.e.RequestID)
		}
		e = row.e
	}
	require.Equal(t, 1, first)
	require.Equal(t, a, e.Scope)
	require.Equal(t, in.Versions.Price, e.Versions.Price)
	in.Registration, _ = journal.Register("submit", []byte(`{"b":true, "a":1}`))
	original, replay, err := s.Admit(ctx, in)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, e.RequestID, original.RequestID)
	in.Registration, _ = journal.Register("submit", []byte(`{"b":false,"a":1}`))
	_, _, err = s.Admit(ctx, in)
	require.ErrorIs(t, err, journal.ErrConflict)
	row, err := s.Dispatch(ctx, e, 1, 81)
	require.NoError(t, err)
	_, err = s.Dispatch(ctx, e, 1, 81)
	require.ErrorIs(t, err, journal.ErrDispatch)
	require.NoError(t, s.Decision(ctx, e.RequestID, attempt.AttemptDecision{AttemptNumber: 1, Action: attempt.Switch, Reason: attempt.FirstOutputTimeoutAbandoned, Acceptance: attempt.Unknown, AbandonedUncertain: true}))
	row.Acceptance, row.Outcome = attempt.Accepted, "transport_response"
	require.NoError(t, s.Observe(ctx, row))
	require.Error(t, receipt(t, db, e, 81, "{}"))
	_, err = s.Dispatch(ctx, e, 2, 82)
	require.NoError(t, err)
	require.NoError(t, receipt(t, db, e, 82, `{"InputTokens":0,"OutputTokens":0,"token_usage_observation":{"input_tokens_known":true,"output_tokens_known":false}}`))
	var input, output sql.NullInt64
	require.NoError(t, db.QueryRow("SELECT input_tokens,output_tokens FROM gateway_execution_usage WHERE request_id=$1 AND receipt_id IS NOT NULL", e.RequestID).Scan(&input, &output))
	require.Equal(t, sql.NullInt64{Valid: true}, input)
	require.False(t, output.Valid)
	require.EqualValues(t, 1, id(t, db, "SELECT count(*) FROM gateway_execution_legacy_ids WHERE request_id=$1 AND legacy_kind='receipt'", e.RequestID))
	require.EqualValues(t, 1, id(t, db, "SELECT count(*) FROM gateway_execution_reconciliation WHERE request_id=$1 AND reason='abandoned_uncertain'", e.RequestID))
	require.EqualValues(t, 0, id(t, db, "SELECT count(*) FROM usage_settlement_receipts WHERE request_id=$1 AND account_id=81", e.RequestID))
	require.NoError(t, s.Finish(ctx, e.RequestID, lifecycle.Terminal, false))
}

type discardAckConnector struct {
	driver.Connector
	prefix string
}

func (c discardAckConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return discardAckConn{conn, c.prefix}, nil
}

type discardAckConn struct {
	driver.Conn
	prefix string
}

func (c discardAckConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil || !strings.HasPrefix(query, c.prefix) {
		return rows, err
	}
	// Drain the committed result before simulating loss on its way to the caller.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return nil, io.ErrUnexpectedEOF
}

func TestLostPostgresAcknowledgements(t *testing.T) {
	db := database(t)
	a, _ := identity(t, db)
	ctx := scope.With(t.Context(), a)
	connector, err := pq.NewConnector(os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	discard := func(prefix string) *journal.Postgres {
		pool := sql.OpenDB(discardAckConnector{connector, prefix})
		t.Cleanup(func() { pool.Close() })
		return journal.New(pool)
	}
	in := request()
	in.Registration, err = journal.Register("ack", []byte(`{"input":"one"}`))
	require.NoError(t, err)
	e, replay, err := discard("WITH admitted").Admit(ctx, in)
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, in.RequestID, e.RequestID)
	_, err = discard("INSERT INTO gateway_execution_attempts").Dispatch(ctx, e, 1, 81)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	_, err = journal.New(db).Dispatch(ctx, e, 1, 81)
	require.ErrorIs(t, err, journal.ErrDispatch)
	_, err = db.Exec("UPDATE gateway_executions SET deadline=now() WHERE request_id=$1", e.RequestID)
	require.NoError(t, err)
	require.EqualValues(t, 1, id(t, db, "SELECT count(*) FROM gateway_execution_reconciliation WHERE request_id=$1", e.RequestID))
	in.RequestID = journal.NewID()
	original, replay, err := journal.New(db).Admit(ctx, in)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, e.RequestID, original.RequestID)
}

func TestRegistrationIsolatedByAPIKey(t *testing.T) {
	db := database(t)
	a, _ := identity(t, db)
	s := journal.New(db)
	in := request()
	in.Registration, _ = journal.Register("submit", []byte(`{"input":1}`))
	first, replay, err := s.Admit(scope.With(t.Context(), a), in)
	require.NoError(t, err)
	require.False(t, replay)
	a.KeyID = id(t, db, "INSERT INTO api_keys(workspace_id,user_id,name) VALUES($1,$2,'another key') RETURNING id", a.WorkspaceID, a.KeyOwnerID)
	in.RequestID = journal.NewID()
	second, replay, err := s.Admit(scope.With(t.Context(), a), in)
	require.NoError(t, err)
	require.False(t, replay)
	require.NotEqual(t, first.RequestID, second.RequestID)
}

func TestQueuedCredentialReauthorization(t *testing.T) {
	db := database(t)
	a, digest := identity(t, db)
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NoError(t, testresources.ValidateRedis(addr))
	rc := testresources.RedisClient(t, &redis.Options{Addr: addr})
	s := journal.New(db)
	ctx := scope.With(t.Context(), a)
	ctx = journal.WithStore(ctx, s)
	ctx = (compat.Validator{DB: db, Scopes: postgres.New(db)}).Install(ctx)
	ctx = compat.BindCredential(ctx, digest, "text.chat", "127.0.0.1", false, 0)
	ctx, cancel := service.WithAttemptBudget(ctx, 2, time.Minute)
	defer cancel()
	account := id(t, db, "INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'execution fixture','openai','apikey') RETURNING id", a.WorkspaceID)
	settlement := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptRepository(db), nil, nil)
	gateway := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settlement)
	ctx, plan := gateway.CaptureRequestPlan(ctx, service.GatewayRequestSpec{WorkspaceID: a.WorkspaceID, UserID: a.KeyOwnerID, APIKeyID: a.KeyID, Route: "/v1/responses", Model: "m", MaxAccountSwitches: 1})
	e, err := plan.AdmittedExecution()
	require.NoError(t, err)
	require.NoError(t, journal.Authorize(ctx, a, "m"))
	queue := []string{"execution_queue"}
	require.NoError(t, rc.Eval(ctx, "return redis.call('RPUSH', KEYS[1], ARGV[1])", queue, e.RequestID).Err())
	_, err = db.Exec("UPDATE api_key_credentials SET revoked_at=now() WHERE api_key_id=$1", a.KeyID)
	require.NoError(t, err)
	queued, err := rc.Eval(ctx, "return redis.call('LPOP', KEYS[1])", queue).Text()
	require.NoError(t, err)
	require.Equal(t, e.RequestID, queued)
	_, err = service.BeginGenerationAttempt(ctx, account)
	require.ErrorIs(t, err, journal.ErrScope)
	require.Zero(t, id(t, db, "SELECT count(*) FROM gateway_execution_attempts WHERE request_id=$1 AND dispatched_at IS NOT NULL", e.RequestID))
}

func BenchmarkGatewayJournal(b *testing.B) {
	db := database(b)
	a, digest := identity(b, db)
	account := id(b, db, "INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'benchmark','openai','apikey') RETURNING id", a.WorkspaceID)
	settlement := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptRepository(db), nil, nil)
	gateway := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settlement)
	spec := service.GatewayRequestSpec{WorkspaceID: a.WorkspaceID, UserID: a.KeyOwnerID, APIKeyID: a.KeyID, Route: "/v1/responses", Model: "model"}
	for _, mode := range []string{"existing", "journal"} {
		b.Run(mode, func(b *testing.B) {
			root := scope.With(b.Context(), a)
			if mode == "journal" {
				root = journal.WithStore(root, journal.New(db))
				root = (compat.Validator{DB: db, Scopes: postgres.New(db)}).Install(root)
				root = compat.BindCredential(root, digest, "text.chat", "127.0.0.1", false, 0)
			}
			b.ReportAllocs()
			for b.Loop() {
				ctx, cancel := service.WithAttemptBudget(root, 1, time.Minute)
				ctx, p := gateway.CaptureRequestPlan(ctx, spec)
				finish, err := service.BeginGenerationAttempt(ctx, account)
				if err != nil {
					b.Fatal(err)
				}
				finish(200, nil)
				p.Finalize(true)
				cancel()
			}
		})
	}
}

func TestJobIdentityAndAbandonedReceiptGuard(t *testing.T) {
	db := database(t)
	a, digest := identity(t, db)
	next := func() int64 {
		return id(t, db, "INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'job','openai','apikey') RETURNING id", a.WorkspaceID)
	}
	first, second := next(), next()
	settlement := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptRepository(db), nil, nil)
	gateway := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settlement)
	root := journal.WithStore(scope.With(t.Context(), a), journal.New(db))
	root = (compat.Validator{DB: db, Scopes: postgres.New(db)}).Install(root)
	root = compat.BindCredential(root, digest, "image.generate", "127.0.0.1", false, 0)
	spec := service.GatewayRequestSpec{WorkspaceID: a.WorkspaceID, UserID: a.KeyOwnerID, APIKeyID: a.KeyID, Route: "/images/generations", Model: "image", ExecutionID: "imgtask_" + journal.NewID(), MaxAccountSwitches: 1}
	ctx, end := gateway.CaptureAsyncImageTaskPlan(root, spec, time.Minute)
	defer end()
	e, err := service.GatewayRequestPlanFromContext(ctx).AdmittedExecution()
	require.NoError(t, err)
	finish, err := service.BeginGenerationAttempt(ctx, first)
	require.NoError(t, err)
	finish(200, nil)
	attempt.Record(ctx, attempt.AttemptDecision{Action: attempt.Switch, Reason: attempt.FirstOutputTimeoutAbandoned, AttemptNumber: 1, Acceptance: attempt.Unknown, AbandonedUncertain: true})
	err = settlement.Submit(ctx, &service.UsageSettlementReceipt{Command: service.UsageBillingCommand{AccountID: first}}, nil)
	require.ErrorIs(t, err, service.ErrAttemptOutcomeUnknown)
	require.EqualValues(t, 1, id(t, db, "SELECT count(*) FROM gateway_execution_attempts WHERE request_id=$1 AND abandoned_uncertain AND decision->>'reason'=$2", e.RequestID, attempt.FirstOutputTimeoutAbandoned))
	require.Zero(t, id(t, db, "SELECT count(*) FROM usage_settlement_receipts WHERE request_id=$1", e.RequestID))
	_, err = db.Exec("UPDATE api_key_credentials SET revoked_at=now() WHERE api_key_id=$1", a.KeyID)
	require.NoError(t, err)
	_, err = service.BeginGenerationAttempt(ctx, second)
	require.ErrorIs(t, err, journal.ErrScope)
	retry, finishRetry := gateway.CaptureAsyncImageTaskPlan(root, spec, time.Minute)
	defer finishRetry()
	prior, err := service.GatewayRequestPlanFromContext(retry).AdmittedExecution()
	require.ErrorIs(t, err, journal.ErrReplay)
	require.Equal(t, e.RequestID, prior.RequestID)
	spec.ExecutionID = "imgtask_" + journal.NewID()
	nextJob, finishNext := gateway.CaptureAsyncImageTaskPlan(root, spec, time.Minute)
	defer finishNext()
	nextAdmission, err := service.GatewayRequestPlanFromContext(nextJob).AdmittedExecution()
	require.NoError(t, err)
	require.NotEqual(t, e.RequestID, nextAdmission.RequestID)
}
