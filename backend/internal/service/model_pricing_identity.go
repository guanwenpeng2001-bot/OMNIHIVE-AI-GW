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
	return s.boundPricingModelLocked(provider, model)
}

// The caller holds the same price-publication read lock through alias binding.
func (s *PricingService) boundPricingModelLocked(provider, model string) (string, error) {
	if provider != "" {
		qualified := provider + "/" + model
		if _, ok := s.pricingData[qualified]; ok {
			return qualified, nil
		}
	}
	if _, ok := s.pricingData[model]; ok {
		prefix, _, qualified := strings.Cut(model, "/")
		if provider == "" || !qualified || prefix == provider {
			return model, nil
		}
	}
	if native, ok := pricingProtocolNativeModel(model); ok {
		return s.boundPricingModelLocked(provider, native)
	}
	base := s.extractBaseName(model)
	if index := s.baseNameIndex.Load(); index != nil && index.revision == s.pricingRevision {
		return index.boundPricingModel(provider, model, base)
	}
	// Direct fixtures may omit the reload-derived index. Production reloads and
	// frozen billing services publish/share it with the same pricing revision.
	matched := ""
	foreignMatch := false
	for key := range s.pricingData {
		prefix, native, qualified := strings.Cut(key, "/")
		fullMatch := s.extractBaseName(key) == base
		nativeMatch := qualified && s.extractBaseName(native) == base
		if !fullMatch && !nativeMatch {
			continue
		}
		if provider != "" && qualified && prefix != provider {
			foreignMatch = true
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
	if provider != "" && (foreignMatch || strings.Contains(model, "/")) {
		return "", fmt.Errorf("%w: tariff identity %s is not bound to provider %s", ErrModelPricingUnavailable, model, provider)
	}
	return model, nil // explicit legacy policies remain owned by BillingService
}

// Gemini/Vertex protocol resource names are syntax aliases, not provider
// namespaces. Accept only the existing resource shapes, then bind the native
// name again; an arbitrary trailing path segment is never a pricing identity.
func pricingProtocolNativeModel(model string) (string, bool) {
	for _, prefix := range []string{"models/", "publishers/google/models/"} {
		if native, ok := strings.CutPrefix(model, prefix); ok {
			return native, native != "" && !strings.Contains(native, "/")
		}
	}
	if !strings.HasPrefix(model, "projects/") {
		return "", false
	}
	parts := strings.Split(model, "/")
	if len(parts) == 8 && parts[0] == "projects" && parts[1] != "" &&
		parts[2] == "locations" && parts[3] != "" && parts[4] == "publishers" &&
		parts[5] == "google" && parts[6] == "models" && parts[7] != "" {
		return parts[7], true
	}
	return "", false
}

// Only matching identities are visited; an unknown model never scans the
// catalog. This index belongs to the same immutable publication as pricingData.
func (index *pricingBaseNameIndex) boundPricingModel(provider, model, base string) (string, error) {
	matched := ""
	foreignMatch := false
	add := func(key string) bool {
		if matched != "" && matched != key {
			return false
		}
		matched = key
		return true
	}
	for _, key := range index.keysByBase[base] {
		prefix, _, qualified := strings.Cut(key, "/")
		if provider != "" && qualified && prefix != provider {
			foreignMatch = true
			continue
		}
		if !add(key) {
			return "", fmt.Errorf("%w: ambiguous tariff identity %s", ErrModelPricingUnavailable, model)
		}
	}
	nativeMatches := index.keysByNative[base]
	if provider != "" {
		for _, key := range nativeMatches[provider] {
			if !add(key) {
				return "", fmt.Errorf("%w: ambiguous tariff identity %s", ErrModelPricingUnavailable, model)
			}
		}
		foreignMatch = foreignMatch || len(nativeMatches) > 0
	} else {
		for _, keys := range nativeMatches {
			for _, key := range keys {
				if !add(key) {
					return "", fmt.Errorf("%w: ambiguous tariff identity %s", ErrModelPricingUnavailable, model)
				}
			}
		}
	}
	if matched != "" {
		return matched, nil
	}
	if provider != "" && (foreignMatch || strings.Contains(model, "/")) {
		return "", fmt.Errorf("%w: tariff identity %s is not bound to provider %s", ErrModelPricingUnavailable, model, provider)
	}
	return model, nil
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
