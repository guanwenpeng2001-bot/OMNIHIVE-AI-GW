package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

// Limits are cumulative across account selection, repair and transport fallback.
const DefaultGenerationAttempts = 1
const DefaultGenerationDeadline = 5 * time.Minute

var ErrAttemptBudgetExhausted = errors.New("generation attempt budget exhausted")
var ErrResponseCommitted = errors.New("downstream response committed; generation replay forbidden")
var ErrAttemptOutcomeUnknown = errors.New("upstream submission outcome unknown; generation replay forbidden")

type attemptBudgetKey struct{}

type attemptResponseBody struct {
	io.ReadCloser
	once     sync.Once
	cleanup  func()
	closeErr error
}

func (b *attemptResponseBody) Close() error {
	b.once.Do(func() { b.cleanup(); b.closeErr = b.ReadCloser.Close() })
	return b.closeErr
}

// AttemptRecord contains no credentials, request bodies or caller-controlled labels.
// A transport error cannot prove that the upstream did not execute the request.
type AttemptRecord struct {
	preHeaderUnknown   bool
	AbandonedUncertain bool
	Turn               int
	Number             int
	AccountID          int64
	Reason             string
	Outcome            string
	Status             int
}

type AttemptBudget struct {
	// Serialize send admission, including the TPM reservation. Competing replacements
	// must see the consumed permit before creating a reservation for another send.
	admissionMu sync.Mutex
	mu          sync.Mutex
	root        context.Context
	deadline    time.Time
	limit       int
	response    *lifecycle.Response
	pendingWS   *AttemptRecord
	observeWS   func(string)
	interrupted *AttemptRecord
	// replayFrom identifies the sole abandoned attempt whose replacement is authorized.
	replayFrom     *AttemptRecord
	replayExcluded []int64
	uncertain      bool
	attempts       []*AttemptRecord
	streamTurns    bool
	turn           int
}

// A WebSocket connection is long-lived; only a new client logical turn may
// renew its budget. Reconnects and retries of the same turn cannot renew it.
func WithStreamAttemptBudget(ctx context.Context) context.Context {
	if AttemptBudgetFromContext(ctx) != nil {
		return ctx
	}
	b := &AttemptBudget{response: new(lifecycle.Response), root: ctx, deadline: streamTurnDeadline(ctx), limit: DefaultGenerationAttempts, streamTurns: true}
	return context.WithValue(ctx, attemptBudgetKey{}, b)
}

func streamTurnDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(DefaultGenerationDeadline)
	if root, ok := ctx.Deadline(); ok && root.Before(deadline) {
		return root
	}
	return deadline
}

func BeginAttemptStreamTurn(ctx context.Context, turn int) {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.streamTurns || turn <= b.turn {
		return
	}
	if b.turn > 0 {
		b.deadline = streamTurnDeadline(b.root)
		b.attempts = nil
		b.response = new(lifecycle.Response)
		b.pendingWS = nil
		b.observeWS = nil
		b.interrupted = nil
		b.replayFrom = nil
		b.replayExcluded = nil
		b.uncertain = false
	}
	b.turn = turn
}

func CommitAttemptResponse(ctx context.Context) { AttemptBudgetFromContext(ctx).Commit() }

func attemptDeadline(ctx context.Context) time.Time {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return time.Time{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deadline
}

func attemptTimeout(ctx context.Context, timeout time.Duration) time.Duration {
	if deadline := attemptDeadline(ctx); !deadline.IsZero() && (timeout <= 0 || time.Until(deadline) < timeout) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return time.Nanosecond
		}
		return remaining
	}
	return timeout
}

// A provider rejection received before output reconciles this WS send. Losing
// the upstream connection after a successful write leaves acceptance unknown.
func rejectAttemptWS(ctx context.Context, accountID int64, status int) {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return
	}
	b.mu.Lock()
	var observe func(string)
	if r := b.pendingWS; r != nil && r.AccountID == accountID {
		r.Outcome, r.Status = "rejected", status
		observe = b.observeWS
		b.pendingWS, b.observeWS = nil, nil
	}
	b.mu.Unlock()
	if observe != nil {
		observe("rejected")
	}
}

