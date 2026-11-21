package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestHTTPRejectionRemainsSentAfterRetryDecision(t *testing.T) {
	ctx, cancel := WithAttemptBudget(t.Context(), 2, time.Minute)
	defer cancel()
	finish, err := BeginGenerationAttempt(ctx, 9)
	require.NoError(t, err)
	finish(http.StatusInternalServerError, nil)
	controller := attempt.New(1)
	decision := DecideAttempt(ctx, &controller, attempt.Failure{AccountID: 9})
	require.Equal(t, attempt.Accepted, decision.Acceptance)
	record := AttemptBudgetFromContext(ctx).Snapshot()[0]
	require.Equal(t, http.StatusInternalServerError, record.Status)
	require.Equal(t, "rejected", record.Outcome)
}

type attemptBudgetHTTPUpstream struct {
	sends  int
	unsent bool
}

func (u *attemptBudgetHTTPUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.sends++
	if u.unsent {
		return nil, &net.OpError{Op: "dial", Err: io.ErrUnexpectedEOF}
	}
	return nil, io.ErrUnexpectedEOF
}

func (u *attemptBudgetHTTPUpstream) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, proxy, id, concurrency)
}

func TestAttemptBudgetAdapterHandoffSharesTransportRetries(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 3, time.Minute)
	defer cancel()
	upstream := &attemptBudgetHTTPUpstream{unsent: true}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid/v1/responses", nil)
	require.NoError(t, err)
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 1}
	for i := 0; i < 2; i++ {
		_, err = gateway.doOpenAIUpstream(req, "", account)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
	// Switching protocol, account, and detaching does not create a new budget.
	child, childCancel := WithAttemptBudget(context.WithoutCancel(ctx), 100, time.Hour)
	defer childCancel()
	ws := &attemptBudgetWS{}
	conn := newOpenAIWSConn("handoff", 2, ws, nil)
	require.NoError(t, conn.acquire(context.Background()))
	lease := &openAIWSConnLease{conn: conn, accountID: 2}
	defer lease.Release()
	require.NoError(t, lease.WriteJSONContext(child, map[string]any{"type": "response.create"}))
	_, err = generationDoWithTLS(upstream, req.WithContext(child), "", 3, 1, nil)
	require.ErrorIs(t, err, ErrAttemptBudgetExhausted)
	require.Equal(t, 2, upstream.sends)
	require.Equal(t, 1, ws.sends)
	require.Len(t, AttemptBudgetFromContext(ctx).Snapshot(), 3)
}

func TestAttemptBudgetActualHTTPAndNestedWS(t *testing.T) {
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch sends.Add(1) {
		case 1:
			w.WriteHeader(401)
		case 2:
			w.WriteHeader(400)
		default:
			w.WriteHeader(503)
		}
	}))
	defer upstream.Close()
	ctx, cancel := WithAttemptBudget(context.Background(), 3, time.Minute)
	defer cancel()
	for i := 0; i < 2; i++ {
		req, err := http.NewRequestWithContext(ctx, "POST", upstream.URL, strings.NewReader(`{}`))
		require.NoError(t, err)
		resp, err := generationHTTP(req, 7, upstream.Client().Do)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	// A bridge and a new account inherit the same object, even after detaching.
	child, childCancel := WithAttemptBudget(context.WithoutCancel(ctx), 100, time.Hour)
	defer childCancel()
	err := generationWS(child, 8, map[string]any{"type": "response.create"}, func(ctx context.Context) error { sends.Add(1); return nil })
	require.NoError(t, err)
	err = generationWS(child, 9, map[string]any{"type": "response.create"}, func(ctx context.Context) error { sends.Add(1); return nil })
	require.ErrorIs(t, err, ErrAttemptBudgetExhausted)
	require.Equal(t, int32(3), sends.Load())
	records := AttemptBudgetFromContext(ctx).Snapshot()
	require.Equal(t, "auth_resend", records[1].Reason)
	require.Equal(t, "account_switch", records[2].Reason)
	require.Equal(t, "unknown", records[2].Outcome)
}

func TestAttemptBudgetConcurrentAdmissionAndCommit(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 3, time.Minute)
	defer cancel()
	var sends atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			finish, err := BeginGenerationAttempt(ctx, 1)
			if err == nil {
				sends.Add(1)
				finish(503, nil)
				finish(503, nil)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(3), sends.Load())
	CommitAttemptResponse(ctx)
	_, err := BeginGenerationAttempt(ctx, 1)
	require.ErrorIs(t, err, ErrResponseCommitted)
}

