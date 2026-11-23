package service

import (
	"context"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type saleMediaRate struct {
	mode   BillingMode
	rate   float64
	source string
}

func (s *BillingService) resolveSaleMediaRates(key *APIKey, binding pricing.Binding, resolved *ResolvedPricing) map[string]saleMediaRate {
	mode := BillingMode(binding.Mode)
	rates := map[string]saleMediaRate{}
	switch mode {
	case BillingModeImage:
		for _, tier := range []string{"1K", "2K", "4K"} {
			if resolved.channelPricing != nil && !apiKeyHasConfiguredImagePrice(key, tier) {
				continue
			}
			rate, source, err := s.imageSaleRate(binding.Provider, binding.Model, tier, imagePriceConfigFromAPIKey(key))
			if err == nil {
				rates[tier] = saleMediaRate{mode, rate, source}
			}
		}
	case BillingModeVideo:
		for _, tier := range []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P} {
			if resolved.channelPricing != nil && !apiKeyHasConfiguredVideoPrice(key, binding.Model, tier) {
				continue
			}
			rate, source, err := s.videoSaleRate(key, binding.Provider, binding.Model, tier)
			if err == nil {
				rates[tier] = saleMediaRate{mode, rate, source}
			}
		}
	}
	return rates
}

func (t *platformTariff) evaluateMedia(rate saleMediaRate, scenario pricing.Scenario) (pricing.Result, error) {
	input := t.input
	input.Mode = rate.mode
	input.Resolved = &ResolvedPricing{Mode: rate.mode}
	key := &APIKey{Group: input.Group, GroupID: input.GroupID}
	if rate.mode == BillingModeImage {
		input.RateMultiplier = resolveImageRateMultiplier(key, t.baseMultiplier)
	} else {
		input.RateMultiplier = resolveVideoRateMultiplier(key, t.baseMultiplier)
	}
	input, quantities, count, reason := platformScenario(input, scenario)
	if reason != "" {
		return pricing.Unavailable(reason), nil
	}
	cost := t.billing.calculateSaleUnits(input.Ctx, key, input.Provider, input.Model, input.Mode, input.SizeTier, rate.source, input.UsageUnits, rate.rate, input.RateMultiplier)
	if cost.PricingState == PricingStateError {
		return pricing.Result{}, ErrInvalidBillingAmount
	}
	return platformResult(cost, input, quantities, count)
}

func (s *BillingService) imageSaleRate(provider, model, size string, group *ImagePriceConfig) (float64, string, error) {
	var configured *float64
	if group != nil {
		switch size {
		case "1K":
			configured = group.Price1K
		case "2K":
			configured = group.Price2K
		case "4K":
			configured = group.Price4K
		}
	}
	if configured != nil {
		return *configured, PricingSourceGroup, nil
	}
	if provider == "" {
		return s.getDefaultImagePrice(model, size), "model_image_default", nil
	}
	if provider == PlatformGrok {
		if price, found := getDefaultGrokImagineImagePrice(model, size); found {
			return price, PricingSourceStaticFallback, nil
		}
	}
	card, err := s.pricingService.providerModelPricing(provider, model, true)
	if err != nil {
		return 0, "", err
	}
	if card == nil || IsGPTImageGenerationModel(model) {
		return 0, "", ErrModelPricingUnavailable
	}
	if quote := card.sourceQuotes["output_cost_per_image"]; quote.Value == nil && card.OutputCostPerImage == 0 {
		return 0, "", ErrModelPricingUnavailable
	}
	source := PricingSourceLiteLLM
	if card.staticTariff {
		source = PricingSourceStaticFallback
	}
	scale := 1.0
	switch size {
	case "2K":
		scale = 1.5
	case "4K":
		scale = 2
	}
	return card.OutputCostPerImage * scale, source, nil
}

func (s *BillingService) calculateImageSale(ctx context.Context, key *APIKey, model, size string, count int, multiplier float64) *CostBreakdown {
	provider := priceBookProviderForKey(ctx, key)
	group := imagePriceConfigFromAPIKey(key)
	if provider == "" {
		return s.CalculateImageCost(model, size, count, group, multiplier)
	}
	size = NormalizeImageBillingTierOrDefault(size)
	rate, source, err := s.imageSaleRate(provider, model, size, group)
	if err != nil {
		return pricingFailure(err)
	}
	return s.calculateSaleUnits(ctx, key, provider, model, BillingModeImage, size, source, float64(count), rate, multiplier)
}

func (s *BillingService) videoSaleRate(key *APIKey, provider, model, resolution string) (float64, string, error) {
	if apiKeyHasConfiguredVideoPrice(key, model, resolution) {
		return *key.Group.GetVideoPriceForModel(model, resolution), PricingSourceGroup, nil
	}
	if provider == PlatformGrok {
		if rate, known := getDefaultGrokImagineVideoPrice(model, resolution); known {
			return rate, PricingSourceStaticFallback, nil
		}
	}
	return 0, "", ErrModelPricingUnavailable
}

func (s *BillingService) calculateVideoSale(ctx context.Context, key *APIKey, model, resolution string, count, duration int, multiplier float64) *CostBreakdown {
	provider := priceBookProviderForKey(ctx, key)
	if provider == "" {
		return s.CalculateVideoCost(model, resolution, count, duration, videoPriceConfigFromAPIKey(key), multiplier)
	}
	if duration < 0 || count < 0 {
		return pricingFailure(ErrInvalidBillingAmount)
	}
	resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
	rate, source, err := s.videoSaleRate(key, provider, model, resolution)
	if err != nil {
		return pricingFailure(err)
	}
	units := float64(NormalizeVideoBillingDurationSecondsOrDefault(duration)) * float64(count)
	return s.calculateSaleUnits(ctx, key, provider, model, BillingModeVideo, resolution, source, units, rate, multiplier)
}

func (s *BillingService) calculateSaleUnits(ctx context.Context, key *APIKey, provider, model string, mode BillingMode, label, source string, units, rate, multiplier float64) *CostBreakdown {
	if !validBillingNumber(rate) || !validBillingNumber(units) {
		return pricingFailure(ErrInvalidBillingAmount)
	}
	resolved := &ResolvedPricing{Mode: mode, Source: source, DefaultPerRequestPrice: rate, DefaultPerRequestPriceFound: true}
	input := CostInput{Provider: provider, Model: model, Mode: mode, GroupID: pricingGroupID(key), SizeTier: label,
		PricingAt: pricingInstant(ctx, time.Time{}), RateMultiplier: multiplier}
	source, version := pricingEvaluationIdentity(resolved, input)
	cost := &CostBreakdown{TotalCost: units * rate, ActualCost: units * rate * multiplier, BillingMode: string(mode), PriceSource: source, PriceCardVersion: version}
	if err := freezePricing(cost, PricingSnapshot{Model: model, Provider: provider, Mode: string(mode), Source: source, PriceCardVersion: version,
		PricingAt: input.PricingAt, UnitPrice: &rate, Units: units, SizeTier: label, RateMultiplier: multiplier, TimeMultiplier: 1, ReasoningMultiplier: 1}); err != nil {
		return pricingFailure(err)
	}
	return cost
}
