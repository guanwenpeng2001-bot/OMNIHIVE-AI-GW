package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// APIKeyBudgetRepository is the persistence port for period budgets. It is a
// separate port from APIKeyRepository so the budget SQL stays in its own file
// and the auth/CRUD projections stay untouched.
type APIKeyBudgetRepository interface {
	// LoadAPIKeyBudgetStates returns the policy and current counters of each key.
	LoadAPIKeyBudgetStates(ctx context.Context, apiKeyIDs []int64) (map[int64]APIKeyBudgetState, error)
	// SaveAPIKeyBudgetPolicy persists the canonical policy of one key.
	SaveAPIKeyBudgetPolicy(ctx context.Context, apiKeyID int64, policy APIKeyBudgetPolicy) error
	// GetAPIKeyBudgetState returns the current period counters for display.
	GetAPIKeyBudgetState(ctx context.Context, apiKeyID int64) (*APIKeyBudgetState, error)
	// AdjustAPIKeyBudgetUsage records an audited administrative adjustment.
	AdjustAPIKeyBudgetUsage(ctx context.Context, cmd *KeyBudgetAdjustmentCommand) (*APIKeyBudgetState, error)
}

// KeyBudgetReservationRepository is the request-level hold port. The settlement
// receipt repository implements it because admission and settlement must share
// the same SQL identities.
type KeyBudgetReservationRepository interface {
	// ReserveKeyBudget atomically holds an estimated cost for one request. It
	// returns a nil reservation when the key has no active period budget.
	ReserveKeyBudget(ctx context.Context, cmd *KeyBudgetReservationCommand) (*KeyBudgetReservationOutcome, error)
	// ReleaseKeyBudgetReservation refunds a hold that will never settle.
	ReleaseKeyBudgetReservation(ctx context.Context, apiKeyID int64, requestID string) error
	// ReleaseExpiredKeyBudgetReservations refunds abandoned holds in bounded batches.
	ReleaseExpiredKeyBudgetReservations(ctx context.Context, now time.Time, limit int) (int, error)
}

// KeyBudgetReservationCommand describes one request-level hold.
type KeyBudgetReservationCommand struct {
	RequestID  string
	APIKeyID   int64
	UserID     int64
	HoldAmount float64
	// SkipBalanceHold keeps the hold on the period budget only. Subscription
	// groups do not pay from the balance, so holding balance there would be wrong.
	SkipBalanceHold bool
	// TTL bounds how long a hold may stay live when the request never settles.
	TTL time.Duration
	Now time.Time
}

// Normalize fills defaults without mutating the caller's identity.
func (c *KeyBudgetReservationCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.HoldAmount = QuantizeUsageBillingAmount(c.HoldAmount)
	if c.TTL <= 0 {
		c.TTL = DefaultKeyBudgetReservationTTL
	}
}

// DefaultKeyBudgetReservationTTL is longer than the default generation deadline
// so a live request is never released underneath its own settlement.
const DefaultKeyBudgetReservationTTL = 20 * time.Minute

