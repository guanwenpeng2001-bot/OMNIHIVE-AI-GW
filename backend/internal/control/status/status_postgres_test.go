//go:build integration

package status

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/status"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

const (
	publicBase = "/api/v2/public/status"
	adminBase  = "/api/v2/admin/status/incidents"
)

type fixture struct {
	t                      *testing.T
	db                     *sql.DB
	engine                 *gin.Engine
	h                      *Handler
	now                    time.Time
	project, admin, member int64
	key, account           int64
	monitoring             bool
}

func TestStatusPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to a disposable job_test PostgreSQL database")
	}
	db, err := testresources.OpenPostgres(t.Context(), dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, repository.ApplyMigrations(t.Context(), db))
	f := &fixture{t: t, db: db, now: time.Now().UTC().Truncate(time.Minute).Add(31 * time.Second), monitoring: true}
	f.admin = f.id(`INSERT INTO users(email,password_hash,role) VALUES ('publisher@example.test','fixture','admin') RETURNING id`)
	f.member = f.id(`INSERT INTO users(email,password_hash) VALUES ('private-member@example.test','fixture') RETURNING id`)
	f.exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES (1,$1,'admin')`, f.member)
	f.project = f.id(`INSERT INTO projects(workspace_id,name) VALUES (1,'pvt-project-j78') RETURNING id`)
	f.key = f.id(`INSERT INTO api_keys(workspace_id,user_id,name) VALUES (1,$1,'pvt-key-k78') RETURNING id`, f.member)
	f.account = f.id(`INSERT INTO accounts(workspace_id,name,platform,type) VALUES (1,'pvt-connection-e78','openai','apikey') RETURNING id`)
	f.h = New(db, postgres.New(db), "integration cursor seed", func(_ context.Context) bool { return f.monitoring })
	f.h.now = func() time.Time { return f.now }
	f.engine = gin.New()
	Register(f.engine.Group("/api/v2"), f.h, nil, []gin.HandlerFunc{func(c *gin.Context) {
		if id, err := strconv.ParseInt(c.GetHeader("X-Fixture-Actor"), 10, 64); err == nil {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		}
	}})
	t.Run("published observations", func(t *testing.T) { f.observations() })
	t.Run("incident lifecycle and validators", func(t *testing.T) { f.incidents() })
}

func (f *fixture) observations() {
	t := f.t
	zero := f.call(0, "GET", publicBase+"?window=1h", "")
	initial := readData[domain.Summary](t, zero, 200)
	c := initial.Components[0]
	require.Equal(t, "unknown", initial.Overall)
	require.Equal(t, int64(0), c.SampleCount)
	require.Equal(t, "INSUFFICIENT_SAMPLES", *c.Reason)
	require.Nil(t, c.SuccessRate)
	require.Nil(t, initial.DataLagSeconds)
	require.Len(t, c.History, 90)
	at := f.now.Truncate(time.Minute).Add(-2 * time.Minute)
	f.usage("client:"+uuid.NewString(), "/v1/responses", at, 45)
	f.refresh()
	c = readData[domain.Summary](t, f.call(0, "GET", publicBase+"?window=1h", ""), 200).Components[0]
	require.Equal(t, int64(1), c.SampleCount)
	require.Equal(t, "INSUFFICIENT_SAMPLES", *c.Reason)
	require.Nil(t, c.SuccessRate)

	incoming := uuid.NewString()
	ok := f.execution(at, incoming, false, 4)
	f.attempt(ok, at, 1, "first_output_timeout_abandoned")
	f.attempt(ok, at, 2, "")
	f.receipt(ok, at, "complete", 275)
	f.usage(ok, "/v1/responses", at, 275)                                  // Also present in usage, with the same journal identity.
	f.ops(incoming, at, "provider", 200, 429, false, nil, "/v1/responses") // Recovered attempt.
	badCorrelation := uuid.NewString()
	rejected := f.execution(at, badCorrelation, false, 4)
	f.attempt(rejected, at, 1, "")
	f.ops(badCorrelation, at, "provider", 502, 429, false, nil, "/v1/responses")
	f.ops(uuid.NewString(), at, "platform", 502, 429, false, nil, "/v1/responses")
	f.ops(uuid.NewString(), at, "provider", 502, 500, false, nil, "/v1/responses")
	f.ops(uuid.NewString(), at, "client", 429, 0, false, nil, "/v1/responses")
	f.ops(uuid.NewString(), at, "client", 499, 0, true, 115, "/v1/responses")
	interrupted := f.execution(at, uuid.NewString(), false, 5)
	f.attempt(interrupted, at, 1, "")
	f.receipt(interrupted, at, "interrupted", 425)
	f.execution(at, uuid.NewString(), true, 2)
	f.exec(`INSERT INTO ops_ingress_reject_aggregates
	    (bucket_start,reject_reason,route_family,protocol,client_ip,request_count,first_seen,last_seen)
	    VALUES ($1,'invalid_api_key','responses','openai','192.0.2.88',5,$1,$1)`, at)
	for range 17 {
		f.usage("client:"+uuid.NewString(), "/v1/responses", at, 45)
	}
	legacy := "client:" + uuid.NewString()
	f.usage(legacy, "/v1/responses", at, 275)
	f.receipt(legacy, at, "complete", 275)
	// Unclassified endpoints, provider traces, actor metadata and pre-output
	// in-flight attempts have no place in the public request denominator.
	f.usage("client:"+uuid.NewString(), "/v1/pvt-private-endpoint-j78", at, 10)
	f.usage("client:"+uuid.NewString(), "/v1/messages", at.AddDate(0, 0, -70), 325)
	f.now = f.now.Add(time.Minute)
	f.refresh()
	resp := f.call(0, "GET", publicBase+"?window=1h", "")
	s := readData[domain.Summary](t, resp, 200)
	c = s.Components[0]
	require.Equal(t, "fresh", c.Freshness)
	require.Equal(t, int64(24), c.SampleCount)
	require.Equal(t, int64(20), c.Counts.Succeeded)
	require.Equal(t, int64(4), c.Counts.Attempts)
	require.Equal(t, int64(1), c.Counts.FirstByteTimeouts)
	require.Equal(t, int64(1), c.Counts.UpstreamRateLimits)
	require.Equal(t, int64(1), c.Counts.UpstreamErrors)
	require.Equal(t, int64(1), c.Counts.GatewayErrors)
	require.Equal(t, int64(1), c.Counts.StreamInterruptions)
	require.Equal(t, int64(6), c.Counts.AdmissionDenials)
	require.Equal(t, int64(2), c.Counts.ClientCancellations)
	require.Equal(t, int64(20), c.TTFTSampleCount)
	require.Equal(t, 50, *c.TTFTMedianMS)
	require.Equal(t, 400, *c.TTFTP95MS)
	require.InDelta(t, 100*20.0/24, *c.SuccessRate, 0.001)
	require.InDelta(t, 100*20.0/21, *c.StreamCompletionRate, 0.001)
	require.Equal(t, "outage", c.State)
	require.Equal(t, "unknown", s.Components[2].State)
	require.Equal(t, "SOURCE_UNAVAILABLE", *s.Components[4].Reason)
	require.Equal(t, at, *c.LastSuccess)
	require.Equal(t, c.Counts, s.Components[1].Counts)
	require.Equal(t, int64(30), *s.DataLagSeconds)
	require.Len(t, s.Components[1].History, 90)
	require.Equal(t, int64(24), s.Components[1].History[len(c.History)-1].SampleCount)
	for _, token := range []string{
		incoming, ok, badCorrelation, rejected, "pvt-", "192.0.2.88", "private-member",
		"actor_id", "project_id", "workspace_id", "api_key_id", "account_id", "endpoint", "trace_id", "error_body", "request_id",
	} {
		require.NotContains(t, resp.Body.String(), token)
	}
	f.cache(publicBase+"?window=1h", resp)
	require.Equal(t, 422, f.call(0, "GET", publicBase+"?window=90d", "").Code)

	for range 91 {
		more, err := f.h.store.Backfill(t.Context())
		require.NoError(t, err)
		if !more {
			break
		}
	}
	history := readData[domain.Summary](t, f.call(0, "GET", publicBase+"?window=30d", ""), 200).Components[2].History
	require.Contains(t, history, domain.Daily{
		Date: at.AddDate(0, 0, -70).Format(time.DateOnly), State: "unknown", SampleCount: 1,
		Reason: ptr("INSUFFICIENT_SAMPLES"),
	})

	f.monitoring = false
	off := readData[domain.Summary](t, f.call(0, "GET", publicBase+"?window=1h", ""), 200).Components[0]
	require.Equal(t, "SOURCE_UNAVAILABLE", *off.Reason)
	require.Nil(t, off.SuccessRate)
	f.monitoring = true
	f.now = f.now.Add(7 * time.Minute)
	stale := readData[domain.Summary](t, f.call(0, "GET", publicBase+"?window=1h", ""), 200).Components[0]
	require.Equal(t, "stale", stale.Freshness)
	require.Equal(t, "unknown", stale.State)
	require.Equal(t, "STALE_DATA", *stale.Reason)
	require.Nil(t, stale.SuccessRate)
	require.Equal(t, at, *stale.LastSuccess)
	f.now = f.now.AddDate(0, 0, 91)
	f.refresh()
	pruned := readData[domain.Summary](t, f.call(0, "GET", publicBase+"?window=1h", ""), 200).Components[0]
	require.Equal(t, at, *pruned.LastSuccess)
	require.Equal(t, "stale", pruned.Freshness)
	require.Zero(t, f.id(`SELECT count(*) FROM public_status_buckets WHERE bucket_start < $1`, at.AddDate(0, 0, 1)))
	f.now = time.Now().UTC()
}

func (f *fixture) incidents() {
	t := f.t
	initialPath := publicBase + "/incidents"
	require.Empty(t, readData[incidentPage](t, f.call(0, "GET", initialPath, ""), 200).Items)
	body := `{"title":"Elevated inference errors","severity":"outage","component_ids":["inference"],"message":"We are investigating."}`
	require.Equal(t, 401, f.call(0, "POST", adminBase, body, "Idempotency-Key", "one").Code)
	require.Equal(t, 403, f.call(f.member, "POST", adminBase, body, "Idempotency-Key", "one").Code)
	first := f.call(f.admin, "POST", adminBase, body, "Idempotency-Key", "one")
	created := readData[domain.PublishedIncident](t, first, 201)
	require.Equal(t, `"1"`, first.Header().Get("ETag"))
	require.Contains(t, first.Header().Get("Cache-Control"), "no-store")
	public := initialPath + "/" + created.ID
	updates := adminBase + "/" + created.ID + "/updates"
	detail1 := f.call(0, "GET", public, "")
	require.Equal(t, "investigating", readData[domain.Incident](t, detail1, 200).State)
	f.cache(public, detail1)
	active := readData[domain.Summary](t, f.call(0, "GET", publicBase, ""), 200)
	require.Equal(t, "outage", active.Overall)
	require.Equal(t, "outage", active.Components[0].State)
	require.Equal(t, "unknown", active.Components[0].ObservedState)
	require.Equal(t, int64(1), active.ActiveIncidentCount)
	require.Equal(t, created.ID, active.Incidents[0].ID)

	second := readData[domain.PublishedIncident](t, f.call(f.admin, "POST", adminBase, body, "Idempotency-Key", "two"), 201)
	require.NotEqual(t, created.ID, second.ID)
	page1 := readData[incidentPage](t, f.call(0, "GET", initialPath+"?limit=1", ""), 200)
	require.NotNil(t, page1.NextCursor)
	next := initialPath + "?limit=1&cursor=" + url.QueryEscape(*page1.NextCursor)
	require.Equal(t, 422, f.call(0, "GET", strings.Replace(next, "limit=1", "limit=2", 1), "").Code)

	change := `{"state":"identified","message":"We identified the affected subsystem."}`
	require.Equal(t, 422, f.call(f.admin, "POST", updates, change, "Idempotency-Key", "change").Code)
	identified := f.call(f.admin, "POST", updates, change, "Idempotency-Key", "change", "If-Match", `"1"`)
	require.Equal(t, int64(2), readData[domain.PublishedIncident](t, identified, 201).Version)
	old := f.call(f.admin, "POST", updates, change, "Idempotency-Key", "stale", "If-Match", `"1"`)
	require.Equal(t, 412, old.Code)
	require.Equal(t, `"2"`, old.Header().Get("ETag"))
	replay := f.call(f.admin, "POST", updates, change, "Idempotency-Key", "change", "If-Match", `"1"`)
	require.JSONEq(t, identified.Body.String(), replay.Body.String())
	require.Equal(t, 409, f.call(f.admin, "POST", updates, `{"state":"monitoring","message":"recovering"}`,
		"Idempotency-Key", "change", "If-Match", `"1"`).Code)
	page2 := readData[incidentPage](t, f.call(0, "GET", next, ""), 200)
	require.Equal(t, page1.AsOf, page2.AsOf)
	require.Equal(t, "investigating", page2.Items[0].State)

	for i, change := range []string{
		`{"state":"monitoring","message":"Monitoring recovery."}`,
		`{"state":"resolved","message":"The service has recovered."}`,
	} {
		version := versionTag(int64(i + 2))
		response := f.call(f.admin, "POST", updates, change, "Idempotency-Key", "update"+version, "If-Match", version)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	}
	current := f.call(0, "GET", public, "", "If-None-Match", detail1.Header().Get("ETag"))
	detail := readData[domain.Incident](t, current, 200)
	require.NotEqual(t, detail1.Header().Get("ETag"), current.Header().Get("ETag"))
	require.Equal(t, "resolved", detail.State)
	require.NotNil(t, detail.ResolvedAt)
	require.Len(t, detail.Updates, 4)
	for i, state := range []string{"investigating", "identified", "monitoring", "resolved"} {
		require.Equal(t, state, detail.Updates[i].State)
		if i > 0 {
			require.True(t, detail.Updates[i-1].At.Before(detail.Updates[i].At))
		}
	}
	require.Empty(t, readData[domain.Summary](t, f.call(0, "GET", publicBase, ""), 200).Incidents[0].ResolvedAt)
	f.cache(public, current)
	require.Equal(t, 409, f.call(f.admin, "POST", updates, change, "Idempotency-Key", "reopen", "If-Match", `"4"`).Code)
	require.JSONEq(t, first.Body.String(), f.call(f.admin, "POST", adminBase, body, "Idempotency-Key", "one").Body.String())
	for _, token := range []string{"actor_id", "project_id", "endpoint", "version", "publisher@example.test", "fingerprint"} {
		require.NotContains(t, current.Body.String(), token)
	}
	require.Equal(t, 404, f.call(0, "GET", initialPath+"/"+uuid.NewString(), "").Code)
}

func (f *fixture) cache(path string, first *httptest.ResponseRecorder) {
	t := f.t
	tag, updated := first.Header().Get("ETag"), first.Header().Get("Last-Modified")
	require.NotEmpty(t, tag)
	require.NotEmpty(t, updated)
	require.Contains(t, first.Header().Get("Cache-Control"), "public")
	for _, conditional := range [][]string{{"If-None-Match", `"other", ` + tag}, {"If-Modified-Since", updated}} {
		resp := f.call(0, "GET", path, "", conditional...)
		require.Equal(t, 304, resp.Code, resp.Body.String())
		require.Empty(t, resp.Body.String())
		require.Equal(t, tag, resp.Header().Get("ETag"))
	}
	require.Equal(t, 200, f.call(0, "GET", path, "", "If-None-Match", `"other"`, "If-Modified-Since", updated).Code)
}

func (f *fixture) refresh() {
	f.t.Helper()
	_, err := f.h.store.Refresh(f.t.Context(), f.now)
	require.NoError(f.t, err)
}

func (f *fixture) id(q string, args ...any) int64 {
	f.t.Helper()
	var id int64
	require.NoError(f.t, f.db.QueryRow(q, args...).Scan(&id))
	return id
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	_, err := f.db.Exec(q, args...)
	require.NoError(f.t, err)
}

func (f *fixture) execution(at time.Time, correlation string, canceled bool, state int) string {
	f.t.Helper()
	id := uuid.NewString()
	f.exec(`INSERT INTO gateway_executions (request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,
	  kind,route,model,legacy_id,reservation_ref,attempt_budget,deadline,model_version,price_version,pricing_at,admitted_at,finished_at,response_state,canceled)
	  VALUES ($1,1,$2,$3,$4,$3,$3,1,'request','/v1/responses','pvt-model-m78',$5,$1,3,$6,$7,$7,$8,$8,$9,$10)`,
		id, f.project, f.member, f.key, "client:"+correlation, at.Add(time.Minute), "pvt-version-v78", at, state, canceled)
	return id
}

func (f *fixture) attempt(id string, at time.Time, ordinal int, reason string) {
	f.t.Helper()
	f.exec(`INSERT INTO gateway_execution_attempts (attempt_id, request_id, ordinal, account_id, dispatched_at,acceptance,outcome,decision)
	    VALUES ($1,$2,$3,$4,$5,'accepted','transport_response',jsonb_build_object('reason',$6::text))`,
		uuid.NewString(), id, ordinal, f.account, at, reason)
}

func (f *fixture) usage(id, endpoint string, at time.Time, ttft int) {
	f.t.Helper()
	f.exec(`INSERT INTO usage_logs (request_id,api_key_id,user_id,account_id,model,inbound_endpoint,stream,request_type,first_token_ms,created_at)
	    VALUES ($1,$2,$3,$4,'pvt-usage-model',$5,true,2,$6,$7)`, id, f.key, f.member, f.account, endpoint, ttft, at)
}

func (f *fixture) receipt(id string, at time.Time, state string, ttft int) {
	f.t.Helper()
	f.exec(`INSERT INTO usage_settlement_receipts (request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,
	    pricing_snapshot,resolution_state,completion_state,state,created_at)
	    VALUES ($1,$2,$3,$4,'pvt-fingerprint', jsonb_build_object('InboundEndpoint','/v1/responses','Stream',true,'FirstTokenMs',$5::int),
	      '{}','free','pvt-price','resolved',$6,'pending',$7)`, id, f.key, f.member, f.account, ttft, state, at)
}

func (f *fixture) ops(clientID string, at time.Time, owner string, status, upstream int, stream bool, ttft any, endpoint string) {
	f.t.Helper()
	f.exec(`INSERT INTO ops_error_logs (request_id,client_request_id,api_key_id,user_id,account_id,inbound_endpoint,error_phase,error_type,
	    error_owner,status_code,upstream_status_code,stream,time_to_first_token_ms,error_message,created_at)
	    VALUES ($1,$2,$3,$4,$5,$6,'upstream','fixture',$7,$8,$9,$10,$11,'pvt-body-a78',$12)`,
		uuid.NewString(), clientID, f.key, f.member, f.account, endpoint, owner, status, upstream, stream, ttft, at)
}

func (f *fixture) call(actor int64, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if actor != 0 {
		req.Header.Set("X-Fixture-Actor", strconv.FormatInt(actor, 10))
	}
	for i := 0; i < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	f.engine.ServeHTTP(w, req)
	return w
}

func readData[T any](t *testing.T, w *httptest.ResponseRecorder, code int) T {
	t.Helper()
	require.Equal(t, code, w.Code, w.Body.String())
	var body struct{ Data T }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Data
}

func ptr(v string) *string { return &v }
