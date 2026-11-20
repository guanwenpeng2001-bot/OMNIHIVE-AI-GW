//go:build integration

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	taskhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/providers/mediajobs"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type mediaSession struct{ actor tasks.Scope }

func (s mediaSession) Evaluate(_ context.Context, actor, w, p int64, action string) (identity.Access, error) {
	a := s.actor
	a.ActorID = actor
	a.WorkspaceID = w
	a.ProjectID = p
	return a, a.Require(action)
}

type mediaObject struct{}

func (mediaObject) Open(context.Context, tasks.ArtifactContent) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(strings.Repeat("a", 44))), nil
}

type mediaCipher struct{}

func (mediaCipher) Encrypt(s string) (string, error) { return s, nil }
func (mediaCipher) Decrypt(s string) (string, error) { return s, nil }

type mediaFixture struct {
	db             *sql.DB
	jobs           *service.UpstreamJobService
	group, account int64
	sends, cancels atomic.Int32
	finished       chan error
}

func (f *mediaFixture) CancelVideo(context.Context, int64, int64, string) error {
	f.cancels.Add(1)
	return nil
}
func (f *mediaFixture) Prepare(ctx context.Context, a tasks.Scope, w tasks.Work) (tasks.Prepared, error) {
	p, ok := media.Lookup(w.Kind, w.Model)
	if !ok {
		return tasks.Prepared{}, media.Unsupported
	}
	per := "1"
	if w.Kind == "audio" {
		per = "1000000"
	}
	rate, err := money.NewRate("USD", "1.25", money.Unit(p.Unit), per)
	if err != nil {
		return tasks.Prepared{}, err
	}
	verify := func(ctx context.Context, account int64) (media.Proof, error) {
		return mediajobs.Verified(ctx, f.db, a.WorkspaceID, a.ProjectID, f.group, account, p, rate)
	}
	proof, err := verify(ctx, f.account)
	if err != nil {
		return tasks.Prepared{}, err
	}
	ctx = scope.With(ctx, scope.Authorized{WorkspaceID: a.WorkspaceID, ProjectID: a.ProjectID, ActorID: a.ActorID, KeyID: w.Key, KeyOwnerID: a.ActorID, FundingUserID: a.ActorID, Version: a.Version})
	ctx = journal.WithReauthorization(ctx, func(context.Context, scope.Authorized, string) error { return nil })
	js := imageJobJournal{Store: journal.New(f.db), db: f.db, request: w.Request, task: w.ID, price: proof.PriceVersion, lease: w.Lease, check: func(ctx context.Context, id int64) error { _, err := verify(ctx, id); return err }}
	ctx = service.WithMediaTaskBinding(ctx, service.MediaTaskBinding{Request: w.Request, Model: p.Model, Kind: p.Kind, Account: f.account})
	settlement := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptRepository(f.db), nil, nil)
	native := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settlement)
	plan, closePlan := native.CaptureAsyncImageTaskPlan(journal.WithStore(ctx, js), service.GatewayRequestSpec{WorkspaceID: a.WorkspaceID, UserID: a.ActorID, APIKeyID: w.Key, BillingGroupID: f.group, Model: w.Model, Route: p.Route, ExecutionID: w.ID}, time.Minute)
	var runErr error
	return tasks.Prepared{Version: proof.ID, Media: &proof, Close: func() { closePlan(); f.finished <- runErr }, Run: func(context.Context) tasks.Outcome {
		unknown := tasks.Outcome{State: "unknown", Acceptance: "unknown"}
		if w.Kind == "audio" {
			permit, e := service.BeginGenerationAttempt(plan, f.account)
			if e != nil {
				runErr = e
				return unknown
			}
			f.sends.Add(1)
			permit(200, nil)
			// Money comes from the independently linked receipt.
			return tasks.Outcome{State: "succeeded", Acceptance: "accepted", Artifacts: []tasks.Output{{ID: "audio_1", ContentType: "audio/wav", Size: 44, Locator: "task-artifacts/" + w.ID + "/audio.wav"}}}
		}
		_, runErr = f.jobs.Submit(plan, &service.UpstreamJob{Platform: "qianwen", Family: "video", UserID: a.ActorID, APIKeyID: w.Key, AccountID: f.account, GroupID: &f.group, Billing: service.UpstreamJobBilling{Model: p.Model, RequestedModel: p.Model, UnitPrice: 1.25, Resolution: "720p", RateMultiplier: 1, AccountRateMultiplier: 1}}, taskPayload(w, p.Model), func(ctx context.Context) (service.UpstreamJobUpdate, error) {
			if w.Input.Prompt == "fenced" {
				_, err := f.db.ExecContext(ctx, "UPDATE image_tasks SET lease_until=now()-interval '1 second' WHERE id=$1", w.ID)
				if err != nil {
					return service.UpstreamJobUpdate{}, err
				}
			}
			permit, e := service.BeginGenerationAttempt(ctx, f.account)
			if e != nil {
				return service.UpstreamJobUpdate{}, e
			}
			f.sends.Add(1)
			if w.Input.Prompt == "lost ack" {
				err := errors.New("provider acknowledgement lost")
				permit(0, err)
				return service.UpstreamJobUpdate{}, err
			}
			permit(200, nil)
			if w.Input.Prompt == "completed" {
				return service.UpstreamJobUpdate{TaskID: uuid.NewString(), Status: "succeeded", ProviderStatus: "SUCCEEDED", ResultRef: "https://dashscope-result-cn.aliyuncs.com/private", Usage: []byte(`{"duration":5,"video_count":1,"SR":720}`)}, nil
			}
			return service.UpstreamJobUpdate{TaskID: uuid.NewString(), Status: "submitted", ProviderStatus: "PENDING"}, nil
		})
		return unknown
	}}, nil
}

