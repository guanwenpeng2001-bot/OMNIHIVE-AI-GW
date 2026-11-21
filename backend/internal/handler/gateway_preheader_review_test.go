//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type timeoutReviewJournal struct {
	entrypointJournal
	decisions []attempt.AttemptDecision
	observed  journal.Attempt
}

func (s *timeoutReviewJournal) Observe(_ context.Context, a journal.Attempt) error {
	s.observed = a
	return nil
}

func (s *timeoutReviewJournal) Decision(ctx context.Context, id string, d attempt.AttemptDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id != s.id {
		return journal.ErrDispatch
	}
	s.decisions = append(s.decisions, d)
	return nil
}

func TestMessagesPreHeaderTimeoutReview(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer upstream.Close()
	handler, group, receipts := nativeMessagesReviewHandler(t, upstream.URL)
	client, rec := requestPlanHTTPContext("/v1/messages", `{"model":"fixture-model","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`, group)
	client.Request.Header.Set("Idempotency-Key", "timeout-review")
	store := &timeoutReviewJournal{}
	owner := scope.Authorized{WorkspaceID: 1, ProjectID: 2, ActorID: 1, KeyOwnerID: 1, KeyID: 2, FundingUserID: 1, Version: 1}
	root := journal.WithReauthorization(journal.WithStore(scope.With(client.Request.Context(), owner), store), func(context.Context, scope.Authorized, string) error { return nil })
	ctx, cancel := service.WithAttemptBudget(root, 2, 250*time.Millisecond)
	defer cancel()
	client.Request = client.Request.WithContext(ctx)
	handler.Messages(client)
	require.Equal(t, 502, rec.Code, rec.Body.String())
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, attempt.Unknown, store.observed.Acceptance)
	require.Equal(t, "unknown", store.observed.Outcome)
	require.EqualValues(t, 1, store.ordinal.Load())
	require.Len(t, store.decisions, 1)
	decision := store.decisions[0]
	require.Equal(t, attempt.Unknown, decision.Acceptance)
	require.Equal(t, attempt.Stop, decision.Action)
	require.Equal(t, "pre_header_timeout", decision.Reason)
	require.False(t, decision.AbandonedUncertain)
	requireReviewOnly(t, receipts)
	for _, r := range receipts.receipts {
		require.Equal(t, store.id, r.Key.RequestID)
		require.Equal(t, store.account.Load(), r.Command.AccountID)
	}
}
