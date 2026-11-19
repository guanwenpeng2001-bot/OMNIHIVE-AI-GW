//go:build integration

package byok_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// The provider boundary has a deterministic, authenticated model inventory.
// All credential, policy, hold, journal and authorization reads use PostgreSQL.
type probe struct{}

func (probe) ProbeConnection(_ context.Context, a *service.Account, _ string) service.ConnectionProbe {
	if a.Credentials["api_key"] == "bad" {
		return service.ConnectionProbe{}
	}
	return service.ConnectionProbe{Network: "passed", Authorization: "passed", Discovery: "passed", Models: []string{"model"}}
}
func TestPilot(t *testing.T) {
	ctx := t.Context()
	db, _, _ := testutil.NewCapabilityPostgres(t)
	cache := testresources.RedisClient(t, &redis.Options{Addr: os.Getenv("AFFINITY_REDIS_TEST_ADDR")})
	require.NoError(t, cache.Ping(ctx).Err())
	before := cache.DBSize(ctx).Val()
	exec := func(q string, args ...any) {
		t.Helper()
		_, e := db.ExecContext(ctx, q, args...)
		require.NoError(t, e)
	}
	number := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&n))
		return n
	}
	wid := func() int64 { return number("INSERT INTO workspaces(name) VALUES('BYOK') RETURNING id") }
	w, other := wid(), wid()
	p := number("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	member := func() int64 {
		return number("INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',10) RETURNING id", uuid.NewString()+"@example.test")
	}
	actor, outsider := member(), member()
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member'),($4,$2,'admin')", w, actor, outsider, other)
	key := func(owner int64) int64 {
		id := number("INSERT INTO api_keys(workspace_id,user_id,name,budget_period_mode,budget_period_seconds,budget_limit) VALUES($1,$2,'pilot','duration',3600,20) RETURNING id", w, owner)
		exec("INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest) VALUES($1,$2,encode(sha256($3::bytea),'hex'))", w, id, uuid.NewString())
		return id
	}
	k, k2, k3 := key(actor), key(actor), key(outsider)
	cfg := &config.Config{}
	cfg.Totp = config.TotpConfig{EncryptionKey: strings.Repeat("1f", 32), EncryptionKeyConfigured: true}
	enc, e := repository.NewAESEncryptor(cfg)
	require.NoError(t, e)
	store := byok.New(db, enc, probe{}, postgres.New(db), []byte("pilot-signing"))
	router := gin.New()
	control.Register(router.Group("/api/v2"), control.New(store), func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor}) })
	path := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/byok/credentials", w, p)
	secret := uuid.NewString()
	replacement := uuid.NewString()
	call := func(method, addr string, in any, version int64, key string, status int, out any) {
		t.Helper()
		raw, e := json.Marshal(in)
		require.NoError(t, e)
		req := httptest.NewRequest(method, addr, bytes.NewReader(raw))
		if version > 0 {
			req.Header.Set("If-Match", fmt.Sprintf("\"%d\"", version))
		}
		req.Header.Set("Idempotency-Key", key)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, status, rr.Code, rr.Body.String())
		require.NotContains(t, rr.Body.String(), secret)
		require.NotContains(t, rr.Body.String(), replacement)
		require.Equal(t, "private, no-store", rr.Header().Get("Cache-Control"))
		if out != nil {
			var env struct{ Data json.RawMessage }
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
			require.NoError(t, json.Unmarshal(env.Data, out))
		}
	}
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	pol := byok.Policy{Projects: []string{id(p)}, Keys: []string{id(k)}, Members: []string{id(actor)}, Models: []string{"model"}, Priority: 10}
	in := byok.CreateInput{Provider: "openai", Auth: "api_key", Secret: secret, Policy: pol}
	createKey := uuid.NewString()
	call("POST", path, in, 0, createKey, 403, nil)
	exec("INSERT INTO byok_pilot_providers(workspace_id,provider,enabled) VALUES($1,'openai',true)", w)
	var c, again byok.Credential
	call("POST", path, in, 0, createKey, 201, &c)
	call("POST", path, in, 0, createKey, 201, &again)
	require.Equal(t, c, again)
	in.Secret = replacement
	call("POST", path, in, 0, createKey, 409, nil)
	require.Equal(t, int64(0), number("SELECT count(*) FROM credential_secrets WHERE account_id=$1", c.ID))
	require.Equal(t, int64(1), number("SELECT count(*) FROM byok_secret_versions WHERE credential_id=$1 AND ciphertext IS NOT NULL AND position($2 in ciphertext)=0", c.ID, secret))
	base := path + "/" + id(c.ID)
	otherPath := fmt.Sprintf("/api/v2/workspaces/%d/byok", other)
	var page byok.Page
	call("GET", otherPath, nil, 0, "", 200, &page)
	require.Empty(t, page.Items)
	call("POST", otherPath+"/"+id(c.ID)+"/verify", struct{}{}, c.Version, uuid.NewString(), 404, nil)
	call("GET", path+"?limit=1", nil, 0, "", 200, &page)
	require.Len(t, page.Items, 1)
	require.Nil(t, page.Next)
	op := struct {
		Result byok.Credential
		State  string
	}{}
	call("POST", base+"/verify", struct{}{}, c.Version, uuid.NewString(), 202, &op)
	c = op.Result
	require.Equal(t, "verified", c.State)
	params := url.Values{"member_id": {id(actor)}, "key_id": {id(k)}, "model_id": {"model"}, "input_token_bound": {"1"}, "output_token_bound": {"1"}}
	estimate := base + "/estimate?" + params.Encode()
	var cost byok.Estimate
	call("GET", estimate, nil, 0, "", 200, &cost)
	require.Nil(t, cost.External)
	require.Equal(t, "unknown", cost.ExternalState)
	require.Equal(t, "0.00000000", cost.Fee.String())
	call("GET", strings.Replace(estimate, "key_id="+id(k)+"&", "key_id="+id(k2)+"&", 1), nil, 0, "", 409, nil)
	a := scope.Authorized{WorkspaceID: w, ProjectID: p, ActorID: actor, KeyID: k, KeyOwnerID: actor, FundingUserID: actor, Version: number("SELECT scope_version FROM workspaces WHERE id=$1", w)}
	for _, change := range []func(*scope.Authorized){func(s *scope.Authorized) { s.WorkspaceID = other }, func(s *scope.Authorized) { s.KeyID = k2 }, func(s *scope.Authorized) {
		s.ActorID = outsider
		s.KeyOwnerID = outsider
		s.FundingUserID = outsider
		s.KeyID = k3
	}, func(s *scope.Authorized) { s.ProjectID = p + 10000 }} {
		wrong := a
		change(&wrong)
		items, e := store.Eligible(ctx, wrong, "model")
		require.NoError(t, e)
		require.Empty(t, items)
	}
	items, e := store.Eligible(ctx, a, "unknown")
	require.NoError(t, e)
	require.Empty(t, items)
	call("POST", base+"/rotate", map[string]any{"replacement": map[string]string{"secret": "bad"}}, c.Version, uuid.NewString(), 422, nil)
	beforeRotate := c
	call("POST", base+"/rotate", map[string]any{"replacement": map[string]string{"secret": replacement}}, c.Version, uuid.NewString(), 202, &op)
	c = op.Result
	require.NotEqual(t, beforeRotate.Fingerprint, c.Fingerprint)
	require.Equal(t, int64(2), c.SecretVersion)
	require.Equal(t, int64(1), number("SELECT count(*) FROM byok_secret_versions WHERE credential_id=$1 AND ciphertext IS NULL AND retired_at IS NOT NULL", c.ID))
	call("PUT", base+"/policy", pol, beforeRotate.Version, "", 412, nil)

	sc := policy.Scenario{Project: id(p), Key: id(k), Model: "model", Protocol: "openai_chat", Action: "text.chat", Input: "1", Output: "1"}
	allow := true
	layer := &policy.Layer{Version: "request", Definition: policy.Definition{Name: "pilot", Scope: policy.Scope{Type: "request", ID: "request"}, Fallback: policy.Fallback{Endpoint: &allow, Platform: &allow}}}
	planner := policy.NewStore(db, []byte("planner"))
	wallet := repository.NewUsageSettlementReceiptRepository(db).(byok.Reservations)
	g := byok.Gateway{Store: store, Planner: planner, Wallet: wallet}
	j := journal.New(db)
	ctx = journal.WithReauthorization(scope.With(ctx, a), func(context.Context, scope.Authorized, string) error { return nil })
	dispatch := func(c byok.Credential, acct int64, lane string) byok.Dispatch {
		return byok.Dispatch{CredentialID: c.ID, Version: c.Version, AccountID: acct, Source: lane, Ordinal: 1, Scenario: sc, RequestPolicy: layer, Consent: true}
	}
	shared := number("INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable) VALUES($1,'platform','openai','apikey','active',true) RETURNING id", w)
	verification := uuid.NewString()
	exec("INSERT INTO provider_connections(account_id,workspace_id,provider,auth_type,name,concurrency,state,verified_id) VALUES($1,$2,'openai','api_key','platform',3,'active',$3)", shared, w, verification)
	exec("INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at) VALUES($1,$2,$3,1,1,'',true,'{}',now()+interval '1 hour')", verification, w, shared)
	exec("INSERT INTO connection_credential_versions(account_id,workspace_id,reference,version) VALUES($1,$2,'fixture',1)", shared, w)
	capture := func(c byok.Credential, accounts ...int64) {
		graph := policy.Graph{Snapshot: snapshot.CandidateSnapshot{ID: uuid.NewString(), AsOf: time.Now().UTC(), PriceVersion: "wallet-card", CatalogVersion: "catalog"}, Facts: map[string]policy.Evidence{}, ExpiresAt: time.Now().Add(time.Minute)}
		for _, acct := range accounts {
			lane := "byok"
			fact := policy.Evidence{Provider: "openai", Protocols: []string{sc.Protocol}, Actions: []string{sc.Action}}
			if acct == shared {
				lane = "wallet"
				fact.Connection = &policy.ConnectionFence{AccountID: acct, VerificationID: verification}
				fact.BYOKToPlatform = true
			}
			candidate, bound, err := byok.BindCandidate(c, acct, true, snapshot.Candidate{ModelID: "model", FundingSource: lane, FundingAuthorized: true}, fact)
			require.NoError(t, err)
			graph.Facts[candidate.ID] = bound
			graph.Snapshot.Candidates = append(graph.Snapshot.Candidates, candidate)
		}
		require.NoError(t, (policy.SnapshotWriter{DB: db}).Capture(ctx, policy.Caller{Actor: actor, Workspace: w, AccessVersion: a.Version}, sc, graph))
	}
	admit := func(version string) string {
		req := uuid.NewString()
		_, _, e := j.Admit(ctx, journal.Admission{RequestID: req, Route: "/v1/messages", Kind: "request", Model: "model", AttemptBudget: 2, Deadline: time.Now().Add(time.Minute), Versions: journal.Versions{Price: version, Model: "model", PricingAt: time.Now().UTC()}})
		require.NoError(t, e)
		return req
	}
	setCard := func(account int64, lane, version, rate string) {
		v := pricing.Version{ID: version, Currency: "USD", Source: lane, EffectiveFrom: time.Now().Add(-time.Hour)}
		for _, q := range []struct {
			kind string
			unit money.Unit
		}{{"input", "input_token"}, {"output", "output_token"}} {
			r, e := money.NewRate("USD", rate, q.unit, "1")
			require.NoError(t, e)
			v.Rules = append(v.Rules, pricing.Rule{Kind: q.kind, Rate: r})
		}
		require.NoError(t, (pricing.VersionStore{DB: db}).Save(ctx, byok.PriceScope(w, p, account, lane, "model"), v))
	}
	rejected := func(context.Context, byok.Funding, map[string]any) error {
		t.Fatal("dispatched a denied attempt")
		return nil
	}
	capture(c, c.ID)
	unknown := dispatch(c, c.ID, "byok")
	unknown.RequestID = admit("wallet-card")
	require.NoError(t, g.Send(ctx, unknown, func(ctx context.Context, f byok.Funding, _ map[string]any) error {
		require.Nil(t, f.External)
		require.Equal(t, "unknown", f.ExternalState)
		require.Zero(t, f.ReservationID)
		actual, err := store.SettlementAmounts(ctx, a, f.Attempt.ID, nil)
		require.NoError(t, err)
		require.Nil(t, actual.ExternalEstimate)
		require.Equal(t, "unknown", actual.ExternalState)
		require.Equal(t, "0.00000000", actual.PlatformCharge.String())
		return nil
	}))
	pol.Budget = true
	call("PUT", base+"/policy", pol, c.Version, "", 200, &c)
	capture(c, c.ID)
	unknown = dispatch(c, c.ID, "byok")
	unknown.RequestID = admit("wallet-card")
	require.Error(t, g.Send(ctx, unknown, rejected))
	pol.Budget = false
	call("PUT", base+"/policy", pol, c.Version, "", 200, &c)
	capture(c, shared)
	paid := dispatch(c, shared, "wallet")
	paid.RequestID = admit("wallet-card")
	require.Error(t, g.Send(ctx, paid, rejected)) // grant default denies platform fallback
	pol.Fallback = true
	call("PUT", base+"/policy", pol, c.Version, "", 200, &c)
	capture(c, shared)
	paid = dispatch(c, shared, "wallet")
	paid.RequestID = admit("wallet-card")
	paid.Consent = false
	require.Error(t, g.Send(ctx, paid, rejected))
	paid.Consent = true
	require.Error(t, g.Send(ctx, paid, rejected)) // no wallet price
	setCard(shared, "wallet", "wallet-card", "1")
	setCard(c.ID, "byok", "external-card", "2")
	exec("UPDATE byok_pilot_providers SET service_fee_usd='0.25' WHERE workspace_id=$1", w)
	paid.RequestID = admit("wrong-card")
	require.Error(t, g.Send(ctx, paid, rejected))
	paid.RequestID = admit("wallet-card")
	exec("UPDATE users SET balance=0 WHERE id=$1", actor)
	require.Error(t, g.Send(ctx, paid, rejected))
	require.Equal(t, int64(0), number("SELECT count(*) FROM byok_attempt_funding WHERE workspace_id=$1 AND source='wallet'", w))
	exec("UPDATE users SET balance=10 WHERE id=$1", actor)
	exec("UPDATE api_keys SET budget_period_mode='off' WHERE id=$1", k)
	require.Error(t, g.Send(ctx, paid, rejected))
	exec("UPDATE api_keys SET budget_period_mode='duration' WHERE id=$1", k)
	call("GET", estimate, nil, 0, "", 200, &cost)
	require.Equal(t, "4.00000000", cost.External.String())
	require.Equal(t, "0.25000000", cost.Fee.String())
	require.Nil(t, cost.Platform)
	// Two workers racing the same paid permit can reserve independently during
	// preflight. Exactly one reaches the transport and keeps a live wallet hold.
	paid.RequestID = admit("wallet-card")
	var transports atomic.Int64
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func(d byok.Dispatch) {
			<-start
			results <- g.Send(ctx, d, func(context.Context, byok.Funding, map[string]any) error { transports.Add(1); return nil })
		}(paid)
	}
	close(start)
	first, second := <-results, <-results
	require.NotEqual(t, first == nil, second == nil)
	require.Equal(t, int64(1), transports.Load())
	require.Equal(t, int64(1), number("SELECT count(*) FROM api_key_budget_reservations WHERE api_key_id=$1 AND status='reserved' AND request_id LIKE $2", k, "byok:"+paid.RequestID+":%"))
	capture(c, c.ID, shared)
	// A later replan and request flag cannot add a lane excluded at the
	// initial funding admission, even when the credential itself permits it.
	for _, initialConsent := range []bool{false, true} {
		if initialConsent {
			capture(c, c.ID)
		} else {
			capture(c, c.ID, shared)
		}
		initial := dispatch(c, c.ID, "byok")
		initial.Consent = initialConsent
		initial.RequestID = admit("wallet-card")
		require.NoError(t, g.Send(ctx, initial, func(ctx context.Context, f byok.Funding, _ map[string]any) error {
			return j.Decision(ctx, initial.RequestID, attempt.AttemptDecision{AttemptNumber: 1, Acceptance: attempt.NotSent, Action: attempt.Switch})
		}))
		capture(c, c.ID, shared)
		switched := dispatch(c, shared, "wallet")
		switched.Ordinal = 2
		switched.RequestID = initial.RequestID
		require.Error(t, g.Send(ctx, switched, rejected))
	}
	req := admit("wallet-card")
	own := dispatch(c, c.ID, "byok")
	own.RequestID = req
	require.NoError(t, g.Send(ctx, own, func(ctx context.Context, f byok.Funding, token map[string]any) error {
		require.Equal(t, replacement, token["api_key"])
		require.Greater(t, f.ReservationID, int64(0))
		require.Equal(t, "4.00000000", f.External.String())
		require.Equal(t, int64(1), number("SELECT count(*) FROM byok_request_funding WHERE attempt_id=$1 AND source='byok' AND platform_service_fee->>'amount'='0.25000000' AND byok_external_estimate->>'amount'='4.00000000'", f.Attempt.ID))
		tokens, _ := money.NewQuantity("token", "1")
		input, _ := money.NewQuantity("input_token", "1")
		observed := pricing.Usage{Context: tokens, Dimensions: []pricing.Dimension{{Kind: "input", Quantity: input}}}
		actual, e := store.SettlementAmounts(ctx, a, f.Attempt.ID, &observed)
		require.NoError(t, e)
		require.Equal(t, "2.00000000", actual.ExternalEstimate.String())
		require.Equal(t, "0.25000000", actual.PlatformCharge.String())
		require.Equal(t, actual.PlatformServiceFee, actual.PlatformCharge)
		require.Equal(t, f.ReservationID, actual.ReservationID)
		wrong := a
		wrong.KeyID = k2
		_, e = store.SettlementAmounts(ctx, wrong, f.Attempt.ID, &observed)
		require.Error(t, e)
		return j.Decision(ctx, req, attempt.AttemptDecision{AttemptNumber: 1, Acceptance: attempt.NotSent, Action: attempt.Switch})
	}))
	require.Error(t, g.Send(ctx, own, rejected))
	paid = dispatch(c, shared, "wallet")
	paid.Ordinal = 2
	paid.RequestID = req
	require.NoError(t, g.Send(ctx, paid, func(_ context.Context, f byok.Funding, token map[string]any) error {
		require.Nil(t, token)
		require.Nil(t, f.External)
		require.Nil(t, f.Fee)
		require.Equal(t, "2.00000000", f.Platform.String())
		require.Equal(t, int64(1), number(`SELECT count(*) FROM byok_attempt_funding f JOIN api_key_budget_reservations h ON h.id=f.reservation_id
        WHERE f.attempt_id=$1 AND h.hold_amount=2 AND h.status='reserved' AND h.balance_held AND f.frozen_price->'card'->'version'->>'id'='wallet-card'`, f.Attempt.ID))
		return nil
	}))
	require.Error(t, g.Send(ctx, paid, rejected))
	capture(c, c.ID)
	own.RequestID = admit("wallet-card")
	inFlight := own
	inFlight.RequestID = admit("wallet-card")
	started := make(chan struct{})
	ended := make(chan error, 1)
	go func() {
		ended <- g.Send(ctx, inFlight, func(transport context.Context, _ byok.Funding, _ map[string]any) error {
			close(started)
			select {
			case <-transport.Done():
				return transport.Err()
			case <-time.After(5 * time.Second):
				return fmt.Errorf("transport still active")
			}
		})
	}()
	select {
	case <-started:
	case err := <-ended:
		t.Fatalf("attempt refused: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("transport did not start")
	}
	call("POST", base+"/revoke", map[string]string{"mode": "force"}, c.Version, uuid.NewString(), 202, &op)
	require.Error(t, g.Send(ctx, own, rejected))
	require.ErrorIs(t, <-ended, context.Canceled)
	require.Equal(t, "revoked", op.Result.State)
	require.Equal(t, int64(0), number("SELECT count(*) FROM byok_secret_versions WHERE credential_id=$1 AND ciphertext IS NOT NULL", c.ID))
	require.Equal(t, before, cache.DBSize(ctx).Val())
}
