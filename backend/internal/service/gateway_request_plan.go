package service

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

// GatewayRequestSpec contains values, never hydrated identities or credentials.
// Route describes the inbound contract; attempts retain their own upstream route.
type GatewayRequestSpec struct {
	Registration                                      *journal.Registration
	UpstreamAccountID                                 int64
	WorkspaceID                                       int64
	Action, UpstreamModel, ExecutionKind, ExecutionID string
	UserID, APIKeyID, BillingGroupID                  int64
	Route, Protocol, Model                            string
	MaxAccountSwitches                                int
	MaxAccountSwitchesGemini                          int
	GenerationAttempts                                int
	Deadline                                          time.Time
	PricingAt                                         time.Time
	PriceVersion, ModelVersion                        string
}

// GatewayRequestState deliberately separates client completion from settlement.
// ReceiptState is empty until persistence acknowledges a durable receipt.
type GatewayRequestState struct {
	UsageRecoveryRequired                              bool
	Finalized, Canceled, ClientCommitted, UsageClaimed bool
	ReceiptState                                       UsageSettlementState
	ReceiptKey                                         UsageSettlementKey
	ResultAccountID                                    int64
}

type gatewayRequestPlanKey struct{}

type ProjectBudgetCoordinator interface {
	ReserveProjectBudget(context.Context, scope.Authorized, string, *money.Money) (bool, error)
	ReleaseProjectBudget(context.Context, scope.Authorized, string) error
}

type projectBudgetKey struct{}

func WithProjectBudgetCoordinator(ctx context.Context, coordinator ProjectBudgetCoordinator) context.Context {
	if coordinator == nil {
		return ctx
	}
	return context.WithValue(ctx, projectBudgetKey{}, coordinator)
}

// GatewayRequestPlan owns one HTTP execution. A websocket adapter must create a
// new plan per turn, never reuse the connection's usage claim across turns.
type GatewayRequestPlan struct {
	store            journal.Store
	execution        journal.AdmittedExecution
	response         *lifecycle.Response
	admissionErr     error
	journalErr       error
	abandoned        map[int64]bool
	registrySnapshot *ModelRegistrySnapshot
	spec             GatewayRequestSpec
	forwarding       *gatewayForwardingSettingsResult
	channels         *ChannelService
	channelSnapshot  *channelCache
	channelError     error
	billing          *BillingService
	settlement       *UsageSettlementService
	budget           *AttemptBudget
	mu               sync.Mutex
	state            GatewayRequestState
	leases           map[*gatewayPlanLease]struct{}
	usage            atomic.Bool
	traceEmitted     atomic.Bool
	// rateLimitCounted 是本逻辑请求的用户级 RPM 计数名额；rateLimitScopes 是已经
	// 计过数的分组维度。重试/账号切换/同组兜底复用同一个请求计划，因此不会把
	// 同一个逻辑请求重复计成多次（T3-2.13）。
	rateLimitCounted atomic.Bool
	rateLimitScopes  map[int64]struct{}
	projectBudget    ProjectBudgetCoordinator
	budgetScope      scope.Authorized
	projectRef       string
	projectHold      *money.Money
	projectReleased  bool
	budgetRate       func(context.Context, int64, int64, float64) float64
	keyBudgetAmount  *money.Money
	budgetHold       *KeyBudgetReservation
	budgetAdmitted   bool
	tpmAttempts      map[int]*tpmPlanAttempt
	tpmSequence      int
	tpmRejection     error
	catalogRepo      AccountRepository
	affinityState    *AffinityBindingState
	bindingAccountID int64
	binding          func(context.Context) error
}

type gatewayPlanLease struct {
	once    sync.Once
	release func()
}

func NewGatewayRequestPlan(spec GatewayRequestSpec) *GatewayRequestPlan {
	return newGatewayRequestPlanWithRegistry(spec, NewModelRegistry())
}

func newGatewayRequestPlanWithRegistry(spec GatewayRequestSpec, registry *ModelRegistry) *GatewayRequestPlan {
	snapshot := registry.Snapshot()
	spec.ModelVersion = snapshot.Version()
	return &GatewayRequestPlan{registrySnapshot: snapshot, spec: spec, leases: make(map[*gatewayPlanLease]struct{})}
}

func (p *GatewayRequestPlan) ModelRegistrySnapshot() *ModelRegistrySnapshot {
	return p.registrySnapshot
}

