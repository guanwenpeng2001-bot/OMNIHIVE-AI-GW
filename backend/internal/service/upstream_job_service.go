package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

var ErrUpstreamJobNotFound = errors.New("upstream job not found")
var ErrUpstreamJobUnsupportedUsage = errors.New("unsupported upstream job usage")
var ErrUpstreamJobTariff = errors.New("explicit video model/resolution tariff is required")

// UpstreamJobBilling is an immutable admission-time price/ownership snapshot.
// Never persist credentials or the client's potentially sensitive input body.
type UpstreamJobBilling struct {
	TaskPriceVersion string               `json:"task_price_version,omitempty"`
	Egress           *ProxyGroupJobEgress `json:"-"`
	PricingState     PricingState         `json:"pricing_state,omitempty"`
	PricingSnapshot  string               `json:"pricing_snapshot,omitempty"`

	Realtime              *QianwenRealtimeSnapshot `json:"realtime,omitempty"`
	Model                 string                   `json:"model"`
	RequestedModel        string                   `json:"requested_model"`
	Resolution            string                   `json:"resolution"`
	UnitPrice             float64                  `json:"unit_price"`
	RateMultiplier        float64                  `json:"rate_multiplier"`
	AccountRateMultiplier float64                  `json:"account_rate_multiplier"`
	SubscriptionID        *int64                   `json:"subscription_id,omitempty"`
	KeyQuota              bool                     `json:"key_quota"`
	KeyRateLimit          bool                     `json:"key_rate_limit"`
	AccountQuota          bool                     `json:"account_quota"`
	KeyPeriodBudget       bool                     `json:"-"`
	AdmissionSeconds      int                      `json:"admission_seconds,omitempty"`

	// Async contains the immutable official tariff and FX provenance.
	Async *QianwenAsyncSnapshot `json:"async,omitempty"`
	// FileBatch contains only validated resource metadata, never file contents.
	FileBatch *QianwenFileBatchSnapshot `json:"file_batch,omitempty"`
	// ResourceFile and ResourceBatch carry the standard resource-action
	// admission snapshots. They contain hashes, counts and frozen rates only.
	ResourceFile  *GatewayResourceFileSnapshot  `json:"resource_file,omitempty"`
	ResourceBatch *GatewayResourceBatchSnapshot `json:"resource_batch,omitempty"`
}

type UpstreamJob struct {
	WorkspaceID        int64                 `json:"workspace_id"`
	Version            int64                 `json:"version"`
	ResourceState      UpstreamResourceState `json:"resource_state"`
	SubmissionKey      string                `json:"submission_key"`
	ID                 int64                 `json:"id"`
	PublicRequestID    string                `json:"public_request_id"`
	RequestFingerprint string                `json:"request_fingerprint"`
	Platform           string                `json:"platform"`
	Family             string                `json:"family"`
	AccountID          int64                 `json:"account_id"`
	UserID             int64                 `json:"user_id"`
	APIKeyID           int64                 `json:"api_key_id"`
	GroupID            *int64                `json:"group_id"`
	UpstreamTaskID     string                `json:"upstream_task_id"`
	Status             string                `json:"status"`
	AttemptCount       int                   `json:"attempt_count"`
	LastPolledAt       *time.Time            `json:"last_polled_at"`
	NextPollAt         time.Time             `json:"next_poll_at"`
	SubmittedAt        time.Time             `json:"submitted_at"`
	FinishedAt         *time.Time            `json:"finished_at"`
	ExpiresAt          time.Time             `json:"expires_at"`
	ResultRef          string                `json:"result_ref"`
	UsageJSON          json.RawMessage       `json:"usage_json"`
	ChargedUsageLogID  *int64                `json:"charged_usage_log_id"`
	ErrorCode          string                `json:"error_code"`
	ErrorMessage       string                `json:"error_message"`
	Billing            UpstreamJobBilling    `json:"billing_snapshot"`
}

func (j *UpstreamJob) Terminal() bool {
	return j.Status == "succeeded" || j.Status == "failed" || j.Status == "expired"
}

