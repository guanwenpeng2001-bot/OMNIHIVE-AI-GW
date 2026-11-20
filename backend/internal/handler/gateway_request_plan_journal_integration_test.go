//go:build integration || unit

package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
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

type finalizationJournal struct {
	journal.Store
	mu       sync.Mutex
	finished map[string]bool
}

func (s *finalizationJournal) Admit(ctx context.Context, in journal.Admission) (journal.AdmittedExecution, bool, error) {
	a, _ := scope.From(ctx)
	return journal.AdmittedExecution{RequestID: in.RequestID, Scope: a}, false, nil
}
func (s *finalizationJournal) Finish(_ context.Context, id string, _ lifecycle.State, canceled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished[id] = s.finished[id] || canceled
	return nil
}

func TestJournalFinalizesAfterPanicAndTimeout(t *testing.T) {
	store := &finalizationJournal{finished: map[string]bool{}}
	auth := func(c *gin.Context) {
		a := scope.Authorized{WorkspaceID: 7, ProjectID: 8, ActorID: 1, KeyID: 2, KeyOwnerID: 1, FundingUserID: 1, Version: 1}
		c.Request = c.Request.WithContext(journal.WithStore(scope.With(c.Request.Context(), a), store))
	}
	testJournalFinalizes(t, auth, func(t *testing.T, id string, canceled bool) {
		store.mu.Lock()
		defer store.mu.Unlock()
		got, ok := store.finished[id]
		require.True(t, ok)
		require.Equal(t, canceled, got)
	})
}
func TestPostgresJournalFinalizesAfterPanicAndTimeout(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	testJournalFinalizes(t, authenticatedJournal(t, db, 2, 1), func(t *testing.T, id string, wantCanceled bool) {
		var finished, canceled bool
		require.NoError(t, db.QueryRow("SELECT finished_at IS NOT NULL,canceled FROM gateway_executions WHERE request_id=$1", id).Scan(&finished, &canceled))
		require.True(t, finished)
		require.Equal(t, wantCanceled, canceled)
	})
}
func testJournalFinalizes(t *testing.T, auth gin.HandlerFunc, assertFinished func(*testing.T, string, bool)) {
	t.Helper()
	for _, exit := range []string{"panic", "timeout"} {
		t.Run(exit, func(t *testing.T) {
			router := gin.New()
			router.Use(gin.Recovery(), auth)
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
			assertFinished(t, requestID, exit == "timeout")
			require.EqualValues(t, 1, releases.Load())
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