// KeyBudgetReservation is one live request hold.
type KeyBudgetReservation struct {
	ID          int64
	RequestID   string
	APIKeyID    int64
	UserID      int64
	HoldAmount  float64
	BalanceHeld bool
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// KeyBudgetReservationOutcome is the admission decision.
type KeyBudgetReservationOutcome struct {
	// Reservation is nil when the key has no active period budget.
	Reservation *KeyBudgetReservation
	// Duplicate reports that this request already holds budget.
	Duplicate   bool
	LimitUSD    float64
	UsedUSD     float64
	ReservedUSD float64
}

// KeyBudgetAdjustmentCommand records an audited administrative adjustment.
// Positive delta increases consumed usage (a charge correction), negative delta
// credits the current period. The owner-facing API never reaches this path.
type KeyBudgetAdjustmentCommand struct {
	APIKeyID    int64
	ActorUserID int64
	DeltaUSD    float64
	Reason      string
	Now         time.Time
}

// Normalize quantizes the adjustment amount.
func (c *KeyBudgetAdjustmentCommand) Normalize() {
	if c == nil {
		return
	}
	c.Reason = strings.TrimSpace(c.Reason)
	c.DeltaUSD = QuantizeUsageBillingAmount(c.DeltaUSD)
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
}

// budgetRepository returns the budget persistence port when the injected API key
// repository implements it.
func (s *APIKeyService) budgetRepository() (APIKeyBudgetRepository, error) {
	if s == nil || s.apiKeyRepo == nil {
		return nil, errors.New("api key budget repository unavailable")
	}
	repo, ok := s.apiKeyRepo.(APIKeyBudgetRepository)
	if !ok {
		return nil, errors.New("api key budget repository unavailable")
	}
	return repo, nil
}

// BudgetPolicy returns the stored period-budget policy of one key.
func (s *APIKeyService) BudgetPolicy(ctx context.Context, apiKeyID int64) (APIKeyBudgetPolicy, error) {
	repo, err := s.budgetRepository()
	if err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	states, err := repo.LoadAPIKeyBudgetStates(ctx, []int64{apiKeyID})
	if err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	return states[apiKeyID].Policy.Normalize(), nil
}

// BudgetState returns the current period counters for display.
func (s *APIKeyService) BudgetState(ctx context.Context, apiKeyID int64) (*APIKeyBudgetState, error) {
	repo, err := s.budgetRepository()
	if err != nil {
		return nil, err
	}
	return repo.GetAPIKeyBudgetState(ctx, apiKeyID)
}

// SaveBudgetPolicy persists a policy after validating it. enforcedChangeAllowed
// must be false for owner-facing calls: an enforced budget can only be moved by
// the audited administrative path.
func (s *APIKeyService) SaveBudgetPolicy(ctx context.Context, apiKeyID int64, policy APIKeyBudgetPolicy, enforcedChangeAllowed bool) (APIKeyBudgetPolicy, error) {
	repo, err := s.budgetRepository()
	if err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	return SaveAPIKeyBudgetPolicy(ctx, repo, apiKeyID, policy, enforcedChangeAllowed)
}

// AdjustBudgetUsage records an audited administrative adjustment and returns the
// resulting state. It is the only supported way to move consumed usage.
func (s *APIKeyService) AdjustBudgetUsage(ctx context.Context, cmd *KeyBudgetAdjustmentCommand) (*APIKeyBudgetState, error) {
	repo, err := s.budgetRepository()
	if err != nil {
		return nil, err
	}
	return AdjustAPIKeyBudgetUsage(ctx, repo, cmd)
}

// SaveAPIKeyBudgetPolicy 是预算策略写入的唯一实现，按仓储取用，供用户侧与管理端
// 共用同一条校验与持久化路径（避免管理端另写一套规则）。
//
// enforcedChangeAllowed=false 时必须保持"强制预算不可被自助改写"的约束；
// 管理端通过 audited 调用点传 true 才能移动强制预算。
func SaveAPIKeyBudgetPolicy(ctx context.Context, repo APIKeyBudgetRepository, apiKeyID int64, policy APIKeyBudgetPolicy, enforcedChangeAllowed bool) (APIKeyBudgetPolicy, error) {
	if repo == nil {
		return APIKeyBudgetPolicy{}, errors.New("api key budget repository unavailable")
	}
	states, err := repo.LoadAPIKeyBudgetStates(ctx, []int64{apiKeyID})
	if err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	current := states[apiKeyID].Policy.Normalize()
	next := policy.Normalize()
	if err := next.Validate(); err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	if !enforcedChangeAllowed {
		if current.Enforced && !budgetPolicyEqual(current, next) {
			return APIKeyBudgetPolicy{}, ErrAPIKeyBudgetEnforced
		}
		if current.Enforced {
			// Unchanged enforced policy: nothing to write, keep the mandate.
			return current, nil
		}
		// The owner may edit their own limit and period but can never promote a
		// policy to enforced.
		next.Enforced = false
	}
	if err := repo.SaveAPIKeyBudgetPolicy(ctx, apiKeyID, next); err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	return next, nil
}

// AdjustAPIKeyBudgetUsage 是预算调账的唯一实现，按仓储取用。
// 它要求非零调整量与可读原因，是移动已消费金额的唯一受支持路径。
func AdjustAPIKeyBudgetUsage(ctx context.Context, repo APIKeyBudgetRepository, cmd *KeyBudgetAdjustmentCommand) (*APIKeyBudgetState, error) {
	if repo == nil {
		return nil, errors.New("api key budget repository unavailable")
	}
	if cmd == nil || cmd.APIKeyID <= 0 {
		return nil, fmt.Errorf("%w: api key id is required", ErrAPIKeyBudgetInvalid)
	}
	if strings.TrimSpace(cmd.Reason) == "" {
		return nil, fmt.Errorf("%w: adjustment requires a reason", ErrAPIKeyBudgetInvalid)
	}
	if cmd.DeltaUSD == 0 {
		return nil, fmt.Errorf("%w: adjustment delta must not be zero", ErrAPIKeyBudgetInvalid)
	}
	cmd.Normalize()
	return repo.AdjustAPIKeyBudgetUsage(ctx, cmd)
}

func budgetPolicyEqual(a, b APIKeyBudgetPolicy) bool {
	a, b = a.Normalize(), b.Normalize()
	return a.Mode == b.Mode &&
		a.DurationSeconds == b.DurationSeconds &&
		a.Anchor == b.Anchor &&
		a.CalendarUnit == b.CalendarUnit &&
		a.EffectiveTimezoneName() == b.EffectiveTimezoneName() &&
		a.LimitUSD == b.LimitUSD &&
		a.Enforced == b.Enforced
}

// ReserveKeyBudget admits one request against the key's period budget and the
// payer balance. It is idempotent per (api key, request id).
func (s *UsageSettlementService) ReserveKeyBudget(ctx context.Context, cmd *KeyBudgetReservationCommand) (*KeyBudgetReservationOutcome, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("key budget reservation repository unavailable")
	}
	repo, ok := s.repo.(KeyBudgetReservationRepository)
	if !ok {
		return nil, errors.New("key budget reservation repository unavailable")
	}
	if cmd == nil {
		return nil, fmt.Errorf("%w: reservation command is required", ErrAPIKeyBudgetInvalid)
	}
	copy := *cmd
	copy.Normalize()
	if copy.RequestID == "" {
		return nil, fmt.Errorf("%w: reservation requires a request id", ErrAPIKeyBudgetInvalid)
	}
	if copy.APIKeyID <= 0 || copy.UserID <= 0 {
		return nil, fmt.Errorf("%w: reservation requires key and user identity", ErrAPIKeyBudgetInvalid)
	}
	return repo.ReserveKeyBudget(ctx, &copy)
}

// ReleaseKeyBudgetReservation refunds a hold that will never settle.
func (s *UsageSettlementService) ReleaseKeyBudgetReservation(ctx context.Context, apiKeyID int64, requestID string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	repo, ok := s.repo.(KeyBudgetReservationRepository)
	if !ok {
		return nil
	}
	requestID = strings.TrimSpace(requestID)
	if apiKeyID <= 0 || requestID == "" {
		return nil
	}
	return repo.ReleaseKeyBudgetReservation(ctx, apiKeyID, requestID)
}

// ReleaseExpiredKeyBudgetReservations refunds abandoned holds. It is called from
// the settlement recovery loop so a crashed process cannot strand budget.
func (s *UsageSettlementService) ReleaseExpiredKeyBudgetReservations(ctx context.Context, limit int) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	repo, ok := s.repo.(KeyBudgetReservationRepository)
	if !ok {
		return 0, nil
	}
	return repo.ReleaseExpiredKeyBudgetReservations(ctx, time.Now().UTC(), limit)
}
