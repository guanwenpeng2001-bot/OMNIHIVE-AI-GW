//go:build unit

package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// Independent audit assertions follow final-plan.md's channel-price denominator.
// Permanent regressions from audit 34 synthetic counterexamples.
func TestAuditC02ChannelPriceDenominator(test *testing.T) {
	for _, scenario := range []struct {
		name               string
		reference, channel float64
		warning            bool
	}{
		{"false_positive", 1, 1.1001, false},
		{"false_negative", 1.1001, 1, true},
		{"upper_below", 1.0999, 1, false}, {"upper_exact", 1.1, 1, false},
		{"lower_below", 0.9001, 1, false}, {"lower_exact", 0.9, 1, false},
		{"lower_above", 0.8999, 1, true},
		{"zero_channel_positive_catalog", 1, 0, true}, {"both_zero", 0, 0, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			service := newStubPricingServiceFromJSON(test, `{"model":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":1}}`)
			quote := service.pricingData["model"].sourceQuotes["input_cost_per_token"]
			quote.Value = &scenario.reference
			service.pricingData["model"].sourceQuotes["input_cost_per_token"] = quote
			card := ChannelModelPricing{BillingMode: BillingModeToken, Models: []string{"model"}, InputPrice: &scenario.channel}
			found := false
			for _, difference := range service.CompareChannelPricing("model", "openai", "text_generation", card) {
				if difference.Field != "input_cost_per_token" {
					continue
				}
				found = true
				require.Equal(test, scenario.channel, *difference.Baseline.Value)
				require.Equal(test, scenario.reference, *difference.Candidate.Value)
				if scenario.channel == 0 && scenario.reference != 0 {
					require.Equal(test, "new_charge", difference.Status)
					require.Nil(test, difference.AbsolutePercent)
				} else {
					require.Equal(test, "comparable", difference.Status)
					expected := 0.0
					if scenario.channel != 0 {
						expected = math.Abs(scenario.reference-scenario.channel) / scenario.channel * 100
					}
					require.NotNil(test, difference.AbsolutePercent)
					require.InDelta(test, expected, *difference.AbsolutePercent, 1e-10)
				}
				require.Equal(test, scenario.warning, difference.Warning, "contract requires channel selling price as denominator")
			}
			require.True(test, found, "input price comparison must exist")
		})
	}
}

func TestAuditC03CachePriceIsNotCondition(test *testing.T) {
	service := newStubPricingServiceFromJSON(test, `{"model":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000001,"cache_creation_input_token_cost_above_1hr":0.000002}}`)
	preview, err := service.PreviewSource([]byte(`{"model":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000002,"cache_creation_input_token_cost_above_1hr":0.000004}}`))
	require.NoError(test, err)
	require.Len(test, preview.Differences, 2)
	for _, difference := range preview.Differences {
		test.Logf("field=%s before_conditions=%s after_conditions=%s status=%s warning=%v", difference.Field, difference.Baseline.Conditions, difference.Candidate.Conditions, difference.Status, difference.Warning)
		require.Equal(test, "comparable", difference.Status, "the 1hr cache write price changed, not its billing condition")
		require.True(test, difference.Warning)
		require.InDelta(test, 100, *difference.AbsolutePercent, 1e-10)
		require.JSONEq(test, `{"mode":"chat"}`, difference.Baseline.Conditions)
	}
}

func TestAuditC03RealConditionsRemainIncomparable(t *testing.T) {
	for _, field := range []string{"region", "supported_regions", "service_tier", "plan", "pricing_conditions", "price_unit", "long_context_input_token_threshold", "long_context_input_cost_multiplier", "long_context_output_cost_multiplier"} {
		t.Run(field, func(t *testing.T) {
			before := map[string]any{"mode": "chat", "litellm_provider": "openai", "input_cost_per_token": 1, field: 1}
			after := map[string]any{"mode": "chat", "litellm_provider": "openai", "input_cost_per_token": 2, field: 2}
			left, err := json.Marshal(before)
			require.NoError(t, err)
			right, err := json.Marshal(after)
			require.NoError(t, err)
			diff := ComparePricingQuotes("reference_to_reference", "model", "input_cost_per_token", pricingEntryQuotes("model", left)["input_cost_per_token"], pricingEntryQuotes("model", right)["input_cost_per_token"])
			require.Equal(t, "incomparable", diff.Status)
			require.False(t, diff.Warning)
			require.Nil(t, diff.AbsolutePercent)
		})
	}
}

func TestAuditC03TierPricesAndThresholdsAreSeparate(t *testing.T) {
	before := pricingEntryQuotes("model", []byte(`{"mode":"chat","litellm_provider":"openai","input_cost_per_token":1,"input_cost_per_token_above_200k_tokens":2}`))
	after := pricingEntryQuotes("model", []byte(`{"mode":"chat","litellm_provider":"openai","input_cost_per_token":1,"input_cost_per_token_above_200k_tokens":4}`))
	field := "input_cost_per_token_above_200k_tokens"
	diff := ComparePricingQuotes("reference_to_reference", "model", field, before[field], after[field])
	require.Equal(t, "comparable", diff.Status)
	require.True(t, diff.Warning)
	require.InDelta(t, 100, *diff.AbsolutePercent, 1e-10)
	changed := pricingEntryQuotes("model", []byte(`{"mode":"chat","litellm_provider":"openai","input_cost_per_token":1,"input_cost_per_token_above_272k_tokens":4}`))
	diff = ComparePricingQuotes("reference_to_reference", "model", "input_cost_per_token", before["input_cost_per_token"], changed["input_cost_per_token"])
	require.Equal(t, "incomparable", diff.Status)
	require.False(t, diff.Warning)
}
