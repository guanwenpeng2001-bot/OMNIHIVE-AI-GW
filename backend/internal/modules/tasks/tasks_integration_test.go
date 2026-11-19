//go:build integration

package tasks_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type cipher struct{}

func (cipher) Encrypt(s string) (string, error) { return "sealed/" + s, nil }
func (cipher) Decrypt(s string) (string, error) { return strings.TrimPrefix(s, "sealed/"), nil }

type runner struct {
	db       *sql.DB
	count    atomic.Int32
	outcomes chan tasks.Outcome
	stopped  chan bool
}

func (r *runner) Prepare(ctx context.Context, a tasks.Scope, w tasks.Work) (tasks.Prepared, error) {
	var funded bool
	e := r.db.QueryRowContext(ctx, "SELECT balance>0 FROM users WHERE id=$1", a.ActorID).Scan(&funded)
	if e != nil {
		return tasks.Prepared{}, e
	}
	if !funded {
		return tasks.Prepared{}, tasks.Budget
	}
	_, e = r.db.ExecContext(ctx, `INSERT INTO gateway_executions(request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,kind,route,model,legacy_id,reservation_ref,attempt_budget,deadline,model_version,price_version,pricing_at)
 VALUES($1::text::uuid,$2,$3,$4,$5,$4,$4,1,'job','/v1/images/generations',$6,$7,$1,1,now()+interval '1 hour','m','p',now())`, w.Request, a.WorkspaceID, a.ProjectID, a.ActorID, w.Key, w.Model, w.ID)
	if e != nil {
		return tasks.Prepared{}, e
	}
	r.count.Add(1)
	return tasks.Prepared{Version: "adapter-v1", Run: func(context.Context) tasks.Outcome { return <-r.outcomes }, Close: func() { r.stopped <- true }}, nil
}

