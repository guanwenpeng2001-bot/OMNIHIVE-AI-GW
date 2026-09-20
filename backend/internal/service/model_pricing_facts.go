package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ModelUnitPrice keeps a missing price distinct from an explicitly free price.
// Each dimension has its own unit and condition; sales never overwrite costs.
type ModelUnitPrice struct {
	Unit       string   `json:"unit"`
	Dimension  string   `json:"dimension"`
	Conditions string   `json:"conditions,omitempty"`
	Currency   string   `json:"currency"`
	Sales      *float64 `json:"sales"`
	Cost       *float64 `json:"cost"`
}

// ModelPriceVariant owns its lifecycle, independently of sibling variants.
// These are operator price facts, not capability or authorization declarations.
type ModelPriceVariant struct {
	ID        string           `json:"id"`
	Canonical string           `json:"canonical"`
	Aliases   []string         `json:"aliases,omitempty"`
	Lifecycle string           `json:"lifecycle,omitempty"`
	ExpiresAt *time.Time       `json:"expires_at,omitempty"`
	Prices    []ModelUnitPrice `json:"prices"`
}

type ModelPricingDescriptor struct {
	configured bool
	Model      string              `json:"model"`
	Platform   string              `json:"platform"`
	Source     string              `json:"source"`
	PricingAt  time.Time           `json:"pricing_at"`
	Variants   []ModelPriceVariant `json:"variants"`
}

func clonePriceNumber(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneModelPriceVariants(variants []ModelPriceVariant) []ModelPriceVariant {
	if variants == nil {
		return nil
	}
	cloned := make([]ModelPriceVariant, len(variants))
	for index, variant := range variants {
		cloned[index] = variant
		cloned[index].Aliases = append([]string(nil), variant.Aliases...)
		if variant.ExpiresAt != nil {
			expiry := *variant.ExpiresAt
			cloned[index].ExpiresAt = &expiry
		}
		cloned[index].Prices = append([]ModelUnitPrice(nil), variant.Prices...)
		for priceIndex, price := range variant.Prices {
			cloned[index].Prices[priceIndex].Sales = clonePriceNumber(price.Sales)
			cloned[index].Prices[priceIndex].Cost = clonePriceNumber(price.Cost)
		}
	}
	return cloned
}

func priceVariantMatches(variant ModelPriceVariant, model string) bool {
	for _, candidate := range append([]string{variant.ID, variant.Canonical}, variant.Aliases...) {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(model)) {
			return true
		}
	}
	return false
}

func priceVariantActive(variant ModelPriceVariant, pricingAt time.Time) bool {
	return variant.Lifecycle != "retired" && variant.Lifecycle != "expired" && variant.Lifecycle != "disabled" &&
		(variant.ExpiresAt == nil || pricingAt.Before(*variant.ExpiresAt))
}

func validateModelPriceVariants(variants []ModelPriceVariant) error {
	seen := make(map[string]bool)
	for _, variant := range variants {
		if strings.TrimSpace(variant.ID) == "" || strings.TrimSpace(variant.Canonical) == "" || seen[variant.ID] || len(variant.Prices) == 0 {
			return fmt.Errorf("invalid or duplicate price variant %q", variant.ID)
		}
		seen[variant.ID] = true
		dimensions := make(map[string]bool)
		for _, price := range variant.Prices {
			identity := price.Unit + "\x00" + price.Dimension + "\x00" + price.Conditions
			if price.Unit == "" || price.Dimension == "" || price.Currency != "USD" || dimensions[identity] ||
				price.Sales != nil && !validBillingNumber(*price.Sales) || price.Cost != nil && !validBillingNumber(*price.Cost) {
				return fmt.Errorf("invalid or duplicate price dimension in variant %q", variant.ID)
			}
			dimensions[identity] = true
		}
	}
	return nil
}

