//go:build integration

package requests_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/requests"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	t                                                                                    *testing.T
	db                                                                                   *sql.DB
	boundary                                                                             time.Time
	w, p, p2, w2, otherProject, actor, observer, financeOnly, key, account, otherAccount int64
	engine                                                                               *gin.Engine
}

func TestRequestsPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to a disposable job_test PostgreSQL database")
	}
	db, err := testresources.OpenPostgres(t.Context(), dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, repository.ApplyMigrations(t.Context(), db))

	f := &fixture{t: t, db: db, boundary: time.Now().UTC().Truncate(time.Second).Add(-time.Minute)}
	f.w = f.id(`INSERT INTO workspaces(name) VALUES ('BE-14') RETURNING id`)
	f.w2 = f.id(`INSERT INTO workspaces(name) VALUES ('BE-14 isolated') RETURNING id`)
	f.p = f.id(`SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, f.w)
	f.p2 = f.id(`INSERT INTO projects(workspace_id,name) VALUES ($1,'BE-14 separate project') RETURNING id`, f.w)
	f.otherProject = f.id(`SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, f.w2)
	f.actor, f.observer, f.financeOnly = f.user(), f.user(), f.user()
	f.exec(`INSERT INTO workspace_members(workspace_id,user_id,role)
	          VALUES ($1,$2,'admin'), ($1,$3,'member'), ($1,$4,'member'), ($5,$2,'admin')`,
		f.w, f.actor, f.observer, f.financeOnly, f.w2)
	f.exec(`INSERT INTO project_grants(workspace_id,project_id,user_id,action)
	          VALUES ($1,$2,$3,'requests.read'), ($1,$2,$3,'requests.export'), ($1,$2,$4,'billing.ledger.read')`,
		f.w, f.p, f.observer, f.financeOnly)
	f.key = f.id(`INSERT INTO api_keys(workspace_id,user_id,name,created_at) VALUES ($1,$2,'BE-14',$3) RETURNING id`,
		f.w, f.actor, f.boundary.Add(-time.Hour))
	f.account = f.connection(f.w, "own-connection")
	f.otherAccount = f.connection(f.w2, "foreign-connection")
	f.routes()

	success := f.execution(f.p, "multi-attempt", f.boundary, "terminal", true)
	abandoned := f.attempt(success, 1, f.otherAccount, f.boundary.Add(time.Second), "unknown", "unknown", true)
	replacement := f.attempt(success, 2, f.account, f.boundary.Add(2*time.Second), "accepted", "transport_response", false)
	f.exec(`UPDATE gateway_execution_attempts
	         SET conversion_plan='{"mode":"bridge","source_protocol":"openai_responses","target_protocol":"gemini_generate",
	           "diagnostics":[{"field":"/tools","reason":"TOOL_ARGUMENT_NORMALIZED","severity":"info"}]}'
	         WHERE attempt_id=$1`, replacement)
	f.exec(`UPDATE gateway_execution_attempts SET decision='{"reason":"first_output_timeout_abandoned","excluded":[12]}'
	         WHERE attempt_id=$1`, abandoned)
	settledAt := f.boundary.Add(8 * time.Second)
	successUsage := f.usage(success, f.account, "multi-attempt", f.boundary.Add(6*time.Second))
	receipt := f.receipt(success, f.account, "complete", "priced", f.boundary.Add(7*time.Second))
	f.exec(`UPDATE usage_settlement_receipts SET state='settled', usage_log_id=$2, settled_at=$3 WHERE id=$1`,
		receipt, successUsage, settledAt)
	f.mapResource(f.p, "usage_logs", successUsage)
	f.mapResource(f.p, "usage_settlement_receipts", receipt)
	snapshot := uuid.NewString()
	require.NoError(t, trace.NewPostgres(db).Record(t.Context(), journal.AdmittedExecution{
		RequestID: success, Scope: scope.Authorized{WorkspaceID: f.w, ProjectID: f.p},
	}, trace.DecisionTrace{
		SchemaVersion: 1, Mode: trace.Execution, SnapshotID: snapshot, AsOf: f.boundary, PriceVersion: "price-fixture",
		Candidates: []trace.Candidate{
			{ID: "foreign-connection", State: "excluded", Reasons: []trace.Reason{{Code: "REGION_UNVERIFIED", Stage: "region"}}},
			{ID: "own-connection", State: "eligible", Factors: []trace.Factor{
				{Name: "upstream_cost", Unit: "score", Value: "4"}, {Name: "ttft", Unit: "score", Value: "0.5"},
			}},
		},
		Selected: &trace.Selection{ModelID: "provider/model-routed", CandidateID: "own-connection", FundingSource: "wallet"},
	}))

	failed := f.execution(f.p, "failure", f.boundary, "terminal", true)
	f.attempt(failed, 1, f.account, f.boundary.Add(time.Second), "not_sent", "rejected", false)
	f.exec(`INSERT INTO ops_error_logs(request_id,api_key_id,user_id,account_id,error_phase,error_type,
	          error_owner,status_code,upstream_status_code,routing_latency_ms,error_message,created_at)
	         VALUES ($1,$2,$3,$4,'upstream','rate_limit','provider',502,429,12,'sensitive-body',$5)`,
		failed, f.key, f.actor, f.account, f.boundary.Add(7*time.Second))

	interrupted := f.execution(f.p, "interrupted", f.boundary.Add(-time.Second), "semantic", true)
	f.attempt(interrupted, 1, f.account, f.boundary, "accepted", "transport_response", false)
	f.receipt(interrupted, f.account, "interrupted", "unpriced", f.boundary.Add(5*time.Second))
	unknown := f.execution(f.p, "unknown", f.boundary.Add(-2*time.Second), "preoutput", false)
	f.attempt(unknown, 1, f.otherAccount, f.boundary, "unknown", "dispatching", false)
	gap := f.execution(f.p, "pre-receipt", f.boundary.Add(-3*time.Second), "writefailed", true)
	f.attempt(gap, 1, f.account, f.boundary, "accepted", "transport_response", false)
	f.exec(`UPDATE gateway_executions SET reservation_ref='missing-receipt-hold' WHERE request_id=$1`, gap)
	f.exec(`INSERT INTO api_key_budget_reservations(api_key_id,user_id,request_id,hold_amount,expires_at)
	         VALUES ($1,$2,'missing-receipt-hold',0.013,now())`, f.key, f.actor)
	atUpperBound := f.execution(f.p, "exclusive-bound", f.boundary.Add(time.Second), "terminal", true)
	f.attempt(atUpperBound, 1, f.account, f.boundary.Add(2*time.Second), "not_sent", "rejected", false)
	other := f.execution(f.p2, "cross-project", f.boundary, "terminal", true)

	legacyUsage := f.usage("local:"+uuid.NewString(), f.account, "legacy-usage", f.boundary.Add(-5*time.Second))
	f.mapResource(f.p, "usage_logs", legacyUsage)
	unmappedUsage := f.usage("local:"+uuid.NewString(), f.account, "unproven-usage", f.boundary.Add(-5*time.Second))
	historicalRequestID := "local:" + uuid.NewString()
	historyUsage := f.usage(historicalRequestID, f.account, "legacy-joined", f.boundary.Add(-7*time.Second))
	f.mapResource(f.p, "usage_logs", historyUsage)
	historyReceipt := f.receipt(historicalRequestID, f.account, "complete", "priced", f.boundary.Add(-7*time.Second))
	f.mapResource(f.p, "usage_settlement_receipts", historyReceipt)
	historyID := fmt.Sprintf("legacy-receipt-%d", historyReceipt)
	usageID := fmt.Sprintf("legacy-usage-%d", legacyUsage)

	t.Run("logical request and permissions", func(t *testing.T) {
		base := f.projectURL(f.w, f.p)
		list := f.call(f.observer, http.MethodGet, base+"?model_id=multi-attempt", "", "")
		require.Equal(t, 200, list.Code, list.Body.String())
		items := array(t, data(t, list)["items"])
		require.Len(t, items, 1)
		item := object(t, items[0])
		require.Equal(t, success, item["id"])
		require.Equal(t, "2", item["attempt_count"])
		require.Equal(t, "succeeded", item["outcome"])
		require.Nil(t, item["charged_amount"])

		detail := f.call(f.observer, http.MethodGet, base+"/"+success, "", "")
		require.Equal(t, 200, detail.Code, detail.Body.String())
		view := data(t, detail)
		require.Equal(t, "private, no-store", detail.Header().Get("Cache-Control"))
		require.Nil(t, view["funding_account_id"])
		require.Equal(t, contract(t, "billing-redacted.json"), view["billing"])
		require.Contains(t, array(t, view["redactions"]), "financial_details")
		require.Contains(t, array(t, view["redactions"]), "connection_identity")
		replaced := object(t, array(t, view["attempts"])[0])
		hidden := object(t, array(t, view["abandoned_attempts"])[0])
		require.Equal(t, replacement, replaced["id"])
		require.Equal(t, abandoned, hidden["id"])
		require.Equal(t, "settled", replaced["receipt_state"])
		require.Equal(t, "unconfirmed", hidden["receipt_state"])
		require.Equal(t, "unknown", hidden["acceptance"])
		require.Nil(t, replaced["connection_id"])
		require.Equal(t, float64(0), replaced["upstream_ttft_ms"])
		require.Equal(t, float64(2000), replaced["duration_ms"])
		require.Equal(t, "bridge", object(t, replaced["conversion"])["mode"])
		require.NotContains(t, detail.Body.String(), "foreign-connection")

		var kinds []string
		for _, v := range array(t, view["timeline"]) {
			kinds = append(kinds, object(t, v)["kind"].(string))
		}
		require.Equal(t, []string{"admitted", "first_output", "client_completed", "settled"}, kinds)
		require.Equal(t, settledAt.Format(time.RFC3339), object(t, array(t, view["timeline"])[3])["at"])
		units := array(t, object(t, view["usage"])["units"])
		require.Equal(t, []any{map[string]any{"kind": "input_token", "quantity": "0"}}, units)

		admin := f.call(f.actor, http.MethodGet, base+"/"+success, "", "")
		require.Equal(t, 200, admin.Code, admin.Body.String())
		adminView := data(t, admin)
		for _, name := range []string{"funding_account_id", "key_owner_id"} {
			require.NotNil(t, adminView[name])
		}
		require.Equal(t, "own-connection", object(t, array(t, adminView["attempts"])[0])["connection_name"])
		require.Nil(t, object(t, array(t, adminView["abandoned_attempts"])[0])["connection_id"])
		require.NotContains(t, admin.Body.String(), "foreign-connection")
		b := object(t, adminView["billing"])
		require.Equal(t, "price-fixture", b["price_version"])
		require.Equal(t, strconv.FormatInt(receipt, 10), b["settlement_id"])
		require.Equal(t, map[string]any{"currency": "USD", "amount": "0.00225000"}, b["platform_charge"])
		require.Equal(t, 403, f.call(f.financeOnly, http.MethodGet, base+"/"+success, "", "").Code)
		require.Equal(t, 403, f.call(f.observer, http.MethodGet, base+"?funding_source=wallet", "", "").Code)
		require.Equal(t, 404, f.call(f.actor, http.MethodGet, base+"/"+other, "", "").Code)
		require.Equal(t, 404, f.call(f.observer, http.MethodGet, f.projectURL(f.w, f.p2)+"/"+other, "", "").Code)
		require.Equal(t, 404, f.call(f.actor, http.MethodGet, f.projectURL(f.w2, f.otherProject)+"/"+success, "", "").Code)

		explanation := f.call(f.observer, http.MethodGet, base+"/"+success+"/route-explain", "", "")
		require.Equal(t, 200, explanation.Code, explanation.Body.String())
		decision := object(t, data(t, explanation)["trace"])
		require.Equal(t, snapshot, decision["snapshot_id"])
		require.Equal(t, "candidate-2", object(t, decision["selected"])["candidate_id"])
		require.Empty(t, object(t, decision["selected"])["funding_source"])
		require.Equal(t, "REGION_UNVERIFIED", object(t, array(t, object(t, array(t, decision["candidates"])[0])["reasons"])[0])["code"])
		require.NotContains(t, explanation.Body.String(), "foreign-connection")
		require.NotContains(t, explanation.Body.String(), "price-fixture")
		require.NotContains(t, explanation.Body.String(), "upstream_cost")
	})
	t.Run("lifecycle and historical partial records", func(t *testing.T) {
		base := f.projectURL(f.w, f.p)
		for _, sample := range []struct{ id, outcome, reason, settlement string }{
			{failed, "failed", "UPSTREAM_RATE_LIMITED", "unknown"},
			{interrupted, "interrupted", "STREAM_INTERRUPTED", "needs_review"},
			{unknown, "unknown", "ACCEPTANCE_UNKNOWN", "unknown"},
			{gap, "interrupted", "STREAM_INTERRUPTED", "unknown"},
			{historyID, "succeeded", "", "pending"},
			{usageID, "succeeded", "", "unknown"},
		} {
			resp := f.call(f.actor, http.MethodGet, base+"/"+sample.id, "", "")
			require.Equal(t, 200, resp.Code, resp.Body.String())
			view := data(t, resp)
			require.Equal(t, sample.outcome, view["outcome"])
			require.Equal(t, sample.settlement, view["settlement_state"])
			if sample.reason != "" {
				require.Equal(t, sample.reason, view["reason"])
			}
			require.Equal(t, "partial", object(t, view["data_completeness"])["state"])
			require.NotContains(t, resp.Body.String(), "sensitive-body")
			if sample.id == interrupted {
				b := object(t, view["billing"])
				require.Equal(t, "pending", b["receipt_state"])
				require.Equal(t, "needs_review", b["state"])
			}
			if sample.id == gap {
				b := object(t, view["billing"])
				require.Equal(t, "unconfirmed", b["receipt_state"])
				require.Nil(t, b["platform_charge"])
				require.Equal(t, true, b["release_pending"])
				require.Equal(t, map[string]any{"amount": "0.01300000", "currency": "USD"}, b["reserved"])
				require.Empty(t, array(t, object(t, view["usage"])["units"]))
			}
		}
		route := f.call(f.actor, http.MethodGet, base+"/"+historyID+"/route-explain", "", "")
		require.Equal(t, 200, route.Code, route.Body.String())
		expected := contract(t, "route-partial.json")
		expected["request_id"] = historyID
		require.Equal(t, expected, data(t, route))
		require.Equal(t, 404, f.call(f.actor, "GET", base+fmt.Sprintf("/legacy-usage-%d", unmappedUsage), "", "").Code)
		legacyList := f.call(f.actor, "GET", base+"?outcome=succeeded&model_id=legacy-joined&settlement_state=pending", "", "")
		require.Equal(t, 200, legacyList.Code, legacyList.Body.String())
		require.Len(t, array(t, data(t, legacyList)["items"]), 1)
	})
	t.Run("stable seek and half-open time", func(t *testing.T) {
		base := f.projectURL(f.w, f.p)
		params := url.Values{
			"from": {f.boundary.Format(time.RFC3339)}, "to": {f.boundary.Add(time.Second).Format(time.RFC3339)},
			"timezone": {"Asia/Shanghai"}, "limit": {"1"}, "outcome": {"succeeded", "failed"},
		}
		resp := f.call(f.actor, "GET", base+"?"+params.Encode(), "", "")
		require.Equal(t, 200, resp.Code, resp.Body.String())
		first := data(t, resp)
		require.Len(t, array(t, first["items"]), 1)
		cursor := first["next_cursor"].(string)
		params.Set("cursor", cursor)
		resp = f.call(f.actor, "GET", base+"?"+params.Encode(), "", "")
		require.Equal(t, 200, resp.Code, resp.Body.String())
		second := data(t, resp)
		require.Equal(t, first["as_of"], second["as_of"])
		require.Nil(t, second["next_cursor"])
		require.ElementsMatch(t, []string{success, failed}, []string{
			object(t, array(t, first["items"])[0])["id"].(string),
			object(t, array(t, second["items"])[0])["id"].(string),
		})
		require.Equal(t, 422, f.call(f.actor, "GET", f.projectURL(f.w, f.p2)+"?"+params.Encode(), "", "").Code)
		params.Set("outcome", "succeeded")
		require.Equal(t, 422, f.call(f.actor, "GET", base+"?"+params.Encode(), "", "").Code)
		params.Del("cursor")
		params.Set("to", f.boundary.Format(time.RFC3339))
		require.Equal(t, 422, f.call(f.actor, "GET", base+"?"+params.Encode(), "", "").Code)
		require.Equal(t, 422, f.call(f.actor, "GET", base+"?status=wat", "", "").Code)
		require.Equal(t, 422, f.call(f.actor, "GET", base+"?timezone=Fake/Zone", "", "").Code)
		empty := f.call(f.actor, "GET", base+"?model_id=absent-model", "", "")
		require.Equal(t, 200, empty.Code, empty.Body.String())
		emptyData := data(t, empty)
		expected := contract(t, "list-empty.json")
		expected["as_of"] = emptyData["as_of"]
		require.Equal(t, expected, emptyData)
	})
	t.Run("scoped idempotent reauthorized export", func(t *testing.T) {
		base := f.projectURL(f.w, f.p)
		path := base + "/" + success + "/exports"
		body, err := os.ReadFile("../../../../contracts/api/requests/export-input.json")
		require.NoError(t, err)
		token := "same-operation-key"
		begin := f.call(f.observer, "POST", path, string(body), token)
		require.Equal(t, 202, begin.Code, begin.Body.String())
		op := data(t, begin)
		id := op["operation_id"].(string)
		statusURL := op["status_url"].(string)
		downloadURL := object(t, op["result"])["download_url"].(string)
		require.Equal(t, "succeeded", op["state"])
		require.Equal(t, 422, f.call(f.observer, "POST", path, string(body), "").Code)
		var wait sync.WaitGroup
		var responses [4]*httptest.ResponseRecorder
		for i := range responses {
			wait.Go(func() {
				responses[i] = f.call(f.observer, "POST", path, `{"format":"json","sections":["timeline","billing","attempts"]}`, token)
			})
		}
		wait.Wait()
		for _, replay := range responses {
			require.Equal(t, 202, replay.Code, replay.Body.String())
			require.Equal(t, id, data(t, replay)["operation_id"])
		}
		require.Equal(t, 409, f.call(f.observer, "POST", path, `{"format":"json","sections":["usage"]}`, token).Code)
		require.Equal(t, 404, f.call(f.actor, "POST", base+"/"+other+"/exports", string(body), token).Code)
		require.Equal(t, 404, f.call(f.actor, "POST", f.projectURL(f.w2, f.otherProject)+"/"+success+"/exports", string(body), token).Code)

		status := f.call(f.observer, "GET", statusURL, "", "")
		require.Equal(t, 200, status.Code, status.Body.String())
		require.Equal(t, downloadURL, object(t, data(t, status)["result"])["download_url"])
		require.Equal(t, 404, f.call(f.actor, "GET", statusURL, "", "").Code)
		require.Equal(t, 404, f.call(f.actor, "GET", strings.Replace(statusURL, fmt.Sprintf("/%d/", f.w), fmt.Sprintf("/%d/", f.w2), 1), "", "").Code)
		require.Equal(t, 404, f.call(f.actor, "GET", downloadURL, "", "").Code)
		require.Equal(t, 404, f.call(f.observer, "GET", base+"/"+failed+"/exports/"+id+"/download", "", "").Code)

		content := f.call(f.observer, "GET", downloadURL, "", "")
		require.Equal(t, 200, content.Code, content.Body.String())
		require.Contains(t, content.Header().Get("Content-Disposition"), "attachment")
		var artifact map[string]any
		require.NoError(t, json.Unmarshal(content.Body.Bytes(), &artifact))
		require.Nil(t, object(t, artifact["billing"])["platform_charge"])
		require.Nil(t, object(t, array(t, artifact["attempts"])[0])["connection_id"])
		require.Len(t, array(t, artifact["abandoned_attempts"]), 1)
		require.NotContains(t, content.Body.String(), "upstream_cost")

		f.exec(`INSERT INTO project_grants(workspace_id,project_id,user_id,action) VALUES ($1,$2,$3,'billing.ledger.read')`, f.w, f.p, f.observer)
		content = f.call(f.observer, "GET", downloadURL, "", "")
		require.Equal(t, 200, content.Code, content.Body.String())
		require.Contains(t, content.Body.String(), "0.00225000")
		f.exec(`DELETE FROM project_grants WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 AND action='billing.ledger.read'`, f.w, f.p, f.observer)
		content = f.call(f.observer, "GET", downloadURL, "", "")
		require.Equal(t, 200, content.Code, content.Body.String())
		require.NotContains(t, content.Body.String(), "0.00225000")

		f.exec(`UPDATE request_metadata_exports SET expires_at=now()-interval '1 second' WHERE id=$1`, id)
		require.Equal(t, 410, f.call(f.observer, "GET", downloadURL, "", "").Code)
		expired := f.call(f.observer, "POST", path, string(body), token)
		require.Equal(t, 202, expired.Code, expired.Body.String())
		require.Equal(t, id, data(t, expired)["operation_id"])
		require.Nil(t, object(t, data(t, expired)["result"])["download_url"])
		f.exec(`DELETE FROM project_grants WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 AND action='requests.export'`, f.w, f.p, f.observer)
		require.Equal(t, 403, f.call(f.observer, "GET", statusURL, "", "").Code)
		require.Equal(t, 403, f.call(f.observer, "GET", downloadURL, "", "").Code)
	})
}