// The transaction owns both the row lock and all billing writes. Implementations
// must roll everything back if the callback or the final job update fails.
type UpstreamJobTx interface {
	Charge(context.Context, *UsageLog, *UsageBillingCommand) error
}
type UpstreamJobRepository interface {
	Create(context.Context, *UpstreamJob) (*UpstreamJob, bool, error)
	Get(context.Context, string, int64, int64) (*UpstreamJob, error)
	Due(context.Context, time.Time, int) ([]string, error)
	WithLocked(context.Context, string, func(*UpstreamJob, UpstreamJobTx) error) (*UpstreamJob, error)
	Expire(context.Context, time.Time) (int64, error)
}

type UpstreamJobUpdate struct {
	ProviderStatus                                     string
	TaskID, Status, ResultRef, ErrorCode, ErrorMessage string
	Usage                                              json.RawMessage
}

type UpstreamJobService struct {
	lifecycleMu sync.Mutex
	draining    bool
	active      sync.WaitGroup
	sessions    map[*realtimeRegistration]struct{}
	stopPolling func()
	afterSettle func(context.Context, *UpstreamJob)
	repo        UpstreamJobRepository
	poll        func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error)
	charge      func(*UpstreamJob) (*UsageLog, *UsageBillingCommand, error)
	now         func() time.Time
}

func NewUpstreamJobService(repo UpstreamJobRepository, poll func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error), charge func(*UpstreamJob) (*UsageLog, *UsageBillingCommand, error)) *UpstreamJobService {
	return NewUpstreamJobServiceWithClock(repo, poll, charge, time.Now)
}

// NewUpstreamJobServiceWithClock uses the supplied clock for receipt scheduling,
// retry budgets and expiry. The clock must be safe for concurrent calls.
func NewUpstreamJobServiceWithClock(repo UpstreamJobRepository, poll func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error), charge func(*UpstreamJob) (*UsageLog, *UsageBillingCommand, error), now func() time.Time) *UpstreamJobService {
	return &UpstreamJobService{repo: repo, poll: poll, charge: charge, now: now}
}

// Fingerprints include tenant, key, group and selected account, in addition to
// canonical JSON and model. Equivalent whitespace/key order is idempotent.
func UpstreamJobFingerprint(j *UpstreamJob, body []byte) (string, error) {
	canonical, err := CanonicalUpstreamJobBody(body)
	if err != nil {
		return "", err
	}
	identity := fmt.Sprintf("%s|%s|%d|%d|%d|%d|%s|", j.Platform, j.Family, j.AccountID, j.UserID, j.APIKeyID, valueOrZero(j.GroupID), j.Billing.Model)
	sum := sha256.Sum256(append([]byte(identity), canonical...))
	return hex.EncodeToString(sum[:]), nil
}