func (p *GatewayRequestPlan) Snapshot() GatewayRequestSpec { return p.spec }

// Attempts reports the existing ingress root budget; a plan never creates a
// second allowance for adapter changes, account switches or detached work.
func (p *GatewayRequestPlan) Attempts() []AttemptRecord { return p.budget.Snapshot() }

func GatewayRequestPlanFromContext(ctx context.Context) *GatewayRequestPlan {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(gatewayRequestPlanKey{}).(*GatewayRequestPlan)
	return p
}

func WithGatewayRequestPlan(ctx context.Context, p *GatewayRequestPlan) context.Context {
	if p != nil && p.affinityState != nil {
		ctx = context.WithValue(ctx, affinityBindingStateKey{}, p.affinityState)
	}
	if p != nil {
		ctx = WithModelRegistrySnapshot(ctx, p.registrySnapshot)
		if p.store != nil {
			ctx = attempt.WithRecorder(ctx, p)
		}
	}
	return context.WithValue(ctx, gatewayRequestPlanKey{}, p)
}

// Acquire adopts an already-acquired adapter lease. Late acquisitions after
// cancellation/finalization are released immediately; callbacks run outside locks.
func (p *GatewayRequestPlan) Acquire(release func()) func() {
	if release == nil {
		return nil
	}
	lease := &gatewayPlanLease{release: release}
	p.mu.Lock()
	done := p.state.Finalized || p.state.Canceled
	if !done {
		p.leases[lease] = struct{}{}
	}
	p.mu.Unlock()
	finish := func() {
		lease.once.Do(func() {
			p.mu.Lock()
			delete(p.leases, lease)
			p.mu.Unlock()
			lease.release()
		})
	}
	if done {
		finish()
	}
	return finish
}

func (p *GatewayRequestPlan) finish(canceled, committed bool) {
	p.mu.Lock()
	p.state.Finalized = true
	p.state.Canceled = p.state.Canceled || canceled
	p.state.ClientCommitted = p.state.ClientCommitted || committed
	leases := p.leases
	p.leases = make(map[*gatewayPlanLease]struct{})
	hold := p.budgetHold
	// Once a financial producer owns the request, settlement (including
	// recovery) consumes the hold. Pending work must still count as reserved.
	// An unresolved receipt persisted in needs_review does not enter the
	// settlement outbox and must release its pre-hold on finalization.
	release := p.state.ReceiptState == UsageSettlementNeedsReview || (!p.usage.Load() && p.state.ReceiptState == "")
	// A context cancellation may race the adapter handing its observed usage
	// to the worker. Only the final adapter unwind can prove there is no producer.
	project := release && p.projectHold != nil && !p.projectReleased
	if project {
		p.projectReleased = true
	}
	p.mu.Unlock()
	for lease := range leases {
		lease.once.Do(lease.release)
	}
	// A hold that never settled must be refunded here; the expiry sweep is only
	// the crash fallback. A settled receipt already consumed its hold.
	if hold != nil && release {
		p.releaseBudgetHold(hold)
	}
	if project {
		p.releaseProjectBudget()
	}
	p.finalizeTPMAttempts()
	if p.store != nil && p.admissionErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.logJournalError(p.store.Finish(ctx, p.execution.RequestID, p.response.State(), canceled))
	}
}

// releaseBudgetHold refunds an unconsumed request hold with a detached context:
// the request context is usually already canceled at this point.
func (p *GatewayRequestPlan) releaseBudgetHold(hold *KeyBudgetReservation) {
	if p == nil || hold == nil || p.settlement == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.settlement.ReleaseKeyBudgetReservation(ctx, hold.APIKeyID, hold.RequestID); err != nil {
		slog.Warn("release request budget hold", "api_key_id", hold.APIKeyID, "request_id", hold.RequestID, "error", err)
	}
}

func (p *GatewayRequestPlan) bindBudgetReceipt(command *UsageBillingCommand) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.projectHold != nil {
		command.ProjectReservationRef = p.projectRef
		command.ProjectWorkspaceID, command.ProjectID = p.budgetScope.WorkspaceID, p.budgetScope.ProjectID
		command.Estimate, command.Reserved = p.projectHold, p.projectHold
	}
	if hold := p.budgetHold; hold != nil {
		command.BudgetReservationID = hold.ID
		if command.Reserved == nil && p.keyBudgetAmount != nil {
			command.Estimate, command.Reserved = p.keyBudgetAmount, p.keyBudgetAmount
		}
		if command.Reserved == nil {
			amount, err := money.NewMoney("USD", quantizedBillingDecimal(hold.HoldAmount).StringFixed(UsageBillingMonetaryScale))
			if err == nil {
				command.Estimate, command.Reserved = &amount, &amount
			}
		}
	}
}

