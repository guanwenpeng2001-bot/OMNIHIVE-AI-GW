package service

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

// GatewayRequestSpec contains values, never hydrated identities or credentials.
// Route describes the inbound contract; attempts retain their own upstream route.
type GatewayRequestSpec struct {
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

// GatewayRequestPlan owns one HTTP execution. A websocket adapter must create a
// new plan per turn, never reuse the connection's usage claim across turns.
type GatewayRequestPlan struct {
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
	// rateLimitCounted 是本逻辑请求的用户级 RPM 计数名额；rateLimitScopes 是已经
	// 计过数的分组维度。重试/账号切换/同组兜底复用同一个请求计划，因此不会把
	// 同一个逻辑请求重复计成多次（T3-2.13）。
	rateLimitCounted atomic.Bool
	rateLimitScopes  map[int64]struct{}
	budgetHold       *KeyBudgetReservation
	budgetAdmitted   bool
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
	snapshot := NewModelRegistry().Snapshot()
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
	settled := p.state.ReceiptState == UsageSettlementSettled
	p.mu.Unlock()
	for lease := range leases {
		lease.once.Do(lease.release)
	}
	// A hold that never settled must be refunded here; the expiry sweep is only
	// the crash fallback. A settled receipt already consumed its hold.
	if hold != nil && !settled {
		p.releaseBudgetHold(hold)
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

// RequestBudgetEstimate is the bounded cost estimate used to reserve budget
// before a request is forwarded. Token counts are upper bounds, never usage.
type RequestBudgetEstimate struct {
	Model          string
	InputTokens    int
	OutputTokens   int
	RateMultiplier float64
	// SkipBalanceHold keeps the reservation on the period budget only.
	SkipBalanceHold bool
}

// Conservative token bounds keep the reservation meaningful when a caller
// cannot supply a bound; settlement still adjusts to the real cost.
const (
	defaultBudgetEstimateInputTokens  = 1024
	defaultBudgetEstimateOutputTokens = 4096
)

// NormalizeBudgetEstimateBounds 把调用方给出的估算收敛成预留用的有界上界。
//
// 这些是**上界**，不是用量：媒体/音频/异步 job 请求没有 token 用量，调用方给出的
// 上界为 0 时使用文档化的默认上界，绝不允许把图片/视频参数折算成 token 数冒充用量。
// 真正的计费始终来自结算阶段的实际上游用量。
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

// ReserveBudget admits one request against the key's period budget and payer
// balance. It is idempotent per plan: failover retries reuse the first hold.
//
// The estimate is priced with the plan's frozen pricing snapshot, so a price
// reload cannot change what this request reserved.
func (p *GatewayRequestPlan) ReserveBudget(ctx context.Context, estimate RequestBudgetEstimate) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.budgetAdmitted {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	if p.settlement == nil {
		return ErrAPIKeyBudgetUnavailable
	}
	spec := p.Snapshot()
	if spec.APIKeyID <= 0 || spec.UserID <= 0 || strings.TrimSpace(spec.ExecutionID) == "" {
		return ErrAPIKeyBudgetUnavailable
	}
	hold := p.estimateBudgetHold(estimate)
	outcome, err := p.settlement.ReserveKeyBudget(ctx, &KeyBudgetReservationCommand{
		RequestID:       spec.ExecutionID,
		APIKeyID:        spec.APIKeyID,
		UserID:          spec.UserID,
		HoldAmount:      hold,
		SkipBalanceHold: estimate.SkipBalanceHold,
	})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.budgetAdmitted = true
	if outcome != nil {
		p.budgetHold = outcome.Reservation
	}
	p.mu.Unlock()
	return nil
}

// estimateBudgetHold prices the request bound with the frozen snapshot. An
// unpriced model yields no hold: the existing pricing-error path owns that case
// and must not be replaced by an admission error here.
func (p *GatewayRequestPlan) estimateBudgetHold(estimate RequestBudgetEstimate) float64 {
	if p.billing == nil {
		return 0
	}
	input, output, multiplier := NormalizeBudgetEstimateBounds(estimate)
	cost, err := p.billing.CalculateCost(estimate.Model, UsageTokens{InputTokens: input, OutputTokens: output}, multiplier)
	if err != nil || cost == nil {
		return 0
	}
	hold := cost.ActualCost
	if hold < 0 || math.IsNaN(hold) || math.IsInf(hold, 0) {
		return 0
	}
	return QuantizeUsageBillingAmount(hold)
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
	if billing != nil && billing.pricingService != nil {
		prices := billing.pricingService
		prices.mu.RLock()
		frozen := &PricingService{cfg: prices.cfg, pricingData: prices.pricingData, registrySnapshot: p.registrySnapshot, localHash: prices.localHash, customFilesHash: prices.customFilesHash, lastUpdated: prices.lastUpdated}
		p.spec.PriceVersion = prices.localHash + ":" + prices.customFilesHash
		p.spec.ModelVersion = p.registrySnapshot.Version()
		prices.mu.RUnlock()
		p.billing = &BillingService{cfg: billing.cfg, pricingService: frozen, fallbackPrices: billing.fallbackPrices}
	}
	ctx = WithAffinityBindingState(ctx)
	p.affinityState = AffinityBindingStateFromContext(ctx)
	return WithGatewayRequestPlan(ctx, p), p
}

func (s *GatewayService) CaptureRequestPlan(ctx context.Context, spec GatewayRequestSpec) (context.Context, *GatewayRequestPlan) {
	if s == nil {
		return captureGatewayRequestPlan(ctx, spec, nil, nil, nil)
	}
	if existing := GatewayRequestPlanFromContext(ctx); existing != nil {
		return ctx, existing
	}
	ctx, plan := captureGatewayRequestPlan(ctx, spec, s.settingService, s.billingService, s.channelService)
	plan.settlement = s.usageSettlement
	plan.catalogRepo = s.accountRepo
	return ctx, plan
}

func (s *OpenAIGatewayService) CaptureRequestPlan(ctx context.Context, spec GatewayRequestSpec) (context.Context, *GatewayRequestPlan) {
	if s == nil {
		return captureGatewayRequestPlan(ctx, spec, nil, nil, nil)
	}
	if existing := GatewayRequestPlanFromContext(ctx); existing != nil {
		return ctx, existing
	}
	ctx, plan := captureGatewayRequestPlan(ctx, spec, s.settingService, s.billingService, s.channelService)
	plan.settlement = s.usageSettlement
	plan.catalogRepo = s.accountRepo
	return ctx, plan
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
	}
	ctx, plan := captureGatewayRequestPlan(ctx, spec, nil, nil, nil)
	if parent != nil {
		plan.billing, plan.channels = parent.billing, parent.channels
		plan.settlement = parent.settlement
		plan.channelSnapshot, plan.channelError = parent.channelSnapshot, parent.channelError
		plan.registrySnapshot = parent.registrySnapshot
		plan.spec.PriceVersion, plan.spec.ModelVersion = parent.spec.PriceVersion, parent.spec.ModelVersion
		plan.spec.PricingAt = parent.spec.PricingAt
		ctx = WithGatewayRequestPlan(ctx, plan)
	}
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
		endpoint = CompositeRouteEndpointResponses
	}
	return !AccountSupportsResolvedModel(ctx, account, plan.Snapshot().Model, AccountModelBindingOptions{Endpoint: endpoint, CredentialAccount: parent})
}
func (s *GatewayService) clearRequestStickySession(ctx context.Context, groupID *int64, hash string) error {
	if s.cache == nil {
		return nil
	}
	if IsV2AffinityKey(hash) {
		accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), hash)
		if err != nil || !requestAffinityPermanentlyInvalid(ctx, s.accountRepo, accountID) {
			return nil
		}
	}
	return s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), hash)
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
