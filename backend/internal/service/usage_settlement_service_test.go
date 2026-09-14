package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type settlementCapture struct {
	UsageSettlementReceiptRepository
	receipt *UsageSettlementReceipt
	settles int
}

func (capture *settlementCapture) Persist(_ context.Context, input *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	capture.receipt = input
	return input, nil
}
func (capture *settlementCapture) Settle(_ context.Context, _ UsageSettlementKey, _ string) (*UsageSettlementResult, error) {
	capture.settles++
	return &UsageSettlementResult{Receipt: capture.receipt, Billing: &UsageBillingApplyResult{Applied: true}}, nil
}

func TestSubmitUsageSettlementFreeUnpricedAndMalformed(t *testing.T) {
	for _, state := range []PricingState{PricingStateFree, PricingStateUnpriced, PricingStateError} {
		t.Run(string(state), func(t *testing.T) {
			repo := &settlementCapture{}
			svc := NewUsageSettlementService(repo, nil, nil)
			price := 0.0
			snapshot, err := json.Marshal(PricingSnapshot{PricingState: PricingStateFree, UnitPrice: &price, Units: 1, RateMultiplier: 1})
			require.NoError(t, err)
			cost := &CostBreakdown{PricingState: state, PricingSnapshot: string(snapshot)}
			if state == PricingStateError {
				cost.PricingSnapshot = "invalid-json"
			}
			params := &postUsageBillingParams{Cost: cost, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3, Platform: PlatformOpenAI}, AccountRateMultiplier: 1, Platform: PlatformOpenAI}
			usage := &UsageLog{RequestID: "original", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model"}
			err = submitUsageSettlement(context.Background(), svc, usage, params, &billingDeps{}, true)
			require.True(t, params.QuotaDerived)
			require.Equal(t, PlatformOpenAI, repo.receipt.Usage.QuotaPlatform)
			require.Equal(t, "interrupted", repo.receipt.CompletionState)
			if state == PricingStateFree {
				require.NoError(t, err)
				require.Equal(t, 1, repo.settles)
			} else {
				require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
				require.Equal(t, UsageSettlementNeedsReview, repo.receipt.State)
				require.Zero(t, repo.settles)
			}
		})
	}
}

func TestReceiptFinalizeSkipsLegacyQuotaAndAdditiveCaches(t *testing.T) {
	// A legacy positive-cost finalize would dereference billingCacheService.
	// Receipt mode must work without it because all views were invalidated.
	require.NotPanics(t, func() {
		finalizePostUsageBilling(context.Background(), &postUsageBillingParams{
			QuotaDerived: true, Cost: &CostBreakdown{ActualCost: 2, TotalCost: 2}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3}, Platform: PlatformOpenAI,
		}, &billingDeps{}, &UsageBillingApplyResult{Applied: true})
	})
}

type settlementRecoveryCapture struct {
	UsageSettlementReceiptRepository
	receipt *UsageSettlementReceipt
	result  UsageBillingApplyResult
	settles int
}

func (capture *settlementRecoveryCapture) ListPending(_ context.Context, cursor int64, _ int) ([]*UsageSettlementReceipt, error) {
	if cursor > 0 {
		return nil, nil
	}
	return []*UsageSettlementReceipt{capture.receipt}, nil
}

func (capture *settlementRecoveryCapture) Settle(_ context.Context, _ UsageSettlementKey, _ string) (*UsageSettlementResult, error) {
	capture.settles++
	return &UsageSettlementResult{Receipt: capture.receipt, Billing: &capture.result}, nil
}

type settlementAuthInvalidator struct {
	APIKeyAuthCacheInvalidator
	userIDs []int64
}

func (invalidator *settlementAuthInvalidator) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	invalidator.userIDs = append(invalidator.userIDs, userID)
}

func TestSettlementRecoveryInvalidatesExhaustedKeyAuth(t *testing.T) {
	for _, test := range []struct {
		name       string
		quotaCost  float64
		result     UsageBillingApplyResult
		invalidate bool
	}{
		{"recovered charge exhausts quota", 1, UsageBillingApplyResult{Applied: true, APIKeyQuotaExhausted: true}, true},
		{"replay after lost acknowledgement", 1, UsageBillingApplyResult{}, true},
		{"charge within quota", 1, UsageBillingApplyResult{Applied: true}, false},
		{"no key quota effect", 0, UsageBillingApplyResult{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &settlementRecoveryCapture{receipt: &UsageSettlementReceipt{ID: 1, Key: UsageSettlementKey{UserID: 7}, Command: UsageBillingCommand{APIKeyQuotaCost: test.quotaCost}}, result: test.result}
			invalidator := &settlementAuthInvalidator{}
			settlement := NewUsageSettlementService(repo, nil, nil)
			settlement.authCacheInvalidator = invalidator
			require.NoError(t, settlement.Recover(context.Background()))
			require.Equal(t, 1, repo.settles)
			if test.invalidate {
				require.Equal(t, []int64{7}, invalidator.userIDs)
			} else {
				require.Empty(t, invalidator.userIDs)
			}
		})
	}
}