func (p *GatewayRequestPlan) releaseProjectBudget() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.projectBudget.ReleaseProjectBudget(ctx, p.budgetScope, p.projectRef); err != nil {
		slog.Warn("release project budget hold", "request_id", p.spec.ExecutionID, "error", err)
		p.mu.Lock()
		p.projectReleased = false
		p.mu.Unlock()
	}
}

// RequestBudgetEstimate is the bounded cost estimate used to reserve budget
// before a request is forwarded. Token counts are upper bounds, never usage.
type RequestBudgetEstimate struct {
	Model                        string
	Group                        *Group
	Meter                        *BudgetMeter
	ServiceTier, ReasoningEffort string
	InputTokens                  int
	OutputTokens                 int
	CacheCreationTokens          int
	RateMultiplier               float64
	// SkipBalanceHold keeps the reservation on the period budget only.
	SkipBalanceHold bool
	SkipKeyBudget   bool
}

// Conservative token bounds keep the reservation meaningful when a caller
// cannot supply a bound; settlement still adjusts to the real cost.
const (
	defaultBudgetEstimateInputTokens  = 1024
	defaultBudgetEstimateOutputTokens = 4096
)

// NormalizeBudgetEstimateBounds supplies defaults for token-priced requests.
func NormalizeBudgetEstimateBounds(estimate RequestBudgetEstimate) (inputTokens, outputTokens int, multiplier float64) {
	inputTokens = estimate.InputTokens
	if inputTokens <= 0 {
		inputTokens = defaultBudgetEstimateInputTokens
	}
	outputTokens = estimate.OutputTokens
	if outputTokens <= 0 {
		outputTokens = defaultBudgetEstimateOutputTokens
	}
	multiplier = estimate.RateMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	return inputTokens, outputTokens, multiplier
}

// ClaimUserRateLimitCount 领取本逻辑请求的用户级 RPM 计数名额。
// 同一个请求计划只成功一次：重试、账号切换、分组兜底都不会把用户的逻辑请求
// 重复计进用户级计数器。没有请求计划（如单测/非网关调用）时返回 true，
// 保持既有的一次一计语义。
func (p *GatewayRequestPlan) ClaimUserRateLimitCount() bool {
	if p == nil {
		return true
	}
	return p.rateLimitCounted.CompareAndSwap(false, true)
}

// ClaimRateLimitScope 领取 (逻辑请求, 分组) 维度的 RPM 计数名额。
// 兜底到另一个分组时那个分组的容量确实被占用，所以按分组分别计数；
// 同一分组内的重试不会重复计数。
func (p *GatewayRequestPlan) ClaimRateLimitScope(groupID int64) bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rateLimitScopes == nil {
		p.rateLimitScopes = make(map[int64]struct{}, 1)
	}
	if _, counted := p.rateLimitScopes[groupID]; counted {
		return false
	}
	p.rateLimitScopes[groupID] = struct{}{}
	return true
}

// BudgetHold returns the live request hold, if any. The settlement path reads it
// to consume the hold instead of deducting the balance twice.
func (p *GatewayRequestPlan) BudgetHold() *KeyBudgetReservation {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.budgetHold
}

// BudgetAdmitted reports whether admission was already decided for this plan.
func (p *GatewayRequestPlan) BudgetAdmitted() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.budgetAdmitted
}

func (p *GatewayRequestPlan) HasProjectBudgetCoordinator() bool {
	return p != nil && p.projectBudget != nil
}