func (f *fixture) routes() {
	gin.SetMode(gin.TestMode)
	f.engine = gin.New()
	access := postgres.New(f.db)
	h := control.New(f.db, access, "request-test-secret")
	session := func(c *gin.Context) {
		if actor, err := strconv.ParseInt(c.GetHeader("Fixture-Actor"), 10, 64); err == nil {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
		}
		c.Next()
	}
	v2 := f.engine.Group("/api/v2")
	control.Register(v2, h, session)
	identityhttp.Register(v2, identityhttp.New(access, identity.Operations{Readers: []identity.OperationReader{h}}, &config.Config{}), session)
}

func (f *fixture) projectURL(workspace, project int64) string {
	return fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/requests", workspace, project)
}

func (f *fixture) call(actor int64, method, path, body, key string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Fixture-Actor", strconv.FormatInt(actor, 10))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	f.engine.ServeHTTP(rr, r)
	return rr
}

func (f *fixture) id(query string, args ...any) int64 {
	f.t.Helper()
	var id int64
	require.NoError(f.t, f.db.QueryRowContext(f.t.Context(), query, args...).Scan(&id))
	return id
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := f.db.ExecContext(f.t.Context(), query, args...)
	require.NoError(f.t, err)
}

func (f *fixture) user() int64 {
	return f.id(`INSERT INTO users(email,password_hash,balance) VALUES ($1,'fixture',2) RETURNING id`, uuid.NewString()+"@example.test")
}

