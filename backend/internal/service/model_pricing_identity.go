package service

import (
	"fmt"
	"strings"
)

func pricingSourceSummary(fields map[string]string) string {
	source := ""
	for field, value := range fields {
		if !strings.Contains(field, "cost") && !strings.Contains(field, "price") && !strings.Contains(field, "multiplier") {
			continue
		}
		if source != "" && source != value {
			return "mixed_sale"
		}
		source = value
	}
	if source == "" {
		source = PricingSourceLiteLLM
	}
	return source
}

// BoundPricingModel keeps provider-qualified tariffs isolated. Only an exact
// legacy key may be used without a provider namespace. Suffix/family matches
// must identify one record; map iteration never decides which tariff to bill.
func (s *PricingService) BoundPricingModel(provider, model string) (string, error) {
	if s == nil {
		return model, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	model = strings.ToLower(strings.TrimSpace(model))
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" {
		qualified := provider + "/" + model
		if _, ok := s.pricingData[qualified]; ok {
			return qualified, nil
		}
	}
	if _, ok := s.pricingData[model]; ok {
		return model, nil
	}
	base := s.extractBaseName(model)
	matched := ""
	qualifiedMatches := map[string]bool{}
	for key := range s.pricingData {
		candidate := key
		if provider != "" && strings.HasPrefix(candidate, provider+"/") {
			candidate = strings.TrimPrefix(candidate, provider+"/")
		}
		if prefix, native, ok := strings.Cut(key, "/"); ok && s.extractBaseName(native) == base {
			qualifiedMatches[prefix] = true
			if provider == "" {
				candidate = native
			}
		}
		if s.extractBaseName(candidate) != base {
			continue
		}
		if matched != "" && matched != key {
			return "", fmt.Errorf("%w: ambiguous tariff identity %s", ErrModelPricingUnavailable, model)
		}
		matched = key
	}
	if matched != "" {
		return matched, nil
	}
	if len(qualifiedMatches) > 1 {
		return "", fmt.Errorf("%w: provider is required for tariff identity %s", ErrModelPricingUnavailable, model)
	}
	return model, nil // explicit legacy policies remain owned by BillingService
}

// Complete provenance for the declared rate dimensions. Source metadata is
// additive and never creates an absent price dimension or changes its amount.
func completePricingFieldSources(p *ModelPricing) {
	if p == nil {
		return
	}
	if p.FieldSources == nil {
		p.FieldSources = make(map[string]string)
	}
	for dimension, raw := range map[string]string{
		"input": "input_cost_per_token", "output": "output_cost_per_token",
		"cache_read": "cache_read_input_token_cost", "cache_write": "cache_creation_input_token_cost",
		"cache_write_1h": "cache_creation_input_token_cost_above_1hr", "image_input": "input_cost_per_image_token",
		"image_output": "output_cost_per_image_token", "image_cache_read": "cache_read_input_image_token_cost",
	} {
		if p.TokenPricePresence[dimension] && p.FieldSources[raw] == "" {
			p.FieldSources[raw] = p.Source
		}
	}
}
