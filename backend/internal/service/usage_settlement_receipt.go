package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type UsageSettlementState string

const (
	UsageSettlementPending     UsageSettlementState = "pending"
	UsageSettlementSettled     UsageSettlementState = "settled"
	UsageSettlementNeedsReview UsageSettlementState = "needs_review"
)

var ErrUsageSettlementNotFound = errors.New("usage settlement receipt not found")
var ErrUsageSettlementNeedsReview = errors.New("usage settlement receipt needs review")

// UsageSettlementKey preserves the ordinary billing identity and its owner.
type UsageSettlementKey struct {
	RequestID string
	APIKeyID  int64
	UserID    int64
}

// UsageSettlementReceipt is immutable recovery input, not another ledger.
// Usage contains only the scalar usage DTO; hydrated ownership objects are never
// serialized (they may contain credentials). Pricing lives here because the
// legacy usage_logs table does not persist PricingState or PricingSnapshot.
type UsageSettlementReceipt struct {
	ID              int64
	Key             UsageSettlementKey
	Command         UsageBillingCommand
	Usage           UsageLog
	ResolutionState string // resolved or unresolved
	CompletionState string // complete or interrupted
	State           UsageSettlementState
	UsageLogID      *int64
	CreatedAt       time.Time
	SettledAt       *time.Time
}

type UsageSettlementResult struct {
	Receipt *UsageSettlementReceipt
	Billing *UsageBillingApplyResult
}

type UsageSettlementReceiptRepository interface {
	Persist(context.Context, *UsageSettlementReceipt) (*UsageSettlementReceipt, error)
	Get(context.Context, UsageSettlementKey) (*UsageSettlementReceipt, error)
	Settle(context.Context, UsageSettlementKey, string) (*UsageSettlementResult, error)
	// ListPending uses an ID cursor; callers start a new sweep at zero so a
	// transient failure is revisited without starving subsequent receipts.
	ListPending(context.Context, int64, int) ([]*UsageSettlementReceipt, error)
}

// PrepareUsageSettlementReceipt copies the input before normalizing. Existing
// fingerprints must be retained: Normalize computes missing ones from RAW money
// before rounding, and must never be repeated from reconstructed rounded money.
func PrepareUsageSettlementReceipt(input *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	if input == nil {
		return nil, errors.New("usage settlement receipt is nil")
	}
	receipt := *input
	receipt.Key.RequestID = strings.TrimSpace(receipt.Key.RequestID)
	command := &receipt.Command
	// Reject negative sub-quantum values before rounding could hide them as zero.
	if receipt.Usage.PricingState == PricingStatePriced || receipt.Usage.PricingState == PricingStateFree {
		for _, amount := range []float64{command.BalanceCost, command.SubscriptionCost, command.APIKeyQuotaCost, command.APIKeyRateLimitCost, command.AccountQuotaCost, receipt.Usage.ActualCost} {
			if err := ValidateUsageBillingAmount(amount); err != nil {
				return nil, err
			}
		}
	}
	command.Normalize()
	usage := &receipt.Usage
	usage.RequestID = strings.TrimSpace(usage.RequestID)
	if receipt.Key.RequestID == "" {
		return nil, ErrUsageBillingRequestIDRequired
	}
	if receipt.Key.UserID <= 0 || receipt.Key.APIKeyID <= 0 || command.AccountID <= 0 ||
		command.RequestID != receipt.Key.RequestID || command.APIKeyID != receipt.Key.APIKeyID || command.UserID != receipt.Key.UserID ||
		usage.RequestID != command.RequestID || usage.UserID != command.UserID || usage.APIKeyID != command.APIKeyID || usage.AccountID != command.AccountID ||
		usage.Model != command.Model || usage.BillingType != command.BillingType || valueOrZero(usage.SubscriptionID) != valueOrZero(command.SubscriptionID) ||
		strings.TrimSpace(optionalStringValue(usage.ServiceTier)) != strings.TrimSpace(command.ServiceTier) ||
		strings.TrimSpace(optionalStringValue(usage.ReasoningEffort)) != strings.TrimSpace(command.ReasoningEffort) ||
		usage.InputTokens != command.InputTokens || usage.OutputTokens != command.OutputTokens ||
		usage.CacheCreationTokens != command.CacheCreationTokens || usage.CacheReadTokens != command.CacheReadTokens || usage.ImageCount != command.ImageCount {
		return nil, ErrUsageBillingRequestConflict
	}
	if receipt.ResolutionState != "resolved" && receipt.ResolutionState != "unresolved" {
		return nil, errors.New("invalid usage resolution state")
	}
	if receipt.CompletionState != "complete" && receipt.CompletionState != "interrupted" {
		return nil, errors.New("invalid usage completion state")
	}
	usage.User, usage.APIKey, usage.Account, usage.Group, usage.Subscription = nil, nil, nil, nil, nil
	usage.ID = 0
	usage.SyncRequestTypeAndLegacyFields()
	if usage.CreatedAt.IsZero() {
		usage.CreatedAt = time.Now().UTC()
	}
	receipt.ID, receipt.UsageLogID, receipt.SettledAt = 0, nil, nil
	receipt.State = UsageSettlementPending
	switch usage.PricingState {
	case PricingStateUnpriced, PricingStateError:
		receipt.State = UsageSettlementNeedsReview
	case PricingStatePriced, PricingStateFree:
		if err := validateUsageSettlementPricing(&receipt); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("invalid usage pricing state")
	}
	if receipt.ResolutionState != "resolved" {
		receipt.State = UsageSettlementNeedsReview
	}
	return &receipt, nil
}