// Lookup requires an exact variant and dimension, and returns copies. Expiry is
// evaluated at the descriptor's frozen pricing instant, never at settlement.
func (d *ModelPricingDescriptor) Lookup(variantID, unit, dimension, conditions string) (ModelUnitPrice, error) {
	for _, variant := range d.Variants {
		if variant.ID != variantID || !priceVariantActive(variant, d.PricingAt) {
			continue
		}
		for _, price := range variant.Prices {
			if price.Unit == unit && price.Dimension == dimension && price.Conditions == conditions {
				if price.Sales == nil || price.Currency != "USD" || !validBillingNumber(*price.Sales) {
					break
				}
				price.Sales, price.Cost = clonePriceNumber(price.Sales), clonePriceNumber(price.Cost)
				return price, nil
			}
		}
	}
	return ModelUnitPrice{}, fmt.Errorf("%w: %s/%s/%s/%s", ErrModelPricingUnavailable, d.Model, variantID, unit, dimension)
}

// UnitPrice rejects ambiguous variants instead of silently choosing the first.
func (d *ModelPricingDescriptor) UnitPrice(unit, dimension, conditions string) (ModelUnitPrice, error) {
	var selected *ModelUnitPrice
	for _, variant := range d.Variants {
		price, err := d.Lookup(variant.ID, unit, dimension, conditions)
		if err != nil {
			continue
		}
		if selected != nil {
			return ModelUnitPrice{}, fmt.Errorf("%w: ambiguous %s price for %s", ErrModelPricingUnavailable, unit, d.Model)
		}
		selected = &price
	}
	if selected == nil {
		return ModelUnitPrice{}, fmt.Errorf("%w: %s %s/%s", ErrModelPricingUnavailable, d.Model, unit, dimension)
	}
	return *selected, nil
}

// ResolveDescriptor uses the same group/channel precedence as billing. It never
// publishes pricing into the capability registry or guesses sibling lifecycles.
func (r *ModelPricingResolver) ResolveDescriptor(ctx context.Context, input PricingInput) (*ModelPricingDescriptor, error) {
	pricingAt := time.Now()
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil && !plan.Snapshot().PricingAt.IsZero() {
		pricingAt = plan.Snapshot().PricingAt
	}
	platform := ""
	if input.Group != nil {
		platform = channelLookupPlatform(ctx, input.Group.Platform)
	}
	descriptor := &ModelPricingDescriptor{Model: input.Model, Platform: platform, PricingAt: pricingAt}
	configured, found, err := r.configuredPriceDescriptor(ctx, input)
	if err != nil {
		return nil, err
	}
	if found {
		return configured, nil
	}
	resolved := r.Resolve(ctx, input)
	if resolved == nil || resolved.BasePricing == nil {
		return nil, fmt.Errorf("%w: %s", ErrModelPricingUnavailable, input.Model)
	}
	descriptor.Source = resolved.Source
	base := resolved.BasePricing
	var upstream *ModelPricing
	if r.billingService != nil {
		upstream, _ = r.billingService.forGatewayRequest(ctx).GetModelPricing(input.Model)
	}
	prices := []ModelUnitPrice{{Unit: "token", Dimension: "input", Currency: "USD", Sales: clonePriceNumber(&base.InputPricePerToken)}, {Unit: "token", Dimension: "output", Currency: "USD", Sales: clonePriceNumber(&base.OutputPricePerToken)}}
	if upstream != nil {
		prices[0].Cost = clonePriceNumber(&upstream.InputPricePerToken)
		prices[1].Cost = clonePriceNumber(&upstream.OutputPricePerToken)
	}
	descriptor.Variants = []ModelPriceVariant{{ID: input.Model, Canonical: input.Model, Prices: prices}}
	return descriptor, nil
}

