package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// This repository fixture models the receipt boundary for pricing tests. Real
// SQL atomicity and rollback are covered by repository settlement tests.
type recordUsageReceiptFixture struct {
	UsageSettlementReceiptRepository
	receipt       *UsageSettlementReceipt
	usage         UsageLogRepository
	users         UserRepository
	subscriptions UserSubscriptionRepository
	billing       func() UsageBillingRepository
}

// receiptFixture returns the receipt fixture backing svc, failing the test when
// the settlement service was wired with a different repository implementation.
func receiptFixture(t *testing.T, svc *OpenAIGatewayService) *recordUsageReceiptFixture {
	t.Helper()
	fixture, ok := svc.usageSettlement.repo.(*recordUsageReceiptFixture)
	require.True(t, ok, "usage settlement repo must be the receipt fixture")
	return fixture
}

func (r *recordUsageReceiptFixture) Persist(_ context.Context, receipt *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	r.receipt = receipt
	return receipt, nil
}

func (r *recordUsageReceiptFixture) Get(_ context.Context, _ UsageSettlementKey) (*UsageSettlementReceipt, error) {
	if r.receipt == nil {
		return nil, ErrUsageSettlementNotFound
	}
	return r.receipt, nil
}

func (r *recordUsageReceiptFixture) Settle(ctx context.Context, _ UsageSettlementKey, _ string) (*UsageSettlementResult, error) {
	receipt := r.receipt
	if _, err := r.usage.Create(ctx, &receipt.Usage); err != nil {
		return nil, err
	}
	result := &UsageBillingApplyResult{Applied: true}
	if billing := r.billing(); billing != nil {
		var err error
		result, err = billing.Apply(ctx, &receipt.Command)
		if err != nil {
			return nil, err
		}
	} else {
		if receipt.Command.BalanceCost > 0 && r.users != nil {
			if err := r.users.DeductBalance(ctx, receipt.Key.UserID, receipt.Command.BalanceCost); err != nil {
				return nil, err
			}
		}
		if receipt.Command.SubscriptionCost > 0 && r.subscriptions != nil {
			if err := r.subscriptions.IncrementUsage(ctx, *receipt.Command.SubscriptionID, receipt.Command.SubscriptionCost); err != nil {
				return nil, err
			}
		}
	}
	receipt.State = UsageSettlementSettled
	return &UsageSettlementResult{Receipt: receipt, Billing: result}, nil
}