// Normalize nested duplicate keys before both tariff inspection and forwarding.
func CanonicalUpstreamJobBody(body []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || !json.Valid(body) {
		return nil, errors.New("invalid job JSON")
	}
	return json.Marshal(value)
}

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// Submit commits an exclusive claim BEFORE any network call. Ambiguous transport
// failures are not retried: without upstream idempotency this is the only safe
// at-most-once contract. A crashed claim is eventually expired.
func (s *UpstreamJobService) Submit(ctx context.Context, job *UpstreamJob, body []byte, submit func(context.Context) (UpstreamJobUpdate, error), accounts ...*Account) (*UpstreamJob, error) {
	if job == nil || job.UserID <= 0 || job.APIKeyID <= 0 || job.AccountID <= 0 || submit == nil {
		return nil, errors.New("invalid job submission")
	}
	if kaggleVideoAllowsUnpricedAdmission(job) {
		if _, err := frozenPerRequestVideoCost(job.Billing); err != nil {
			return nil, err
		}
	} else if (job.Family == "video" || job.Family == "3d" || job.Family == "asr") && (!finitePositive(job.Billing.UnitPrice) || !finitePositive(job.Billing.RateMultiplier)) {
		return nil, ErrUpstreamJobTariff
	}
	if len(accounts) > 0 {
		if accounts[0] == nil || accounts[0].ID != job.AccountID {
			return nil, ErrProxyBindingConflict
		}
		selected, err := accounts[0].PinJobEgress(ctx)
		if err != nil {
			return nil, err
		}
		job.Billing.Egress = selected
	}
	fingerprint, err := UpstreamJobFingerprint(job, body)
	if err != nil {
		return nil, err
	}
	claim := *job
	// Pin the first selected account across scheduler changes on client retries.
	logical := *job
	logical.AccountID = 0
	logical.Billing.Model = job.Billing.RequestedModel
	claim.SubmissionKey, err = UpstreamJobFingerprint(&logical, body)
	if err != nil {
		return nil, err
	}
	claim.ID = 0
	claim.PublicRequestID = uuid.NewString()
	claim.RequestFingerprint = fingerprint
	binding, bound := ctx.Value(mediaTaskKey{}).(MediaTaskBinding)
	if bound {
		ex, err := GatewayRequestPlanFromContext(ctx).AdmittedExecution()
		if err != nil || ex.RequestID != binding.Request || ex.Scope.KeyID != job.APIKeyID || ex.Scope.FundingUserID != job.UserID ||
			binding.Kind != "video" || job.Family != "video" || job.Platform != PlatformQianwen || job.AccountID != binding.Account || job.Billing.Model != binding.Model {
			return nil, ErrUpstreamJobTariff
		}
		// v2 idempotency belongs to the journaled command. Fresh paid commands
		// with identical input have independent claims, receipts and cancel IDs.
		claim.PublicRequestID = ex.RequestID
		claim.Billing.TaskPriceVersion = ex.Versions.Price
		hash := sha256.Sum256([]byte(ex.RequestID + "|" + fingerprint))
		claim.RequestFingerprint = hex.EncodeToString(hash[:])
		claim.SubmissionKey = claim.RequestFingerprint
	}
	if err := admitUpstreamJobBudget(ctx, job, body, bound); err != nil {
		return nil, err
	}
	claim.Status = "submitted"
	claim.UpstreamTaskID = ""
	claim.ChargedUsageLogID = nil
	claim.FinishedAt = nil
	claim.SubmittedAt = s.now().UTC()
	claim.NextPollAt = claim.SubmittedAt
	claim.ExpiresAt = claim.SubmittedAt.Add(24 * time.Hour)
	if job.Family == "batch" {
		claim.ExpiresAt = claim.SubmittedAt.Add(25 * time.Hour)
	}
	existing, created, err := s.repo.Create(ctx, &claim)
	if err != nil || !created {
		return existing, err
	}
	logJobEvent(ctx, existing, "admitted")
	if !bound {
		var finishPlan func()
		ctx, finishPlan = CaptureGatewayJobPlan(ctx, GatewayRequestSpec{UpstreamAccountID: existing.AccountID, UserID: existing.UserID, APIKeyID: existing.APIKeyID, BillingGroupID: valueOrZero(existing.GroupID), Model: existing.Billing.RequestedModel, UpstreamModel: existing.Billing.Model, Action: existing.Family, ExecutionID: existing.PublicRequestID})
		defer finishPlan()
	}

	networkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	update, upstreamErr := submit(networkCtx)
	cancel()
	// Persist the outcome even if the client disconnected during submission.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer persistCancel()
	return s.locked(persistCtx, existing.PublicRequestID, func(j *UpstreamJob, tx UpstreamJobTx) error {
		if j.Terminal() {
			return nil
		}
		if upstreamErr != nil {
			logJobEvent(ctx, j, "submission_unknown")
			s.finish(j, "failed", "submission_unknown", "Upstream submission outcome is unknown; automatic resubmission is disabled")
			return nil
		}
		if update.TaskID == "" && update.Status != "failed" {
			s.finish(j, "failed", "invalid_upstream_response", "Upstream did not return a task ID")
			return nil
		}
		j.UpstreamTaskID = update.TaskID
		j.NextPollAt = s.now().Add(5 * time.Second)
		return s.advance(persistCtx, j, tx, update)
	})
}

func (s *UpstreamJobService) Get(ctx context.Context, id string, userID, keyID int64) (*UpstreamJob, error) {
	job, err := s.repo.Get(ctx, id, userID, keyID)
	if err != nil {
		return nil, err
	}
	if !visibleModelPolicyAllows(ctx, job.Billing.RequestedModel) || !modelAccessAccountAllows(ctx, job.AccountID, job.Billing.RequestedModel) {
		return nil, ErrUpstreamJobNotFound
	}
	return job, nil
}

