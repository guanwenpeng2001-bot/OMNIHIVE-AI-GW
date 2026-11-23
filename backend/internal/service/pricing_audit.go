package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// The content hash identifies the exact resolved card, including conditional
// rates and channel scheduling. It is not a guessed upstream publication date.
func pricingAuditIdentity(resolved *ResolvedPricing) (string, string) {
	if resolved == nil || resolved.Source == "" {
		return "", ""
	}
	var descriptor *ModelPricingDescriptor
	if resolved.descriptor != nil {
		copy := *resolved.descriptor
		copy.PricingAt = time.Time{}
		descriptor = &copy
	}
	payload := struct {
		Resolved    *ResolvedPricing
		Config      *ChannelModelPricing
		LongContext bool
		Descriptor  *ModelPricingDescriptor
		BaseSource  string
	}{resolved, resolved.channelPricing, resolved.longContextPricingEnabled, descriptor, resolved.baseSource}
	data, err := json.Marshal(payload)
	if err != nil {
		return resolved.Source, ""
	}
	digest := sha256.Sum256(data)
	source := resolved.Source
	if resolved.channelPricing != nil && resolved.channelPricing.NativePricingSource != "" {
		source += ":" + resolved.channelPricing.NativePricingSource
	}
	return source, "sha256:" + hex.EncodeToString(digest[:])
}

func pricingEvaluationIdentity(resolved *ResolvedPricing, input CostInput) (string, string) {
	source, card := pricingAuditIdentity(resolved)
	if card == "" {
		return source, card
	}
	var group int64
	if input.GroupID != nil {
		group = *input.GroupID
	} else if input.Group != nil {
		group = input.Group.ID
	}
	peak := float64(1)
	if resolved.descriptor == nil && (resolved.Source == PricingSourceLiteLLM || resolved.Source == PricingSourceStaticFallback) && isDeepSeekModel(input.Model) {
		peak = deepseekPeakMultiplierAt(input.PricingAt)
	}
	adjustment := resolvedChannelTimeMultiplier(resolved, input.PricingAt)
	if resolved.Mode != BillingModeToken {
		adjustment = 1
	}
	data, err := json.Marshal(struct {
		Card, Provider, Model, Tier, Effort, Size string
		Group                                     int64
		Rate, Time, Peak                          float64
		Thinking                                  *bool
		LongContext                               bool
	}{card, input.Provider, saleModel(input.Model), normalizeBillingServiceTier(input.ServiceTier),
		strings.ToLower(strings.TrimSpace(input.ReasoningEffort)), input.SizeTier, group, input.RateMultiplier,
		adjustment, peak, input.ThinkingEnabled, resolved.longContextPricingEnabled || input.LongContextBillingEnabled != nil && *input.LongContextBillingEnabled})
	if err != nil {
		return source, ""
	}
	digest := sha256.Sum256(data)
	return source, "sha256:" + hex.EncodeToString(digest[:])
}