func TestMediaTaskPostgres(t *testing.T) {
	ctx := t.Context()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	id := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&n))
		return n
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	w := id("INSERT INTO workspaces(name) VALUES('media') RETURNING id")
	project := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	actor := id("INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',10) RETURNING id", uuid.NewString()+"@test.invalid")
	group := id("INSERT INTO groups(workspace_id,name,platform) VALUES($1,$2,'qianwen') RETURNING id", w, uuid.NewString())
	account := id("INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'fixture','qianwen','apikey') RETURNING id", w)
	exec("INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)", account, group)
	key := id("INSERT INTO api_keys(workspace_id,user_id,group_id,name) VALUES($1,$2,$3,'fixture') RETURNING id", w, actor, group)
	a := tasks.Scope{WorkspaceID: w, ProjectID: project, ActorID: actor, Version: 1, Actions: []string{"tasks.create", "tasks.read", "tasks.retry", "tasks.cancel", "tasks.artifacts.read", "billing.read", "requests.read"}}
	f := &mediaFixture{db: db, group: group, account: account, jobs: service.NewUpstreamJobService(repository.NewUpstreamJobRepository(db), nil, service.QianwenVideoJobCharge), finished: make(chan error, 8)}
	s := tasks.New(db, f, mediaCipher{}, mediaObject{}, nil)
	s.Video = f
	in := tasks.Create{Kind: "video", Model: "wan2.6-t2v", Key: key, Acknowledged: true, Input: tasks.Input{Prompt: "sea", Format: "mp4", Size: "1280x720", Duration: 5}}
	_, err = s.Create(ctx, a, in, "disabled", "")
	require.ErrorIs(t, err, media.Unsupported)
	add := func(model, action, unit string, per int) string {
		v := uuid.NewString()
		exec(`INSERT INTO task_media_verifications(id,workspace_id,project_id,group_id,account_id,model,action,adapter,capability_evidence,action_evidence,funding_evidence,cancel_evidence,price_version,billing_unit,usd_rate,per_units,verified_at,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'qianwen.'||$7,'cap-fixture','action-fixture','funding-fixture','cancel-fixture','fixture-v1',$8,1.25,$9,now()-interval '1 minute',now()+interval '1 day')`, v, w, project, group, account, model, action, unit, per)
		return v
	}
	videoProof := add(in.Model, "video.generate", "second", 1)
	var results [6]tasks.Detail
	var errs [6]error
	var workers sync.WaitGroup
	for i := range results {
		workers.Go(func() { results[i], errs[i] = s.Create(ctx, a, in, "replay", "") })
	}
	workers.Wait()
	for i := range results {
		require.NoError(t, errs[i])
		require.Equal(t, results[0].ID, results[i].ID)
	}
	require.NoError(t, <-f.finished)
	d := results[0]
	require.EqualValues(t, 1, f.sends.Load())
	require.EqualValues(t, 1, id("SELECT count(*) FROM gateway_execution_attempts WHERE request_id=$1 AND dispatched_at IS NOT NULL", *d.Request))
	d, err = s.Detail(ctx, a, d.ID)
	require.NoError(t, err)
	require.Equal(t, "running", d.State)
	require.Equal(t, "accepted", d.Provider)
	require.Equal(t, "available", d.Cancel)
	require.Nil(t, d.Charge)
	require.EqualValues(t, 1, id("SELECT count(*) FROM task_media_attributes WHERE provider_job_id=$1", *d.Request))
	other := a
	other.ProjectID++
	_, err = s.Cancel(ctx, other, d.ID, "foreign")
	require.ErrorIs(t, err, fault.NotFound)
	for i := 0; i < 2; i++ {
		c, e := s.Cancel(ctx, a, d.ID, "cancel")
		require.NoError(t, e)
		require.Equal(t, "requested", c.Cancel)
	}
	require.EqualValues(t, 1, f.cancels.Load())
	exec(`UPDATE upstream_jobs SET status='failed',finished_at=now(),resource_state='{"provider_status":"CANCELED"}' WHERE public_request_id=$1`, *d.Request)
	d, err = s.Detail(ctx, a, d.ID)
	require.NoError(t, err)
	require.Equal(t, "canceled", d.State)
	require.Equal(t, "confirmed", d.Cancel)
	in.Input.Prompt = "lost ack"
	_, err = s.Create(ctx, a, in, "replay", "")
	require.ErrorIs(t, err, fault.Conflict)
	lost, err := s.Create(ctx, a, in, "new", "")
	require.NoError(t, err)
	require.NoError(t, <-f.finished)
	lost, err = s.Detail(ctx, a, lost.ID)
	require.NoError(t, err)
	require.Equal(t, "unknown", lost.State)
	_, err = s.Retry(ctx, a, lost.ID, "retry", "", tasks.Retry{Acknowledged: true})
	require.ErrorIs(t, err, tasks.Unknown)
	in.Input.Prompt = "fenced"
	priorSends := f.sends.Load()
	stale, err := s.Create(ctx, a, in, "fenced", "")
	require.NoError(t, err)
	require.NoError(t, <-f.finished)
	require.NoError(t, s.Recover(ctx))
	stale, err = s.Detail(ctx, a, stale.ID)
	require.NoError(t, err)
	require.Equal(t, "unknown", stale.State)
	require.Empty(t, stale.Artifacts)
	require.Equal(t, priorSends, f.sends.Load())
	require.Zero(t, id("SELECT count(*) FROM gateway_execution_attempts WHERE request_id=$1 AND dispatched_at IS NOT NULL", *stale.Request))
	in.Input.Prompt = "completed"
	paid, err := s.Create(ctx, a, in, "paid", "")
	require.NoError(t, err)
	require.NoError(t, <-f.finished)
	paid, err = s.Detail(ctx, a, paid.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", paid.State)
	require.Equal(t, "settled", paid.Settlement)
	require.NotNil(t, paid.Charge)
	require.Equal(t, "6.25000000", paid.Charge.String())
	require.EqualValues(t, 1, id(`SELECT count(*) FROM gateway_execution_attempts a JOIN gateway_execution_receipts l USING(attempt_id) JOIN billing_settlement_outbox o USING(receipt_id) WHERE a.request_id=$1`, *paid.Request))
	require.EqualValues(t, 1, id("SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", *paid.Request, key))
	require.EqualValues(t, 1, id("SELECT count(*) FROM users WHERE id=$1 AND balance=3.75", actor))
	exec("UPDATE task_media_verifications SET revoked_at=now() WHERE id=$1", videoProof)
	_, err = s.Create(ctx, a, in, "revoked", "")
	require.ErrorIs(t, err, media.Unsupported)
	// Older native jobs keep their original ID, account, result expiry and charge anchor.
	legacy := uuid.NewString()
	exec("INSERT INTO identity_scope_quarantine(kind,resource_id,reason) VALUES('api_keys',$1,'fixture')", key)
	jobID := id(`INSERT INTO upstream_jobs(workspace_id,public_request_id,request_fingerprint,submission_key,platform,family,account_id,user_id,api_key_id,upstream_task_id,status,finished_at,expires_at,result_ref,billing_snapshot,charged_usage_log_id)
 VALUES($1,$2,$2,$2,'qianwen','video',$3,$4,$5,'old','succeeded',now(),now()+interval '1 day','https://dashscope-result-cn.aliyuncs.com/private','{"model":"wan2.6-i2v"}',12345678) RETURNING id`, w, legacy, account, actor, key)
	old := fmt.Sprintf("media_%d", jobID)
	_, err = s.Detail(ctx, a, old)
	require.ErrorIs(t, err, fault.NotFound)
	exec("DELETE FROM identity_scope_quarantine WHERE kind='api_keys' AND resource_id=$1", key)
	exec("SELECT task_media_import($1)", jobID)
	exec("SELECT task_media_import($1)", jobID)
	oldD, err := s.Detail(ctx, a, old)
	require.NoError(t, err)
	require.Equal(t, "video", oldD.Kind)
	require.Len(t, oldD.Artifacts, 1)
	require.Nil(t, oldD.Charge)
	_, err = s.Artifact(ctx, other, old, "video_1")
	require.ErrorIs(t, err, fault.NotFound)
	ar, err := s.Artifact(ctx, a, old, "video_1")
	require.NoError(t, err)
	require.Equal(t, "media_video", ar.Source)
	require.Equal(t, legacy, ar.Locator)
	usage := id("INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,total_cost,actual_cost) VALUES($1,$2,$3,$4,'wan2.6-i2v',6.25,6.25) RETURNING id", actor, key, account, legacy)
	exec("UPDATE upstream_jobs SET charged_usage_log_id=$2 WHERE id=$1", jobID, usage)
	oldD, err = s.Detail(ctx, a, old)
	require.NoError(t, err)
	require.Equal(t, "6.25000000", oldD.Charge.String())
	require.Equal(t, fmt.Sprintf("upstream_job_usage:%d", usage), *oldD.SettlementID)
	exec("UPDATE upstream_jobs SET expires_at=now()-interval '1 second' WHERE id=$1", jobID)
	_, err = s.Artifact(ctx, a, old, "video_1")
	require.ErrorIs(t, err, tasks.Gone)
	in.Kind = "audio"
	in.Model = "qwen3-tts-flash"
	in.Input = tasks.Input{Prompt: "hello", Format: "wav"}
	add(in.Model, "audio.tts", "character", 1000000)
	audio, err := s.Create(ctx, a, in, "audio", "")
	require.NoError(t, err)
	require.NoError(t, <-f.finished)
	audio, err = s.Detail(ctx, a, audio.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", audio.State)
	require.Nil(t, audio.Charge)
	_, err = s.Cancel(ctx, a, audio.ID, "audio-cancel")
	require.ErrorIs(t, err, tasks.UnsupportedCancel)
	receipt := id(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,settled_at,usage_log_id)
 VALUES($1,$2,$3,$4,'fp','{"characters":5}','{"BalanceCost":0.00000625}','priced','{}','resolved','complete','settled',now(),$5) RETURNING id`, *audio.Request, key, actor, account, usage+100000)
	audio, err = s.Detail(ctx, a, audio.ID)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(receipt), *audio.SettlementID)
	require.Equal(t, "0.00000625", audio.Charge.String())
	rows, err := s.List(ctx, a, tasks.Page{AsOf: time.Now(), Limit: 10, Kind: []string{"audio"}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, audio.ID, rows[0].ID)

	exec("UPDATE accounts SET credentials=jsonb_build_object('model_mapping','{}'::jsonb) WHERE id=$1", account)
	_, err = s.Create(ctx, a, in, "rotated", "")
	require.ErrorIs(t, err, media.Unsupported)
	cfg := &config.Config{}
	cfg.JWT.Secret = uuid.NewString()
	server := gin.New()
	taskhttp.Register(server.Group("/api/v2"), taskhttp.New(s, mediaSession{a}, cfg), func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor}) })
	call := func(method, path, ticket, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Artifact-Ticket", ticket)
		req.Header.Set("Idempotency-Key", "audio")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	base := tasks.Base(a) + "/tasks"
	httpReplay := call("POST", base, "", string(raw))
	require.Equal(t, 202, httpReplay.Code, httpReplay.Body.String())
	require.Contains(t, httpReplay.Body.String(), audio.ID)
	require.Equal(t, 200, call("GET", base+"?kind=audio", "", "").Code)
	artifact := base + "/" + audio.ID + "/artifacts/audio_1"
	ticket := call("POST", artifact+"/download", "", "{}")
	require.Equal(t, 200, ticket.Code, ticket.Body.String())
	var response struct{ Data struct{ Ticket, URL string } }
	require.NoError(t, json.Unmarshal(ticket.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data.Ticket)
	downloaded := call("GET", response.Data.URL, response.Data.Ticket, "")
	require.Equal(t, 200, downloaded.Code)
	require.Equal(t, "audio/wav", downloaded.Header().Get("Content-Type"))
	foreign := strings.Replace(response.Data.URL, fmt.Sprintf("/projects/%d/", project), fmt.Sprintf("/projects/%d/", project+200000), 1)
	require.Equal(t, 404, call("GET", foreign, response.Data.Ticket, "").Code)
	exec("UPDATE image_task_artifacts SET expires_at=now()-interval '1 second' WHERE task_id=$1", audio.ID)
	require.Equal(t, 410, call("GET", response.Data.URL, response.Data.Ticket, "").Code)
}
