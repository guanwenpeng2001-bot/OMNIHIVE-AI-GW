//go:build unit

package service

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBillingExactSourceDecimalsSurviveFloatCollision(t *testing.T) {
	catalog := &PricingService{}
	data, err := catalog.parsePricingData([]byte(`{
		"lower": {"input_cost_per_token": 0.123456784999999999},
		"higher": {"input_cost_per_token": 0.123456785000000001}
	}`))
	require.NoError(t, err)
	require.Equal(t, data["lower"].InputCostPerToken, data["higher"].InputCostPerToken)
	catalog.replacePricingData(data)
	service := NewBillingService(&config.Config{}, catalog)
	for model, want := range map[string]string{"lower": "0.12345678", "higher": "0.12345679"} {
		input := CostInput{Model: model, RateMultiplier: 1, Tokens: UsageTokens{InputTokens: 1}}
		cost, err := service.CalculateCostUnified(input)
		require.NoError(t, err)
		charge, err := frozenReceiptCharge(cost.PricingSnapshot)
		require.NoError(t, err)
		require.Equal(t, want, charge.String())
		replayed, err := RecalculatePricingSnapshot(cost.PricingSnapshot)
		require.NoError(t, err)
		require.Equal(t, QuantizeUsageBillingAmount(cost.ActualCost), QuantizeUsageBillingAmount(replayed.ActualCost))
		frozen, err := service.FreezeExactCost(input)
		require.NoError(t, err)
		exact, err := frozen.Calculate()
		require.NoError(t, err)
		require.Equal(t, want, exact.Charge.String())
	}
}