// Poll rechecks due time and terminal state under the same lock as settlement.
// Bounded network calls hold one row lock, never a global worker lock.
func (s *UpstreamJobService) Poll(ctx context.Context, id string) (*UpstreamJob, error) {
	ctx, release, err := s.RegisterQianwenRealtime(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	attempted := -1
	realtime := false
	// Persist the attempt before money work. A second instance cannot slip into
	// the rollback-to-retry gap and exceed the global automatic attempt budget.
	prepared, err := s.locked(ctx, id, func(j *UpstreamJob, _ UpstreamJobTx) error {
		if j.Family != "realtime_turn" {
			return nil
		}
		realtime = true
		now := s.now().UTC()
		if j.Terminal() || j.ChargedUsageLogID != nil || j.UpstreamTaskID == "" || len(j.UsageJSON) == 0 || j.ErrorCode == "usage_conflict" || j.NextPollAt.After(now) {
			return nil
		}
		if j.AttemptCount >= 20 || j.LastPolledAt != nil && !now.Before(j.LastPolledAt.Add(24*time.Hour)) {
			s.finish(j, "failed", "settlement_needs_review", "Automatic settlement exhausted; receipt retained for manual review")
			return nil
		}
		// Realtime has no provider polling: this field anchors its first
		// settlement attempt and is never moved by subsequent retries.
		if j.LastPolledAt == nil {
			j.LastPolledAt = &now
		}
		j.AttemptCount++
		attempted = j.AttemptCount
		j.NextPollAt = now.Add(5 * time.Second * time.Duration(1<<min(j.AttemptCount-1, 6)))
		return nil
	})
	if err != nil || realtime && attempted < 0 {
		return prepared, err
	}
	job, err := s.locked(ctx, id, func(j *UpstreamJob, tx UpstreamJobTx) error {
		if j.Family == "realtime_turn" {
			if j.Terminal() || j.ChargedUsageLogID != nil || j.AttemptCount != attempted {
				return nil
			}
			return s.settle(ctx, j, tx)
		}
		now := s.now().UTC()
		if j.Terminal() {
			return nil
		}
		if !j.ExpiresAt.After(now) {
			s.finish(j, "expired", "expired", "Task expired")
			return nil
		}
		if j.NextPollAt.After(now) || j.UpstreamTaskID == "" {
			return nil
		}
		j.AttemptCount++
		j.LastPolledAt = &now
		delay := 5 * time.Second * time.Duration(1<<min(j.AttemptCount-1, 6))
		j.NextPollAt = now.Add(delay)
		networkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		update, err := s.poll(networkCtx, j)
		if err != nil {
			logJobEvent(ctx, j, billingFailureCategory(err))
			j.ErrorCode = "poll_unavailable"
			j.ErrorMessage = "Upstream poll unavailable; retry scheduled"
			return nil
		}
		if (update.TaskID != "" || update.Status == "succeeded") && update.TaskID != j.UpstreamTaskID {
			s.finish(j, "failed", "task_mismatch", "Upstream task identity mismatch")
			return nil
		}
		if !j.ExpiresAt.After(s.now()) {
			s.finish(j, "expired", "expired", "Task expired")
			return nil
		}
		return s.advance(ctx, j, tx, update)
	})
	if err != nil && attempted >= 0 {
		// Money failure rolled back only money writes. Advance in a separate short
		// transaction, checking both the success anchor and the observed generation.
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		retryJob, retryErr := s.locked(persist, id, func(j *UpstreamJob, _ UpstreamJobTx) error {
			if j.Terminal() || j.ChargedUsageLogID != nil || j.AttemptCount != attempted {
				return nil
			}
			now := s.now().UTC()
			j.NextPollAt = now.Add(5 * time.Second * time.Duration(1<<min(j.AttemptCount-1, 6)))
			j.ErrorCode = "settlement_pending"
			j.ErrorMessage = "Settlement failed; durable receipt queued for retry"
			if j.AttemptCount >= 20 || !now.Before(j.LastPolledAt.Add(24*time.Hour)) {
				s.finish(j, "failed", "settlement_needs_review", "Automatic settlement exhausted; receipt retained for manual review")
			}
			return nil
		})
		if retryErr == nil {
			job = retryJob
		}
		err = errors.Join(err, retryErr)
	}
	return job, err
}

// PollDue is the bounded restart-safe scan used by the existing timing wheel.
func (s *UpstreamJobService) PollDue(ctx context.Context, limit int) error {
	ctx, release, err := s.RegisterQianwenRealtime(ctx)
	if err != nil {
		return nil
	}
	defer release()
	if _, err := s.Expire(ctx); err != nil {
		return err
	}
	ids, err := s.repo.Due(ctx, s.now(), limit)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !s.Ready() {
			break
		}
		job, pollErr := s.Poll(ctx, id)
		if pollErr != nil {
			if job == nil {
				job = &UpstreamJob{PublicRequestID: id}
			}
			logJobEvent(ctx, job, billingFailureCategory(pollErr))
		}
		err = errors.Join(err, pollErr)
		if ctx.Err() != nil {
			break
		}
	}
	return err
}