// ReserveBudget admits one request against its configured project and key budgets. It is idempotent per plan: failover retries reuse the first hold.
//
// The estimate is priced with the plan's frozen pricing snapshot, so a price
// reload cannot change what this request reserved.
func (p *GatewayRequestPlan) ReserveBudget(ctx context.Context, estimate RequestBudgetEstimate) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.admissionErr != nil {
		return p.admissionErr
	}
	if p.budgetAdmitted {
		return nil
	}
	if estimate.SkipKeyBudget && p.projectBudget == nil {
		p.budgetAdmitted = true
		return nil
	}
	if p.state.Finalized || p.spec.APIKeyID <= 0 || p.spec.UserID <= 0 || strings.TrimSpace(p.spec.ExecutionID) == "" {
		return ErrAPIKeyBudgetUnavailable
	}
	amount := p.estimateBudgetHold(ctx, estimate)
	// Acquire the key hold first. If project admission fails the request's
	// finalizer refunds it, including after client cancellation.
	if !estimate.SkipKeyBudget && p.budgetHold == nil {
		if amount == nil && p.settlement != nil && GatewayBudgetModelUnknown(p, estimate) {
			return ErrGatewayBudgetModelNotFound
		}
		if p.settlement == nil || amount == nil {
			return ErrAPIKeyBudgetUnavailable
		}
		payer := p.spec.UserID
		if p.store != nil {
			payer = p.execution.Scope.FundingUserID
		}
		outcome, err := p.settlement.reserveBudgetAmount(ctx, &KeyBudgetReservationCommand{
			RequestID: p.spec.ExecutionID, APIKeyID: p.spec.APIKeyID, UserID: payer,
			SkipBalanceHold: estimate.SkipBalanceHold,
		}, *amount)
		if err != nil {
			return err
		}
		if outcome != nil {
			p.budgetHold = outcome.Reservation
			p.keyBudgetAmount = amount
		}
	}
	if p.projectBudget != nil {
		if p.projectRef == "" {
			p.projectRef = p.execution.RequestID
			if p.projectRef == "" {
				p.projectRef = journal.NewID()
			}
		}
		held, err := p.projectBudget.ReserveProjectBudget(ctx, p.budgetScope, p.projectRef, amount)
		if held {
			// A lost commit acknowledgement still needs a scoped release on failure.
			p.projectHold = amount
		}
		if err != nil {
			return err
		}
	}
	p.budgetAdmitted = true
	return nil
}

func (p *GatewayRequestPlan) Finalize(clientCommitted bool) { p.finish(false, clientCommitted) }
func (p *GatewayRequestPlan) Cancel()                       { p.finish(true, false) }

// Record claims the financial producer before dispatch, including after a client
// disconnect. Failure is recovered from the durable receipt, not by a second
// producer. It makes no assertion that the task has persisted or settled usage.
func (p *GatewayRequestPlan) Record() bool { return p.usage.CompareAndSwap(false, true) }

func (p *GatewayRequestPlan) State() GatewayRequestState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.state
	state.UsageClaimed = p.usage.Load()
	return state
}

func (p *GatewayRequestPlan) setReceiptState(state UsageSettlementState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state.ReceiptState != UsageSettlementSettled {
		p.state.ReceiptState = state
	}
}

func (p *GatewayRequestPlan) acknowledgeReceipt(receipt *UsageSettlementReceipt) {
	p.mu.Lock()
	p.state.ReceiptKey = receipt.Key
	p.state.ResultAccountID = receipt.Command.AccountID
	p.mu.Unlock()
	p.setReceiptState(receipt.State)
}

// Capture before invoking any adapter. Price publications replace the complete
// map under the pricing lock; the captured map and capability snapshot remain
// immutable across reloads. No locks or background updater are copied.
func captureGatewayRequestPlan(ctx context.Context, spec GatewayRequestSpec, settings *SettingService, billing *BillingService, channels *ChannelService) (context.Context, *GatewayRequestPlan) {
	if existing := GatewayRequestPlanFromContext(ctx); existing != nil {
		return ctx, existing
	}
	if spec.PricingAt.IsZero() {
		spec.PricingAt = time.Now()
	}
	if spec.ExecutionKind == "" {
		spec.ExecutionKind = "request"
	}
	if spec.ExecutionID == "" {
		spec.ExecutionID = resolveUsageBillingRequestID(ctx, "")
	}
	if spec.Action == "" {
		spec.Action = gatewayRequiredAction(ctx)
		if spec.Action == "" {
			spec.Action = spec.Route
		}
	}
	p := NewGatewayRequestPlan(spec)
	ConfigureAttemptBudget(ctx, spec.MaxAccountSwitches)
	p.budget = AttemptBudgetFromContext(ctx)
	if p.budget != nil {
		p.budget.mu.Lock()
		p.spec.GenerationAttempts, p.spec.Deadline = p.budget.limit, p.budget.deadline
		p.budget.mu.Unlock()
	}
	if channels != nil {
		p.channels = channels
		p.channelSnapshot, p.channelError = channels.loadCache(ctx)
	}
	if settings != nil {
		settings := settings.getGatewayForwardingSettingsCached(ctx)
		p.forwarding = &settings
	}
	p.billing = billing
	if billing != nil && billing.pricingService != nil {
		prices := billing.pricingService
		prices.mu.RLock()
		frozen := &PricingService{cfg: prices.cfg, pricingData: prices.pricingData, registrySnapshot: p.registrySnapshot, localHash: prices.localHash, customFilesHash: prices.customFilesHash, lastUpdated: prices.lastUpdated}
		p.spec.PriceVersion = prices.localHash + ":" + prices.customFilesHash
		prices.mu.RUnlock()
		p.billing = &BillingService{cfg: billing.cfg, pricingService: frozen, fallbackPrices: billing.fallbackPrices}
	}
	ctx = WithAffinityBindingState(ctx)
	p.affinityState = AffinityBindingStateFromContext(ctx)
	return WithGatewayRequestPlan(ctx, p), p
}

