//go:build unit

package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

type ShadowPrice struct {
	State                   PricingState
	Source, Version, Amount string
}

type PriceBookShadowDelta struct {
	Legacy, Current ShadowPrice
}

func PriceBookShadow(ctx context.Context, resolver *ModelPricingResolver, input CostInput) PriceBookShadowDelta {
	input.Ctx, input.Resolver = ctx, resolver
	if input.GroupID == nil && input.Group != nil && input.Group.ID != 0 {
		input.GroupID = &input.Group.ID
	}
	if input.PricingAt.IsZero() {
		input.PricingAt = pricingInstant(ctx, input.PricingAt)
	}
	pricingInput := PricingInput{Mode: input.Mode, Model: input.Model, Group: input.Group, GroupID: input.GroupID, Provider: input.Provider, At: input.PricingAt}
	evaluate := func(legacy bool) ShadowPrice {
		pricingInput.shadowLegacy = legacy
		input.Resolved = resolver.Resolve(ctx, pricingInput)
		cost, _ := resolver.billingService.CalculateCostUnified(input)
		return ShadowPrice{State: cost.PricingState, Source: cost.PriceSource, Version: cost.PriceCardVersion, Amount: quantizedBillingDecimal(cost.ActualCost).StringFixed(8)}
	}
	return PriceBookShadowDelta{Legacy: evaluate(true), Current: evaluate(false)}
}

func NewPriceBookShadowResolver(pricesPath, fallbackPath, overridePath string, channels *ChannelService) (*ModelPricingResolver, error) {
	cfg := &config.Config{}
	cfg.Pricing.FallbackFile, cfg.Pricing.OverrideFile = fallbackPath, overridePath
	source := &PricingService{cfg: cfg}
	body := modelpricing.JSON()
	if pricesPath != "" {
		var err error
		body, err = os.ReadFile(pricesPath)
		if err != nil {
			return nil, err
		}
	}
	cards, _, err := source.buildPricingData(body)
	if err != nil {
		return nil, err
	}
	source.replacePricingData(cards)
	return NewModelPricingResolver(channels, NewBillingService(cfg, source)), nil
}

func TestPriceBookShadowParity(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	resolver, err := NewPriceBookShadowResolver("", "", "", nil)
	require.NoError(t, err)
	for _, fixture := range []struct {
		provider, model        string
		group                  *Group
		tokens                 UsageTokens
		rate                   float64
		wantSource, correction string
	}{
		{PlatformOpenAI, "gpt-5.4", nil, UsageTokens{InputTokens: 1234, OutputTokens: 47}, 1.02, PricingSourceLiteLLM, ""},
		{PlatformOpenAI, "gpt-5.5", nil, UsageTokens{InputTokens: 280001, OutputTokens: 89, CacheReadTokens: 9200}, 1, PricingSourceLiteLLM, ""},
		{PlatformAnthropic, "claude-fable-5-1", nil, UsageTokens{InputTokens: 17, OutputTokens: 3}, 1, PricingSourceStaticFallback, ""},
		{PlatformAntigravity, "gemini-3.6-flash-high", nil, UsageTokens{InputTokens: 440, OutputTokens: 45}, 1, PricingSourceLiteLLM, ""},
		{PlatformOpenRouter, "gpt-5.4", nil, UsageTokens{InputTokens: 12, OutputTokens: 3}, 1, "", "unbound aggregator cannot inherit the OpenAI source"},
		{PlatformQianwen, "native", pricingFactsGroup(), UsageTokens{InputTokens: 4, OutputTokens: 1}, 1.25, PricingSourceGroup, ""},
		{PlatformAnthropic, "claude-sonnet-4", &Group{ID: 7, Platform: PlatformAnthropic, ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, InputPrice: priceFactNumber(0), OutputPrice: priceFactNumber(0)}}}, UsageTokens{InputTokens: 8, OutputTokens: 2}, 2, PricingSourceGroup, ""},
	} {
		t.Run(fixture.provider+"/"+fixture.model, func(t *testing.T) {
			report := PriceBookShadow(ctx, resolver, CostInput{Provider: fixture.provider, Model: fixture.model, Group: fixture.group,
				PricingAt: at, Tokens: fixture.tokens, RateMultiplier: fixture.rate})
			t.Logf("%+v", report)
			if fixture.correction == "" {
				require.Equal(t, report.Legacy.State, report.Current.State)
				require.Equal(t, report.Legacy.Amount, report.Current.Amount)
			} else {
				t.Log(fixture.correction)
				require.Equal(t, PricingStatePriced, report.Legacy.State)
			}
			require.Equal(t, fixture.wantSource, report.Current.Source)
			if fixture.wantSource == "" {
				require.Equal(t, PricingStateUnpriced, report.Current.State)
				require.Equal(t, "0.00000000", report.Current.Amount)
			}
		})
	}
}