// Settle consumes trusted upstream success evidence; this is an internal service
// contract, never a client-facing endpoint. Prices come only from the stored claim.
func (s *UpstreamJobService) Settle(ctx context.Context, id string, evidence UpstreamJobUpdate) (*UpstreamJob, error) {
	return s.locked(ctx, id, func(j *UpstreamJob, tx UpstreamJobTx) error {
		if j.Terminal() || j.ChargedUsageLogID != nil {
			return nil
		}
		if !j.ExpiresAt.After(s.now()) {
			s.finish(j, "expired", "expired", "Task expired")
			return nil
		}
		if evidence.Status != "succeeded" || evidence.TaskID == "" || evidence.TaskID != j.UpstreamTaskID {
			return errors.New("matching upstream success evidence is required")
		}
		return s.advance(ctx, j, tx, evidence)
	})
}

func (s *UpstreamJobService) settle(ctx context.Context, j *UpstreamJob, tx UpstreamJobTx) error {
	if j.ChargedUsageLogID != nil {
		return nil
	}
	if j.Platform == PlatformOpenAI && j.Family == "video" && j.Billing.AdmissionSeconds > 0 {
		usage := gjson.ParseBytes(j.UsageJSON)
		seconds := usage.Get("billable_seconds")
		if !seconds.Exists() {
			seconds = usage.Get("seconds")
		}
		if seconds.Float() > float64(j.Billing.AdmissionSeconds) {
			logJobEvent(ctx, j, "needs_review")
			j.ResultRef = ""
			s.finish(j, "failed", "unsupported_usage", "Video exceeded its admitted duration")
			return nil
		}
	}
	log, cmd, err := s.charge(j)
	if errors.Is(err, ErrUpstreamJobUnsupportedUsage) || errors.Is(err, ErrUpstreamJobTariff) {
		logJobEvent(ctx, j, "needs_review")
		if kaggleVideoAllowsUnpricedAdmission(j) {
			j.ResultRef = ""
		}
		s.finish(j, "failed", "unsupported_usage", "Usage cannot be priced; evidence retained for review")
		return nil
	}
	if err != nil {
		return err
	}
	if err = tx.Charge(ctx, log, cmd); err != nil {
		logJobEvent(ctx, j, billingFailureCategory(err))
		return err
	}
	j.ChargedUsageLogID = &log.ID
	s.finish(j, "succeeded", "", "")
	return nil
}