func (f *fixture) connection(workspace int64, name string) int64 {
	account := f.id(`INSERT INTO accounts(workspace_id,name,platform,type) VALUES ($1,$2,'openai','apikey') RETURNING id`, workspace, name)
	f.exec(`INSERT INTO provider_connections(account_id,workspace_id,provider,auth_type,name,concurrency,state)
	         VALUES ($1,$2,'openai','apikey',$3,1,'active') ON CONFLICT DO NOTHING`, account, workspace, name)
	return account
}

func (f *fixture) execution(project int64, model string, at time.Time, state string, finished bool) string {
	id := uuid.NewString()
	var finish *time.Time
	if finished {
		t := at.Add(6 * time.Second)
		finish = &t
	}
	response := map[string]int{"terminal": 4, "semantic": 2, "preoutput": 0, "writefailed": 5}
	f.exec(`INSERT INTO gateway_executions(request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,
	       authorization_version,kind,route,model,legacy_id,reservation_ref,attempt_budget,deadline,model_version,price_version,pricing_at,
	       admitted_at,finished_at,response_state)
	       VALUES ($1,$2,$3,$4,$5,$4,$4,1,'request','/v1/responses',$6,'','',3,$7::timestamptz+interval '15 seconds',
	               'catalog-fixture','price-fixture',$7,$7,$8,$9)`, id, f.w, project, f.actor, f.key, model, at, finish, response[state])
	return id
}