func TestAttemptBudgetCanceledAndDeadlineSurviveDetach(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 3, time.Minute)
	child := context.WithoutCancel(ctx)
	cancel()
	_, err := BeginGenerationAttempt(child, 1)
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel = WithAttemptBudget(context.Background(), 3, time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	_, err = BeginGenerationAttempt(context.WithoutCancel(ctx), 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAttemptBudgetStreamTurnsAndControlFrames(t *testing.T) {
	ctx := WithStreamAttemptBudget(context.Background())
	BeginAttemptStreamTurn(ctx, 1)
	finish, err := BeginGenerationAttempt(ctx, 1)
	require.NoError(t, err)
	finish(0, errors.New("lost ack"))
	CommitAttemptResponse(ctx)
	BeginAttemptStreamTurn(ctx, 1)
	_, err = BeginGenerationAttempt(ctx, 2)
	require.ErrorIs(t, err, ErrResponseCommitted)
	BeginAttemptStreamTurn(ctx, 2)
	finish, err = BeginGenerationAttempt(ctx, 2)
	require.NoError(t, err)
	finish(400, nil)
	require.NoError(t, generationWS(ctx, 2, map[string]any{"type": "response.create", "generate": false}, func(ctx context.Context) error { return nil }))
	require.Len(t, AttemptBudgetFromContext(ctx).Snapshot(), 1)
	BeginAttemptStreamTurn(ctx, 1)
	require.Len(t, AttemptBudgetFromContext(ctx).Snapshot(), 1)
}

func TestAttemptBudgetHTTPBodyClosesAndUnknownOutcome(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 3, time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://example.invalid/responses", nil)
	resp, err := generationHTTP(req, 1, func(r *http.Request) (*http.Response, error) {
		require.True(t, HTTPUpstreamRedirectsDisabled(r.Context()))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, err = generationHTTP(req, 1, func(r *http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Equal(t, "unknown", AttemptBudgetFromContext(ctx).Snapshot()[1].Outcome)
}

type attemptBudgetWS struct{ sends int }

func (c *attemptBudgetWS) WriteJSON(context.Context, any) error        { c.sends++; return nil }
func (c *attemptBudgetWS) ReadMessage(context.Context) ([]byte, error) { return nil, io.EOF }
func (c *attemptBudgetWS) Ping(context.Context) error                  { return nil }
func (c *attemptBudgetWS) Close() error                                { return nil }

func TestAttemptBudgetLeaseReleaseExactlyOnceAfterDenial(t *testing.T) {
	for _, terminal := range []string{"exhausted", "committed", "canceled"} {
		t.Run(terminal, func(t *testing.T) {
			ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
			defer cancel()
			ws := &attemptBudgetWS{}
			conn := newOpenAIWSConn("budget", 1, ws, nil)
			require.NoError(t, conn.acquire(context.Background()))
			lease := &openAIWSConnLease{conn: conn, accountID: 1}
			payload := map[string]any{"type": "response.create"}
			require.NoError(t, lease.WriteJSONWithContextTimeout(ctx, payload, time.Second))
			switch terminal {
			case "committed":
				CommitAttemptResponse(ctx)
			case "canceled":
				cancel()
			}
			require.Error(t, lease.WriteJSONWithContextTimeout(ctx, payload, time.Second))
			require.Equal(t, 1, ws.sends)
			lease.Release()
			require.NoError(t, conn.acquire(context.Background()))
			// An old cleanup must not release the next request's lease.
			lease.Release()
			require.Empty(t, conn.leaseCh)
			conn.release()
		})
	}
}

func TestAttemptBudgetLateCompletionDoesNotCorruptNextTurn(t *testing.T) {
	ctx := WithStreamAttemptBudget(context.Background())
	BeginAttemptStreamTurn(ctx, 1)
	finish, err := BeginGenerationAttempt(ctx, 1)
	require.NoError(t, err)
	BeginAttemptStreamTurn(ctx, 2)
	finish(503, nil)
	require.Empty(t, AttemptBudgetFromContext(ctx).Snapshot())
}

type attemptBudgetFrameConn struct{}

func (*attemptBudgetFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	<-ctx.Done()
	return coderws.MessageText, nil, ctx.Err()
}
func (*attemptBudgetFrameConn) WriteFrame(context.Context, coderws.MessageType, []byte) error {
	return nil
}
func (*attemptBudgetFrameConn) Close() error { return nil }

func TestAttemptBudgetPassthroughCumulativeDeadline(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 3, 20*time.Millisecond)
	defer cancel()
	c := &openAIWSPassthroughFirstOutputFrameConn{
		inner: &attemptBudgetFrameConn{}, activeReadTimeout: time.Hour,
		deadlineChanged: make(chan struct{}, 1),
		resolveDeadline: func([]byte) openAIWSPassthroughFirstOutputDeadline {
			return openAIWSPassthroughFirstOutputDeadline{timeout: time.Hour}
		},
	}
	c.armDeadline([]byte(`{"type":"response.create"}`))
	c.armActiveReadDeadline()
	_, _, err := c.ReadFrame(context.WithoutCancel(ctx))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAttemptBudgetOpaqueWSControlFrame(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancel()
	sent := false
	require.NoError(t, generationWS(ctx, 1, json.RawMessage{0, 1, 2}, func(context.Context) error { sent = true; return nil }))
	require.True(t, sent)
	require.Empty(t, AttemptBudgetFromContext(ctx).Snapshot())
}

func TestAttemptBudgetCancelsDetachedInFlightHTTP(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer upstream.Close()
	ctx, cancel := WithAttemptBudget(context.Background(), 3, time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(context.WithoutCancel(ctx), "POST", upstream.URL, nil)
	result := make(chan error, 1)
	go func() { _, err := generationHTTP(req, 1, upstream.Client().Do); result <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream not reached")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("detached send ignored root cancellation")
	}
	require.Equal(t, "unknown", AttemptBudgetFromContext(ctx).Snapshot()[0].Outcome)
}

func TestAttemptBudgetDoesNotCountDiscoveryUploadsCountingOrCancellation(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancel()
	CommitAttemptResponse(ctx)
	for _, route := range []struct{ method, path string }{
		{"GET", "/models"}, {"POST", "/messages/count_tokens"}, {"POST", "/responses/input_tokens"},
		{"POST", "/models/gemini:countTokens"}, {"POST", "/files"}, {"POST", "/uploads/a/complete"}, {"POST", "/batches/a/cancel"},
	} {
		req, _ := http.NewRequestWithContext(ctx, route.method, "https://example.invalid"+route.path, nil)
		called := false
		_, err := generationHTTP(req, 1, func(*http.Request) (*http.Response, error) { called = true; return nil, nil })
		require.NoError(t, err)
		require.True(t, called)
	}
	require.Empty(t, AttemptBudgetFromContext(ctx).Snapshot())
}

func TestAttemptBudgetWebSocketReconcileAndTerminal(t *testing.T) {
	ctx := WithStreamAttemptBudget(t.Context())
	ConfigureAttemptBudget(ctx, 3)
	BeginAttemptStreamTurn(ctx, 1)
	msg := json.RawMessage(`{"type":"response.create"}`)
	send := func(context.Context) error { return nil }
	require.NoError(t, generationWS(ctx, 7, msg, send))
	require.Equal(t, attempt.Unknown, AttemptDecisionBudget(ctx).Acceptance)
	BeginAttemptStreamTurn(ctx, 1)
	require.ErrorIs(t, generationWS(ctx, 8, msg, send), ErrAttemptOutcomeUnknown)
	rejectAttemptWS(ctx, 8, 429)
	require.ErrorIs(t, CheckAttemptBudget(ctx), ErrAttemptOutcomeUnknown)
	rejectAttemptWS(ctx, 7, 429)
	require.NoError(t, CheckAttemptBudget(ctx))
	require.Equal(t, attempt.Accepted, AttemptDecisionBudget(ctx).Acceptance)
	require.NoError(t, generationWS(ctx, 8, msg, send))

	calls := 0
	frame := func() error { calls++; return nil }
	require.NoError(t, writeAttemptWS(ctx, []byte(`{"type":"ping"}`), frame))
	r := AttemptBudgetFromContext(ctx).Response()
	require.Equal(t, lifecycle.HeartbeatOnly, r.State())
	require.NoError(t, writeAttemptWS(ctx, []byte(`{"type":"response.output_text.delta","delta":"a"}`), frame))
	require.Equal(t, lifecycle.SemanticOutput, r.State())
	terminal := []byte(`{"type":"response.completed"}`)
	require.ErrorIs(t, writeAttemptWS(ctx, terminal, func() error { calls++; return io.ErrClosedPipe }), io.ErrClosedPipe)
	require.NoError(t, writeAttemptWS(ctx, terminal, frame))
	require.Equal(t, 3, calls)
	require.Equal(t, lifecycle.TerminalFailed, r.State())
	require.ErrorIs(t, CheckAttemptBudget(ctx), ErrResponseCommitted)
	BeginAttemptStreamTurn(ctx, 2)
	require.Equal(t, lifecycle.PreOutput, AttemptBudgetFromContext(ctx).Response().State())
	require.Equal(t, lifecycle.TerminalFailed, r.State())
	require.NoError(t, CheckAttemptBudget(ctx))
}

func TestAttemptBudgetSilentTimeoutPermit(t *testing.T) {
	repo := &tpmBoundaryRepository{}
	ctx := WithStreamAttemptBudget(WithGatewayRequestPlan(t.Context(), boundaryTPMPlan(repo, "replay")))
	ConfigureAttemptBudget(ctx, 4)
	BeginAttemptStreamTurn(ctx, 1)
	send := func(context.Context) error { return nil }
	msg := json.RawMessage(`{"type":"response.create"}`)
	require.NoError(t, generationWS(ctx, 1, msg, send))
	interruptGeneration(ctx, 1, true)
	controller := attempt.New(3)
	require.ErrorIs(t, generationWS(ctx, 2, msg, send), ErrAttemptOutcomeUnknown)
	// Only the failing account's decision may open the send boundary.
	require.Equal(t, attempt.Stop, DecideAttempt(ctx, &controller, attempt.Failure{AccountID: 2}).Action)
	controller.FailedAccountIDs[99] = struct{}{}
	d := DecideAttempt(ctx, &controller, attempt.Failure{AccountID: 1})
	require.Equal(t, attempt.Switch, d.Action)
	BeginAttemptStreamTurn(ctx, 1)
	require.ErrorIs(t, generationWS(ctx, 1, msg, send), ErrAttemptOutcomeUnknown)
	require.ErrorIs(t, generationWS(ctx, 99, msg, send), ErrAttemptOutcomeUnknown)
	var sent atomic.Int32
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Go(func() {
			_ = generationWS(ctx, int64(2+i), msg, func(context.Context) error { sent.Add(1); return nil })
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, sent.Load())
	require.Len(t, repo.commands, 2)
	records := AttemptBudgetFromContext(ctx).Snapshot()
	require.Len(t, records, 2)
	require.True(t, records[0].AbandonedUncertain)
	require.Equal(t, "unknown", records[0].Outcome)
	interruptGeneration(ctx, records[1].AccountID, true)
	controller = attempt.New(3)
	d = DecideAttempt(ctx, &controller, attempt.Failure{AccountID: records[1].AccountID})
	require.Equal(t, attempt.Stop, d.Action)
	require.Equal(t, "acceptance_unknown", d.Reason)
	BeginAttemptStreamTurn(ctx, 2)
	require.NoError(t, generationWS(ctx, 1, msg, send))
}

func TestWebSocketSilentAttemptClosesUpstreamBeforeReplay(t *testing.T) {
	ctx := WithStreamAttemptBudget(t.Context())
	ConfigureAttemptBudget(ctx, 3)
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
	defer server.Close()
	conn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = conn.CloseNow() }()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	controller := attempt.New(3)
	require.Equal(t, "acceptance_unknown", AttemptDecisionBudget(ctx).StopReason)
	require.False(t, AttemptDecisionBudget(ctx).ReplayAvailable)
	select {
	case err := <-serverErr:
		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout did not return")
	}
	select {
	case <-upstream.closed:
	default:
		t.Fatal("timeout returned before upstream close")
	}
	require.ErrorIs(t, CheckAttemptBudget(ctx), ErrAttemptOutcomeUnknown)
	require.Equal(t, attempt.Switch, DecideAttempt(ctx, &controller, attempt.Failure{AccountID: account.ID}).Action)
	require.NoError(t, CheckAttemptBudget(ctx))
}