func (s *UpstreamJobService) advance(ctx context.Context, j *UpstreamJob, tx UpstreamJobTx, u UpstreamJobUpdate) error {
	if (j.Family == "batch" || (j.Family == "video" && j.Platform == PlatformQianwen)) && u.ProviderStatus != "" {
		j.ResourceState.ProviderStatus = u.ProviderStatus
	}
	if len(u.Usage) > 0 {
		j.UsageJSON = append(json.RawMessage(nil), u.Usage...)
	}
	j.ResultRef = u.ResultRef
	switch u.Status {
	case "submitted", "running":
		if j.Status != "running" || u.Status == "running" {
			j.Status = u.Status
		}
		j.ErrorCode = ""
		j.ErrorMessage = ""
	case "succeeded":
		return s.settle(ctx, j, tx)
	case "failed":
		s.finish(j, "failed", u.ErrorCode, u.ErrorMessage)
	case "expired":
		s.finish(j, "expired", "upstream_expired", "Upstream task expired")
	default:
		s.finish(j, "failed", "unsupported_status", "Unsupported upstream task status")
	}
	return nil
}
func (s *UpstreamJobService) finish(j *UpstreamJob, status, code, message string) {
	now := s.now().UTC()
	j.Status = status
	j.ErrorCode = code
	j.ErrorMessage = message
	j.FinishedAt = &now
}
func (s *UpstreamJobService) Expire(ctx context.Context) (int64, error) {
	return s.repo.Expire(ctx, s.now())
}

func (s *UpstreamJobService) locked(ctx context.Context, id string, fn func(*UpstreamJob, UpstreamJobTx) error) (*UpstreamJob, error) {
	charged := false
	job, err := s.repo.WithLocked(ctx, id, func(j *UpstreamJob, tx UpstreamJobTx) error {
		before := j.ChargedUsageLogID
		err := fn(j, tx)
		charged = before == nil && j.ChargedUsageLogID != nil
		return err
	})
	if err == nil && charged && s.afterSettle != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.afterSettle(cacheCtx, job)
	}
	return job, err
}

