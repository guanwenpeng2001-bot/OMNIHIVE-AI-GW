package service

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// CompareChannelPricing reads exact catalog quotes. A protocol/platform label is
// not a deployment profile; callers must obtain the profile from account evidence.
// Conditional, tiered and media cards remain incomparable rather than guessing
// a unit conversion or treating a base price as the effective charge.
func (pricingService *PricingService) CompareChannelPricing(model, profile, taskType string, card ChannelModelPricing) []PricingDifference {
	pricingService.mu.RLock()
	defer pricingService.mu.RUnlock()
	fields := map[string]*float64{
		"input_cost_per_token": card.InputPrice, "output_cost_per_token": card.OutputPrice,
		"cache_creation_input_token_cost": card.CacheWritePrice, "cache_read_input_token_cost": card.CacheReadPrice,
		"cache_creation_input_token_cost_above_1hr": card.CacheWrite1hPrice,
		"input_cost_per_image_token":                card.ImageInputPrice, "output_cost_per_image_token": card.ImageOutputPrice,
	}
	switch card.BillingMode {
	case BillingModeImage:
		fields = map[string]*float64{"output_cost_per_image": card.PerRequestPrice}
	case BillingModePerRequest:
		fields = map[string]*float64{"per_request_price": card.PerRequestPrice}
	}
	result := make([]PricingDifference, 0, len(fields))
	for field, value := range fields {
		var reference PricingQuote
		if pricing := pricingService.pricingData[model]; pricing != nil {
			reference = pricing.sourceQuotes[field]
		}
		unit := pricingPreviewUnits[field]
		if card.BillingMode != BillingModeToken {
			unit = "request"
		}
		channel := PricingQuote{Value: value, Currency: "USD", Unit: unit}
		if profile != "" {
			channel.Scope = profile + "/" + model
		}
		if taskType == "text_generation" && card.BillingMode == BillingModeToken && len(card.Intervals) == 0 && card.TimePricing == nil {
			channel.Conditions = `{"mode":"chat"}`
		}
		// The catalog quote is the denominator of channel-to-reference deviation.
		result = append(result, ComparePricingQuotes("channel_to_reference", model, field, reference, channel))
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Field < result[right].Field })
	return result
}

// PricingPreviewSample describes a NEW request against saved group/channel
// configuration. No draft price or capability is written into a billing plan.
func (channels *ChannelService) PricingPreviewSample(ctx context.Context, groupID int64, provider, model string, inputTokens, outputTokens int) (CostInput, error) {
	group, err := channels.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return CostInput{}, err
	}
	if group.Platform != PlatformComposite && group.Platform != provider {
		return CostInput{}, fmt.Errorf("group platform does not match preview provider")
	}
	previewAt := time.Now()
	return CostInput{Ctx: WithResolvedTargetPlatform(ctx, provider), Provider: provider, Model: model, GroupID: &groupID, Group: group,
		Tokens:         UsageTokens{InputTokens: inputTokens, OutputTokens: outputTokens},
		RequestCount:   1,
		RateMultiplier: group.RateMultiplier * group.PeakMultiplierAt(previewAt), PricingAt: previewAt}, nil
}

// ChannelPricingPreview is an administrative read projection, never a save DTO.
type ChannelPricingPreview struct {
	Differences []PricingDifference   `json:"differences"`
	Source      *PricingSourcePreview `json:"source,omitempty"`
	Impacts     []PricingChargeImpact `json:"impacts,omitempty"`
}
