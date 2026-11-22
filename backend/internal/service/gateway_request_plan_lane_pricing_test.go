//go:build unit

package service

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayBudgetBindsFreeTariffToSelectedAccount(t *testing.T) {
	const model = "kilo-auto/free"
	free := freeAccount(PlatformKilo, KiloAccountModeFree)
	paid := freeAccount(PlatformKilo, KiloAccountModeAPIKey)
	price := 1.0
	group := &Group{ID: 7, Platform: PlatformKilo, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{Models: []string{model}, InputPrice: &price, OutputPrice: &price}}}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, UserID: 1, APIKeyID: 2, ExecutionID: "test"})
	plan.billing = NewBillingService(&config.Config{}, nil)
	plan.catalogRepo = &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{1: free, 2: paid}}
	plan.budgetPlatform = PlatformKilo
	plan.budgetEstimate = &RequestBudgetEstimate{Group: group, InputTokens: 1, OutputTokens: 1, SkipKeyBudget: true}

	ctx := withTPMWire(WithGatewayRequestPlan(t.Context(), plan), []byte(`{"model":"kilo-auto/free"}`), "/chat/completions")
	require.NoError(t, plan.admitPublicModelLane(ctx, 1))
	require.ErrorIs(t, plan.admitPublicModelLane(ctx, 2), ErrAPIKeyBudgetUnavailable)
	require.True(t, plan.Record())
	cost, err := plan.billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, RateMultiplier: 1,
		Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1, CacheReadTokens: 1}})
	require.NoError(t, err)
	require.Equal(t, PricingStateFree, cost.PricingState)
	require.Zero(t, cost.ActualCost)
	require.False(t, verifiedKiloFreeTariff(free, "unverified/free"))
}
