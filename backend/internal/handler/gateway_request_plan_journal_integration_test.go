//go:build integration && unit

package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func authenticatedJournal(t *testing.T, db *sql.DB, key, user int64) gin.HandlerFunc {
	t.Helper()
	var a scope.Authorized
	require.NoError(t, db.QueryRow("INSERT INTO workspaces(name) VALUES('journal route') RETURNING id").Scan(&a.WorkspaceID))
	require.NoError(t, db.QueryRow("SELECT identity_default_project($1)", a.WorkspaceID).Scan(&a.ProjectID))
	a.ActorID, a.KeyOwnerID, a.FundingUserID, a.KeyID, a.Version = user, user, user, key, 1
	install := InstallExecutionJournal(db, nil)
	return func(c *gin.Context) {
		ctx := scope.With(c.Request.Context(), a)
		c.Request = c.Request.WithContext(journal.WithReauthorization(ctx, func(_ context.Context, frozen scope.Authorized, _ string) error {
			if a != frozen {
				return journal.ErrScope
			}
			return nil
		}))
		install(c)
	}
}

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

func TestJournalFinalizesAfterPanicAndTimeout(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	for _, exit := range []string{"panic", "timeout"} {
		t.Run(exit, func(t *testing.T) {
			router := gin.New()
			router.Use(gin.Recovery(), authenticatedJournal(t, db, 2, 1))
			requestID := ""
			var releases atomic.Int32
			router.POST("/v1/responses", func(c *gin.Context) {
				ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				finish, ok := beginResponsesExecution(c, (*service.GatewayService)(nil), &service.APIKey{ID: 2, UserID: 1}, "m", []byte("{}"), 0, func(c *gin.Context, code int, _, _ string) { c.Status(code) })
				defer finish()
				require.True(t, ok)
				p := service.GatewayRequestPlanFromContext(c.Request.Context())
				requestID = p.Snapshot().ExecutionID
				p.Acquire(func() { releases.Add(1) })
				if exit == "panic" {
					panic("fixture")
				}
				<-ctx.Done()
				require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
			var finished, canceled bool
			require.NoError(t, db.QueryRow("SELECT finished_at IS NOT NULL,canceled FROM gateway_executions WHERE request_id=$1", requestID).Scan(&finished, &canceled))
			require.True(t, finished)
			require.Equal(t, exit == "timeout", canceled)
			require.EqualValues(t, 1, releases.Load())
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

func TestInstallExecutionJournalPreservesExistingStore(t *testing.T) {
	for _, route := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses", "/v1/images/generations/async", "/images/generations/async", "/v1/chat/completions", "/chat/completions", "/v1/messages", "/messages", "/antigravity/v1/messages"} {
		t.Run(route, func(t *testing.T) {
			store := journal.New(nil)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				ctx := journal.WithStore(c.Request.Context(), store)
				ctx = journal.WithReauthorization(ctx, func(context.Context, scope.Authorized, string) error { return journal.ErrConflict })
				c.Request = c.Request.WithContext(ctx)
			}, InstallExecutionJournal(nil, nil))
			router.POST(route, func(c *gin.Context) {
				require.Same(t, store, journal.From(c.Request.Context()))
				require.ErrorIs(t, journal.Authorize(c.Request.Context(), scope.Authorized{}, "model"), journal.ErrConflict)
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, route, nil))
			require.Equal(t, http.StatusNoContent, w.Code)
		})
	}
}