func TestImageTaskCenterPostgresRedis(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("disposable PostgreSQL and Redis required")
	}
	ctx := t.Context()
	db, e := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, e)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	require.NoError(t, testresources.ValidateRedis(addr))
	rd := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { rd.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, e := db.ExecContext(ctx, query, args...)
		require.NoError(t, e)
	}
	id := func(query string, args ...any) int64 {
		t.Helper()
		var v int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&v))
		return v
	}
	actor := id("INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',10) RETURNING id", uuid.NewString()+"@test.invalid")
	workspace := id("INSERT INTO workspaces(name) VALUES('task test') RETURNING id")
	project := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", workspace)
	key := id("INSERT INTO api_keys(workspace_id,user_id,name) VALUES($1,$2,'task test') RETURNING id", workspace, actor)
	a := tasks.Scope{ActorID: actor, WorkspaceID: workspace, ProjectID: project, Version: 1, Actions: []string{"tasks.read", "tasks.create", "tasks.retry", "tasks.cancel", "tasks.artifacts.read", "tasks.artifacts.revoke", "requests.read", "billing.read"}}
	gateway := &runner{db: db, outcomes: make(chan tasks.Outcome, 10), stopped: make(chan bool, 10)}
	s := tasks.New(db, gateway, cipher{}, nil, nil)
	in := tasks.Create{Kind: "image", Model: "model", Key: key, Input: tasks.Input{Prompt: "leaf", Count: 1}, Acknowledged: true}
	token := uuid.NewString()
	const clients = 8
	var group sync.WaitGroup
	ds := make([]tasks.Detail, clients)
	es := make([]error, clients)
	for i := range clients {
		group.Go(func() { ds[i], es[i] = s.Create(ctx, a, in, token, "127.0.0.1") })
	}
	group.Wait()
	for i, e := range es {
		require.NoError(t, e)
		require.Equal(t, ds[0].ID, ds[i].ID)
	}
	d := ds[0]
	require.EqualValues(t, 1, gateway.count.Load())
	require.Nil(t, d.Progress)
	require.Equal(t, "unreported", d.ProgressSource)
	require.NotNil(t, d.RequestURL)
	require.Contains(t, *d.RequestURL, *d.Request)
	altered := in
	altered.Input.Prompt = "another"
	_, e = s.Create(ctx, a, altered, token, "")
	require.ErrorIs(t, e, fault.Conflict)
	other := a
	other.ProjectID++
	_, e = s.Detail(ctx, other, d.ID)
	require.ErrorIs(t, e, fault.NotFound)
	_, e = s.Cancel(ctx, a, d.ID, "cancel")
	require.ErrorIs(t, e, tasks.UnsupportedCancel)
	_, e = s.Retry(ctx, a, d.ID, "retry", "", tasks.Retry{Items: []string{"1"}, Acknowledged: true})
	require.ErrorIs(t, e, tasks.Unknown)
	gateway.outcomes <- tasks.Outcome{State: "failed", Acceptance: "not_sent"}
	<-gateway.stopped
	_, e = s.Retry(ctx, a, d.ID, "retry", "", tasks.Retry{})
	require.Error(t, e)
	_, e = s.Retry(ctx, a, d.ID, token, "", tasks.Retry{Acknowledged: true})
	require.ErrorIs(t, e, fault.Conflict)
	child, e := s.Retry(ctx, a, d.ID, "paid-retry", "", tasks.Retry{Acknowledged: true, Items: []string{"1"}})
	require.NoError(t, e)
	require.NotEqual(t, *d.Request, child.Request)
	require.Equal(t, d.ID, child.Parent)
	replay, e := s.Retry(ctx, a, d.ID, "paid-retry", "", tasks.Retry{Acknowledged: true, Items: []string{"1"}})
	require.NoError(t, e)
	require.Equal(t, child, replay)
	require.EqualValues(t, 2, gateway.count.Load())
	gateway.outcomes <- tasks.Outcome{State: "succeeded", Acceptance: "accepted", Artifacts: []tasks.Output{{ID: "result", ContentType: "image/png", Size: 40, Locator: "private/key"}}}
	<-gateway.stopped
	account := id("INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'fixture','openai','apikey') RETURNING id", workspace)
	exec(`INSERT INTO gateway_execution_attempts(attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome) VALUES($1,$2,1,$3,now(),'accepted','transport_response')`, uuid.NewString(), child.Request, account)
	receipt := id(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,settled_at,usage_log_id)
 VALUES($1,$2,$3,$4,'fp','{}','{"BalanceCost":0.12}','priced','{}','resolved','complete','settled',now(),9999) RETURNING id`, child.Request, key, actor, account)
	exec("UPDATE users SET balance=0 WHERE id=$1", actor)
	purchased, e := s.Detail(ctx, a, child.ID)
	require.NoError(t, e)
	require.Equal(t, fmt.Sprint(receipt), *purchased.SettlementID)
	require.Equal(t, "0.12000000", purchased.Charge.String())
	_, e = s.Artifact(ctx, a, child.ID, "result")
	require.NoError(t, e)
	_, e = s.Artifact(ctx, other, child.ID, "result")
	require.ErrorIs(t, e, fault.NotFound)
	_, e = s.Create(ctx, a, in, "insufficient", "")
	require.ErrorIs(t, e, tasks.Budget)
	exec("UPDATE image_task_artifacts SET expires_at=now()-interval '1 second' WHERE task_id=$1", child.ID)
	_, e = s.Artifact(ctx, a, child.ID, "result")
	require.ErrorIs(t, e, tasks.Gone)
	exec("UPDATE image_task_artifacts SET expires_at=now()+interval '1 day' WHERE task_id=$1", child.ID)
	require.NoError(t, s.Revoke(ctx, a, child.ID, "result"))
	_, e = s.Artifact(ctx, a, child.ID, "result")
	require.ErrorIs(t, e, tasks.Gone)
	exec("UPDATE users SET balance=10 WHERE id=$1", actor)
	lost, e := s.Create(ctx, a, in, "lost", "")
	require.NoError(t, e)
	var owner string
	require.NoError(t, db.QueryRowContext(ctx, "UPDATE image_tasks SET lease_until=now()-interval '1 second' WHERE id=$1 RETURNING lease_owner", lost.ID).Scan(&owner))
	require.ErrorIs(t, s.Finish(ctx, tasks.Lease{ID: lost.ID, Owner: owner, Generation: 1}, tasks.Outcome{State: "failed", Acceptance: "not_sent"}), tasks.ErrLostLease)
	require.NoError(t, s.Recover(ctx))
	gateway.outcomes <- tasks.Outcome{State: "succeeded", Acceptance: "accepted", Artifacts: []tasks.Output{{ID: "stale", Size: 1, Locator: "private"}}}
	<-gateway.stopped
	unknown, e := s.Detail(ctx, a, lost.ID)
	require.NoError(t, e)
	require.Equal(t, "unknown", unknown.State)
	require.Empty(t, unknown.Artifacts)
	batch := uuid.NewString()
	bid := id(`INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,account_id,provider,model,status,item_count,success_count,fail_count,estimated_cost,actual_cost,settled_at,output_expires_at)
 VALUES($1,$2,$3,$4,'gemini_api','image','completed',2,1,1,.4,.2,now(),now()+interval '1 day') RETURNING id`, batch, actor, key, account)
	exec(`INSERT INTO batch_image_items(job_id,custom_id,status,image_count) VALUES($1,'i1','success',1),($1,'i2','failed',0)`, batch)
	batchTask := fmt.Sprintf("batch_%d", bid)
	b, e := s.Detail(ctx, a, batchTask)
	require.NoError(t, e)
	require.Equal(t, "partially_succeeded", b.Stage)
	require.EqualValues(t, 1, b.Counts.Failed)
	require.Len(t, b.Artifacts, 1)
	items, e := s.Items(ctx, a, batchTask, tasks.Page{AsOf: time.Now(), Limit: 2})
	require.NoError(t, e)
	require.Len(t, items, 2)
	require.Equal(t, "failed", items[1].State)
	exec("UPDATE batch_image_jobs SET status='cancelled' WHERE id=$1", bid)
	b, e = s.Detail(ctx, a, batchTask)
	require.NoError(t, e)
	require.Equal(t, "confirmed", b.Cancel)
	require.Equal(t, "canceled", b.State)
	migrated := tasks.LegacyImport{DB: db, Redis: rd}
	rid := "imgtask_" + uuid.NewString()
	rec, _ := json.Marshal(map[string]any{"id": rid, "workspace_id": workspace, "user_id": actor, "api_key_id": key, "status": "processing", "version": 1, "created_at": time.Now().Unix(), "expires_at": time.Now().Add(time.Hour).Unix(), "execution_deadline": time.Now().Add(time.Minute).Unix()})
	require.NoError(t, rd.Set(ctx, "image_task:"+rid, rec, time.Hour).Err())
	require.NoError(t, rd.Set(ctx, "batch_image:queue:lock:"+batch, "existing-owner", time.Minute).Err())
	for _, scan := range []func(context.Context, uint64) (uint64, error){migrated.Page, migrated.BatchLeases} {
		var cursor uint64
		for {
			cursor, e = scan(ctx, cursor)
			require.NoError(t, e)
			if cursor == 0 {
				break
			}
		}
	}
	rd.Del(ctx, "image_task:"+rid, "batch_image:queue:lock:"+batch)
	old, e := s.Detail(ctx, a, "redis_"+rid)
	require.NoError(t, e)
	require.Equal(t, "running", old.State)
	require.Nil(t, old.Progress)
	require.EqualValues(t, 1, id("SELECT count(*) FROM image_tasks WHERE id=$1 AND lease_owner='legacy' AND lease_until>now()", batchTask))
}