// Keep the submission gate at the common job entry point. These holds protect
// admission on the parent execution; terminal jobs still use their own ID.
func admitUpstreamJobBudget(ctx context.Context, job *UpstreamJob, body []byte, bound bool) error {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil || plan.store == nil || job.Family != "video" && job.Family != "batch" {
		return nil
	}
	amount := upstreamJobBudgetAmount(plan, job, body, bound)
	if job.Billing.SubscriptionID == nil && plan.settlement != nil {
		if err := plan.settlement.cache.checkBalanceUpperBound(ctx, plan.execution.Scope.FundingUserID, amount); err != nil {
			return err
		}
	}
	// An unknown estimate must go through the project hard-budget policy.
	if project, ok := plan.projectBudget.(interface {
		CheckProjectBudget(context.Context, scope.Authorized, string, *money.Money) error
	}); ok {
		if err := project.CheckProjectBudget(ctx, plan.budgetScope, plan.execution.RequestID, amount); err != nil {
			return err
		}
	}
	if !job.Billing.KeyPeriodBudget || bound {
		if plan.settlement == nil {
			return nil
		}
		keys, ok := plan.settlement.authCacheInvalidator.(interface {
			BudgetState(context.Context, int64) (*APIKeyBudgetState, error)
		})
		if !ok {
			return nil
		}
		state, err := keys.BudgetState(ctx, job.APIKeyID)
		if err != nil {
			return err
		}
		if state == nil || !state.Policy.IsActive() || state.LimitUSD <= 0 {
			return nil
		}
		if amount == nil {
			return ErrAPIKeyBudgetUnavailable
		}
		if decimal.RequireFromString(amount.String()).GreaterThan(decimal.NewFromFloat(state.RemainingAfterReservations())) {
			return ErrAPIKeyBudgetExceeded
		}
		return nil
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	estimate := RequestBudgetEstimate{Model: job.Billing.Model, frozenJob: true,
		frozenAmount: amount, SkipBalanceHold: job.Billing.SubscriptionID != nil}
	return plan.reserveKeyBudgetLocked(ctx, estimate, amount)
}

func upstreamJobBudgetAmount(plan *GatewayRequestPlan, job *UpstreamJob, body []byte, bound bool) *money.Money {
	billing := job.Billing
	one := decimal.NewFromInt(1)
	if job.Family == "video" {
		if kaggleVideoAllowsUnpricedAdmission(job) {
			cost, err := frozenPerRequestVideoCost(billing)
			if err != nil {
				return nil
			}
			return budgetAmount(decimal.NewFromFloat(cost.ActualCost), one)
		}
		seconds := int64(billing.AdmissionSeconds)
		if job.Platform == PlatformQianwen {
			seconds = gjson.GetBytes(body, "parameters.duration").Int()
			if seconds < 5 {
				seconds = 5
			}
			if bound {
				seconds = 5
			} else if isQianwenVaceVideoModel(billing.Model) || strings.Contains(billing.Model, "-r2v") {
				// Source-video duration contributes to the invoice.
				return nil
			}
		}
		if seconds <= 0 {
			return nil
		}
		return budgetAmount(decimal.NewFromInt(seconds).Mul(decimal.NewFromFloat(billing.UnitPrice)).
			Mul(decimal.NewFromFloat(billing.RateMultiplier)), one)
	}
	batch := billing.ResourceBatch
	if batch == nil || batch.RequestCount <= 0 {
		return nil
	}
	if batch.PricingMode == GatewayResourcePricingModeFree {
		return budgetAmount(decimal.Zero, one)
	}
	if batch.TokenPrices == nil || plan.registrySnapshot == nil {
		return nil
	}
	record, ok := plan.registrySnapshot.CapabilitySnapshot().LookupExact(job.Platform, batch.UpstreamModel)
	if !ok {
		return nil
	}
	var output int64
	if batch.Endpoint != CompositeRouteEndpointEmbeddings && batch.Endpoint != "/v1/embeddings" {
		if record.MaxOutputTokens == nil || *record.MaxOutputTokens <= 0 {
			return nil
		}
		output = *record.MaxOutputTokens
	}
	input := record.ContextWindow
	if record.MaxInputTokens != nil && *record.MaxInputTokens > 0 {
		input = *record.MaxInputTokens
	}
	if input <= 0 {
		return nil
	}
	rates := batch.TokenPrices
	inputRate := rates.InputPricePerToken
	for _, rate := range []float64{rates.InputPricePerTokenPriority, rates.CacheReadPricePerToken,
		rates.CacheReadPricePerTokenPriority, rates.CacheCreationPricePerToken, rates.CacheCreationPricePerTokenPriority,
		rates.CacheCreation5mPrice, rates.CacheCreation1hPrice, rates.ImageInputPricePerToken, rates.ImageCacheReadPricePerToken} {
		inputRate = math.Max(inputRate, rate)
	}
	outputRate := math.Max(math.Max(rates.OutputPricePerToken, rates.OutputPricePerTokenPriority), rates.ImageOutputPricePerToken)
	if !validBillingNumber(inputRate) || !validBillingNumber(outputRate) || !finitePositive(billing.RateMultiplier) {
		return nil
	}
	quote := decimal.NewFromInt(input).Mul(decimal.NewFromFloat(inputRate)).Mul(decimal.NewFromFloat(math.Max(1, rates.LongContextInputMultiplier))).
		Add(decimal.NewFromInt(output).Mul(decimal.NewFromFloat(outputRate)).Mul(decimal.NewFromFloat(math.Max(1, rates.LongContextOutputMultiplier))))
	return budgetAmount(quote.Mul(decimal.NewFromInt(int64(batch.RequestCount))).Mul(decimal.NewFromFloat(billing.RateMultiplier)), one)
}

func frozenPerRequestVideoCost(billing UpstreamJobBilling) (*CostBreakdown, error) {
	var snapshot PricingSnapshot
	if json.Unmarshal([]byte(billing.PricingSnapshot), &snapshot) != nil ||
		snapshot.Mode != string(BillingModePerRequest) || snapshot.Model != billing.Model ||
		snapshot.Units != 1 || snapshot.UnitPrice == nil ||
		(snapshot.Source != PricingSourceGroup && snapshot.Source != PricingSourceChannel) ||
		(billing.PricingState != PricingStatePriced && billing.PricingState != PricingStateFree) {
		return nil, ErrUpstreamJobTariff
	}
	cost, err := RecalculatePricingSnapshot(billing.PricingSnapshot)
	if err != nil || cost.PricingState != billing.PricingState ||
		(billing.PricingState == PricingStateFree && *snapshot.UnitPrice != 0) {
		return nil, ErrUpstreamJobTariff
	}
	return cost, nil
}
