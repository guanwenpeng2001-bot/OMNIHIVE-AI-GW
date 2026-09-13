//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPricingAuditIdentityChangesWithCard(t *testing.T) {
	input, output := 1e-6, 2e-6
	card := ChannelModelPricing{Platform: PlatformQianwen, Models: []string{"qwen-flash"}, InputPrice: &input, OutputPrice: &output, NativePricingSource: QianwenPricingSource}
	bs := newTestBillingServiceForResolver()
	resolver := NewModelPricingResolver(nil, bs)
	group := &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{card}}
	calculate := func() *CostBreakdown {
		cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "qwen-flash", Group: group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10}, RateMultiplier: 1})
		require.NoError(t, err)
		return cost
	}
	first := calculate()
	require.Equal(t, "group:"+QianwenPricingSource, first.PriceSource)
	require.Len(t, first.PriceCardVersion, 71)
	require.Equal(t, first.PriceCardVersion, calculate().PriceCardVersion)
	output = 3e-6
	require.NotEqual(t, first.PriceCardVersion, calculate().PriceCardVersion)
	source, version := pricingAuditIdentity(nil)
	require.Empty(t, source)
	require.Empty(t, version)
}
