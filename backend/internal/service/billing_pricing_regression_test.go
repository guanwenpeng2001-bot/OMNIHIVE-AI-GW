//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCompositeGroupPricingUsesTargetPlatform(t *testing.T) {
	ccIn, qwIn, out := 0.2e-6, 1.35/7e6, 1e-6
	group := &Group{Platform: PlatformComposite, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformCommandCode, Models: []string{"stepfun/Step-3.7-Flash"}, InputPrice: &ccIn, OutputPrice: &out},
		{Platform: PlatformQianwen, Models: []string{"stepfun/step-3.7-flash"}, InputPrice: &qwIn, OutputPrice: &out},
	}}
	r := NewModelPricingResolver(nil, newTestBillingServiceForResolver())
	for platform, price := range map[string]float64{PlatformCommandCode: ccIn, PlatformQianwen: qwIn} {
		resolved := r.Resolve(WithResolvedTargetPlatform(context.Background(), platform), PricingInput{Model: "stepfun/step-3.7-flash", Group: group})
		require.Equal(t, platform, resolved.channelPricing.Platform)
		require.Equal(t, price, resolved.BasePricing.InputPricePerToken)
	}
	require.Nil(t, matchGroupModelPricing(group, "stepfun/step-3.7-flash"))
	require.Nil(t, matchGroupModelPricing(group, "stepfun/step-3.7-flash", PlatformOpenCodeGo))
}

func TestQianwenTurboDefaultNonzeroCost(t *testing.T) {
	in, out, think := 0.3/7e6, 0.6/7e6, 3.0/7e6
	group := &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{{Platform: PlatformQianwen, NativePricingSource: QianwenPricingSource, Models: []string{"qwen-turbo"}, InputPrice: &in, OutputPrice: &out, ThinkingPricing: &QianwenModePricing{InputPrice: &in, OutputPrice: &think}}}}
	bs := newTestBillingServiceForResolver()
	r := NewModelPricingResolver(nil, bs)
	for _, enabled := range []*bool{nil, new(bool), func() *bool { v := true; return &v }()} {
		cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "qwen-turbo", Group: group, Resolver: r, ThinkingEnabled: enabled, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 100}, RateMultiplier: 1})
		require.NoError(t, err)
		want := 1000*in + 100*out
		if enabled != nil && *enabled {
			want = 1000*in + 100*think
		}
		require.InDelta(t, want, cost.TotalCost, 1e-12)
		t.Logf("thinking=%v cost=%.12f", enabled, cost.TotalCost)
	}
}

func TestCursorComposerFastUsesVariantPrice(t *testing.T) {
	bs := newTestBillingServiceForResolver()
	bs.initFallbackPricing()
	in, out, cache := 0.5e-6, 2.5e-6, 0.2e-6
	group := &Group{Platform: PlatformCursor, ModelPricing: []ChannelModelPricing{{Platform: PlatformCursor, Models: []string{"composer-2.5"}, InputPrice: &in, OutputPrice: &out, CacheReadPrice: &cache}}}
	s := &GatewayService{billingService: bs, resolver: NewModelPricingResolver(nil, bs)}
	result := &ForwardResult{Model: "composer-2.5", UpstreamModel: "composer-2.5-fast", Usage: ClaudeUsage{InputTokens: 3480, OutputTokens: 34, CacheReadInputTokens: 416}}
	cost := s.calculateTokenCost(context.Background(), result, &APIKey{Group: group}, result.Model, 1, time.Now())
	require.InDelta(t, 0.011158, cost.TotalCost, 1e-12)
	t.Logf("Fast replay cost=%.12f", cost.TotalCost)
	result.UpstreamModel = "composer-2.5"
	cost = s.calculateTokenCost(context.Background(), result, &APIKey{Group: group}, result.Model, 1, time.Now())
	require.InDelta(t, 0.0019082, cost.TotalCost, 1e-12)
}

func TestCommandCodeReasoningCapabilityWhitelist(t *testing.T) {
	for _, model := range []string{"moonshotai/Kimi-K2.7-Code-Highspeed", "unknown/model"} {
		for _, effort := range []string{"low", "medium", "high"} {
			body, err := normalizeCommandCodeReasoning([]byte(`{"reasoning_effort":"`+effort+`"}`), model)
			require.NoError(t, err)
			require.Equal(t, model != "unknown/model" && effort != "medium", gjson.GetBytes(body, "reasoning_effort").Exists())
		}
	}
}