func (r *ModelPricingResolver) configuredPriceDescriptor(ctx context.Context, input PricingInput) (*ModelPricingDescriptor, bool, error) {
	pricingAt := time.Now()
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil && !plan.Snapshot().PricingAt.IsZero() {
		pricingAt = plan.Snapshot().PricingAt
	}
	platform := ""
	if input.Group != nil {
		platform = channelLookupPlatform(ctx, input.Group.Platform)
	}
	descriptor := &ModelPricingDescriptor{configured: true, Model: input.Model, Platform: platform, PricingAt: pricingAt}
	var cards []ChannelModelPricing
	if input.Group != nil {
		cards = input.Group.ModelPricing
	}
	for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
		if source == PricingSourceChannel {
			if r.channelService == nil || input.GroupID == nil {
				continue
			}
			cache, err := r.channelService.loadCache(ctx)
			if err != nil || cache == nil || cache.generation < 0 {
				return nil, false, fmt.Errorf("%w: channel prices unavailable: %v", ErrModelPricingUnavailable, err)
			}
			channel := cache.channelByGroupID[*input.GroupID]
			if channel == nil || !channel.IsActive() {
				continue
			}
			cards = channel.ModelPricing
			if platform == "" {
				platform = channelLookupPlatform(ctx, cache.groupPlatform[*input.GroupID])
				descriptor.Platform = platform
			}
		}
		if legacy := matchGroupModelPricing(&Group{Platform: platform, ModelPricing: cards}, input.Model, platform); legacy != nil && len(legacy.PriceVariants) == 0 {
			return nil, false, nil
		}
		for _, card := range cards {
			if card.Platform != "" && card.Platform != platform {
				continue
			}
			for _, variant := range card.PriceVariants {
				if priceVariantMatches(variant, input.Model) {
					descriptor.Variants = append(descriptor.Variants, cloneModelPriceVariants([]ModelPriceVariant{variant})...)
				}
			}
		}
		if len(descriptor.Variants) > 0 {
			if err := descriptor.normalizeVariants(); err != nil {
				return nil, false, err
			}
			descriptor.Source = source
			return descriptor, true, nil
		}
	}
	if r.billingService != nil {
		billing := r.billingService.forGatewayRequest(ctx)
		if billing.pricingService != nil {
			descriptor.Variants = billing.pricingService.modelPriceVariants(input.Model, platform)
			if len(descriptor.Variants) > 0 {
				if err := descriptor.normalizeVariants(); err != nil {
					return nil, false, err
				}
				descriptor.Source = PricingSourceLiteLLM
				return descriptor, true, nil
			}
		}
	}
	return nil, false, nil
}

func (s *PricingService) modelPriceVariants(model, platform string) []ModelPriceVariant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry := s.pricingData[strings.ToLower(strings.TrimSpace(model))]
	if entry == nil || platform != "" && entry.LiteLLMProvider != platform {
		return nil
	}
	var matches []ModelPriceVariant
	for _, variant := range entry.PriceVariants {
		if priceVariantMatches(variant, model) {
			matches = append(matches, cloneModelPriceVariants([]ModelPriceVariant{variant})...)
		}
	}
	return matches
}

// Index aliases in the existing immutable price map at publication, avoiding a
// full catalog scan on every request. Conflicting identities are rejected.
func indexModelPriceVariants(prices map[string]*LiteLLMModelPricing) error {
	aliases := make(map[string]*LiteLLMModelPricing)
	for _, entry := range prices {
		for _, variant := range entry.PriceVariants {
			names := append([]string{variant.ID, variant.Canonical}, variant.Aliases...)
			for _, name := range names {
				name = strings.ToLower(strings.TrimSpace(name))
				if name == "" {
					continue
				}
				if existing := prices[name]; existing != nil && existing != entry {
					return fmt.Errorf("conflicting price identity %s", name)
				}
				if existing := aliases[name]; existing != nil && existing != entry {
					return fmt.Errorf("ambiguous price alias %s", name)
				}
				aliases[name] = entry
			}
		}
	}
	for name, entry := range aliases {
		prices[name] = entry
	}
	return nil
}

// tokenPricing selects the requested variant without borrowing a live sibling.
func (d *ModelPricingDescriptor) tokenPricing() *ModelPricing {
	var selected *ModelPricing
	for _, variant := range d.Variants {
		if !priceVariantActive(variant, d.PricingAt) {
			continue
		}
		var input, output *float64
		for _, price := range variant.Prices {
			if price.Unit != "token" || price.Conditions != "" || price.Currency != "USD" {
				continue
			}
			switch price.Dimension {
			case "input":
				input = price.Sales
			case "output":
				output = price.Sales
			}
		}
		if input == nil || output == nil {
			continue
		}
		if selected != nil {
			return nil
		}
		selected = &ModelPricing{InputPricePerToken: *input, OutputPricePerToken: *output}
	}
	return selected
}