func writeAttemptWS(ctx context.Context, payload []byte, send func() error) error {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return send()
	}
	event := gjson.GetBytes(payload, "type").String()
	b.mu.Lock()
	r := b.response
	var observe func(string)
	if b.pendingWS != nil && b.pendingWS.Outcome != "transport_response" && strings.HasPrefix(event, "response.") {
		b.pendingWS.Outcome = "transport_response"
		observe = b.observeWS
	}
	b.mu.Unlock()
	if observe != nil {
		observe("transport_response")
	}
	if isOpenAIWSTerminalEvent(event) {
		if !r.BeginTerminal() {
			return nil
		}
		err := send()
		r.FinishTerminal(err == nil)
		return err
	}
	if event == "ping" && IsStreamHeartbeatData(string(payload)) {
		r.Heartbeat()
	} else {
		r.Semantic()
	}
	return send()
}

func generationWS(ctx context.Context, accountID int64, value any, send func(ctx context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var payload []byte
	if raw, ok := value.(json.RawMessage); ok {
		payload = raw
	} else {
		var err error
		payload, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	if gjson.GetBytes(payload, "type").String() != "response.create" || gjson.GetBytes(payload, "generate").Type == gjson.False {
		return send(ctx)
	}
	ctx = withTPMWire(ctx, payload, "/responses")
	finish, err := beginGenerationAttempt(ctx, accountID, true)
	if err != nil {
		return err
	}
	ctx, cancel := AttemptExecutionContext(ctx)
	defer cancel()
	err = send(ctx)
	// A successful WS write is not proof of generation success or an HTTP response.
	finish(0, err)
	return err
}

// WithAttemptBudget never replaces an existing root, including on detached contexts.
func WithAttemptBudget(ctx context.Context, limit int, duration time.Duration) (context.Context, context.CancelFunc) {
	if AttemptBudgetFromContext(ctx) != nil {
		return ctx, func() {}
	}
	if limit <= 0 {
		limit = DefaultGenerationAttempts
	}
	if duration <= 0 {
		duration = DefaultGenerationDeadline
	}
	root, cancel := context.WithTimeout(ctx, duration)
	deadline, _ := root.Deadline()
	b := &AttemptBudget{response: new(lifecycle.Response), root: root, deadline: deadline, limit: limit}
	return context.WithValue(root, attemptBudgetKey{}, b), cancel
}

func AttemptBudgetFromContext(ctx context.Context) *AttemptBudget {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(attemptBudgetKey{}).(*AttemptBudget)
	return b
}

// ConfigureAttemptBudget freezes the configured allowance before the first send.
// Account repairs and protocol fallbacks consume this same allowance.
func ConfigureAttemptBudget(ctx context.Context, maxAccountSwitches int) {
	budget := AttemptBudgetFromContext(ctx)
	if budget == nil {
		return
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if len(budget.attempts) == 0 {
		budget.limit = 1 + max(0, maxAccountSwitches)
	}
}

func (b *AttemptBudget) Commit() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.response.Semantic()
	b.mu.Unlock()
}

// interruptGeneration records the outcome only after the upstream has been closed or canceled.
func interruptGeneration(ctx context.Context, accountID int64, silentFirstOutputTimeout bool) {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.attempts) == 0 {
		return
	}
	r := b.attempts[len(b.attempts)-1]
	if r.AccountID != accountID || r.Outcome == "not_sent" || r.Outcome == "rejected" {
		return
	}
	r.Outcome = "unknown"
	r.AbandonedUncertain = r.preHeaderUnknown || silentFirstOutputTimeout && b.response.State() == lifecycle.PreOutput && !b.response.ReplayBlocked()
	b.interrupted = r
}

// IsAbandonedAttemptReplacement identifies the actual replacement send in the current turn.
func IsAbandonedAttemptReplacement(ctx context.Context, accountID int64) bool {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.replayFrom == nil {
		return false
	}
	current := b.attempts[len(b.attempts)-1]
	return current != b.replayFrom && current.AccountID == accountID
}

func pendingWSTurn(ctx context.Context) bool {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pendingWS != nil && b.response.State() < lifecycle.EmittingTerminal
}

func (b *AttemptBudget) checkLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.root.Err(); err != nil {
		return err
	}
	if !time.Now().Before(b.deadline) {
		return context.DeadlineExceeded
	}
	if b.interrupted != nil && (b.interrupted != b.replayFrom || !b.interrupted.AbandonedUncertain || b.response.State() != lifecycle.PreOutput || b.response.ReplayBlocked()) {
		return ErrAttemptOutcomeUnknown
	}
	if b.replayFrom != nil && b.attempts[len(b.attempts)-1] != b.replayFrom {
		return ErrAttemptOutcomeUnknown
	}
	if b.response.ReplayBlocked() {
		return ErrResponseCommitted
	}
	if b.uncertain && b.interrupted == nil {
		return ErrAttemptOutcomeUnknown
	}
	if len(b.attempts) >= b.limit {
		return ErrAttemptBudgetExhausted
	}
	if b.pendingWS != nil && b.pendingWS != b.replayFrom {
		return ErrAttemptOutcomeUnknown
	}
	return nil
}