type gatewayPlanFactory struct {
	settings   *SettingService
	billing    *BillingService
	channels   *ChannelService
	settlement *UsageSettlementService
	catalog    AccountRepository
	budgetRate func(context.Context, int64, int64, float64) float64
}

func (f gatewayPlanFactory) capture(ctx context.Context, spec GatewayRequestSpec) (context.Context, *GatewayRequestPlan) {
	if existing := GatewayRequestPlanFromContext(ctx); existing != nil {
		return ctx, existing
	}
	ctx, p := captureGatewayRequestPlan(ctx, spec, f.settings, f.billing, f.channels)
	p.settlement, p.catalogRepo, p.budgetRate = f.settlement, f.catalog, f.budgetRate
	return p.admitExecution(ctx, spec.ExecutionID)
}

func (s *GatewayService) CaptureRequestPlan(ctx context.Context, spec GatewayRequestSpec) (context.Context, *GatewayRequestPlan) {
	var factory gatewayPlanFactory
	if s != nil {
		factory = gatewayPlanFactory{s.settingService, s.billingService, s.channelService, s.usageSettlement, s.accountRepo, s.ResolveUserGroupRateMultiplier}
	}
	return factory.capture(ctx, spec)
}

func (s *OpenAIGatewayService) CaptureRequestPlan(ctx context.Context, spec GatewayRequestSpec) (context.Context, *GatewayRequestPlan) {
	var factory gatewayPlanFactory
	if s != nil {
		factory = gatewayPlanFactory{s.settingService, s.billingService, s.channelService, s.usageSettlement, s.accountRepo, s.ResolveUserGroupRateMultiplier}
	}
	return factory.capture(ctx, spec)
}

func (p *GatewayRequestPlan) admitExecution(ctx context.Context, legacyID string) (context.Context, *GatewayRequestPlan) {
	if owner, ok := scope.From(ctx); ok && owner.ProjectID > 0 && owner.KeyID == p.spec.APIKeyID &&
		owner.KeyOwnerID == p.spec.UserID && (p.spec.WorkspaceID == 0 || p.spec.WorkspaceID == owner.WorkspaceID) {
		p.projectBudget, _ = ctx.Value(projectBudgetKey{}).(ProjectBudgetCoordinator)
		p.budgetScope = owner
	}
	p.store = journal.ForAdmission(ctx)
	if p.store == nil {
		return ctx, p
	}
	frozen, ok := scope.From(ctx)
	if !ok || frozen.KeyID != p.spec.APIKeyID || frozen.KeyOwnerID != p.spec.UserID || (p.spec.WorkspaceID != 0 && frozen.WorkspaceID != p.spec.WorkspaceID) {
		p.admissionErr = journal.ErrScope
		return ctx, p
	}
	if legacyID == "" && p.spec.ExecutionKind != "job" {
		legacyID = p.spec.ExecutionID
	}
	p.response = p.budget.Response()
	if p.response == nil {
		p.admissionErr = journal.ErrDispatch
		return ctx, p
	}
	p.spec.WorkspaceID = frozen.WorkspaceID
	route := p.spec.Route
	if route == "" {
		route = p.spec.Action
	}
	in := journal.Admission{RequestID: journal.NewID(), Kind: p.spec.ExecutionKind, Route: route, Model: p.spec.Model, LegacyID: legacyID, AttemptBudget: p.spec.GenerationAttempts, Deadline: p.spec.Deadline,
		Versions: journal.Versions{Model: p.spec.ModelVersion, Price: p.spec.PriceVersion, PricingAt: p.spec.PricingAt}, Registration: p.spec.Registration}
	var replay bool
	p.execution, replay, p.admissionErr = p.store.Admit(ctx, in)
	if p.admissionErr == nil {
		p.spec.ExecutionID = p.execution.RequestID
	}
	if replay {
		p.admissionErr = journal.ErrReplay
	}
	return WithGatewayRequestPlan(ctx, p), p
}

