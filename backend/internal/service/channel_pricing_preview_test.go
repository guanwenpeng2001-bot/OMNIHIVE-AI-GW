//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelPricingComparisonReadOnly(test *testing.T) {
	pricing := newStubPricingServiceFromJSON(test, `{"model":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000001,"output_cost_per_token":0}}`)
	card := ChannelModelPricing{BillingMode: BillingModeToken, Models: []string{"model"}, InputPrice: testPtrFloat64(1.1001e-6), OutputPrice: testPtrFloat64(1e-6)}
	before := card.Clone()
	for _, scenario := range []struct {
		name, profile, task string
		status              string
	}{
		{"known deployment", "openai", "text_generation", "comparable"},
		{"hosted identity differs", "openai-codex", "text_generation", "incomparable"},
		{"unknown identity", "", "text_generation", "incomparable"},
		{"unknown task", "openai", "", "incomparable"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			differences := pricing.CompareChannelPricing("model", scenario.profile, scenario.task, card)
			require.Len(test, differences, 7)
			for _, difference := range differences {
				switch difference.Field {
				case "input_cost_per_token":
					require.Equal(test, scenario.status, difference.Status)
					if scenario.status == "comparable" {
						require.False(test, difference.Warning)
						require.InDelta(test, (1.1001-1)/1.1001*100, *difference.AbsolutePercent, 1e-8)
						require.Equal(test, "USD", difference.Baseline.Currency)
						require.Equal(test, "token", difference.Baseline.Unit)
					} else {
						require.Nil(test, difference.AbsolutePercent)
					}
				case "output_cost_per_token":
					if scenario.status == "comparable" {
						require.Equal(test, "comparable", difference.Status)
						require.InDelta(test, 100, *difference.AbsolutePercent, 1e-8)
						require.True(test, difference.Warning)
					}
				default:
					require.Equal(test, "missing", difference.Status)
				}
			}
		})
	}
	require.Equal(test, before, card)
	require.InDelta(test, 1e-6, pricing.GetModelPricing("model").InputCostPerToken, 1e-15)
	for _, mode := range []BillingMode{BillingModeImage, BillingModePerRequest} {
		card.BillingMode = mode
		for _, difference := range pricing.CompareChannelPricing("model", "openai", "text_generation", card) {
			require.NotEqual(test, "comparable", difference.Status)
		}
	}
	card.BillingMode = BillingModeToken
	card.Intervals = []PricingInterval{{MinTokens: 100}}
	for _, difference := range pricing.CompareChannelPricing("model", "openai", "text_generation", card) {
		require.NotEqual(test, "comparable", difference.Status)
	}
	test.Run("media request and per-image units are never equated", func(test *testing.T) {
		mediaPricing := newStubPricingServiceFromJSON(test, `{"image-model":{"litellm_provider":"openai","mode":"image_generation","output_cost_per_image":0.01}}`)
		mediaCard := ChannelModelPricing{BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.02)}
		differences := mediaPricing.CompareChannelPricing("image-model", "openai", "image_generation", mediaCard)
		require.Len(test, differences, 1)
		require.Equal(test, "request", differences[0].Baseline.Unit)
		require.Equal(test, "image", differences[0].Candidate.Unit)
		require.Equal(test, "incomparable", differences[0].Status)
		require.Nil(test, differences[0].AbsolutePercent)
		require.False(test, differences[0].Warning)
	})
}
