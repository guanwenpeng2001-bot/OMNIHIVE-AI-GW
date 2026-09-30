package service

import (
	"context"
	"encoding/json"
	"errors"
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
	return result != nil && (result.AudioUsage != nil || result.AudioUsageUnresolved) && strings.HasSuffix(result.UpstreamEndpoint, "/services/aigc/multimodal-generation/generation") && qianwenNativeAudioMode(result.UpstreamModel) != ""
}

func (s *OpenAIGatewayService) calculateQianwenNativeAudioCost(ctx context.Context, result *OpenAIForwardResult, key *APIKey, multiplier float64) (*CostBreakdown, error) {
	if result.AudioUsageUnresolved {
		return nil, fmt.Errorf("%w: DashScope ASR duration requires review", ErrUpstreamJobUnsupportedUsage)
	}
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
	if !isQianwenNativeAudioResult(result) || result.AudioUsage == nil {
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

// A completed ASR response with unbillable duration must have a durable review
// receipt before either the native JSON or standard transcript is committed.
// Reuse the existing settlement state and request-plan producer claim; do not
// let the ordinary queued recorder turn missing units into text-token billing.
func (gateway *OpenAIGatewayService) recordQianwenASRUnresolved(ctx context.Context, result *OpenAIForwardResult, key *APIKey, account *Account, body []byte) error {
	if gateway == nil || gateway.usageSettlement == nil || gateway.usageSettlement.repo == nil || key == nil || account == nil {
		return errors.New("DashScope ASR review settlement unavailable")
	}
	usage := gjson.GetBytes(canonicalQianwenAudioBody(body), "usage")
	rawUsage := json.RawMessage(usage.Raw)
	if len(rawUsage) == 0 {
		rawUsage = json.RawMessage("null")
	}
	seconds := usage.Get("seconds")
	reason := "invalid_duration"
	if !seconds.Exists() {
		reason = "missing_duration"
	} else if seconds.Type == gjson.Number && seconds.Float() == 0 {
		reason = "zero_duration"
	}
	evidence, err := json.Marshal(struct {
		Reason         string          `json:"reason"`
		SecondsPresent bool            `json:"seconds_present"`
		Usage          json.RawMessage `json:"usage"`
	}{Reason: reason, SecondsPresent: seconds.Exists(), Usage: rawUsage})
	if err != nil {
		return err
	}
	requestID := resolveUsageBillingRequestID(ctx, result.RequestID)
	requestedModel := result.Model
	if original, present := RequestedPublicModelFromContext(ctx); present {
		requestedModel = original
	}
	receipt := &UsageSettlementReceipt{
		Key:     UsageSettlementKey{RequestID: requestID, UserID: key.UserID, APIKeyID: key.ID},
		Command: UsageBillingCommand{RequestID: requestID, UserID: key.UserID, APIKeyID: key.ID, AccountID: account.ID, AccountType: account.Type, Model: result.Model, MediaType: "audio", RequestPayloadHash: HashUsageRequestPayload(evidence)},
		Usage: UsageLog{RequestID: requestID, UserID: key.UserID, APIKeyID: key.ID, AccountID: account.ID, GroupID: key.GroupID, Model: result.Model, RequestedModel: requestedModel,
			UpstreamModel: optionalTrimmedStringPtr(result.UpstreamModel), UpstreamResponseModel: optionalTrimmedStringPtr(result.UpstreamResponseModel),
			PricingState: PricingStateError, PricingSnapshot: string(evidence)},
		ResolutionState: "unresolved", CompletionState: "complete",
	}
	err = gateway.usageSettlement.Submit(ctx, receipt, nil)
	if err != nil && !errors.Is(err, ErrUsageSettlementNeedsReview) {
		return err
	}
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
		plan.Record()
	}
	return nil
}