func (f *fixture) attempt(request string, ordinal int, account int64, dispatched time.Time, acceptance, outcome string, abandoned bool) string {
	id := uuid.NewString()
	f.exec(`INSERT INTO gateway_execution_attempts(attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome,abandoned_uncertain)
	         VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, request, ordinal, account, dispatched, acceptance, outcome, abandoned)
	return id
}

func (f *fixture) usage(request string, account int64, model string, at time.Time) int64 {
	return f.id(`INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,model,requested_model,inbound_endpoint,input_tokens,created_at)
	         VALUES ($1,$2,$3,$4,$5,$5,'/v1/responses',17,$6) RETURNING id`, request, f.actor, f.key, account, model, at)
}

func (f *fixture) receipt(request string, account int64, completion, pricing string, at time.Time) int64 {
	model := "legacy-joined"
	if _, err := uuid.Parse(request); err == nil {
		model = "multi-attempt"
	}
	usage := fmt.Sprintf(`{"Model":%q,"UpstreamModel":"provider/model-routed","InputTokens":0,"OutputTokens":0,
		"DurationMs":2000,"FirstTokenMs":0,"token_usage_observation":{"input_tokens_known":true}}`, model)
	return f.id(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,
	          usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,created_at)
	          VALUES ($1,$2,$3,$4,'be14-fixture',$5,'{"BalanceCost":0.00225}',$6,'','resolved',$7,'pending',$8) RETURNING id`,
		request, f.key, f.actor, account, usage, pricing, completion, at)
}

func (f *fixture) mapResource(project int64, kind string, id int64) {
	f.exec(`INSERT INTO project_resources(workspace_id,project_id,kind,resource_id) VALUES ($1,$2,$3,$4)`, f.w, project, kind, id)
}

func data(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct{ Data map[string]any }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data, "%s", response.Body.String())
	return envelope.Data
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	v, ok := value.(map[string]any)
	require.True(t, ok, "%v", value)
	return v
}

func array(t *testing.T, value any) []any {
	t.Helper()
	v, ok := value.([]any)
	require.True(t, ok, "%v", value)
	return v
}

func contract(t *testing.T, filename string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../../../contracts/api/requests/" + filename)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}