func (b *AttemptBudget) Response() *lifecycle.Response {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.response
}

func AttemptDecisionBudget(ctx context.Context) attempt.Budget {
	b := AttemptBudgetFromContext(ctx)
	if b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
	}
	return decisionBudgetLocked(ctx, b)
}

func decisionBudgetLocked(ctx context.Context, b *AttemptBudget) attempt.Budget {
	view := attempt.Budget{Acceptance: attempt.NotSent, Remaining: -1}
	var err error
	if b != nil {
		view.Number = len(b.attempts)
		view.Remaining = b.limit - view.Number
		view.Deadline = b.deadline
		if view.Number > 0 {
			r := b.attempts[view.Number-1]
			view.AbandonedUncertain = r.AbandonedUncertain
			view.ReplayAvailable = !r.preHeaderUnknown && b.replayFrom == nil && view.AbandonedUncertain && b.response.State() == lifecycle.PreOutput && !b.response.ReplayBlocked()
			switch r.Outcome {
			case "unknown":
				view.Acceptance = attempt.Unknown
			case "transport_response", "rejected":
				view.Acceptance = attempt.Accepted
			}
		}
		err = b.checkLocked(ctx)
	} else if ctx != nil {
		err = ctx.Err()
		view.Deadline, _ = ctx.Deadline()
	}
	switch {
	case errors.Is(err, context.Canceled):
		view.StopReason = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		view.StopReason = "deadline"
	case errors.Is(err, ErrResponseCommitted):
		view.StopReason = "response_committed"
	case errors.Is(err, ErrAttemptOutcomeUnknown):
		view.StopReason = "acceptance_unknown"
	case errors.Is(err, ErrAttemptBudgetExhausted):
		view.StopReason = "attempt_limit"
	}
	return view
}

// DecideAttempt authorizes a silent-timeout switch atomically with its budget snapshot.
func DecideAttempt(ctx context.Context, controller *attempt.Controller, failure attempt.Failure) attempt.AttemptDecision {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return controller.Decide(AttemptDecisionBudget(ctx), failure)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	view := decisionBudgetLocked(ctx, b)
	view.ReplayAvailable = view.ReplayAvailable && b.interrupted != nil && b.interrupted.AccountID == failure.AccountID
	d := controller.Decide(view, failure)
	if d.Action == attempt.Switch && d.Reason == attempt.FirstOutputTimeoutAbandoned {
		b.replayFrom = b.interrupted
		b.replayExcluded = slices.Clone(d.Excluded)
	}
	return d
}

func (b *AttemptBudget) checkSendLocked(ctx context.Context, accountID int64) error {
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	if slices.Contains(b.replayExcluded, accountID) {
		return ErrAttemptOutcomeUnknown
	}
	return nil
}

func CheckAttemptBudget(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if b := AttemptBudgetFromContext(ctx); b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.checkLocked(ctx)
	}
	return ctx.Err()
}

