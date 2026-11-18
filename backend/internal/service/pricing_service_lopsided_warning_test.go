package service

import (
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestParsePricingData_LopsidedWarningRequiresExplicitZeroOutputWithoutTier(t *testing.T) {
	tests := []struct {
		name             string
		fields           map[string]any
		warn             bool
		inputMultiplier  float64
		outputMultiplier float64
	}{
		{name: "explicit zero", fields: map[string]any{"output_cost_per_token": 0}},
		{name: "missing output", warn: true},
		{name: "null output", fields: map[string]any{"output_cost_per_token": nil}, warn: true},
		{name: "paid output without tier", fields: map[string]any{"output_cost_per_token": 1e-7}, warn: true},
		{name: "paid output with unchanged tier", fields: map[string]any{"output_cost_per_token": 1e-7, "output_cost_per_token_above_272k_tokens": 1e-7}, warn: true},
		{name: "zero output with positive tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_272k_tokens": 1e-7}, warn: true},
		{name: "zero output with zero tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_272k_tokens": 0}, warn: true},
		{name: "zero output with null tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_272k_tokens": nil}, warn: true},
		{name: "zero output with unknown tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_272k_tokens": "unknown"}, warn: true},
		{name: "zero output with later tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_400k_tokens": 1e-7}, warn: true},
		{name: "zero output with priority tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_272k_tokens_priority": 1e-7}, warn: true},
		{name: "zero output with flex tier", fields: map[string]any{"output_cost_per_token": 0, "output_cost_per_token_above_272k_tokens_flex": 1e-7}, warn: true},
		{name: "both sides increase", fields: map[string]any{"output_cost_per_token": 1e-7, "output_cost_per_token_above_272k_tokens": 1.5e-7}, outputMultiplier: 1.5},
		{name: "output only increases", fields: map[string]any{"input_cost_per_token_above_272k_tokens": 1e-7, "output_cost_per_token": 1e-7, "output_cost_per_token_above_272k_tokens": 1.5e-7}, warn: true, inputMultiplier: 1, outputMultiplier: 1.5},
		{name: "zero input does not exempt output ladder", fields: map[string]any{"input_cost_per_token": 0, "output_cost_per_token": 1e-7, "output_cost_per_token_above_272k_tokens": 1.5e-7}, warn: true, inputMultiplier: 1, outputMultiplier: 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logSink, restore := captureStructuredLog(t)
			defer restore()
			fields := map[string]any{
				"input_cost_per_token":                   1e-7,
				"input_cost_per_token_above_272k_tokens": 2e-7,
			}
			for key, value := range tt.fields {
				fields[key] = value
			}
			body, err := json.Marshal(map[string]any{"test-model": fields})
			require.NoError(t, err)
			data, err := (&PricingService{}).parsePricingData(body)
			require.NoError(t, err)
			require.Equal(t, tt.warn, logSink.ContainsMessageAtLevel("one-sided long-context ladder", "warn"))
			// Warning policy must not change derived billing multipliers.
			inputMultiplier, outputMultiplier := tt.inputMultiplier, tt.outputMultiplier
			if inputMultiplier == 0 {
				inputMultiplier = 2
			}
			if outputMultiplier == 0 {
				outputMultiplier = 1
			}
			require.Equal(t, 272000, data["test-model"].LongContextInputTokenThreshold)
			require.InDelta(t, inputMultiplier, data["test-model"].LongContextInputCostMultiplier, 1e-12)
			require.InDelta(t, outputMultiplier, data["test-model"].LongContextOutputCostMultiplier, 1e-12)
		})
	}
}

func TestPricingZeroOutputLadderPreservesBillingAndCacheWarning(t *testing.T) {
	const model = "vercel_ai_gateway/openai/gpt-6-luna-decisions"
	const catalog = `{"vercel_ai_gateway/openai/gpt-6-luna-decisions": {
		"input_cost_per_token": 1e-7,
		"output_cost_per_token": 0,
		"input_cost_per_token_above_272k_tokens": 2e-7,
		"cache_read_input_token_cost": 1e-8,
		"cache_read_input_token_cost_above_272k_tokens": 2e-8,
		"cache_creation_input_token_cost": 1.25e-7,
		"cache_creation_input_token_cost_above_272k_tokens": 2.5e-7
	}}`
	logSink, restore := captureStructuredLog(t)
	defer restore()
	pricingService := newStubPricingServiceFromJSON(t, catalog)
	require.False(t, logSink.ContainsMessage("one-sided long-context ladder"))
	billing := NewBillingService(&config.Config{}, pricingService)
	for _, inputTokens := range []int{1000, 273000} {
		cost, err := billing.CalculateCost(model, UsageTokens{
			InputTokens: inputTokens, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 1000,
		}, 1)
		require.NoError(t, err)
		multiplier := 1.0
		if inputTokens > 272000 {
			multiplier = 2
		}
		require.Equal(t, multiplier > 1, cost.LongContextBillingApplied)
		require.InDelta(t, float64(inputTokens)*1e-7*multiplier, cost.InputCost, 1e-12)
		require.Zero(t, cost.OutputCost)
		require.InDelta(t, 1000*1e-8*multiplier, cost.CacheReadCost, 1e-12)
		require.InDelta(t, 1000*1.25e-7*multiplier, cost.CacheCreationCost, 1e-12)
	}

	// Exempting the ladder warning must not hide an orphan cache tier.
	_, err := (&PricingService{}).parsePricingData([]byte(`{"zero-output-orphan-cache": {
		"input_cost_per_token": 1e-7, "output_cost_per_token": 0,
		"input_cost_per_token_above_272k_tokens": 2e-7,
		"cache_read_input_token_cost_above_272k_tokens": 2e-8
	}}`))
	require.NoError(t, err)
	require.False(t, logSink.ContainsMessage("one-sided long-context ladder"))
	require.True(t, logSink.ContainsMessageAtLevel("zero-output-orphan-cache", "warn"))
}
