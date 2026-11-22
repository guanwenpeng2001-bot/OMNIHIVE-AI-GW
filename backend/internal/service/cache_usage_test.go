//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheHitReceiptUsesNewIdentityAndOriginalCharge(t *testing.T) {
	repo := &settlementCapture{}
	settlement := NewUsageSettlementService(repo, nil, nil)
	price := 0.25
	snapshot, err := json.Marshal(PricingSnapshot{PricingState: PricingStatePriced, UnitPrice: &price, Units: 1, RateMultiplier: 1, ActualCost: price, TotalCost: price, ChargedAmount: price})
	require.NoError(t, err)
	source := &UsageSettlementReceipt{
		ID:              42,
		CompletionState: "complete", ResolutionState: "resolved", State: UsageSettlementSettled,
		Key:     UsageSettlementKey{RequestID: "original", UserID: 1, APIKeyID: 2},
		Command: UsageBillingCommand{RequestID: "original", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model", BalanceCost: 0.25, AccountQuotaCost: 0.25},
		Usage:   UsageLog{RequestID: "original", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model", ActualCost: price, TotalCost: price, AccountStatsCost: &price, PricingState: PricingStatePriced, PricingSnapshot: string(snapshot)},
	}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{ExecutionID: "hit", UserID: 1, APIKeyID: 2, Route: "/v1/chat/completions"})
	err = settlement.RecordCacheHit(WithGatewayRequestPlan(t.Context(), plan), source)
	require.NoError(t, err)
	require.Equal(t, "hit", repo.receipt.Key.RequestID)
	require.True(t, repo.receipt.Usage.BillingEvidence.CacheHit)
	require.Equal(t, int64(42), repo.receipt.Usage.BillingEvidence.CacheSourceID)
	require.Equal(t, 1, repo.settles)
	require.Equal(t, price, repo.receipt.Command.BalanceCost)
	require.Zero(t, repo.receipt.Command.AccountQuotaCost)
	require.NotNil(t, repo.receipt.Usage.AccountStatsCost)
	require.Zero(t, *repo.receipt.Usage.AccountStatsCost)
	require.Equal(t, price, source.Command.AccountQuotaCost)
	require.Equal(t, price, *source.Usage.AccountStatsCost)
}
