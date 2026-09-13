//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQianwenThinkingBillingModesAndTiers(t *testing.T) {
	for _, tiered := range []bool{false, true} {
		m := qianwenTestModel()
		if tiered {
			m = qianwenTieredTestModel()
		}
		for i := range m.Prices {
			for _, p := range append([]QianwenPrice(nil), m.Prices[i].Prices...) {
				p.Type = "thinking_" + p.Type
				if p.Type == "thinking_output_token" {
					p.Price = "72"
				}
				m.Prices[i].Prices = append(m.Prices[i].Prices, p)
			}
		}
		parsed := parseQianwenModelPrices(m, 0.125)
		require.NotNil(t, parsed.Pricing, parsed.Reason)
		require.NoError(t, validatePricingEntries([]ChannelModelPricing{*parsed.Pricing}))
		bs := newTestBillingServiceForResolver()
		resolver := NewModelPricingResolver(nil, bs)
		group := &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{*parsed.Pricing}}
		resolved := resolver.Resolve(context.Background(), PricingInput{Model: m.Model, Group: group})
		original, _ := json.Marshal(group)
		for _, n := range []int{100, 128001} {
			for _, enabled := range []bool{false, true, false} {
				cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: m.Model, Group: group, Tokens: UsageTokens{InputTokens: n, OutputTokens: 10}, RateMultiplier: 1, Resolver: resolver, Resolved: resolved, ThinkingEnabled: &enabled})
				require.NoError(t, err)
				wantOutput := 0.0
				if !tiered {
					wantOutput = *parsed.Pricing.OutputPrice
				}
				if tiered {
					wantOutput = *FindMatchingInterval(parsed.Pricing.Intervals, n).OutputPrice
				}
				if enabled {
					wantOutput = 9e-6
				}
				require.InDelta(t, 10*wantOutput, cost.OutputCost, 1e-12)
			}
		}
		_, err := bs.CalculateCostUnified(CostInput{Model: m.Model, Group: group, Resolver: resolver, Resolved: resolved, RateMultiplier: 1})
		require.ErrorIs(t, err, ErrQianwenThinkingUndetermined)
		require.False(t, isUsagePricingUnavailableError(err))
		after, _ := json.Marshal(group)
		require.Equal(t, string(original), string(after))
	}
}

func TestQianwenExplicitThinkingDoesNotCoerce(t *testing.T) {
	for _, value := range []string{`null`, `"true"`, `1`, `{}`} {
		require.Nil(t, qianwenExplicitThinking([]byte(`{"enable_thinking":`+value+`}`), "enable_thinking"))
	}
	require.Nil(t, qianwenExplicitThinking([]byte(`{}`), "enable_thinking"))
	require.False(t, *qianwenExplicitThinking([]byte(`{"enable_thinking":false}`), "enable_thinking"))
	require.True(t, *qianwenExplicitThinking([]byte(`{"parameters":{"enable_thinking":true}}`), "parameters.enable_thinking"))
}
