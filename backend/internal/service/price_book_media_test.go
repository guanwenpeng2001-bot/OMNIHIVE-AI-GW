//go:build unit

package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

func TestPriceBookMediaV033Parity(t *testing.T) {
	prices := &PricingService{}
	data, err := prices.parsePricingData([]byte(`{
		"legacy-image": {"output_cost_per_image": 0.12},
		"scoped-image": {"litellm_provider": "gemini", "output_cost_per_image": 0.16},
		"reference-only": {"price_role": "reference", "output_cost_per_image": 99},
		"legacy-variant": {"price_variants": [{
			"id": "legacy-variant", "canonical": "legacy-variant", "prices": [
				{"unit": "image", "dimension": "output", "currency": "USD", "sales": 0.2},
				{"unit": "second", "dimension": "output", "currency": "USD", "sales": 0.25}
			]
		}]},
		"scoped-video": {"litellm_provider": "openrouter", "price_variants": [{
			"id": "scoped-video", "canonical": "scoped-video", "prices": [
				{"unit": "second", "dimension": "output", "currency": "USD", "sales": 0.3}
			]
		}]}
	}`))
	require.NoError(t, err)
	require.NoError(t, indexModelPriceVariants(data))
	prices.replacePricingData(data)
	billing := NewBillingService(nil, prices)
	resolver := NewModelPricingResolver(nil, billing)
	gateway := &OpenAIGatewayService{billingService: billing, resolver: resolver}
	genericGateway := &GatewayService{billingService: billing, resolver: resolver}
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	const multiplier = 1.25

	for _, tc := range []struct {
		name, provider, model string
		mode                  BillingMode
		source, amount        string
	}{
		{"provider-less image", "", "models/legacy-image", BillingModeImage, legacyImageCatalogSource, "0.45000000"},
		{"provider-less video", "", "legacy-image", BillingModeVideo, legacyVideoImageSource, "1.80000000"},
		{"provider-scoped image", PlatformGemini, "scoped-image", BillingModeImage, PricingSourceLiteLLM, "0.60000000"},
		{"provider-scoped video", PlatformOpenRouter, "scoped-video", BillingModeVideo, PricingSourceLiteLLM, "3.00000000"},
		{"scoped request using global image rate", PlatformOpenRouter, "models/legacy-image", BillingModeImage, legacyImageCatalogSource, "0.45000000"},
		{"scoped request using the built-in image rate", PlatformOpenRouter, "reference-only", BillingModeImage, legacyImageDefaultSource, "0.50250000"},
		{"non-Grok video using global 2K image rate", PlatformOpenRouter, "legacy-image", BillingModeVideo, legacyVideoImageSource, "1.80000000"},
		{"non-Grok video using built-in 2K image rate", PlatformOpenRouter, "reference-only", BillingModeVideo, legacyVideoImageSource, "2.01000000"},
		{"legacy global image variant", PlatformOpenRouter, "legacy-variant", BillingModeImage, legacyMediaVariantSource, "0.50000000"},
		{"legacy global video variant", PlatformOpenRouter, "legacy-variant", BillingModeVideo, legacyMediaVariantSource, "2.50000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.provider != "" {
				ctx = WithResolvedTargetPlatform(ctx, tc.provider)
			}
			ctx, plan := captureGatewayRequestPlan(ctx, GatewayRequestSpec{PricingAt: at}, nil, billing, nil)
			key := &APIKey{}
			var legacy *CostBreakdown
			forward := &OpenAIForwardResult{Model: tc.model}
			meter := BudgetMeter{Count: 2, Denominator: 1}
			var kind, quantity string
			var unit money.Unit
			switch tc.mode {
			case BillingModeImage:
				meter.Unit, meter.Size, meter.Numerator = "image", "2K", 1
				forward.ImageSize, forward.ImageCount = meter.Size, int(meter.Count)
				legacy = billing.CalculateImageCost(tc.model, meter.Size, forward.ImageCount, nil, multiplier)
				kind, quantity, unit = "image", "2", "image"
				generic := genericGateway.calculateImageCost(ctx, &ForwardResult{ImageCount: forward.ImageCount, ImageSize: forward.ImageSize}, key, tc.model, multiplier)
				require.Equal(t, tc.amount, quantizedBillingDecimal(generic.ActualCost).StringFixed(8))
				require.Equal(t, tc.source, generic.PriceSource)
			case BillingModeVideo:
				meter.Unit, meter.Size, meter.Numerator = "video_second", "720p", 4
				forward.VideoResolution, forward.VideoCount, forward.VideoDurationSeconds = meter.Size, int(meter.Count), int(meter.Numerator)
				legacy = billing.CalculateVideoCost(tc.model, meter.Size, forward.VideoCount, forward.VideoDurationSeconds, nil, multiplier)
				kind, quantity, unit = "video", "8", "second"
			}

			cost, err := gateway.calculateOpenAIRecordUsageCost(ctx, forward, key, []string{tc.model}, 1, multiplier, multiplier, 1, UsageTokens{}, "", nil, at)
			require.NoError(t, err)
			require.Equal(t, PricingStatePriced, legacy.PricingState)
			require.Equal(t, legacy.PricingState, cost.PricingState)
			require.Equal(t, legacy.ActualCost, cost.ActualCost)
			require.Equal(t, tc.amount, quantizedBillingDecimal(cost.ActualCost).StringFixed(8))
			require.Equal(t, tc.source, cost.PriceSource)
			require.NotEmpty(t, cost.PriceCardVersion)
			var snapshot PricingSnapshot
			require.NoError(t, json.Unmarshal([]byte(cost.PricingSnapshot), &snapshot))
			require.Equal(t, tc.provider, snapshot.Provider)
			require.Equal(t, tc.source, snapshot.Source)
			require.Equal(t, cost.PriceCardVersion, snapshot.PriceCardVersion)
			for _, receipt := range []*CostBreakdown{legacy, cost} {
				replay, err := ReplayExactPricing(receipt.PricingSnapshot)
				require.NoError(t, err)
				require.NotNil(t, replay.Charge)
				require.Equal(t, tc.amount, replay.Charge.String())
			}

			reserve := plan.estimateBudgetHold(ctx, RequestBudgetEstimate{Model: tc.model, Meter: &meter, RateMultiplier: multiplier})
			require.NotNil(t, reserve)
			require.Equal(t, tc.amount, reserve.String())
			if tc.provider != "" {
				tariff, err := NewPlatformPriceBook(resolver).Resolve(ctx, pricing.Binding{
					Provider: tc.provider, Model: tc.model, Mode: string(tc.mode), At: at, RateMultiplier: multiplier,
				})
				require.NoError(t, err)
				q, err := money.NewQuantity(unit, quantity)
				require.NoError(t, err)
				quote, err := tariff.Evaluate(pricing.Scenario{Usage: pricing.Usage{
					Label: meter.Size, Dimensions: []pricing.Dimension{{Kind: kind, Quantity: q}},
				}})
				require.NoError(t, err)
				require.NotNil(t, quote.Charge)
				require.Equal(t, tc.amount, quote.Charge.String())
				require.Equal(t, tc.source, *quote.Source)
			}
		})
	}
}

