package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"time"
)

// OptionalTPMLimit preserves omitted versus explicit zero. A pointer to int64
// alone cannot reject JSON null because encoding/json treats it as omission.
type OptionalTPMLimit struct {
	Value   int64
	Present bool
}

func (limit *OptionalTPMLimit) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return errors.New("tpm_limit must be a non-negative integer, not null")
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || value < 0 {
		return errors.New("tpm_limit must be a non-negative int64 integer")
	}
	limit.Value, limit.Present = value, true
	return nil
}

func (limit OptionalTPMLimit) Pointer() *int64 {
	if !limit.Present {
		return nil
	}
	return &limit.Value
}

// TPM is a budget assigned to the database admission minute, not a sliding
// throughput limit. Monetary pricing and rate multipliers never enter it.
type TPMScope string

const (
	TPMScopeAPIKey  TPMScope = "api_key"
	TPMScopeGroup   TPMScope = "group"
	TPMScopeAccount TPMScope = "account"
)

var (
	ErrTPMEstimateUnavailable = errors.New("TPM_ESTIMATE_UNAVAILABLE")
	ErrTPMStateUnavailable    = errors.New("TPM_STATE_UNAVAILABLE")
	ErrTPMReservationConflict = errors.New("TPM_RESERVATION_CONFLICT")
)

type TPMLimitExceeded struct {
	Scope   TPMScope
	ResetAt time.Time
}

func (limit *TPMLimitExceeded) Error() string {
	switch limit.Scope {
	case TPMScopeAPIKey:
		return "API_KEY_TPM_EXCEEDED"
	case TPMScopeGroup:
		return "GROUP_TPM_EXCEEDED"
	default:
		return "ACCOUNT_TPM_EXCEEDED"
	}
}

// TPMReservationIdentity belongs to one actual upstream attempt. Retries of a
// storage operation retain this identity; a new upstream send uses a new index.
type TPMReservationIdentity struct {
	WorkspaceID  int64
	APIKeyID     int64
	ExecutionID  string
	AttemptIndex int
}

type TPMReservationCommand struct {
	TPMReservationIdentity
	BillingGroupID int64
	AccountID      int64
	Fingerprint    string
	Estimate       TPMEstimate
	ExpiresAt      time.Time
}

type TPMReservation struct {
	TPMReservationIdentity
	WindowStart time.Time
	Estimate    int64
	Fingerprint string
}

// TPMReservationRepository is implemented by the existing receipt repository.
// There is no separate usage ledger or replay queue for token reservations.
type TPMReservationRepository interface {
	ReserveTPMAttempt(context.Context, TPMReservationCommand) (*TPMReservation, error)
	SettleTPMAttempt(context.Context, TPMReservationIdentity, int64) error
	ReleaseUnsentTPMAttempt(context.Context, TPMReservationIdentity) error
	MarkTPMAttemptUnknown(context.Context, TPMReservationIdentity) error
}

// TPMEstimate requires verified bounds. Unknown is deliberately different from
// tokenless, including for multimodal inputs and server-owned conversation state.
type TPMEstimate struct {
	InputBound  int64
	OutputBound int64
	Candidates  int64
	Verified    bool
	Tokenless   bool
}

func (estimate TPMEstimate) Tokens() (int64, error) {
	if estimate.Tokenless {
		if estimate.InputBound != 0 || estimate.OutputBound != 0 {
			return 0, ErrTPMEstimateUnavailable
		}
		return 0, nil
	}
	if !estimate.Verified || estimate.InputBound < 0 || estimate.OutputBound < 0 || estimate.Candidates <= 0 {
		return 0, ErrTPMEstimateUnavailable
	}
	if estimate.OutputBound > math.MaxInt64/estimate.Candidates {
		return 0, ErrTPMEstimateUnavailable
	}
	return sumTPMTokens(estimate.InputBound, estimate.OutputBound*estimate.Candidates)
}

func sumTPMTokens(parts ...int64) (int64, error) {
	var total int64
	for _, quantity := range parts {
		if quantity < 0 || quantity > math.MaxInt64-total {
			return 0, ErrTPMEstimateUnavailable
		}
		total += quantity
	}
	return total, nil
}

// TPMUsageTokens accepts only the existing normalized, mutually exclusive
// categories. Cache 5m/1h, image cache and reasoning are subdivisions, not extras.
func TPMUsageTokens(usage UsageTokens) (int64, error) {
	return sumTPMTokens(int64(usage.InputTokens), int64(usage.OutputTokens), int64(usage.CacheCreationTokens), int64(usage.CacheReadTokens))
}

