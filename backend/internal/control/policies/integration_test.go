//go:build integration

package policies_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/policies"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPolicyWorkflow(t *testing.T) {
	ctx := t.Context()
	db, _, _ := testutil.NewCapabilityPostgres(t)
	migration, err := os.ReadFile("../../../migrations/299_versioned_routing_policies.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NotEmpty(t, addr)
	cache := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { cache.Close() })
	require.NoError(t, cache.Ping(ctx).Err())
	id := func(q string, args ...any) int64 {
		t.Helper()
		var v int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&v))
		return v
	}
	actor := id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	w := id("INSERT INTO workspaces(name) VALUES('policy test') RETURNING id")
	p := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	_, err = db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", w, actor)
	require.NoError(t, err)
	c := policy.Caller{Actor: actor, Workspace: w, AccessVersion: id("SELECT scope_version FROM workspaces WHERE id=$1", w)}
	store := policy.NewStore(db, []byte("integration-signing-key"))
	sessionActor := actor
	router := gin.New()
	control.Register(router.Group("/api/v2"), control.New(store, postgres.New(db)), func(g *gin.Context) {
		g.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: sessionActor})
	})
	base := fmt.Sprintf("/api/v2/workspaces/%d/routing/policies", w)
	send := func(status int, method, path string, input any, etag int64, key string, out any) {
		t.Helper()
		b, err := json.Marshal(input)
		require.NoError(t, err)
		req := httptest.NewRequest(method, base+path, bytes.NewReader(b))
		if etag > 0 {
			req.Header.Set("If-Match", strconv.Quote(strconv.FormatInt(etag, 10)))
		}
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, rec.Body.String())
		require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		if out != nil {
			var env struct{ Data json.RawMessage }
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.NoError(t, json.Unmarshal(env.Data, out))
		}
	}
	yes := true
	sc := policy.Scope{Type: "project", ID: strconv.FormatInt(p, 10)}
	d := policy.Definition{Name: "regional", Scope: sc, Constraints: policy.Constraints{Regions: &policy.List{Mode: "allow_list", Values: []string{"EU"}}}, Fallback: policy.Fallback{Endpoint: &yes}}
	var h policy.Head
	send(201, "POST", "", d, 0, "create", &h)
	require.NotEmpty(t, h.Draft)
	var replay policy.Head
	send(201, "POST", "", d, 0, "create", &replay)
	require.Equal(t, h, replay)
	send(409, "POST", "", d, 0, "other-lineage", nil)
	path := "/" + h.ID
	send(412, "PUT", path+"/draft", d, 200, "", nil)
	var page policy.Page[policy.Head]
	send(200, "GET", "?scope_type=project&scope_id="+sc.ID, nil, 0, "", &page)
	require.Len(t, page.Items, 1)
	scenario := policy.Scenario{Project: sc.ID, Model: "m", Protocol: "openai_chat", Action: "text.chat", Input: "100", Output: "20"}
	g := policy.Graph{Snapshot: snapshot.CandidateSnapshot{CatalogVersion: "cat", PriceVersion: "price"}, Facts: map[string]policy.Evidence{}}
	for _, region := range []string{"US", "EU"} {
		g.Snapshot.Candidates = append(g.Snapshot.Candidates, snapshot.Candidate{ID: "private-" + region, ModelID: "m", EndpointFallback: true, Region: region, RegionVerified: true, FundingSource: "wallet", FundingAuthorized: true})
		g.Facts["private-"+region] = policy.Evidence{Provider: "provider", Protocols: []string{"openai_chat"}, Actions: []string{"text.chat"}}
	}
	capture := func(ttl time.Duration) {
		t.Helper()
		now := time.Now().UTC()
		g.Snapshot.ID = uuid.NewString()
		g.Snapshot.AsOf = now
		g.ExpiresAt = now.Add(ttl)
		require.NoError(t, (policy.SnapshotWriter{DB: db}).Capture(ctx, c, scenario, g))
	}
	capture(450 * time.Millisecond)
	simIn := policy.SimulationInput{Scenario: scenario, Version: h.Draft, Seed: "0"}
	countEffects := func() []int64 {
		return []int64{id("SELECT count(*) FROM gateway_executions"), id("SELECT count(*) FROM gateway_execution_attempts"), id("SELECT count(*) FROM api_key_budget_reservations"), id("SELECT count(*) FROM usage_settlement_receipts"), id("SELECT count(*) FROM billing_wallet_events"), id("SELECT count(*) FROM routing_policy_receipts"), id("SELECT count(*) FROM routing_policy_outbox"), cache.DBSize(ctx).Val()}
	}
	before := countEffects()
	var sim policy.Simulation
	send(200, "POST", path+"/simulate", simIn, 0, "", &sim)
	require.Equal(t, before, countEffects())
	require.Nil(t, sim.Baseline)
	require.Nil(t, sim.Diff)
	require.NotContains(t, sim.Trace.Selected.CandidateID, "private")
	time.Sleep(time.Until(sim.Expires) + 5*time.Millisecond)
	pub := policy.Publish{Draft: h.Draft, Base: h.Active, Simulation: sim.ID, Note: "initial publication"}
	send(409, "POST", path+"/publish", pub, h.ETag, "expired", nil)
	capture(4 * time.Minute)
	send(200, "POST", path+"/simulate", simIn, 0, "", &sim)
	var repeated policy.Simulation
	send(200, "POST", path+"/simulate", simIn, 0, "", &repeated)
	require.Equal(t, sim, repeated)
	pub.Simulation = sim.ID
	type result struct {
		key string
		pub policy.Publication
		err error
	}
	var wg sync.WaitGroup
	results := make(chan result, 2)
	for _, key := range []string{"publish-a", "publish-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := store.Publish(ctx, c, h.ID, key, h.ETag, pub)
			results <- result{key, v, e}
		}()
	}
	wg.Wait()
	close(results)
	var active policy.Publication
	var won string
	conflicts := 0
	for r := range results {
		if r.err != nil {
			require.ErrorIs(t, r.err, policy.Conflict)
			conflicts++
			continue
		}
		won, active = r.key, r.pub
	}
	require.Equal(t, 1, conflicts)
	require.NotEmpty(t, active.Active)
	require.Equal(t, int64(1), active.Impact.Projects)
	var replayed policy.Publication
	send(200, "POST", path+"/publish", pub, h.ETag, won, &replayed)
	require.Equal(t, active, replayed)
	pub.Note = "different"
	send(409, "POST", path+"/publish", pub, h.ETag, won, nil)
	_, err = db.ExecContext(ctx, "UPDATE routing_policy_versions SET change_note='tamper' WHERE id=$1", active.Active)
	require.ErrorContains(t, err, "immutable")
	gateway := policy.NewStore(db, []byte("separate reader"))
	auth := scope.Authorized{ActorID: actor, WorkspaceID: w, ProjectID: p, Version: c.AccessVersion}
	exec := func(selected, version string) {
		t.Helper()
		tr, err := gateway.PlanExecution(ctx, auth, scenario, nil, 0)
		require.NoError(t, err)
		require.Equal(t, trace.Execution, tr.Mode)
		require.Equal(t, version, tr.PolicyVersion)
		require.NotNil(t, tr.Selected)
		require.Equal(t, selected, tr.Selected.CandidateID)
	}
	exec("private-EU", active.Active)
	d.Constraints.Regions.Values = []string{"US"}
	send(200, "PUT", path+"/draft", d, 2, "", &h)
	simIn.Version, simIn.Compare = h.Draft, active.Active
	send(200, "POST", path+"/simulate", simIn, 0, "", &sim)
	require.True(t, sim.Diff.SelectionChanged)
	require.Equal(t, sim.Trace.Seed, sim.Baseline.Seed)
	require.Equal(t, sim.Trace.SnapshotID, sim.Baseline.SnapshotID)
	var second policy.Publication
	send(200, "POST", path+"/publish", policy.Publish{Draft: h.Draft, Base: active.Active, Simulation: sim.ID, Note: "US only"}, h.ETag, "second", &second)
	exec("private-US", second.Active)
	var restored policy.Publication
	send(200, "POST", path+"/rollback", policy.Rollback{From: second.Active, Restore: active.Active, Note: "restore EU"}, h.ETag+1, "rollback", &restored)
	require.NotEqual(t, active.Active, restored.Active)
	require.Less(t, active.Epoch, restored.Epoch)
	exec("private-EU", restored.Active)
	events, err := gateway.Changes(ctx, active.Epoch, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, restored.Active, events[1].Version)
	var history policy.Page[policy.Version]
	send(200, "GET", path+"/versions?limit=2", nil, 0, "", &history)
	require.Len(t, history.Items, 2)
	require.NotNil(t, history.Next)
	send(200, "GET", path+"/versions?limit=2&cursor="+*history.Next, nil, 0, "", &history)
	require.Len(t, history.Items, 1)
	require.Equal(t, active.Active, history.Items[0].ID)
	testConnectionRevocation(t, ctx, db, c, g, scenario, h.ID, restored.Active)
	testBackfill(t, ctx, db, w, p, actor)
	key := id("INSERT INTO api_keys(workspace_id,user_id,name,status) VALUES($1,$2,'owned','active') RETURNING id", w, actor)
	_, err = db.ExecContext(ctx, "INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest,status) VALUES($1,$2,encode(sha256(convert_to($3,'UTF8')),'hex'),'current')", w, key, uuid.NewString())
	require.NoError(t, err)
	sessionActor = id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	_, err = db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')", w, sessionActor)
	require.NoError(t, err)
	grant := func(action string) {
		t.Helper()
		_, err := db.ExecContext(ctx, "INSERT INTO project_grants(workspace_id,user_id,project_id,action) VALUES($1,$2,$3,$4)", w, sessionActor, p, action)
		require.NoError(t, err)
	}
	grant("routing.simulate")
	simIn.Version, simIn.Compare, simIn.Key = restored.Active, "", strconv.FormatInt(key, 10)
	send(403, "POST", path+"/simulate", simIn, 0, "", nil)
	send(403, "GET", path, nil, 0, "", nil)
	grant("keys.act_as")
	send(410, "POST", path+"/simulate", simIn, 0, "", nil) // No authorized graph for the alternate actor/key.
	send(403, "POST", path+"/publish", pub, h.ETag, "forbidden", nil)
}

