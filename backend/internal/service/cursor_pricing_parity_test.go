//go:build unit

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCursorDefaultPlanPricingParity(t *testing.T) {
	fallback, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	pricing := newHotReloadPricingService(t, string(fallback), "")
	model := pricing.GetModelPricing("default")
	require.NotNil(t, model)
	require.Equal(t, 0.000003, model.InputCostPerToken)
	require.Equal(t, 0.000015, model.OutputCostPerToken)
	billing := NewBillingService(&config.Config{}, pricing)
	cost, err := billing.CalculateCost("default", UsageTokens{InputTokens: 1000, OutputTokens: 100}, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.0045, cost.TotalCost, 1e-10)
	require.Nil(t, pricing.GetModelPricing("unknown-cursor-parity-model"))

	pricing.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "override.json")
	require.NoError(t, os.WriteFile(pricing.cfg.Pricing.OverrideFile, []byte(`{"default":{"input_cost_per_token":0.000006,"output_cost_per_token":0.00002}}`), 0600))
	pricing.reloadIfCustomFilesChanged()
	require.Equal(t, 0.000006, pricing.GetModelPricing("default").InputCostPerToken)
	cost, err = billing.CalculateCost("default", UsageTokens{InputTokens: 1000, OutputTokens: 100}, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.008, cost.TotalCost, 1e-10)
}
