//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestJournalNativeRoutes(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	for _, route := range []string{"/responses", "/v1/responses", "/backend-api/codex/responses"} {
		t.Run(route, func(t *testing.T) {
			h, _, upstream, _, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
			defer cleanup()
			router := gin.New()
			router.Use(authenticatedJournal(t, db, 902, 903))
			group := int64(901)
			key := &service.APIKey{ID: 902, UserID: 903, GroupID: &group, User: &service.User{ID: 903, Status: service.StatusActive}, Group: &service.Group{ID: group, Platform: service.PlatformGrok, Status: service.StatusActive}}
			router.POST(route, func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 903, Concurrency: 1})
				h.Responses(c)
			})
			send := func(body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", route, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", "submit")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				return w
			}
			w := send(`{"model":"grok","input":"hello","stream":false}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "resp_healthy")
			req := w.Header().Get("X-Execution-ID")
			require.NotEmpty(t, req)
			replay := send(`{"stream":false,"model":"grok","input":"hello"}`)
			require.Equal(t, http.StatusNoContent, replay.Code, replay.Body.String())
			require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
			require.Empty(t, replay.Body.Bytes())
			require.Equal(t, req, replay.Header().Get("X-Execution-ID"))
			conflict := send(`{"model":"grok","input":"other","stream":false}`)
			require.Equal(t, 409, conflict.Code)
			require.Contains(t, conflict.Body.String(), "invalid_request_error")
			require.Len(t, upstream.accountHits(), 1)
			var outcome string
			require.NoError(t, db.QueryRow("SELECT outcome FROM gateway_execution_attempts WHERE request_id=$1", req).Scan(&outcome))
			require.Equal(t, "transport_response", outcome)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				route: route, middleware: authenticatedJournal(t, db, 1801, 1701),
				ingressMode:   service.OpenAIWSIngressModePassthrough,
				firstPayload:  `{"type":"response.create","model":"gpt-5.1","input":"one"}`,
				secondPayload: `{"type":"response.create","model":"gpt-5.1","input":"two"}`,
			})
			require.Len(t, got.plans, 2)
			for i, p := range got.plans {
				e, err := p.AdmittedExecution()
				require.NoError(t, err)
				require.NotEmpty(t, e.RequestID)
				require.Equal(t, e.RequestID, got.logs[i].RequestID)
				require.NoError(t, db.QueryRow("SELECT outcome FROM gateway_execution_attempts WHERE request_id=$1 AND ordinal=1", e.RequestID).Scan(&outcome))
				require.Equal(t, "transport_response", outcome)
			}
			require.NotEqual(t, got.logs[0].RequestID, got.logs[1].RequestID)
		})
	}
}

func TestJournalChatCompletionsRoutes(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	for _, route := range []string{"/v1/chat/completions", "/chat/completions"} {
		t.Run(route, func(t *testing.T) {
			h, _, upstream, _, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
			defer cleanup()
			router := gin.New()
			router.Use(authenticatedJournal(t, db, 902, 903))
			group := int64(901)
			key := &service.APIKey{ID: 902, UserID: 903, GroupID: &group, User: &service.User{ID: 903, Status: service.StatusActive}, Group: &service.Group{ID: group, Platform: service.PlatformGrok, Status: service.StatusActive}}
			router.POST(route, func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 903, Concurrency: 1})
				h.ChatCompletions(c)
			})
			send := func(body, idempotencyKey string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", idempotencyKey)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				return w
			}
			body := `{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`
			w := send(body, "submit")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			req := w.Header().Get("X-Execution-ID")
			require.NotEmpty(t, req)
			replay := send(`{"stream":false,"messages":[{"content":"hello","role":"user"}],"model":"grok"}`, "submit")
			require.Equal(t, http.StatusNoContent, replay.Code, replay.Body.String())
			require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
			require.Equal(t, req, replay.Header().Get("X-Execution-ID"))
			require.Empty(t, replay.Body.Bytes())
			conflict := send(strings.Replace(body, "hello", "other", 1), "submit")
			require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
			require.Contains(t, conflict.Body.String(), "invalid_request_error")
			require.Len(t, upstream.accountHits(), 1)

			// Ordinary clients need no idempotency header to appear in requests/billing.
			unkeyed := send(body, "")
			require.Equal(t, http.StatusOK, unkeyed.Code, unkeyed.Body.String())
			unkeyedID := unkeyed.Header().Get("X-Execution-ID")
			require.NotEmpty(t, unkeyedID)
			require.NotEqual(t, req, unkeyedID)
			for _, requestID := range []string{req, unkeyedID} {
				var storedRoute, outcome string
				var finished bool
				require.NoError(t, db.QueryRow("SELECT route, finished_at IS NOT NULL FROM gateway_executions WHERE request_id=$1", requestID).Scan(&storedRoute, &finished))
				require.Equal(t, route, storedRoute)
				require.True(t, finished)
				require.NoError(t, db.QueryRow("SELECT outcome FROM gateway_execution_attempts WHERE request_id=$1 AND ordinal=1", requestID).Scan(&outcome))
				require.Equal(t, "transport_response", outcome)
			}
		})
	}
}

// Admission and dispatch are independent failure points. The transport checks
// the recorded attempt from inside its round trip, before producing a receipt.
type entrypointJournal struct {
	journal.Store
	original     *journal.Registration
	id           string
	dispatched   atomic.Bool
	account      atomic.Int64
	ordinal      atomic.Int64
	failDispatch bool
}

func (s *entrypointJournal) Admit(ctx context.Context, in journal.Admission) (journal.AdmittedExecution, bool, error) {
	if in.Registration == nil {
		return journal.AdmittedExecution{}, false, journal.ErrRegistration
	}
	owner, _ := scope.From(ctx)
	ex := journal.AdmittedExecution{RequestID: in.RequestID, Scope: owner}
	if s.original != nil {
		if *s.original != *in.Registration {
			return journal.AdmittedExecution{}, false, journal.ErrConflict
		}
		ex.RequestID = s.id
		return ex, true, nil
	}
	s.original, s.id = in.Registration, in.RequestID
	return ex, false, nil
}
func (s *entrypointJournal) Dispatch(_ context.Context, ex journal.AdmittedExecution, n int, account int64) (journal.Attempt, error) {
	if s.failDispatch || ex.RequestID != s.id || int64(n) != s.ordinal.Load()+1 {
		return journal.Attempt{}, journal.ErrDispatch
	}
	s.dispatched.Store(true)
	s.account.Store(account)
	s.ordinal.Store(int64(n))
	return journal.Attempt{ID: journal.NewID(), RequestID: s.id, Number: n, AccountID: account}, nil
}
func (*entrypointJournal) Decision(context.Context, string, attempt.AttemptDecision) error {
	return nil
}
func (*entrypointJournal) Observe(context.Context, journal.Attempt) error              { return nil }
func (*entrypointJournal) Finish(context.Context, string, lifecycle.State, bool) error { return nil }

var testedJournalRoutes = []string{"/v1/images/generations", "/images/edits", "/v1/images/variations", "/v1/audio/speech", "/audio/transcriptions", "/v1/audio/translations", "/embeddings", "/v1/realtime", "/v1/chat/completions", "/backend-api/codex/responses", "/v1/responses/compact", "/messages", "/v1beta/models/*modelAction", "/rerank", "/v1/completions"}

func TestJournalEntrypointsBeforeTransport(t *testing.T) {
	h, _, _, _, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
	defer cleanup()
	for _, path := range testedJournalRoutes {
		t.Run(path, func(t *testing.T) {
			method := http.MethodPost
			if path == "/v1/realtime" {
				method = http.MethodGet
			}
			store := &entrypointJournal{}
			var sends atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !store.dispatched.Load() {
					t.Error("transport entered before dispatch")
				}
				sends.Add(1)
				w.WriteHeader(200)
			}))
			defer upstream.Close()
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				ctx := scope.With(c.Request.Context(), scope.Authorized{WorkspaceID: 1, ProjectID: 2, ActorID: 3, KeyOwnerID: 3, KeyID: 4, FundingUserID: 3, Version: 1})
				ctx = journal.WithReauthorization(journal.WithStore(ctx, store), func(context.Context, scope.Authorized, string) error { return nil })
				c.Request = c.Request.WithContext(ctx)
			}, InstallExecutionJournal(nil, nil))
			engine.Handle(method, path, func(c *gin.Context) {
				if !prepareGatewayDispatch(c) {
					return
				}
				defer beginGatewayRequestPlan(c, h.gatewayService, &service.APIKey{ID: 4, UserID: 3}, "m", "media", 0)()
				if c.IsAborted() {
					return
				}
				finish, err := service.BeginGenerationAttempt(c.Request.Context(), 5)
				if err != nil {
					c.String(503, "%s", err)
					return
				}
				resp, e := http.Get(upstream.URL)
				require.NoError(t, e)
				resp.Body.Close()
				finish(resp.StatusCode, nil)
				c.Status(200)
			})
			send := func(input string) *httptest.ResponseRecorder {
				target := strings.Replace(path, "*modelAction", "m:generateContent", 1)
				if method == http.MethodGet {
					target += "?model=" + input
				}
				req := httptest.NewRequest(method, target, strings.NewReader(`{"model":"`+input+`"}`))
				req.Header.Set("Idempotency-Key", "one")
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, req)
				return w
			}
			first := send("m")
			require.Equal(t, 200, first.Code, first.Body.String())
			require.NotEmpty(t, first.Header().Get("X-Execution-ID"))
			require.Equal(t, 204, send("m").Code)
			require.Equal(t, 409, send("different").Code)
			require.EqualValues(t, 1, sends.Load())
			store.original = nil
			store.failDispatch = true
			require.Equal(t, 503, send("different").Code)
			require.EqualValues(t, 1, sends.Load())
		})
	}
}
func TestJournalUnavailableOnlyBlocksDispatch(t *testing.T) {
	r := gin.New()
	r.Use(InstallExecutionJournal(nil, nil))
	for _, path := range []string{"/v1/models", "/v1/images/tasks/:id", "/v1/videos/:id", "/v1/responses/:id"} {
		r.GET(path, func(c *gin.Context) { c.Status(200) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", strings.Replace(path, ":id", "one", 1), nil))
		require.Equal(t, 200, w.Code)
	}
	r.POST("/v1/images/generations", func(*gin.Context) { t.Fatal("entered adapter with no journal") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/v1/images/generations", nil))
	require.Equal(t, 503, w.Code)
}

// The upstream reads from a separate PostgreSQL connection, so an uncommitted
// attempt or one first written when the response returns cannot satisfy this test.
func TestJournalDispatchRowsVisibleAtTransport(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	h, _, _, _, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
	defer cleanup()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ready bool
		err := db.QueryRow(`SELECT EXISTS(SELECT FROM gateway_execution_attempts WHERE request_id=$1 AND account_id=5 AND ordinal=1 AND dispatched_at IS NOT NULL AND outcome='dispatching')`, r.URL.Query().Get("id")).Scan(&ready)
		if err != nil || !ready {
			t.Errorf("upstream reached without committed dispatch: ready=%v err=%v", ready, err)
		}
		w.WriteHeader(200)
	}))
	defer upstream.Close()
	for _, path := range testedJournalRoutes {
		t.Run(path, func(t *testing.T) {
			method := http.MethodPost
			if path == "/v1/realtime" {
				method = http.MethodGet
			}
			r := gin.New()
			r.Use(authenticatedJournal(t, db, 4, 3))
			var sends int
			r.Handle(method, path, func(c *gin.Context) {
				if !prepareGatewayDispatch(c) {
					return
				}
				defer beginGatewayRequestPlan(c, h.gatewayService, &service.APIKey{ID: 4, UserID: 3}, "m", "media", 0)()
				if c.IsAborted() {
					return
				}
				ctx := c.Request.Context()
				finish, err := service.BeginGenerationAttempt(ctx, 5)
				require.NoError(t, err)
				id := service.GatewayRequestPlanFromContext(ctx).Snapshot().ExecutionID
				resp, err := http.Get(upstream.URL + "?id=" + url.QueryEscape(id))
				require.NoError(t, err)
				resp.Body.Close()
				sends++
				finish(resp.StatusCode, nil)
				c.Status(200)
			})
			send := func(input string) int {
				target := strings.Replace(path, "*modelAction", "m:generateContent", 1)
				if method == http.MethodGet {
					target += "?model=" + input
				}
				req := httptest.NewRequest(method, target, strings.NewReader(`{"model":"`+input+`"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Idempotency-Key", "one")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w.Code
			}
			require.Equal(t, 200, send("m"))
			require.Equal(t, 204, send("m"))
			require.Equal(t, 409, send("conflict"))
			require.Equal(t, 1, sends)
		})
	}
}
