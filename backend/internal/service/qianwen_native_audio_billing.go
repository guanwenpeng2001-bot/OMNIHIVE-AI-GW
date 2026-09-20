package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/tidwall/gjson"
)

// Audio group prices have explicit units: USD/million characters and USD/hour.
// Generic per-request cards do not establish which audio unit their price uses.
// Reject those cards and missing rates before consuming upstream resources.
func (s *OpenAIGatewayService) qianwenNativeAudioPrice(ctx context.Context, key *APIKey, model string) (*audioPriceConfig, error) {
	if key == nil || key.Group == nil {
		return nil, fmt.Errorf("%w: DashScope audio requires an explicit group audio rate", ErrModelPricingUnavailable)
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(nil, s.billingService)
	}
	descriptor, found, err := resolver.configuredPriceDescriptor(ctx, PricingInput{Model: model, Group: key.Group, GroupID: key.GroupID})
	if err != nil {
		return nil, err
	}
	if found {
		unit, scale := "character", 1_000_000.0
		if qianwenNativeAudioMode(model) == "stt" {
			unit, scale = "second", 3600
		}
		price, err := descriptor.UnitPrice(unit, "output", "")
		if err != nil {
			return nil, err
		}
		amount := *price.Sales * scale
		if unit == "character" {
			return &audioPriceConfig{TTSPerMChars: &amount}, nil
		}
		return &audioPriceConfig{STTPerHour: &amount}, nil
	}
	if resolved := s.resolveOpenAIChannelPricing(ctx, model, key); resolved != nil && resolved.Mode == BillingModePerRequest {
		return nil, fmt.Errorf("unsupported DashScope audio: per-request model cards have no verified audio unit; use an explicit group audio rate")
	}
	cfg := groupAudioPriceConfigFromAPIKey(key)
	var price *float64
	switch qianwenNativeAudioMode(model) {
	case "tts":
		price = cfg.TTSPerMChars
	case "stt":
		price = cfg.STTPerHour
	default:
		return nil, fmt.Errorf("unsupported DashScope audio billing model")
	}
	if price == nil || !validBillingNumber(*price) {
		return nil, fmt.Errorf("%w: DashScope audio requires an explicit nonnegative group audio rate (TTS USD/million characters; ASR USD/hour)", ErrModelPricingUnavailable)
	}
	return cfg, nil
}

func isQianwenNativeAudioResult(result *OpenAIForwardResult) bool {
	return result != nil && result.AudioUsage != nil && strings.HasSuffix(result.UpstreamEndpoint, "/services/aigc/multimodal-generation/generation") && qianwenNativeAudioMode(result.UpstreamModel) != ""
}

func (s *OpenAIGatewayService) calculateQianwenNativeAudioCost(ctx context.Context, result *OpenAIForwardResult, key *APIKey, multiplier float64) (*CostBreakdown, error) {
	cfg, err := s.qianwenNativeAudioPrice(ctx, key, result.UpstreamModel)
	if err != nil {
		return nil, err
	}
	u := result.AudioUsage
	if u.Mode != qianwenNativeAudioMode(result.UpstreamModel) || !positiveFinite(u.DurationOrUnits) || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 0 {
		return nil, fmt.Errorf("invalid DashScope audio billing units or multiplier")
	}
	cost := s.billingService.CalculateAudioCost(u.Mode, u.DurationOrUnits, cfg, multiplier)
	if cost.PricingState == PricingStateError || ValidateUsageBillingAmount(cost.TotalCost) != nil {
		return nil, fmt.Errorf("invalid DashScope audio cost")
	}
	cost.PriceSource = "qianwen_audio_group"
	return cost, nil
}

// Persist the provider quantity in the existing BillingTier text field so a
// usage row is auditable without pretending that characters/seconds are tokens.
// A future dedicated audio schema can migrate these versioned labels losslessly.
func recordQianwenNativeAudioUnits(log *UsageLog, result *OpenAIForwardResult) {
	if !isQianwenNativeAudioResult(result) {
		return
	}
	field, scale := "seconds", 3600.0
	if result.AudioUsage.Mode == "tts" {
		field, scale = "characters", 1_000_000
	}
	label := fmt.Sprintf("qianwen_audio_v1:%s=%g", field, result.AudioUsage.DurationOrUnits*scale)
	log.BillingTier = &label
	if log.TotalCost > 0 {
		log.RateMultiplier = log.ActualCost / log.TotalCost
	}
}

// QianwenNativeAudioAuditText supplies TTS text to the existing moderation view.
// Original native JSON remains the upstream request body.
func QianwenNativeAudioAuditText(body []byte) (string, bool) {
	body = canonicalQianwenAudioBody(body)
	if qianwenNativeAudioMode(gjson.GetBytes(body, "model").String()) != "tts" {
		return "", false
	}
	return gjson.GetBytes(body, "input.text").String(), true
}
