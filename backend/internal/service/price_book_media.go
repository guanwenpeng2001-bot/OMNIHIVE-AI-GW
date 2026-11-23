package service

import (
	"context"
	"math"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

const (
	legacyImageCatalogSource = "legacy-fallback:image-global"
	legacyImageDefaultSource = "legacy-fallback:image-default"
	legacyVideoImageSource   = "legacy-fallback:video-image-2k"
	legacyGrokMediaSource    = "legacy-fallback:grok-media"
	legacyMediaVariantSource = "legacy-fallback:media-variant"
	legacyGPTImageSource     = "legacy-fallback:gpt-image-token"
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
	if provider != "" && !IsGPTImageGenerationModel(model) {
		card, err := s.pricingService.providerModelPricing(provider, model, true)
		if err != nil {
			return 0, "", err
		}
		if card != nil && card.tariffDeleted {
			return 0, "", ErrModelPricingUnavailable
		}
		if hasScopedImageRate(card) {
			source := PricingSourceLiteLLM
			if card.staticTariff {
				source = PricingSourceStaticFallback
			}
			return scaleImageRate(card.OutputCostPerImage, size), source, nil
		}
	}
	rate, source := s.legacyDefaultImageRate(model, size)
	if math.IsNaN(rate) && IsGPTImageGenerationModel(model) {
		return 0, "", ErrModelPricingUnavailable
	}
	if provider == PlatformGrok && source == legacyGrokMediaSource {
		source = PricingSourceStaticFallback
	}
	return rate, source, nil
}

func hasScopedImageRate(card *LiteLLMModelPricing) bool {
	return card != nil && (card.sourceQuotes["output_cost_per_image"].Value != nil || card.OutputCostPerImage != 0)
}

func scaleImageRate(base float64, size string) float64 {
	switch size {
	case "2K":
		return base * 1.5
	case "4K":
		return base * 2
	default:
		return base
	}
}

func (s *BillingService) calculateImageSale(ctx context.Context, key *APIKey, model, size string, count int, multiplier float64) *CostBreakdown {
	provider := priceBookProviderForKey(ctx, key)
	group := imagePriceConfigFromAPIKey(key)
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
	if rate, known := getDefaultGrokImagineVideoPrice(model, resolution); known {
		if provider == PlatformGrok {
			return rate, PricingSourceStaticFallback, nil
		}
		return rate, legacyGrokMediaSource, nil
	}
	// v0.3.3 used the default 2K image rate as a per-second video tariff.
	rate := s.getDefaultImagePrice(model, ImageBillingSize2K)
	if !validBillingNumber(rate) {
		return 0, "", ErrInvalidBillingAmount
	}
	return rate, legacyVideoImageSource, nil
}

func (s *BillingService) calculateVideoSale(ctx context.Context, key *APIKey, model, resolution string, count, duration int, multiplier float64) *CostBreakdown {
	provider := priceBookProviderForKey(ctx, key)
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
	if !validBillingNumber(rate) || !validBillingNumber(units) || !validBillingNumber(multiplier) {
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
