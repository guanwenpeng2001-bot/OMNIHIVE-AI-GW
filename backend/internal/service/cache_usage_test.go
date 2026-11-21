//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type cacheReceiptCapture struct {
	*settlementCapture
	source *UsageSettlementReceipt
}

func (r *cacheReceiptCapture) Get(context.Context, UsageSettlementKey) (*UsageSettlementReceipt, error) {
	return r.source, nil
}

func TestCacheHitReceiptUsesNewIdentityAndOriginalCharge(t *testing.T) {
	repo := &cacheReceiptCapture{settlementCapture: &settlementCapture{}}
	settlement := NewUsageSettlementService(repo, nil, nil)
	price := 0.25
	snapshot, err := json.Marshal(PricingSnapshot{PricingState: PricingStatePriced, UnitPrice: &price, Units: 1, RateMultiplier: 1, ActualCost: price, TotalCost: price, ChargedAmount: price})
	require.NoError(t, err)
	source := &UsageSettlementReceipt{
		Key:     UsageSettlementKey{RequestID: "original", UserID: 1, APIKeyID: 2},
		Command: UsageBillingCommand{RequestID: "original", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model", BalanceCost: 0.25},
		Usage:   UsageLog{RequestID: "original", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model", ActualCost: price, TotalCost: price, PricingState: PricingStatePriced, PricingSnapshot: string(snapshot)},
	}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{ExecutionID: "hit", UserID: 1, APIKeyID: 2, Route: "/v1/chat/completions"})
	repo.source, repo.receipt = source, source
	err = settlement.RecordCacheHit(WithGatewayRequestPlan(t.Context(), plan), source.Key)
	require.NoError(t, err)
	require.Equal(t, "hit", repo.receipt.Key.RequestID)
	require.True(t, repo.receipt.Usage.BillingEvidence.CacheHit)
	require.Equal(t, 1, repo.settles)
	require.Equal(t, price, repo.receipt.Command.BalanceCost)
}