func TestPriceBookGPTImageV033TokenFallbackParity(t *testing.T) {
	const model = "gpt-image-legacy-fixture"
	prices := &PricingService{}
	data, err := prices.parsePricingData([]byte(`{
		"gpt-image-legacy-fixture": {
			"input_cost_per_token": 0.000005,
			"output_cost_per_image_token": 0.00003
		},
		"openrouter/gpt-image-legacy-fixture": {
			"input_cost_per_token": 0.000007,
			"output_cost_per_image_token": 0.00006
		}
	}`))
	require.NoError(t, err)
	prices.replacePricingData(data)
	billing := NewBillingService(nil, prices)
	resolver := NewModelPricingResolver(nil, billing)
	gateway := &OpenAIGatewayService{billingService: billing, resolver: resolver}
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 200, ImageOutputTokens: 200}
	legacy, err := billing.CalculateCost(model, tokens, 1.5)
	require.NoError(t, err)
	require.Equal(t, "0.00975000", quantizedBillingDecimal(legacy.ActualCost).StringFixed(8))
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name, provider, source, amount string
	}{
		{"provider-less historical token lane", "", legacyGPTImageSource, "0.00975000"},
		{"provider with legacy global fallback", PlatformOpenAI, legacyGPTImageSource, "0.00975000"},
		{"provider exact token card takes precedence", PlatformOpenRouter, PricingSourceLiteLLM, "0.01905000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.provider != "" {
				ctx = WithResolvedTargetPlatform(ctx, tc.provider)
			}
			forward := &OpenAIForwardResult{Model: model, ImageCount: 1, ImageSize: "2K"}
			key := &APIKey{}
			require.NoError(t, gateway.validateOpenAIImagePricing(ctx, key, model, forward.ImageSize))
			cost, err := gateway.calculateOpenAIRecordUsageCost(ctx, forward, key, []string{model}, 1.5, 1, 1, 1, tokens, "", nil, at)
			require.NoError(t, err)
			require.Equal(t, PricingStatePriced, cost.PricingState)
			require.Equal(t, string(BillingModeToken), cost.BillingMode)
			require.Equal(t, tc.source, cost.PriceSource)
			require.Equal(t, tc.amount, quantizedBillingDecimal(cost.ActualCost).StringFixed(8))
			require.NotEmpty(t, cost.PriceCardVersion)
			if tc.source == legacyGPTImageSource {
				require.Equal(t, legacy.ActualCost, cost.ActualCost)
			}
			replay, err := ReplayExactPricing(cost.PricingSnapshot)
			require.NoError(t, err)
			require.Equal(t, tc.amount, replay.Charge.String())
			_, err = gateway.calculateOpenAIRecordUsageCost(ctx, forward, key, []string{model}, 1.5, 1, 1, 1, UsageTokens{}, "", nil, at)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}

func TestPriceBookMediaScopedZeroPrecedesLegacyImageRate(t *testing.T) {
	prices := &PricingService{}
	data, err := prices.parsePricingData([]byte(`{
		"image-fixture": {"output_cost_per_image": 0.12},
		"openrouter/image-fixture": {"output_cost_per_image": 0}
	}`))
	require.NoError(t, err)
	prices.replacePricingData(data)
	billing := NewBillingService(nil, prices)
	ctx := WithResolvedTargetPlatform(t.Context(), PlatformOpenRouter)
	cost := billing.calculateImageSale(ctx, nil, "image-fixture", "2K", 1, 1)
	require.Equal(t, PricingStateFree, cost.PricingState)
	require.Zero(t, cost.ActualCost)
	require.Equal(t, PricingSourceLiteLLM, cost.PriceSource)
}