func (b *AttemptBudget) Snapshot() []AttemptRecord {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]AttemptRecord, len(b.attempts))
	for i, record := range b.attempts {
		result[i] = *record
	}
	return result
}

// BeginGenerationAttempt is called after local validation and immediately before
// the actual send. Denials never enter the upstream telemetry denominator.
func BeginGenerationAttempt(ctx context.Context, accountID int64) (func(int, error), error) {
	return beginGenerationAttempt(ctx, accountID, false)
}

func beginGenerationAttempt(ctx context.Context, accountID int64, ws bool) (func(int, error), error) {
	b := AttemptBudgetFromContext(ctx)
	if b != nil {
		b.admissionMu.Lock()
		defer b.admissionMu.Unlock()
		b.mu.Lock()
		err := b.checkSendLocked(ctx, accountID)
		b.mu.Unlock()
		if err != nil {
			return nil, err
		}
	} else if err := ctx.Err(); err != nil {
		return nil, err
	}
	plan := GatewayRequestPlanFromContext(ctx)
	if journal.From(ctx) != nil {
		if plan == nil || plan.store == nil {
			return nil, journal.ErrDispatch
		}
	}
	tpmSequence, tpmErr := plan.beginTPMSend(ctx, accountID)
	if tpmErr != nil {
		return nil, tpmErr
	}
	var durable journal.Attempt
	if plan != nil && plan.store != nil {
		if b == nil {
			plan.releaseUnsentTPM(tpmSequence)
			return nil, journal.ErrDispatch
		}
		b.mu.Lock()
		number := len(b.attempts) + 1
		b.mu.Unlock()
		var err error
		durable, err = plan.journalDispatch(ctx, accountID, number)
		if err != nil {
			plan.releaseUnsentTPM(tpmSequence)
			return nil, err
		}
	}
	var record *AttemptRecord
	if b != nil {
		b.mu.Lock()
		if err := b.checkSendLocked(ctx, accountID); err != nil {
			b.mu.Unlock()
			plan.releaseUnsentTPM(tpmSequence)
			plan.observeJournalAttempt(durable, "not_sent")
			return nil, err
		}
		index := len(b.attempts)
		reason := "initial"
		if index > 0 {
			previous := b.attempts[index-1]
			switch {
			case previous.AccountID != accountID:
				reason = "account_switch"
			case previous.Status == http.StatusUnauthorized:
				reason = "auth_resend"
			case previous.Status == http.StatusBadRequest:
				reason = "request_repair"
			default:
				reason = "same_account_retry"
			}
		}
		record = &AttemptRecord{Turn: b.turn, Number: index + 1, AccountID: accountID, Reason: reason, Outcome: "unknown"}
		b.attempts = append(b.attempts, record)
		b.interrupted = nil
		b.uncertain = false
		b.pendingWS, b.observeWS = nil, nil
		if ws && b.streamTurns {
			b.pendingWS = record
			if durable.ID != "" {
				b.observeWS = func(outcome string) { plan.observeJournalAttempt(durable, outcome) }
			}
		}
		b.mu.Unlock()
	} else if err := ctx.Err(); err != nil {
		return nil, err
	}
	var once sync.Once
	return func(status int, err error) {
		once.Do(func() {
			plan.finishTPMSend(tpmSequence, err)
			if b != nil {
				outcome := "unknown"
				if err == nil && status >= 400 {
					outcome = "rejected"
				}
				if err == nil && status >= 200 && status < 400 {
					outcome = "transport_response"
				}
				b.mu.Lock()
				if record.Status == 0 {
					record.Status = status
				}
				if record.Outcome == "unknown" {
					record.Outcome = outcome
				}
				if err != nil {
					var networkError *net.OpError
					unsent := errors.As(err, &networkError) && networkError.Op == "dial"
					if unsent {
						if b.pendingWS == record {
							b.pendingWS = nil
						}
						record.Outcome = "not_sent"
					} else if record.Turn == b.turn {
						b.uncertain = true
						// A timeout before HTTP headers cannot prove the POST was not accepted.
						if !ws && status == 0 && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
							record.preHeaderUnknown, record.AbandonedUncertain = true, true
							b.interrupted = record
						}
					}
				}
				outcome = record.Outcome
				b.mu.Unlock()
				plan.observeJournalAttempt(durable, outcome)
			}
		})
	}, nil
}

