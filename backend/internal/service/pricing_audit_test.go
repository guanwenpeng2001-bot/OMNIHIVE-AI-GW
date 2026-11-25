//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func TestPricingTermsIdentityUsesEffectiveMonetaryTerms(t *testing.T) {
	base := &ModelPricing{InputPricePerToken: 1e-6, OutputPricePerToken: 2e-6}
	input, output := 1e-6, 2e-6
	card := ChannelModelPricing{ID: 1, ChannelID: 2, Models: []string{"claude-sonnet-4"}, InputPrice: &input, OutputPrice: &output}
	card.Intervals = []PricingInterval{{ID: 3, PricingID: 4, SortOrder: 5, MinTokens: 1000, InputPrice: &input, OutputPrice: &output}}
	resolved := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceChannel, BasePricing: base, channelPricing: &card}
	cost := CostInput{Provider: PlatformAnthropic, Model: "claude-sonnet-4", RateMultiplier: 1}
	_, terms := pricingTermsIdentity(resolved, cost)
	require.NotEmpty(t, terms)

	card.ID, card.ChannelID, card.Models = 10, 20, []string{"another-model"}
	card.Intervals[0].ID, card.Intervals[0].PricingID, card.Intervals[0].SortOrder = 30, 40, 50
	_, equivalent := pricingTermsIdentity(resolved, cost)
	require.Equal(t, terms, equivalent)

	resolved.longContextPricingEnabled = true
	_, irrelevantLongContext := pricingTermsIdentity(resolved, cost)
	require.Equal(t, terms, irrelevantLongContext)

	base.LongContextInputThreshold = 200000
	base.LongContextInputMultiplier = 2
	_, applicableLongContext := pricingTermsIdentity(resolved, cost)
	require.NotEqual(t, terms, applicableLongContext)

	cost.RateMultiplier = 1.2
	_, multiplied := pricingTermsIdentity(resolved, cost)
	require.NotEqual(t, applicableLongContext, multiplied)
}

func TestPricingTermsIdentityKeepsConditionalAndTimeTerms(t *testing.T) {
	input, output := 1e-6, 2e-6
	base := &ModelPricing{InputPricePerToken: input, OutputPricePerToken: output}
	card := &ChannelModelPricing{Models: []string{"model"}, InputPrice: &input, OutputPrice: &output}
	resolved := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceChannel, BasePricing: base, channelPricing: card}
	cost := CostInput{Provider: PlatformOpenAI, Model: "model", PricingAt: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC), RateMultiplier: 1}
	_, original := pricingTermsIdentity(resolved, cost)

	card.TimePricing = &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "00:00", EndTime: "23:59", Multiplier: 2}}}
	_, scheduled := pricingTermsIdentity(resolved, cost)
	require.NotEqual(t, original, scheduled)

	card.TimePricing = nil
	card.ThinkingPricing = &QianwenModePricing{InputPrice: &input}
	_, conditional := pricingTermsIdentity(resolved, cost)
	require.NotEqual(t, original, conditional)
}