func testBackfill(t *testing.T, ctx context.Context, db *sql.DB, w, p, actor int64) {
	t.Helper()
	var group, route int64
	var accounts [2]int64
	for i := range accounts {
		require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,'policy fixture','openai','apikey') RETURNING id", w).Scan(&accounts[i]))
	}
	routing, err := json.Marshal(map[string][2]int64{"eu-*": accounts})
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(workspace_id,name,platform,model_routing_enabled,model_allowlist,model_routing) VALUES($1,$2,'composite',true,'{"enabled":true,"models":["eu-*"]}',$3) RETURNING id`, w, uuid.NewString(), string(routing)).Scan(&group))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO composite_model_routes(group_id,public_model,match_type,target_platform,upstream_model,endpoint,priority,enabled,ordered_targets)
 VALUES($1,'eu-','prefix','openai','legacy','any',5,true,'[{"position":0,"target_platform":"openai","region":"de","enabled":true,"scheduling_weight":4,"affinity_ttl_seconds":120}]') RETURNING id`, group).Scan(&route))
	_, err = db.ExecContext(ctx, `INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('groups',$1,$3,$4),('composite_model_routes',$2,$3,$4)`, group, route, w, p)
	require.NoError(t, err)
	script, err := os.ReadFile("../../../../contracts/migrations/policies/backfill.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(script))
		require.NoError(t, err)
	}
	g, rs, err := service.LegacyRoutingIntent(ctx, db, w, p, group)
	require.NoError(t, err)
	require.True(t, g.ModelAllowlist.Allows("eu-a"))
	require.False(t, g.ModelAllowlist.Allows("us-a"))
	require.Equal(t, accounts[:], g.ModelRouting["eu-*"])
	require.Len(t, rs, 1)
	require.Equal(t, 120, rs[0].EffectiveTargets()[0].AffinityTTLSeconds)
	pref := service.LegacyRoutingPreferences{Strategy: "smart", Budget: "0"}
	require.Error(t, service.ImportRoutingPreferences(ctx, db, actor, w, p, pref))
	_, err = db.ExecContext(ctx, "INSERT INTO routing_account_preference_owners(user_id,workspace_id,project_id,reviewed_by) VALUES($1,$2,$3,$1)", actor, w, p)
	require.NoError(t, err)
	require.NoError(t, service.ImportRoutingPreferences(ctx, db, actor, w, p, pref))
}