func validateUsageSettlementPricing(receipt *UsageSettlementReceipt) error {
	usage, command := &receipt.Usage, &receipt.Command
	var snapshot PricingSnapshot
	if err := json.Unmarshal([]byte(usage.PricingSnapshot), &snapshot); err != nil {
		return err
	}
	replayed, err := RecalculatePricingSnapshot(usage.PricingSnapshot)
	if err != nil {
		return err
	}
	if snapshot.PricingState != usage.PricingState || replayed.PricingState != usage.PricingState ||
		snapshot.ChargedAmount != QuantizeUsageBillingAmount(snapshot.ActualCost) ||
		snapshot.ChargedAmount != QuantizeUsageBillingAmount(replayed.ActualCost) ||
		QuantizeUsageBillingAmount(snapshot.TotalCost) != QuantizeUsageBillingAmount(replayed.TotalCost) ||
		QuantizeUsageBillingAmount(usage.ActualCost) != snapshot.ChargedAmount ||
		QuantizeUsageBillingAmount(usage.TotalCost) != QuantizeUsageBillingAmount(snapshot.TotalCost) {
		return ErrInvalidBillingAmount
	}
	for _, quantity := range []int{usage.InputTokens, usage.OutputTokens, usage.CacheCreationTokens, usage.CacheReadTokens, usage.CacheCreation5mTokens, usage.CacheCreation1hTokens, usage.ImageInputTokens, usage.ImageOutputTokens, usage.ImageCount, usage.VideoCount} {
		if quantity < 0 {
			return ErrInvalidBillingAmount
		}
	}
	if usage.AccountStatsCost != nil {
		if err := ValidateUsageBillingAmount(*usage.AccountStatsCost); err != nil {
			return err
		}
	}
	for _, amount := range []float64{command.BalanceCost, command.SubscriptionCost, command.APIKeyQuotaCost, command.APIKeyRateLimitCost, command.AccountQuotaCost,
		usage.InputCost, usage.OutputCost, usage.CacheCreationCost, usage.CacheReadCost, usage.ImageInputCost, usage.ImageOutputCost, usage.TotalCost, snapshot.ChargedAmount} {
		if err := ValidateUsageBillingAmount(amount); err != nil {
			return err
		}
	}
	if command.BalanceCost+command.SubscriptionCost != snapshot.ChargedAmount ||
		(command.APIKeyQuotaCost != 0 && command.APIKeyQuotaCost != snapshot.ChargedAmount) ||
		(command.APIKeyRateLimitCost != 0 && command.APIKeyRateLimitCost != snapshot.ChargedAmount) ||
		(command.AccountQuotaCost != 0 && command.AccountQuotaCost != QuantizeUsageBillingAmount(snapshot.TotalCost*snapshot.AccountRateMultiplier)) ||
		(command.SubscriptionCost > 0 && command.SubscriptionID == nil) ||
		(command.SubscriptionCost > 0 && command.BillingType != BillingTypeSubscription) ||
		(command.BalanceCost > 0 && command.BillingType != BillingTypeBalance) ||
		(command.BalanceCost > 0 && command.SubscriptionCost > 0) {
		return ErrInvalidBillingAmount
	}
	usage.ActualCost = snapshot.ChargedAmount
	return nil
}