func generationHTTP(request *http.Request, accountID int64, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	// Status polling, discovery, uploads and token counting do not generate.
	path := request.URL.Path
	if request.Method != http.MethodPost || strings.HasSuffix(path, "/count_tokens") || strings.HasSuffix(path, "/input_tokens") || strings.Contains(path, ":countTokens") ||
		strings.HasSuffix(path, "/files") || strings.Contains(path, "/files/") || strings.HasSuffix(path, "/uploads") || strings.Contains(path, "/uploads/") || strings.HasSuffix(path, "/cancel") {
		return send(request)
	}
	// A 307/308 redirect can silently replay the POST below our send boundary.
	request = request.WithContext(WithHTTPUpstreamRedirectsDisabled(tpmHTTPRequestContext(request)))
	finish, err := BeginGenerationAttempt(request.Context(), accountID)
	if err != nil {
		return nil, err
	}
	// Detached streaming contexts still obey the original cumulative deadline.
	ctx, cancelGeneration := context.WithCancel(request.Context())
	ctx, cancelDeadline := AttemptExecutionContext(ctx)
	cancel := func() { cancelGeneration(); cancelDeadline() }
	request = request.WithContext(ctx)
	response, sendErr := send(request)
	status := 0
	if response != nil {
		status = response.StatusCode
		if status >= 400 && response.Body != nil {
			if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
				if identity := plan.TPMReceiptIdentity(accountID); identity != nil {
					response.Body = &tpmFailureBody{ReadCloser: response.Body, plan: plan, identity: *identity}
				}
			}
		}
	}
	finish(status, sendErr)
	if sendErr != nil || response == nil || response.Body == nil {
		cancel()
	} else {
		response.Body = &attemptResponseBody{ReadCloser: response.Body, cleanup: cancel}
	}
	return response, sendErr
}

// AttemptExecutionContext binds detached HTTP/WS work to the root cancellation
// and the current logical turn's original deadline, not a renewed retry window.
func AttemptExecutionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	b := AttemptBudgetFromContext(ctx)
	if b == nil {
		return context.WithTimeout(ctx, DefaultGenerationDeadline)
	}
	b.mu.Lock()
	deadline := b.deadline
	b.mu.Unlock()
	if d, ok := ctx.Deadline(); ok && !d.After(deadline) {
		return context.WithCancel(ctx)
	}
	child, cancel := context.WithDeadline(ctx, deadline)
	stop := context.AfterFunc(b.root, func() {
		if !errors.Is(b.root.Err(), context.DeadlineExceeded) {
			cancel()
		}
	})
	return child, func() { stop(); cancel() }
}

func IsAttemptAdmissionError(err error) bool {
	return IsTPMAdmissionError(err) || errors.Is(err, ErrAttemptBudgetExhausted) || errors.Is(err, ErrResponseCommitted) || errors.Is(err, ErrAttemptOutcomeUnknown)
}

func generationDo(upstream HTTPUpstream, req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return generationHTTP(req, id, func(r *http.Request) (*http.Response, error) { return upstream.Do(r, proxy, id, concurrency) })
}

func generationDoWithTLS(upstream HTTPUpstream, req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return generationHTTP(req, id, func(r *http.Request) (*http.Response, error) {
		return upstream.DoWithTLS(r, proxy, id, concurrency, profile)
	})
}

// Batch creation uses provider-owned clients but shares the generation boundary.
// Uploads, status reads and cancellation must not call this helper.
func generationBatchHTTP(request *http.Request, client *http.Client) (*http.Response, error) {
	accountID := int64(0)
	if plan := GatewayRequestPlanFromContext(request.Context()); plan != nil {
		accountID = plan.spec.UpstreamAccountID
	}
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return generationHTTP(request, accountID, func(req *http.Request) (*http.Response, error) { return batchImageDo(req, &noRedirect) })
}