func testConnectionRevocation(t *testing.T, ctx context.Context, db *sql.DB, c policy.Caller, g policy.Graph, s policy.Scenario, policyID, version string) {
	t.Helper()
	var account int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable) VALUES($1,'policy verified','openai','apikey','active',true) RETURNING id", c.Workspace).Scan(&account))
	verification := uuid.NewString()
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	exec("INSERT INTO provider_connections(account_id,workspace_id,provider,auth_type,name,concurrency,state,verified_id) VALUES($1,$2,'openai','api_key','fixture',1,'active',$3)", account, c.Workspace, verification)
	exec("INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at) VALUES($1,$2,$3,1,1,'',true,'{}',now()+interval '1 hour')", verification, c.Workspace, account)
	exec("INSERT INTO connection_credential_versions(account_id,workspace_id,reference,version) VALUES($1,$2,'metadata-only',1)", account, c.Workspace)
	f := g.Facts["private-EU"]
	f.Connection = &policy.ConnectionFence{AccountID: account, VerificationID: verification}
	g.Facts["private-EU"] = f
	g.Snapshot.ID = uuid.NewString()
	g.Snapshot.AsOf = time.Now().UTC()
	g.ExpiresAt = g.Snapshot.AsOf.Add(time.Minute)
	require.NoError(t, (policy.SnapshotWriter{DB: db}).Capture(ctx, c, s, g))
	// Run the actual simulation as a reader unable to select ciphertext or touch
	// execution, billing or outbox tables.
	role := "be10_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	exec("CREATE ROLE " + role)
	t.Cleanup(func() {
		_, e := db.ExecContext(context.Background(), "DROP OWNED BY "+role+"; DROP ROLE "+role)
		if e != nil {
			t.Error(e)
		}
	})
	exec("GRANT SELECT ON workspaces,projects,project_resources,project_key_policies,identity_scope_quarantine,routing_policies,routing_policy_versions,routing_candidate_snapshots,provider_connections,connection_verifications,proxies,connection_pool_members TO " + role)
	exec("GRANT SELECT(id,workspace_id,status,deleted_at,schedulable) ON accounts TO " + role)
	exec("GRANT SELECT(id,workspace_id,deleted_at) ON api_keys TO " + role)
	exec("GRANT SELECT(workspace_id,account_id,version,revoked_at) ON connection_credential_versions TO " + role)
	reader, err := sql.Open("postgres", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	reader.SetMaxOpenConns(1)
	defer reader.Close()
	_, err = reader.ExecContext(ctx, "SET ROLE "+role)
	require.NoError(t, err)
	_, err = reader.ExecContext(ctx, "SELECT ciphertext FROM connection_credential_versions LIMIT 0")
	require.ErrorContains(t, err, "permission denied")
	st := policy.NewStore(reader, []byte("policy-signing-key"))
	in := policy.SimulationInput{Scenario: s, Version: version, Seed: "2"}
	_, err = st.Simulate(ctx, c, policyID, in)
	require.NoError(t, err)
	exec("UPDATE connection_credential_versions SET revoked_at=now() WHERE account_id=$1 AND version=1", account)
	_, err = st.Simulate(ctx, c, policyID, in)
	require.ErrorIs(t, err, policy.Expired)
}
