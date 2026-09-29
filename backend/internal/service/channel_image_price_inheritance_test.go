//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyChannelImagePriceInheritanceAndFrozenCost(t *testing.T) {
	for _, test := range []struct {
		name           string
		override       *float64
		expectedOutput float64
		expectedInput  float64
	}{
		{"unset inherits catalog", nil, 30e-6, 8e-6},
		{"explicit zero", testPtrFloat64(0), 0, 0},
		{"explicit paid", testPtrFloat64(12e-6), 12e-6, 12e-6},
	} {
		t.Run(test.name, func(t *testing.T) {
			card := ChannelModelPricing{Platform: "anthropic", Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
				ImageOutputPrice: test.override, ImageInputPrice: test.override}
			resolver := newResolverWithChannel(t, []ChannelModelPricing{card})
			catalog := resolver.billingService.fallbackPrices["claude-sonnet-4"]
			catalog.ImageOutputPricePerToken = 30e-6
			catalog.ImageInputPricePerToken = 8e-6
			original := *catalog
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", GroupID: groupIDPtr()})
			require.Equal(t, PricingSourceChannel, resolved.Source)
			legacy, err := resolver.billingService.GetModelPricingWithChannel("claude-sonnet-4", &card)
			require.NoError(t, err)
			interval := intervalToModelPricing(&PricingInterval{InputPrice: testPtrFloat64(4e-6)}, resolved.BasePricing, &card)
			for _, pricing := range []*ModelPricing{resolved.BasePricing, legacy, interval} {
				require.Equal(t, test.expectedOutput, pricing.ImageOutputPricePerToken)
				require.Equal(t, test.expectedInput, pricing.ImageInputPricePerToken)
				require.Equal(t, test.override != nil, pricing.ImageOutputPriceExplicit)
			}
			require.Equal(t, original, *catalog)
			cost, err := resolver.billingService.CalculateCostUnified(CostInput{Model: "claude-sonnet-4", Tokens: UsageTokens{OutputTokens: 100, ImageOutputTokens: 100}, RateMultiplier: 1, Resolver: resolver, Resolved: resolved})
			require.NoError(t, err)
			require.InDelta(t, 100*test.expectedOutput, cost.TotalCost, 1e-12)
			catalog.ImageOutputPricePerToken = 1
			replay, err := RecalculatePricingSnapshot(cost.PricingSnapshot)
			require.NoError(t, err)
			require.Equal(t, cost.TotalCost, replay.TotalCost)
		})
	}
}
