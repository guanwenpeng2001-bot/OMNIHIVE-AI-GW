package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
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
	Turn      int
	Number    int
	AccountID int64
	Reason    string
	Outcome   string
	Status    int
}

type AttemptBudget struct {
	mu          sync.Mutex
	root        context.Context
	deadline    time.Time
	limit       int
	committed   bool
	uncertain   bool
	attempts    []*AttemptRecord
	streamTurns bool
	turn        int
}

// A WebSocket connection is long-lived; only a new client logical turn may
// renew its budget. Reconnects and retries of the same turn cannot renew it.
func WithStreamAttemptBudget(ctx context.Context) context.Context {
	if AttemptBudgetFromContext(ctx) != nil {
		return ctx
	}
	b := &AttemptBudget{root: ctx, deadline: time.Now().Add(DefaultGenerationDeadline), limit: DefaultGenerationAttempts, streamTurns: true}
	return context.WithValue(ctx, attemptBudgetKey{}, b)
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
		b.deadline = time.Now().Add(DefaultGenerationDeadline)
		b.attempts = nil
		b.committed = false
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
	finish, err := BeginGenerationAttempt(ctx, accountID)
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
	b := &AttemptBudget{root: root, deadline: deadline, limit: limit}
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
	b.committed = true
	b.mu.Unlock()
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
	if b.committed {
		return ErrResponseCommitted
	}
	if b.uncertain {
		return ErrAttemptOutcomeUnknown
	}
	if len(b.attempts) >= b.limit {
		return ErrAttemptBudgetExhausted
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
	if err := CheckAttemptBudget(ctx); err != nil {
		return nil, err
	}
	plan := GatewayRequestPlanFromContext(ctx)
	tpmSequence, tpmErr := plan.beginTPMSend(ctx, accountID)
	if tpmErr != nil {
		return nil, tpmErr
	}
	b := AttemptBudgetFromContext(ctx)
	var record *AttemptRecord
	if b != nil {
		b.mu.Lock()
		if err := b.checkLocked(ctx); err != nil {
			b.mu.Unlock()
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
				record.Status = status
				record.Outcome = outcome
				if err != nil {
					var networkError *net.OpError
					unsent := errors.As(err, &networkError) && networkError.Op == "dial"
					if unsent {
						record.Outcome = "not_sent"
					} else if record.Turn == b.turn {
						b.uncertain = true
					}
				}
				b.mu.Unlock()
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