func (p *GatewayRequestPlan) AdmittedExecution() (journal.AdmittedExecution, error) {
	if p == nil {
		return journal.AdmittedExecution{}, journal.ErrDispatch
	}
	return p.execution, p.admissionErr
}

func (p *GatewayRequestPlan) MarkTraceEmitted() {
	if p != nil {
		p.traceEmitted.Store(true)
	}
}

func (p *GatewayRequestPlan) HasTraceEmitted() bool {
	return p != nil && p.traceEmitted.Load()
}

func (p *GatewayRequestPlan) logJournalError(err error) {
	if err != nil {
		p.mu.Lock()
		p.journalErr = err
		p.mu.Unlock()
		slog.Error("execution journal write failed", "request_id", p.execution.RequestID, "error", err)
	}
}

func (p *GatewayRequestPlan) journalDispatch(ctx context.Context, accountID int64, number int) (journal.Attempt, error) {
	if p == nil || p.store == nil {
		return journal.Attempt{}, nil
	}
	p.mu.Lock()
	failed := p.journalErr
	p.mu.Unlock()
	if p.admissionErr != nil {
		return journal.Attempt{}, p.admissionErr
	}
	if failed != nil {
		return journal.Attempt{}, failed
	}
	if err := journal.Authorize(ctx, p.execution.Scope, p.spec.Model); err != nil {
		p.logJournalError(err)
		return journal.Attempt{}, err
	}
	a, err := p.store.Dispatch(ctx, p.execution, number, accountID)
	p.logJournalError(err)
	if err == nil && number == 1 && accountID > 0 && !p.HasTraceEmitted() {
		id := strconv.FormatInt(accountID, 10)
		rank := 1
		trace.Emit(ctx, trace.DecisionTrace{
			SchemaVersion:    1,
			Mode:             trace.Execution,
			SnapshotID:       journal.NewID(),
			AsOf:             time.Now().UTC(),
			CatalogVersion:   p.execution.Versions.Model,
			PriceVersion:     p.execution.Versions.Price,
			Candidates:       []trace.Candidate{{ID: id, State: "eligible", Rank: &rank}},
			Selected:         &trace.Selection{CandidateID: id, ModelID: p.spec.Model},
			ExecutionOutcome: "selected",
			SelectionLayer:   "gateway",
		})
	}
	return a, err
}

func (p *GatewayRequestPlan) observeJournalAttempt(a journal.Attempt, outcome string, status int) {
	if a.ID == "" {
		return
	}
	a.Outcome = outcome
	a.Acceptance = generationAcceptance(outcome, status)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.logJournalError(p.store.Observe(ctx, a))
}