type tpmPlanAttempt struct {
	reservation *TPMReservation
	sent        bool
	settled     bool
	accountID   int64
}

func (p *GatewayRequestPlan) tpmRepository() TPMReservationRepository {
	if p == nil || p.settlement == nil {
		return nil
	}
	repository, _ := p.settlement.repo.(TPMReservationRepository)
	return repository
}

// ReserveTPMAttempt is independent of the monetary budget guard. The repository
// reads all three policies even when the authentication snapshot says unlimited.
func (p *GatewayRequestPlan) ReserveTPMAttempt(ctx context.Context, attemptIndex int, accountID int64, fingerprint string, estimate TPMEstimate) (*TPMReservation, error) {
	repository := p.tpmRepository()
	if repository == nil {
		return nil, ErrTPMStateUnavailable
	}
	spec := p.Snapshot()
	command := TPMReservationCommand{
		TPMReservationIdentity: TPMReservationIdentity{WorkspaceID: spec.WorkspaceID, APIKeyID: spec.APIKeyID, ExecutionID: spec.ExecutionID, AttemptIndex: attemptIndex},
		BillingGroupID:         spec.BillingGroupID, AccountID: accountID, Fingerprint: fingerprint, Estimate: estimate,
		ExpiresAt: spec.Deadline.Add(24 * time.Hour),
	}
	reservation, err := repository.ReserveTPMAttempt(ctx, command)
	if err != nil {
		var exceeded *TPMLimitExceeded
		if errors.As(err, &exceeded) || errors.Is(err, ErrTPMEstimateUnavailable) || errors.Is(err, ErrTPMReservationConflict) {
			return nil, err
		}
		// An uncertain commit is resolved with the SAME identity. This may finish
		// the original reservation, but must never create a replacement attempt.
		retryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		reservation, err = repository.ReserveTPMAttempt(retryCtx, command)
		cancel()
		if err != nil {
			return nil, errors.Join(ErrTPMStateUnavailable, err)
		}
	}
	if reservation != nil {
		p.mu.Lock()
		if p.tpmAttempts == nil {
			p.tpmAttempts = make(map[int]*tpmPlanAttempt)
		}
		if previous := p.tpmAttempts[attemptIndex]; previous == nil {
			p.tpmAttempts[attemptIndex] = &tpmPlanAttempt{reservation: reservation, accountID: accountID}
		}
		p.mu.Unlock()
	}
	return reservation, nil
}

// MarkTPMAttemptSent is called at the network boundary. From this point forward
// cancellation or a missing usage frame is never proof of zero consumption.
func (p *GatewayRequestPlan) MarkTPMAttemptSent(attemptIndex int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if attempt := p.tpmAttempts[attemptIndex]; attempt != nil {
		attempt.sent = true
	}
}

func (p *GatewayRequestPlan) SettleTPMAttempt(ctx context.Context, attemptIndex int, usage UsageTokens) error {
	actual, err := TPMUsageTokens(usage)
	if err != nil {
		return err
	}
	p.mu.Lock()
	attempt := p.tpmAttempts[attemptIndex]
	p.mu.Unlock()
	if attempt == nil {
		return nil
	}
	if err := p.tpmRepository().SettleTPMAttempt(ctx, attempt.reservation.TPMReservationIdentity, actual); err != nil {
		return err
	}
	p.mu.Lock()
	attempt.settled = true
	p.mu.Unlock()
	return nil
}

func (p *GatewayRequestPlan) finalizeTPMAttempts() {
	repository := p.tpmRepository()
	if repository == nil {
		return
	}
	p.mu.Lock()
	attempts := make([]tpmPlanAttempt, 0, len(p.tpmAttempts))
	for _, attempt := range p.tpmAttempts {
		attempts = append(attempts, *attempt)
	}
	p.mu.Unlock()
	for _, attempt := range attempts {
		if attempt.settled {
			continue
		}
		var err error
		for retry := 0; retry < 2; retry++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if attempt.sent {
				err = repository.MarkTPMAttemptUnknown(ctx, attempt.reservation.TPMReservationIdentity)
			} else {
				err = repository.ReleaseUnsentTPMAttempt(ctx, attempt.reservation.TPMReservationIdentity)
			}
			cancel()
			if err == nil {
				break
			}
		}
		if err != nil {
			slog.Warn("finalize token reservation", "api_key_id", attempt.reservation.APIKeyID, "execution_id", attempt.reservation.ExecutionID, "error", err)
		}
	}
}
