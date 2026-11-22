//go:build integration

package billing_test

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/billing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	rm "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/readmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type gate struct{ ok bool }

func (g *gate) HasStepUpGrant(context.Context, int64, string) (bool, error) { return g.ok, nil }
func TestCostWorkbenchPostgres(t *testing.T) {
	ctx := t.Context()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to a disposable job_test database")
	}
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	number := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	w := number("INSERT INTO workspaces(name) VALUES ('BE-16') RETURNING id")
	p := number("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	other := number("INSERT INTO projects(workspace_id,name) VALUES ($1,'other') RETURNING id", w)
	user := number("INSERT INTO users(email,password_hash,balance) VALUES($1,'test',10) RETURNING id", uuid.NewString()+"@example.test")
	viewer := number("INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id", uuid.NewString()+"@example.test")
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member')", w, user, viewer)
	exec("INSERT INTO project_grants(workspace_id,project_id,user_id,action) VALUES($1,$2,$3,'billing.export'),($1,$2,$3,'billing.read')", w, p, viewer)
	group := number("INSERT INTO groups(workspace_id,name,platform,daily_limit_usd) VALUES ($1,$2,'openai',7) RETURNING id", w, uuid.NewString())
	k := number("INSERT INTO api_keys(workspace_id,user_id,group_id,name) VALUES($1,$2,$3,'key') RETURNING id", w, user, group)
	exec("INSERT INTO api_keys(workspace_id,user_id,group_id,name) VALUES($1,$2,$3,'key2')", w, user, group)
	exec("INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,now()-interval '1 day',now()+interval '2 days')", user, group)
	account := number("INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'test','openai','apikey') RETURNING id", w)
	from := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	to := from.Add(time.Minute)
	anchor := func(at time.Time) string {
		id := uuid.NewString()
		exec(`INSERT INTO gateway_executions(request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,kind,route,model,legacy_id,reservation_ref,attempt_budget,deadline,model_version,price_version,pricing_at,admitted_at)
  VALUES($1,$2,$3,$4,$5,$4,$4,1,'request','/responses','model','','',2,$6,'v1','frozen-price',$6,$6)`, id, w, p, user, k, at)
		exec(`INSERT INTO gateway_execution_attempts(attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome)
   VALUES($1,$2,1,$3,$4,'accepted','transport_response')`, uuid.NewString(), id, account, at)
		return id
	}
	paid := anchor(from)
	unknown := anchor(from.Add(time.Second))
	anchor(to)
	receipt := number(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state)
 VALUES($1,$2,$3,$4,'frozen','{"InputTokens":0,"token_usage_observation":{"input_tokens_known":true}}','{"BalanceCost":2}','priced','','resolved','complete','pending') RETURNING id`, paid, k, user, account)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "UPDATE users SET balance=balance-1.75 WHERE id=$1", user)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "UPDATE usage_settlement_receipts SET state='settled',usage_log_id=1,settled_at=now() WHERE id=$1", receipt)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	ev := number("SELECT event_id FROM billing_movement_links WHERE receipt_id=$1", receipt)
	dbStore := rm.Store{DB: db}
	gin.SetMode(gin.TestMode)
	api := gin.New()
	step := &gate{}
	h := billing.New(db, postgres.New(db), step, "fixture")
	billing.Register(api.Group("/api/v2"), h, func(c *gin.Context) {
		n, _ := strconv.ParseInt(c.GetHeader("actor"), 10, 64)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: n})
		c.Next()
	})
	base := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/billing", w, p)
	call := func(who int64, method, path, body, key, ver string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("actor", fmt.Sprint(who))
		req.Header.Set("Idempotency-Key", key)
		if ver != "" {
			req.Header.Set("If-Match", ver)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, rec.Body.String())
		return rec
	}
	data := func(rec *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var env struct{ Data map[string]any }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		return env.Data
	}
	q := "?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&timezone=America%2FNew_York&currency=USD"
	activity := data(call(user, "GET", base+"/activity"+q, "", "", "", 200))
	totals := activity["totals"].(map[string]any)
	require.Equal(t, "2", totals["logical_requests"])
	require.Equal(t, "0", totals["input_tokens"])
	require.Nil(t, totals["output_tokens"])
	require.Equal(t, "2.00000000", totals["platform_charge"].(map[string]any)["amount"])
	eur := data(call(user, "GET", base+"/activity"+strings.ReplaceAll(q, "USD", "EUR"), "", "", "", 200))
	require.Nil(t, eur["totals"].(map[string]any)["platform_charge"])
	page := data(call(user, "GET", base+"/charges"+q+"&limit=1", "", "", "", 200))
	next := page["next_cursor"].(string)
	require.Equal(t, unknown, page["items"].([]any)[0].(map[string]any)["request_id"])
	last := data(call(user, "GET", base+"/charges"+q+"&limit=1&cursor="+next, "", "", "", 200))
	require.Equal(t, int64(1), number(`SELECT count(*) FROM billing_cost_at($1) WHERE workspace_id=$2 AND project_id=$3 AND request_id=$4 AND charge IS NULL AND state='unknown'`, from.Add(time.Second), w, p, paid))
	require.Equal(t, paid, last["items"].([]any)[0].(map[string]any)["request_id"])
	call(viewer, "GET", base+"/charges"+q+"&cursor="+next, "", "", "", 422)
	quota := data(call(user, "GET", base+"/quotas", "", "", "", 200))
	nsub := 0
	for _, r := range quota["rules"].([]any) {
		if r.(map[string]any)["source"] == "subscription" {
			nsub++
		}
	}
	require.Equal(t, 1, nsub)
	exportBody := fmt.Sprintf(`{"from":%q,"to":%q,"timezone":"UTC","currency":"USD","format":"csv"}`, from.Format(time.RFC3339), to.Format(time.RFC3339))
	export := data(call(user, "POST", base+"/exports", exportBody, "retry", "", 202))
	again := data(call(user, "POST", base+"/exports", exportBody, "retry", "", 202))
	require.Equal(t, export, again)
	call(user, "POST", base+"/exports", strings.ReplaceAll(exportBody, "USD", "EUR"), "retry", "", 409)
	download := export["result"].(map[string]any)["download_url"].(string)
	call(viewer, "GET", download, "", "", "", 404)
	call(user, "GET", strings.Replace(download, fmt.Sprintf("/projects/%d/", p), fmt.Sprintf("/projects/%d/", other), 1), "", "", "", 404)
	csv := call(user, "GET", download, "", "", "", 200).Body.String()
	require.Contains(t, csv, "2.00000000")
	require.Contains(t, csv, unknown)
	require.Contains(t, csv, "frozen-price")
	reconcile := fmt.Sprintf("/api/v2/workspaces/%d/billing/reconciliation", w)
	cases := data(call(user, "GET", reconcile+"?project_id="+fmt.Sprint(p), "", "", "", 200))
	require.Contains(t, fmt.Sprint(cases), "WALLET_DIFFERENCE")
	require.Contains(t, fmt.Sprint(cases), "0.25000000")
	resolve := fmt.Sprintf("%s/receipt-%d/resolve", reconcile, receipt)
	msg := `{"action":"attach_evidence","evidence_id":"statement-7","note":"reviewed"}`
	res := data(call(user, "POST", resolve, msg, "resolve", `"1"`, 202))
	require.Equal(t, res, data(call(user, "POST", resolve, msg, "resolve", `"1"`, 202)))
	call(user, "POST", resolve, msg, "other-resolution", `"1"`, 412)
	adj := fmt.Sprintf(`{"ledger_entry_id":"%d","amount":{"currency":"USD","amount":"0.25000000"},"reason":"billing_correction","note":"reviewed"}`, ev)
	call(user, "POST", base+"/adjustments", adj, "correction", "", 403)
	step.ok = true
	a := data(call(user, "POST", base+"/adjustments", adj, "correction", "", 201))
	require.Equal(t, a, data(call(user, "POST", base+"/adjustments", adj, "correction", "", 201)))
	require.Equal(t, fmt.Sprint(ev), a["reverses_id"])
	require.Equal(t, int64(1), number(`SELECT count(*) FROM usage_settlement_receipts WHERE id=$1 AND billing_command='{"BalanceCost":2}' AND request_fingerprint='frozen' AND state='settled'`, receipt))
	require.Equal(t, int64(1), number("SELECT count(*) FROM billing_wallet_events WHERE id=$1 AND amount=-1.75", ev))
	require.Equal(t, int64(1), number("SELECT count(*) FROM users WHERE id=$1 AND balance=8.50", user))
	_, err = db.ExecContext(ctx, "UPDATE billing_adjustment_effects SET note='changed' WHERE id=$1", a["id"])
	require.Error(t, err)
	ledgerQ := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}, "timezone": {"UTC"}, "currency": {"USD"}, "settlement_id": {fmt.Sprint(receipt)}}
	entries := data(call(user, "GET", base+"/ledger?"+ledgerQ.Encode(), "", "", "", 200))["items"].([]any)
	require.Len(t, entries, 2)
	require.Equal(t, "adjustment", entries[0].(map[string]any)["kind"])
	require.Equal(t, "0.25000000", entries[0].(map[string]any)["signed_amount"].(map[string]any)["amount"])
	require.Equal(t, "-1.75000000", entries[1].(map[string]any)["signed_amount"].(map[string]any)["amount"])
	// Only an explicit resource mapping establishes historical scope. Even
	// with an observed estimated cost, the unconfirmed paid amount stays null.
	history := number(`INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,model,input_tokens,actual_cost,created_at)
 VALUES($1,$2,$3,$4,'historical',3,.12,$5) RETURNING id`, uuid.NewString(), user, k, account, from)
	exec("INSERT INTO project_resources(workspace_id,project_id,kind,resource_id) VALUES($1,$2,'usage_logs',$3)", w, p, history)
	legacy := data(call(user, "GET", base+"/charges"+q+"&model_id=historical", "", "", "", 200))["items"].([]any)[0].(map[string]any)
	require.Equal(t, "estimated", legacy["state"])
	require.Nil(t, legacy["platform_charge"])
	require.Equal(t, "0.12000000", legacy["estimate"].(map[string]any)["amount"])
	require.NotContains(t, call(user, "GET", download, "", "", "", 200).Body.String(), "historical")

	call(user, "PUT", base+"/budgets/project-EUR",
		`{"mode":"hard","limit":{"currency":"EUR","amount":"1"},"timezone":"America/New_York","window":"calendar_day","include_byok_external_estimates":false}`,
		"budget-EUR", `"0"`, 422)
	softProject := number("INSERT INTO projects(workspace_id,name) VALUES ($1,'soft') RETURNING id", w)
	checkBudget := func(project int64, mode string, want int) {
		t.Helper()
		projectBase := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/billing", w, project)
		projectScope := rm.Scope{Workspace: w, Project: project, Actor: user}
		b := fmt.Sprintf(`{"mode":%q,"limit":{"currency":"USD","amount":"1"},"timezone":"America/New_York","window":"calendar_day","include_byok_external_estimates":false}`, mode)
		call(user, "PUT", projectBase+"/budgets/project-USD", b, "budget-"+mode, `"0"`, 200)
		call(user, "PUT", projectBase+"/budgets/project-USD", b, "stale-"+mode, `"0"`, 412)
		unit, err := money.NewMoney("USD", "0.25")
		require.NoError(t, err)
		errs := make(chan error, 8)
		for range 8 {
			go func() {
				tx, e := db.BeginTx(ctx, nil)
				if e == nil {
					defer tx.Rollback()
					e = rm.Reserve(ctx, tx, projectScope, uuid.NewString(), unit)
					if e == nil {
						e = tx.Commit()
					}
				}
				errs <- e
			}()
		}
		ok := 0
		for range 8 {
			e := <-errs
			if e == nil {
				ok++
			} else {
				require.ErrorIs(t, e, rm.Exhausted)
			}
		}
		require.Equal(t, want, ok)
		quotas, e := dbStore.Quotas(ctx, projectScope, rm.QuotaFilter{}, true)
		require.NoError(t, e)
		for _, r := range quotas.Rules {
			if r.ID == "project-USD" {
				require.Equal(t, map[bool]string{true: "1.00000000", false: "2.00000000"}[mode == "hard"], *r.Reserved)
				return
			}
		}
		t.Fatal("missing budget")
	}
	checkBudget(other, "hard", 4)
	checkBudget(softProject, "soft", 8)
	// Drop only the derived cache record to exercise live fallback while a
	// historical backfill is interrupted and resumed.
	exec("DELETE FROM billing_movement_links WHERE event_id=$1", ev)
	require.Equal(t, int64(1), number("SELECT count(*) FROM billing_scoped_postings WHERE event_id=$1", ev))
	for number("SELECT billing_backfill_movements(1)") > 0 {
	}
	require.Equal(t, int64(0), number("SELECT billing_backfill_movements(10)"))
	require.Equal(t, int64(1), number("SELECT count(*) FROM billing_scoped_postings WHERE event_id=$1", ev))
	ownExport := data(call(viewer, "POST", base+"/exports", exportBody, "viewer-export", "", 202))
	ownURL := ownExport["result"].(map[string]any)["download_url"].(string)
	call(viewer, "GET", ownURL, "", "", "", 200)
	exec("DELETE FROM project_grants WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 AND action='billing.export'", w, p, viewer)
	call(viewer, "GET", ownURL, "", "", "", 403)
	_, err = h.ReadOperation(ctx, w, viewer, ownExport["operation_id"].(string))
	require.ErrorIs(t, err, fault.Forbidden)
	_, err = h.ReadOperation(ctx, w, viewer, export["operation_id"].(string))
	require.True(t, errors.Is(err, fault.NotFound))
	exec("UPDATE billing_commands SET expires_at=now()-interval '1 second' WHERE id=$1", export["operation_id"])
	call(user, "GET", download, "", "", "", 410)
}