func (p *GatewayRequestPlan) RecordAttemptDecision(ctx context.Context, d attempt.AttemptDecision) {
	if p.admissionErr != nil {
		return
	}
	if d.AbandonedUncertain {
		records := p.budget.Snapshot()
		if d.AttemptNumber > 0 && d.AttemptNumber <= len(records) {
			p.mu.Lock()
			if p.abandoned == nil {
				p.abandoned = make(map[int64]bool)
			}
			p.abandoned[records[d.AttemptNumber-1].AccountID] = true
			p.mu.Unlock()
		}
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	p.logJournalError(p.store.Decision(writeCtx, p.execution.RequestID, d))
}

func (p *GatewayRequestPlan) journalIdentity() string {
	if p == nil || p.store == nil || p.admissionErr != nil {
		return ""
	}
	return p.execution.RequestID
}

func (p *GatewayRequestPlan) journalReceiptAllowed(accountID int64) bool {
	if p == nil || p.store == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.admissionErr == nil && !p.abandoned[accountID]
}

// CaptureAsyncImageTaskPlan transfers an accepted task out of its submission
// request. Only this handoff creates a new root; retries retain the task's plan
// and allowance, and detached transports still obey the task's cancellation.
func (s *OpenAIGatewayService) CaptureAsyncImageTaskPlan(ctx context.Context, spec GatewayRequestSpec, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = context.WithoutCancel(ctx)
	ctx = context.WithValue(ctx, attemptBudgetKey{}, (*AttemptBudget)(nil))
	ctx = WithGatewayRequestPlan(ctx, nil)
	ctx = context.WithValue(ctx, affinityBindingStateKey{}, &AffinityBindingState{})
	ctx, cancel := WithAttemptBudget(ctx, 1+max(0, spec.MaxAccountSwitches), timeout)
	spec.ExecutionKind = "job"
	ctx, plan := s.CaptureRequestPlan(ctx, spec)
	stop := context.AfterFunc(ctx, plan.Cancel)
	return ctx, func() {
		stop()
		if ctx.Err() != nil {
			plan.Cancel()
		}
		plan.Finalize(false)
		cancel()
	}
}

func (s *BillingService) forGatewayRequest(ctx context.Context) *BillingService {
	if p := GatewayRequestPlanFromContext(ctx); p != nil && p.billing != nil {
		return p.billing
	}
	return s
}

// GenerateRequestSessionHash preserves the legacy algorithm when affinity is disabled.
// The authenticated request plan supplies scope; callers cannot derive it from JSON.
func (s *GatewayService) GenerateRequestSessionHash(ctx context.Context, parsed *ParsedRequest, explicit string) string {
	if s.cfg == nil || !s.cfg.Gateway.SessionAffinityEnabled {
		return s.GenerateSessionHash(parsed)
	}
	return scopedRequestSessionHash(ctx, s.cfg, parsed, explicit)
}
func scopedRequestSessionHash(ctx context.Context, cfg *config.Config, parsed *ParsedRequest, explicit string) string {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil || parsed == nil {
		return ""
	}
	spec := plan.Snapshot()
	if parsed.SessionContext == nil {
		parsed.SessionContext = &SessionContext{}
	}
	scope := parsed.SessionContext
	scope.OwnerID, scope.APIKeyID, scope.RouteScope = spec.UserID, spec.APIKeyID, spec.Route
	scope.AffinityHMACKey, scope.AffinityHMACKeyVersion = []byte(cfg.Gateway.AffinityHMACKey), cfg.Gateway.AffinityHMACKeyVersion
	group := spec.BillingGroupID
	parsed.GroupID = &group
	parsed.Model = spec.Model
	// Keep the inbound protocol even when a bridge has already converted the body.
	if explicit == "" && parsed.MetadataUserID != "" {
		if metadata := ParseMetadataUserID(parsed.MetadataUserID); metadata != nil {
			explicit = metadata.SessionID
		}
	}
	if len(explicit) > 512 || strings.ContainsAny(explicit, "\r\n\x00") {
		explicit = ""
	}
	return (&GatewayService{}).GenerateScopedSessionHash(parsed, explicit)
}
func (s *OpenAIGatewayService) GenerateRequestSessionHash(c *gin.Context, body []byte) string {
	if s.cfg == nil || !s.cfg.Gateway.SessionAffinityEnabled {
		return s.GenerateSessionHash(c, body)
	}
	if c == nil || c.Request == nil {
		return ""
	}
	plan := GatewayRequestPlanFromContext(c.Request.Context())
	if plan == nil {
		return ""
	}
	protocol := plan.Snapshot().Protocol
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), protocol)
	if err != nil {
		return ""
	}
	return scopedRequestSessionHash(c.Request.Context(), s.cfg, parsed, explicitOpenAIRequestSessionID(c, body))
}

func deferGatewayRequestBinding(ctx context.Context, accountID int64, bind func(context.Context) error) bool {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return false
	}
	plan.mu.Lock()
	plan.bindingAccountID, plan.binding = accountID, bind
	plan.mu.Unlock()
	return true
}
func admitGatewayRequestBinding(ctx context.Context, accountID int64) error {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return nil
	}
	plan.mu.Lock()
	bind := plan.binding
	if plan.bindingAccountID != accountID {
		bind = nil
	}
	plan.mu.Unlock()
	if bind != nil {
		if err := bind(WithAffinityAdmission(ctx)); err != nil {
			slog.Warn("gateway affinity binding unavailable", "account_id", accountID, "error", err)
		}
		return nil
	}
	return nil
}

