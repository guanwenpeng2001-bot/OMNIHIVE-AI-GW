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

func (r *settlementCapture) Persist(_ context.Context, input *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	r.receipt = input
	return input, nil
}
func (r *settlementCapture) Settle(_ context.Context, _ UsageSettlementKey, _ string) (*UsageSettlementResult, error) {
	r.settles++
	return &UsageSettlementResult{Receipt: r.receipt, Billing: &UsageBillingApplyResult{Applied: true}}, nil
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
