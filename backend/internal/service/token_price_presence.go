package service

import "fmt"

// TokenPricePresence records explicit tariffs, including zero. Unlike variant
// TokenDimensions, legacy tariffs may inherit text prices for image tokens.
func catalogTokenPricePresence(pricing *LiteLLMModelPricing) map[string]bool {
	present := make(map[string]bool)
	for dimension, field := range map[string]string{
		"input": "input_cost_per_token", "output": "output_cost_per_token",
		"input_priority": "input_cost_per_token_priority", "output_priority": "output_cost_per_token_priority",
		"cache_write": "cache_creation_input_token_cost", "cache_write_priority": "cache_creation_input_token_cost_priority",
		"cache_write_1h": "cache_creation_input_token_cost_above_1hr",
		"cache_read":     "cache_read_input_token_cost", "cache_read_priority": "cache_read_input_token_cost_priority",
		"image_input": "input_cost_per_image_token", "image_output": "output_cost_per_image_token",
		"image_cache_read": "cache_read_input_image_token_cost",
	} {
		present[dimension] = pricing.sourceQuotes[field].Value != nil
	}
	return present
}

// Keep known nonzero rates known even if a later interval multiplies them by zero.
func (p *ModelPricing) preserveTokenPricePresence() {
	present := make(map[string]bool, len(p.TokenPricePresence)+12)
	for dimension, found := range p.TokenPricePresence {
		present[dimension] = found
	}
	for dimension, value := range map[string]float64{
		"input": p.InputPricePerToken, "output": p.OutputPricePerToken,
		"input_priority": p.InputPricePerTokenPriority, "output_priority": p.OutputPricePerTokenPriority,
		"cache_write": p.CacheCreationPricePerToken, "cache_write_priority": p.CacheCreationPricePerTokenPriority,
		"cache_write_1h": p.CacheCreation1hPrice, "cache_read": p.CacheReadPricePerToken,
		"cache_read_priority": p.CacheReadPricePerTokenPriority,
		"image_input":         p.ImageInputPricePerToken, "image_output": p.ImageOutputPricePerToken,
		"image_cache_read": p.ImageCacheReadPricePerToken,
	} {
		if value != 0 {
			present[dimension] = true
		}
	}
	p.TokenPricePresence = present
}

func (p *ModelPricing) hasTokenPrice(dimension string, value float64) bool {
	return value != 0 || p.TokenPricePresence[dimension] || p.TokenDimensions[dimension]
}

// Copy on write: resolved cards and frozen request cards can share these maps.
func (p *ModelPricing) markTokenPrices(dimensions ...string) {
	for _, target := range []*map[string]bool{&p.TokenPricePresence, &p.TokenDimensions} {
		if *target == nil {
			continue
		}
		cloned := make(map[string]bool, len(*target)+len(dimensions))
		for dimension, present := range *target {
			cloned[dimension] = present
		}
		for _, dimension := range dimensions {
			cloned[dimension] = true
		}
		*target = cloned
	}
}

func (p *ModelPricing) resetPriorityPresence(dimension string, value float64) {
	if p.TokenPricePresence != nil {
		p.markTokenPrices(dimension)
		p.TokenPricePresence[dimension] = value != 0
	}
}

func usesCacheCreationBreakdown(p *ModelPricing) bool {
	return p.SupportsCacheBreakdown && (p.hasTokenPrice("cache_write", p.CacheCreation5mPrice) || p.hasTokenPrice("cache_write_1h", p.CacheCreation1hPrice))
}

// Validate the same rates selected by computeTokenBreakdown. A missing unused
// dimension is harmless; multipliers cannot turn a missing rate into a tariff.
func validateLegacyTokenPrices(p *ModelPricing, tokens UsageTokens, serviceTier string) error {
	if p.TokenPricePresence == nil {
		return nil // old frozen snapshots retain their original billing semantics
	}
	present := map[string]bool{
		"input":       p.hasTokenPrice("input", p.InputPricePerToken),
		"output":      p.hasTokenPrice("output", p.OutputPricePerToken),
		"cache_read":  p.hasTokenPrice("cache_read", p.CacheReadPricePerToken),
		"cache_write": p.hasTokenPrice("cache_write", p.CacheCreationPricePerToken),
	}
	if usePriorityServiceTierPricing(serviceTier, p) {
		for dimension, value := range map[string]float64{"input": p.InputPricePerTokenPriority, "output": p.OutputPricePerTokenPriority, "cache_read": p.CacheReadPricePerTokenPriority, "cache_write": p.CacheCreationPricePerTokenPriority} {
			present[dimension] = present[dimension] || p.hasTokenPrice(dimension+"_priority", value)
		}
	}
	present["image_input"] = p.hasTokenPrice("image_input", p.ImageInputPricePerToken) || present["input"]
	present["image_output"] = p.hasTokenPrice("image_output", p.ImageOutputPricePerToken) || p.ImageOutputPriceExplicit || present["output"]
	present["image_cache_read"] = p.hasTokenPrice("image_cache_read", p.ImageCacheReadPricePerToken) || present["cache_read"]
	split := usesCacheCreationBreakdown(p)
	if !split && tokens.CacheCreationTokens == 0 && (tokens.CacheCreation5mTokens > 0 || tokens.CacheCreation1hTokens > 0) {
		return fmt.Errorf("%w: cache details require an aggregate or split tariff", ErrModelPricingUnavailable)
	}
	if split {
		present["cache_write"] = p.hasTokenPrice("cache_write", p.CacheCreation5mPrice)
		present["cache_write_1h"] = p.hasTokenPrice("cache_write_1h", p.CacheCreation1hPrice)
	}
	for dimension, quantity := range variantTokenQuantities(tokens, split) {
		if quantity > 0 && !present[dimension] {
			return fmt.Errorf("%w: token/%s", ErrModelPricingUnavailable, dimension)
		}
	}
	return nil
}