// CaptureRequestTurn starts a separate logical WS request while retaining the
// connection's cancellation and existing attempt budget. Retries retain its plan.
func (s *OpenAIGatewayService) CaptureRequestTurn(ctx context.Context, spec GatewayRequestSpec) (context.Context, *GatewayRequestPlan) {
	spec.ExecutionKind = "turn"
	ctx = WithGatewayRequestPlan(ctx, nil)
	ctx = context.WithValue(ctx, affinityBindingStateKey{}, &AffinityBindingState{})
	return s.CaptureRequestPlan(ctx, spec)
}

// CaptureGatewayJobPlan gives an already-persisted job its own execution identity.
// It retains inbound pricing snapshots and allowance; retries cannot renew either.
func CaptureGatewayJobPlan(ctx context.Context, spec GatewayRequestSpec) (context.Context, func()) {
	spec.ExecutionKind = "job"
	parent := GatewayRequestPlanFromContext(ctx)
	ctx, cancel := WithAttemptBudget(ctx, 1, DefaultGenerationDeadline)
	ctx = WithGatewayRequestPlan(ctx, nil)
	if parent != nil {
		spec.MaxAccountSwitches = parent.spec.MaxAccountSwitches
		spec.WorkspaceID = parent.spec.WorkspaceID
		if spec.BillingGroupID == 0 {
			spec.BillingGroupID = parent.spec.BillingGroupID
		}
	}
	ctx, plan := captureGatewayRequestPlan(ctx, spec, nil, nil, nil)
	if parent != nil {
		plan.billing, plan.channels = parent.billing, parent.channels
		plan.settlement = parent.settlement
		plan.catalogRepo = parent.catalogRepo
		plan.channelSnapshot, plan.channelError = parent.channelSnapshot, parent.channelError
		plan.registrySnapshot = parent.registrySnapshot
		plan.spec.PriceVersion, plan.spec.ModelVersion = parent.spec.PriceVersion, parent.spec.ModelVersion
		plan.spec.PricingAt = parent.spec.PricingAt
		ctx = WithGatewayRequestPlan(ctx, plan)
	}
	ctx, plan = plan.admitExecution(ctx, spec.ExecutionID)
	return ctx, func() { plan.Finalize(false); cancel() }
}

// A transient scheduler veto can spill this request without destroying the
// session's recovery binding. Permanent revocation still uses T6's version CAS.
func requestAffinityPermanentlyInvalid(ctx context.Context, repo AccountRepository, accountID int64) bool {
	if repo == nil || accountID <= 0 {
		return false
	}
	account, err := repo.GetByID(ctx, accountID)
	if err != nil {
		return false
	}
	if account == nil || !catalogAccountEnabled(account) {
		return true
	}
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return false
	}
	parent, err := loadCatalogCredentialAccount(ctx, repo, account)
	if err != nil {
		return false
	}
	endpoint := gatewaySelectionCatalogEndpoint(ctx, account)
	if account.IsOpenAICompatible() {
		endpoint = openAICompatiblePlanCatalogEndpoint(ctx, account)
	}
	return !AccountSupportsResolvedModel(ctx, account, plan.Snapshot().Model, AccountModelBindingOptions{Endpoint: endpoint, CredentialAccount: parent})
}
func (s *GatewayService) clearRequestStickySession(ctx context.Context, groupID *int64, hash string) error {
	return clearRequestAffinityBinding(ctx, s.cache, s.accountRepo, derefGroupID(groupID), hash)
}

func clearRequestAffinityBinding(ctx context.Context, cache GatewayCache, repo AccountRepository, groupID int64, hash string) error {
	if cache == nil {
		return nil
	}
	if IsV2AffinityKey(hash) {
		accountID, err := cache.GetSessionAccountID(ctx, groupID, hash)
		if err != nil || !requestAffinityPermanentlyInvalid(ctx, repo, accountID) {
			return nil
		}
	}
	return cache.DeleteSessionAccountID(ctx, groupID, hash)
}
func (s *OpenAIGatewayService) clearRequestStickySession(ctx context.Context, groupID *int64, hash string) error {
	if IsV2AffinityKey(hash) {
		accountID, err := s.getStickySessionAccountID(ctx, groupID, hash)
		if err != nil || !requestAffinityPermanentlyInvalid(ctx, s.accountRepo, accountID) {
			return nil
		}
	}
	return s.deleteStickySessionAccountID(ctx, groupID, hash)
}