func clonePricingIntervalPrices(interval PricingInterval) PricingInterval {
	cloned := interval
	cloned.InputPrice = clonePriceNumber(interval.InputPrice)
	cloned.OutputPrice = clonePriceNumber(interval.OutputPrice)
	cloned.CacheWritePrice = clonePriceNumber(interval.CacheWritePrice)
	cloned.CacheWrite1hPrice = clonePriceNumber(interval.CacheWrite1hPrice)
	cloned.CacheReadPrice = clonePriceNumber(interval.CacheReadPrice)
	cloned.InputMultiplier = clonePriceNumber(interval.InputMultiplier)
	cloned.OutputMultiplier = clonePriceNumber(interval.OutputMultiplier)
	cloned.CacheWriteMultiplier = clonePriceNumber(interval.CacheWriteMultiplier)
	cloned.CacheReadMultiplier = clonePriceNumber(interval.CacheReadMultiplier)
	cloned.PerRequestPrice = clonePriceNumber(interval.PerRequestPrice)
	if interval.MaxTokens != nil {
		maximum := *interval.MaxTokens
		cloned.MaxTokens = &maximum
	}
	return cloned
}

func (d *ModelPricingDescriptor) tokenCost(tokens UsageTokens) (float64, error) {
	quantities := map[string]int{"input": tokens.InputTokens, "output": tokens.OutputTokens, "cache_read": tokens.CacheReadTokens, "cache_write": tokens.CacheCreationTokens}
	total := 0.0
	for dimension, quantity := range quantities {
		if quantity == 0 {
			continue
		}
		var unitCost *float64
		for _, variant := range d.Variants {
			if !priceVariantActive(variant, d.PricingAt) {
				continue
			}
			for _, price := range variant.Prices {
				if price.Unit != "token" || price.Dimension != dimension || price.Conditions != "" {
					continue
				}
				if unitCost != nil || price.Currency != "USD" || price.Cost == nil || !validBillingNumber(*price.Cost) {
					return 0, fmt.Errorf("%w: ambiguous or missing %s cost", ErrModelPricingUnavailable, dimension)
				}
				unitCost = price.Cost
			}
		}
		if unitCost == nil || quantity < 0 {
			return 0, fmt.Errorf("%w: missing %s cost", ErrModelPricingUnavailable, dimension)
		}
		total += float64(quantity) * *unitCost
	}
	if err := ValidateUsageBillingAmount(total); err != nil {
		return 0, err
	}
	return total, nil
}

func (d *ModelPricingDescriptor) normalizeVariants() error {
	merged := make(map[string]ModelPriceVariant)
	for _, variant := range d.Variants {
		if previous, found := merged[variant.ID]; found {
			sameExpiry := previous.ExpiresAt == nil && variant.ExpiresAt == nil || previous.ExpiresAt != nil && variant.ExpiresAt != nil && previous.ExpiresAt.Equal(*variant.ExpiresAt)
			if previous.Canonical != variant.Canonical || previous.Lifecycle != variant.Lifecycle || !sameExpiry {
				return fmt.Errorf("%w: conflicting variant lifecycle %s", ErrModelPricingUnavailable, variant.ID)
			}
			previous.Prices = append(previous.Prices, variant.Prices...)
			merged[variant.ID] = previous
		} else {
			merged[variant.ID] = variant
		}
	}
	d.Variants = nil
	for _, variant := range merged {
		d.Variants = append(d.Variants, variant)
	}
	sort.Slice(d.Variants, func(left, right int) bool { return d.Variants[left].ID < d.Variants[right].ID })
	if err := validateModelPriceVariants(d.Variants); err != nil {
		return fmt.Errorf("%w: %v", ErrModelPricingUnavailable, err)
	}
	for _, variant := range d.Variants {
		if strings.EqualFold(variant.ID, d.Model) {
			d.Variants = []ModelPriceVariant{variant}
			break
		}
	}
	return nil
}
