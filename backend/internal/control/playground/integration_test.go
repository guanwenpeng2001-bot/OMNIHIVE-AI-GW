//go:build integration

package playground

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/settlement"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type snapshotReader catalog.Snapshot

func (r snapshotReader) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	return catalog.Snapshot(r), nil
}

// Only the external model output and catalog acquisition are fixtures. Route admission,
// dispatch reauthorization, the single send permit, reservations and receipts use production services.
func TestPaidPlayground(t *testing.T) {
	ctx := t.Context()
	db, ent, _ := testutil.NewCapabilityPostgres(t)
	cache := redis.NewClient(&redis.Options{Addr: os.Getenv("AFFINITY_REDIS_TEST_ADDR")})
	t.Cleanup(func() { cache.Close() })
	require.NoError(t, cache.Ping(ctx).Err())
	id := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRow(q, args...).Scan(&n))
		return n
	}
	exec := func(q string, args ...any) { t.Helper(); _, err := db.Exec(q, args...); require.NoError(t, err) }
	user := func() int64 {
		return id("INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',100) RETURNING id", uuid.NewString()+"@example.test")
	}
	actor, other := user(), user()
	w := id("INSERT INTO workspaces(name) VALUES('Playground') RETURNING id")
	p := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'admin')", w, actor, other)
	group := id("INSERT INTO groups(name,workspace_id,platform) VALUES($1,$2,'openai') RETURNING id", uuid.NewString(), w)
	exec(`UPDATE groups SET model_pricing='[{"platform":"openai","models":["model-a"],"billing_mode":"token","input_price":0.000001,"output_price":0.000002}]' WHERE id=$1`, group)
	key := id("INSERT INTO api_keys(workspace_id,user_id,group_id,name,budget_period_mode,budget_period_seconds,budget_limit) VALUES($1,$2,$3,'playground','duration',86400,5) RETURNING id", w, actor, group)
	exec("INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest) VALUES($1,$2,$3)", w, key, service.APIKeyCredentialDigest(uuid.NewString()))
	account := id("INSERT INTO accounts(name,workspace_id,platform,type) VALUES('endpoint',$1,'openai','apikey') RETURNING id", w)
	exec("INSERT INTO account_groups(group_id,account_id) VALUES($1,$2)", group, account)
	v := uuid.NewString()
	exec("INSERT INTO provider_connections(account_id,workspace_id,provider,auth_type,name,concurrency,state,verified_id) VALUES($1,$2,'openai','api_key','endpoint',1,'active',$3)", account, w, v)
	exec("INSERT INTO connection_credential_versions(account_id,workspace_id,reference,version) VALUES($1,$2,'credential',1)", account, w)
	exec("INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at) VALUES($1,$2,$3,1,1,'',true,'{}',now()+interval '10 minutes')", v, w, account)
	now := time.Now()
	ep := "ep_" + catalog.Digest([2]int64{p, account})[:32]
	model := "model-a"
	mid := catalog.StableID("openai", model)
	inRate, err := money.NewRate("USD", "0.000001", "input_token", "1")
	require.NoError(t, err)
	outRate, err := money.NewRate("USD", "0.000002", "output_token", "1")
	require.NoError(t, err)
	sale := catalog.Price{State: "priced", Kind: "sale", Source: "group", Input: &inRate, Output: &outRate}
	c := snapshotReader{Version: "cat", Details: []catalog.Detail{{Model: catalog.Summary{ID: mid, Provider: "openai", Model: model, AccessState: "allowed", RouteState: "eligible"}, GroupPrices: map[int64]catalog.Price{group: sale}, Endpoints: []catalog.Endpoint{{ID: ep, Capabilities: []catalog.Capability{{Protocol: "openai_chat", Action: "text.chat", Tools: "supported", VerifiedAt: &now, Evidence: catalog.Evidence{State: "fresh"}}}}}}}}
	cfg := &config.Config{}
	repo := repository.NewUsageSettlementReceiptRepository(db)
	financial := service.NewUsageSettlementService(repo, nil, nil)
	plans := service.ProvideGatewayService(nil, repository.NewGroupRepository(ent, db), nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(nil, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, financial)
	keys := service.NewAPIKeyService(repository.NewAPIKeyRepository(ent, db), nil, nil, nil, nil, nil, cfg)
	queue := "playground/" + uuid.NewString()
	settle := func(request string) error {
		receiptKey := service.UsageSettlementKey{RequestID: request, APIKeyID: key, UserID: actor}
		receipt, err := repo.Persist(ctx, &service.UsageSettlementReceipt{Key: receiptKey, Command: service.UsageBillingCommand{RequestID: request, APIKeyID: key, UserID: actor, AccountID: account, Model: model, RequestFingerprint: "playground-fixture", BalanceCost: .01}, Usage: service.UsageLog{RequestID: request, APIKeyID: key, UserID: actor, AccountID: account, Model: model, RateMultiplier: 1, ActualCost: .01, TotalCost: .01, PricingState: service.PricingStatePriced, PricingSnapshot: `{"pricing_state":"priced","model":"model-a","provider":"openai","mode":"per_request","source":"configured","unit_price":0.01,"units":1,"rate_multiplier":1,"time_multiplier":1,"reasoning_multiplier":1,"total_cost":0.01,"actual_cost":0.01,"charged_amount":0.01}`, TokenUsageObservation: &service.TokenUsageObservation{InputTokensKnown: true}}, ResolutionState: "resolved", CompletionState: "complete"})
		if err != nil {
			return err
		}
		_, err = repo.Settle(ctx, receiptKey, receipt.Command.RequestFingerprint)
		return err
	}
	var nativeCalls atomic.Int64
	native := func(c *gin.Context) {
		nativeCalls.Add(1)
		if !ctxkey.PrivateGatewayContent(c.Request.Context()) {
			t.Error("native Playground request is missing its private content marker")
			c.AbortWithStatus(500)
			return
		}
		b, _ := io.ReadAll(c.Request.Body)
		plan := service.GatewayRequestPlanFromContext(c.Request.Context())
		if !plan.BudgetAdmitted() {
			c.AbortWithStatus(503)
			return
		}
		ex, err := plan.AdmittedExecution()
		if err != nil {
			c.AbortWithStatus(503)
			return
		}
		end, err := service.BeginGenerationAttempt(c.Request.Context(), account)
		if err != nil {
			t.Log("send permit:", err)
			c.AbortWithStatus(503)
			return
		}
		if strings.Contains(string(b), "wait-provider") {
			cache.RPush(c.Request.Context(), queue, ex.RequestID)
			<-c.Request.Context().Done()
			end(0, c.Request.Context().Err())
			return
		}
		end(200, nil)
		if strings.Contains(string(b), "private-prompt") {
			if !plan.Record() {
				t.Error("receipt producer was already claimed")
				c.AbortWithStatus(500)
				return
			}
			if err := settle(ex.RequestID); err != nil {
				t.Error(err)
				c.AbortWithStatus(500)
				return
			}
		}
		if !gjson.GetBytes(b, "stream").Bool() {
			c.JSON(200, gin.H{"choices": []gin.H{{"message": gin.H{"role": "assistant", "content": "sync"}}}, "private_debug": "must be removed"})
			return
		}
		c.Header("Content-Type", "text/event-stream")
		fmt.Fprint(c.Writer, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}],\"debug\":\"provider-private\"}\n\ndata: [DONE]\n\n")
	}
	descriptor := domain.PlatformAction(domain.ActionIdentity{Platform: "openai"}, "chat")
	routes := NativeRoutes(service.NewGatewayActionDispatch(service.GatewayActionHandlers{descriptor.Adapter: native}), nil, func(c *gin.Context) { c.Next() })
	direct := httptest.NewRecorder()
	routes.ServeHTTP(direct, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}")))
	require.Equal(t, 403, direct.Code)
	ids := postgres.New(db)
	h := New(&Gateway{DB: db, Access: ids, Scopes: ids, Keys: keys, Catalog: c, Policies: policy.NewStore(db, []byte("fixture")), Plans: plans, Routes: routes}, false)
	router := gin.New()
	Register(router.Group("/api/v2"), h, func(c *gin.Context) {
		a, _ := strconv.ParseInt(c.GetHeader("X-Test-Actor"), 10, 64)
		if a > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: a})
		}
	})
	base := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d", w, p)
	runs := base + "/playground/runs"
	send := func(ctx context.Context, who int64, method, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("X-Test-Actor", strconv.FormatInt(who, 10))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}
	payload := func(text string) string {
		return fmt.Sprintf(`{"api_key_id":"%d","model_id":"%s","protocol":"openai_chat","action":"text.chat","funding_acknowledged":true,"input":{"messages":[{"role":"user","content":"%s"}]}}`, key, mid, text)
	}
	require.Equal(t, 401, send(ctx, 0, "POST", runs, "one", payload("private-prompt")).Code)
	require.Equal(t, 404, send(ctx, other, "POST", runs, "one", payload("private-prompt")).Code)
	stalePrice := strings.Replace(payload("not-sent"), `"action":"text.chat"`, `"expected_price_version":"obsolete","action":"text.chat"`, 1)
	staleResult := send(ctx, actor, "POST", runs, "stale-price", stalePrice)
	require.Equal(t, 409, staleResult.Code, staleResult.Body.String())
	require.Contains(t, staleResult.Body.String(), "PRICE_QUOTE_CHANGED")
	// Reference-only catalog entries must not create a run, reservation or charge.
	referenceOnly := c
	referenceOnly.Details = append([]catalog.Detail{}, c.Details...)
	referenceOnly.Details[0].GroupPrices = nil
	ref := sale
	ref.Kind, ref.Source = "reference", "pi-ai"
	referenceOnly.Details[0].Model.ReferencePrice = &ref
	h.Gateway.Catalog = referenceOnly
	partial := sale
	partial.Output = nil
	conflict := sale
	conflict.State, conflict.BillingState = "unpriced", "price_conflict"
	conditional := sale
	conditional.ContextBasis = "unverified_provider_basis"
	for _, tc := range []struct {
		name  string
		price *catalog.Price
	}{{"reference-only", nil}, {"partial", &partial}, {"conflict", &conflict}, {"conditional", &conditional}} {
		if tc.price == nil {
			// The private fixture identity has no platform fallback tariff. Clear
			// the real group configuration as well as its catalog projection.
			exec(`UPDATE groups SET model_pricing='[]' WHERE id=$1`, group)
		} else {
			exec(`UPDATE groups SET model_pricing='[{"platform":"openai","models":["model-a"],"billing_mode":"token","input_price":0.000001,"output_price":0.000002}]' WHERE id=$1`, group)
		}
		blocked := referenceOnly
		blocked.Details = append([]catalog.Detail{}, referenceOnly.Details...)
		if tc.price != nil {
			blocked.Details[0].GroupPrices = map[int64]catalog.Price{group: *tc.price}
		}
		h.Gateway.Catalog = blocked
		for _, expectedVersion := range []bool{false, true} {
			request := payload("not-sent")
			if expectedVersion {
				request = strings.Replace(request, `"action":"text.chat"`, `"expected_price_version":"any-version","action":"text.chat"`, 1)
			}
			unpriced := send(ctx, actor, "POST", runs, fmt.Sprintf("%s-%t", tc.name, expectedVersion), request)
			require.Equal(t, 422, unpriced.Code, tc.name+unpriced.Body.String())
			require.Zero(t, nativeCalls.Load(), tc.name)
			require.Zero(t, id("SELECT count(*) FROM playground_runs WHERE workspace_id=$1", w))
			require.Zero(t, id("SELECT count(*) FROM api_key_budget_reservations WHERE api_key_id=$1", key))
			require.Zero(t, id("SELECT count(*) FROM usage_settlement_receipts WHERE api_key_id=$1", key))
			require.Zero(t, id("SELECT count(*) FROM usage_logs WHERE api_key_id=$1", key))
			require.Zero(t, id("SELECT count(*) FROM billing_wallet_events e JOIN billing_funding_accounts f ON f.id=e.funding_account_id WHERE f.user_id=$1 AND e.kind='balance_change'", actor))
			require.EqualValues(t, 100, id("SELECT balance::bigint FROM users WHERE id=$1", actor))
		}
	}
	exec(`UPDATE groups SET model_pricing='[{"platform":"openai","models":["model-a"],"billing_mode":"token","input_price":0.000001,"output_price":0.000002}]' WHERE id=$1`, group)
	h.Gateway.Catalog = c
	result := send(ctx, actor, "POST", runs, "one", payload("private-prompt"))
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), "event: finished")
	require.Contains(t, result.Body.String(), "tool_arguments")
	require.NotContains(t, result.Body.String(), "provider-private")
	repeat := send(ctx, actor, "POST", runs, "one", payload("private-prompt"))
	require.Contains(t, repeat.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, repeat.Body.String(), "private-prompt")
	var summary struct{ Data pg.Summary }
	require.NoError(t, json.Unmarshal(repeat.Body.Bytes(), &summary))
	first := summary.Data
	require.Equal(t, "succeeded", first.State)
	require.Equal(t, actor, first.Payer)
	sync := strings.Replace(payload("sync"), `"action":"text.chat"`, `"options":{"stream":false},"action":"text.chat"`, 1)
	syncResult := send(ctx, actor, "POST", runs, "sync", sync)
	require.Equal(t, "sync", gjson.GetBytes(syncResult.Body.Bytes(), "data.output.choices.0.message.content").String())
	require.NotContains(t, syncResult.Body.String(), "must be removed")
	delegated := strings.Replace(payload("delegated"), `"action":"text.chat"`, fmt.Sprintf(`"payer_user_id":"%d","action":"text.chat"`, actor), 1)
	result = send(ctx, other, "POST", runs, "delegated", delegated)
	require.Contains(t, result.Body.String(), "event: finished")
	require.EqualValues(t, 1, id("SELECT count(*) FROM gateway_executions WHERE request_id=$1 AND actor_id=$2 AND owner_id=$3 AND funding_user_id=$3", result.Header().Get("X-Request-ID"), other, actor))
	require.EqualValues(t, 1, id("SELECT count(*) FROM api_key_budget_reservations WHERE request_id=$1 AND api_key_id=$2 AND user_id=$3", first.Request, key, actor))
	require.False(t, first.Retained)
	require.Equal(t, 409, send(ctx, actor, "POST", runs, "one", payload("changed")).Code)
	require.Equal(t, 404, send(ctx, other, "GET", first.StatusURL, "", "").Code)
	require.EqualValues(t, 1, id("SELECT count(*) FROM gateway_execution_attempts WHERE request_id=$1 AND dispatched_at IS NOT NULL", first.Request))
	require.EqualValues(t, 1, id("SELECT count(*) FROM gateway_routing_decisions WHERE request_id=$1", first.Request))
	// The accounting reader observes the original receipt, including a measured zero.
	detail := send(ctx, actor, "GET", first.StatusURL, "", "")
	require.Equal(t, "settled", gjson.GetBytes(detail.Body.Bytes(), "data.settlement_state").String())
	var aid string
	require.NoError(t, db.QueryRow("SELECT attempt_id FROM gateway_execution_attempts WHERE request_id=$1", first.Request).Scan(&aid))
	evidence, err := (settlement.FinancialReader{DB: db}).Read(ctx, settlement.Scope{WorkspaceID: w, ProjectID: p}, first.Request, aid)
	require.NoError(t, err)
	require.NotNil(t, evidence.PlatformCharge)
	require.NotEmpty(t, evidence.LedgerEventIDs)
	for _, byClient := range []bool{false, true} {
		name := fmt.Sprintf("side-%t", byClient)
		wait, cancel := context.WithTimeout(ctx, 15*time.Second)
		done := make(chan struct{})
		go func() { defer close(done); send(wait, actor, "POST", runs, name, payload("wait-provider")) }()
		_, err := cache.BLPop(ctx, 5*time.Second, queue).Result()
		require.NoError(t, err)
		duplicate := send(ctx, actor, "POST", runs, name, payload("wait-provider"))
		require.Contains(t, duplicate.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "running", gjson.GetBytes(duplicate.Body.Bytes(), "data.state").String())
		lookup := send(ctx, actor, "GET", runs+"/by-idempotency-key", name, "")
		status := gjson.GetBytes(lookup.Body.Bytes(), "data.status_url").String()
		if byClient {
			cancel()
		} else {
			otherRun := send(ctx, actor, "POST", runs, "other-side", payload("ok"))
			require.Contains(t, otherRun.Body.String(), "event: finished")
			require.Equal(t, 202, send(ctx, actor, "POST", status+"/cancel", "", "{}").Code)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("provider failed to stop")
		}
		cancel()
		unknown := send(ctx, actor, "GET", status, "", "")
		require.Equal(t, "unknown", gjson.GetBytes(unknown.Body.Bytes(), "data.state").String())
	}
	replay := fmt.Sprintf(`{"api_key_id":"%d","input":{"messages":[{"role":"user","content":"replay"}]},"funding_acknowledged":true}`, key)
	require.Equal(t, 409, send(ctx, actor, "POST", first.DetailURL+"/replay", "one", replay).Code)
	paid := send(ctx, actor, "POST", first.DetailURL+"/replay", "replay", replay)
	require.Contains(t, paid.Body.String(), "event: finished")
	require.NotEqual(t, first.Request, paid.Header().Get("X-Request-ID"))
}
