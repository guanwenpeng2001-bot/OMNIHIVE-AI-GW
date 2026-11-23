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
	configured  bool
	billingMode BillingMode
	Model       string              `json:"model"`
	Platform    string              `json:"platform"`
	Source      string              `json:"source"`
	PricingAt   time.Time           `json:"pricing_at"`
	Variants    []ModelPriceVariant `json:"variants"`
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
	pricingAt := pricingInstant(ctx, input.At)
	platform := pricingPlatform(ctx, input)
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
		upstream, _ = r.billingService.forGatewayRequest(ctx).getProviderModelPricingAt(platform, input.Model, pricingAt)
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
	pricingAt := pricingInstant(ctx, input.At)
	platform := pricingPlatform(ctx, input)
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
					mode := card.BillingMode
					if mode == "" {
						mode = BillingModeToken
					}
					if len(descriptor.Variants) == 0 {
						descriptor.billingMode = mode
					} else if descriptor.billingMode != mode {
						// Multi-unit cards require the consumer's explicit unit.
						descriptor.billingMode = BillingModeToken
					}
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
			if input.shadowLegacy || platform == "" {
				descriptor.Variants = billing.pricingService.modelPriceVariants(input.Model, platform)
			} else {
				card, err := billing.pricingService.providerModelPricing(platform, input.Model, true)
				if err != nil {
					return nil, false, err
				}
				if card != nil {
					descriptor.Variants = matchingPriceVariants(card, input.Model)
					if card.staticTariff {
						descriptor.Source = PricingSourceStaticFallback
					}
				}
			}
			if len(descriptor.Variants) > 0 {
				if err := descriptor.normalizeVariants(); err != nil {
					return nil, false, err
				}
				if descriptor.Source == "" {
					descriptor.Source = PricingSourceLiteLLM
				}
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
	return matchingPriceVariants(entry, model)
}

func matchingPriceVariants(entry *LiteLLMModelPricing, model string) []ModelPriceVariant {
	_, native := qualifiedTariffName(strings.ToLower(strings.TrimSpace(model)))
	var matches []ModelPriceVariant
	for _, variant := range entry.PriceVariants {
		if priceVariantMatches(variant, model) || priceVariantMatches(variant, native) {
			matches = append(matches, cloneModelPriceVariants([]ModelPriceVariant{variant})...)
		}
	}
	return matches
}

// Index aliases in the existing immutable price map at publication, avoiding a
// full catalog scan on every request. Conflicting identities are rejected.
func indexModelPriceVariants(prices map[string]*LiteLLMModelPricing) error {
	aliases := make(map[string]*LiteLLMModelPricing)
	conflicts := make(map[string]bool)
	for _, entry := range prices {
		for _, variant := range entry.PriceVariants {
			names := append([]string{variant.ID, variant.Canonical}, variant.Aliases...)
			for _, name := range names {
				name = strings.ToLower(strings.TrimSpace(name))
				if name == "" {
					continue
				}
				if entry.tariffOwner != "" {
					_, native := qualifiedTariffName(name)
					qualified := entry.tariffOwner + "/" + native
					if existing := prices[qualified]; existing != nil && existing != entry {
						return fmt.Errorf("conflicting price identity %s", qualified)
					}
					if existing := aliases[qualified]; existing != nil && existing != entry {
						return fmt.Errorf("ambiguous price alias %s", qualified)
					}
					aliases[qualified] = entry
				}
				if existing := prices[name]; existing != nil && existing != entry {
					if entry.tariffOwner == "" || existing.tariffOwner == "" || entry.tariffOwner == existing.tariffOwner {
						return fmt.Errorf("conflicting price identity %s", name)
					}
					continue
				}
				if existing := aliases[name]; existing != nil && existing != entry {
					if entry.tariffOwner == "" || existing.tariffOwner == "" || entry.tariffOwner == existing.tariffOwner {
						return fmt.Errorf("ambiguous price alias %s", name)
					}
					conflicts[name] = true
				}
				aliases[name] = entry
			}
		}
	}
	for name, entry := range aliases {
		if !conflicts[name] {
			prices[name] = entry
		}
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
		hasTokenPrice := false
		for _, price := range variant.Prices {
			if price.Unit != "token" || price.Conditions != "" || price.Currency != "USD" || price.Sales == nil {
				continue
			}
			hasTokenPrice = true
			switch price.Dimension {
			case "input":
				input = price.Sales
			case "output":
				output = price.Sales
			}
		}
		if !hasTokenPrice {
			continue
		}
		if selected != nil {
			return nil
		}
		selected = &ModelPricing{Source: d.Source, TokenDimensions: map[string]bool{}}
		if input != nil {
			selected.InputPricePerToken = *input
			selected.TokenDimensions["input"] = true
		}
		if output != nil {
			selected.OutputPricePerToken = *output
			selected.TokenDimensions["output"] = true
		}
		for _, price := range variant.Prices {
			if price.Unit == "token" && price.Dimension == "cache_write_1h" && price.Conditions == "" {
				selected.SupportsCacheBreakdown = true
			}
		}
		for _, dimension := range []string{"cache_read", "cache_write", "cache_write_1h", "image_input", "image_output", "image_cache_read"} {
			price, err := d.Lookup(variant.ID, "token", dimension, "")
			if err != nil {
				continue
			}
			selected.TokenDimensions[dimension] = true
			switch dimension {
			case "cache_read":
				selected.CacheReadPricePerToken = *price.Sales
			case "cache_write":
				selected.CacheCreationPricePerToken = *price.Sales
				selected.CacheCreationPriceExplicit = true
				selected.CacheCreation5mPrice = *price.Sales
			case "cache_write_1h":
				selected.CacheCreation1hPrice = *price.Sales
			case "image_input":
				selected.ImageInputPricePerToken = *price.Sales
			case "image_output":
				selected.ImageOutputPricePerToken = *price.Sales
				selected.ImageOutputPriceExplicit = true
			case "image_cache_read":
				selected.ImageCacheReadPricePerToken = *price.Sales
			}
		}
	}
	return selected
}

// billingModeUnit is shared by configuration validation and consumers. A price
// per second must never be interpreted as a price per generated video.
func billingModeUnit(mode BillingMode) string {
	switch mode {
	case BillingModeImage:
		return "image"
	case BillingModeVideo:
		return "second"
	case BillingModePerRequest:
		return "request"
	default:
		return "token"
	}
}

func (d *ModelPricingDescriptor) resolvePricing(mode BillingMode) *ResolvedPricing {
	if mode == "" {
		mode = d.billingMode
	}
	if mode == "" {
		mode = BillingModeToken
	}
	resolved := &ResolvedPricing{Mode: mode, Source: d.Source, descriptor: d}
	if mode == BillingModeToken {
		resolved.BasePricing = d.tokenPricing()
	} else if price, err := d.UnitPrice(billingModeUnit(mode), "output", ""); err == nil {
		resolved.DefaultPerRequestPrice = *price.Sales
		resolved.DefaultPerRequestPriceFound = true
	}
	return resolved
}

// Variant cards require explicit dimensions. Legacy cards allow their existing
// image/priority/cache fallbacks, but must still have a tariff for used tokens.
func validateTokenDimensions(pricing *ModelPricing, tokens UsageTokens, serviceTiers ...string) error {
	if pricing.TokenDimensions == nil {
		tier := ""
		if len(serviceTiers) > 0 {
			tier = serviceTiers[0]
		}
		return validateLegacyTokenPrices(pricing, tokens, tier)
	}
	for dimension, quantity := range variantTokenQuantities(tokens, pricing.SupportsCacheBreakdown) {
		if quantity != 0 && !pricing.TokenDimensions[dimension] {
			return fmt.Errorf("%w: token/%s", ErrModelPricingUnavailable, dimension)
		}
	}
	if (tokens.CacheCreation5mTokens != 0 || tokens.CacheCreation1hTokens != 0) && tokens.CacheCreationTokens == 0 {
		return fmt.Errorf("%w: cache details require an aggregate", ErrModelPricingUnavailable)
	}
	return nil
}

func variantTokenQuantities(tokens UsageTokens, splitCache bool) map[string]int {
	imageInput := min(tokens.ImageInputTokens, tokens.InputTokens)
	imageRead := min(tokens.ImageCacheReadTokens, tokens.CacheReadTokens)
	quantities := map[string]int{
		"input": tokens.InputTokens - imageInput, "output": max(0, tokens.OutputTokens-tokens.ImageOutputTokens),
		"cache_read": tokens.CacheReadTokens - imageRead, "cache_write": tokens.CacheCreationTokens,
		"image_input": imageInput, "image_output": tokens.ImageOutputTokens, "image_cache_read": imageRead,
	}
	if splitCache && (tokens.CacheCreation5mTokens != 0 || tokens.CacheCreation1hTokens != 0) {
		quantities["cache_write"], quantities["cache_write_1h"] = normalizeCacheCreationBreakdown(tokens)
	}
	return quantities
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
	splitCache := false
	for _, variant := range d.Variants {
		if !priceVariantActive(variant, d.PricingAt) {
			continue
		}
		for _, price := range variant.Prices {
			if price.Unit == "token" && price.Dimension == "cache_write_1h" && price.Conditions == "" {
				splitCache = true
			}
		}
	}
	if (tokens.CacheCreation5mTokens != 0 || tokens.CacheCreation1hTokens != 0) && tokens.CacheCreationTokens == 0 {
		return 0, ErrModelPricingUnavailable
	}
	quantities := variantTokenQuantities(tokens, splitCache)
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

// A configured variant is authoritative even when its required unit is missing.
// Never fall through to a legacy group or catalog tariff after selecting it.
func (s *BillingService) calculateVariantMediaCost(ctx context.Context, resolver *ModelPricingResolver, model string, key *APIKey, mode BillingMode, units, multiplier float64) (*CostBreakdown, bool) {
	if resolver == nil {
		resolver = NewModelPricingResolver(nil, s)
	}
	input := PricingInput{Model: model, Mode: mode}
	if key != nil {
		input.Group, input.GroupID = key.Group, key.GroupID
	}
	resolved := resolver.Resolve(ctx, input)
	if resolved.pricingErr != nil {
		return pricingFailure(resolved.pricingErr), true
	}
	if resolved.descriptor == nil {
		return nil, false
	}
	if !validBillingNumber(units) {
		return pricingFailure(ErrInvalidBillingAmount), true
	}
	if !resolved.DefaultPerRequestPriceFound {
		return pricingFailure(ErrModelPricingUnavailable), true
	}
	if units == 0 {
		return calculateUnitCost(model, "", string(mode), resolved.Source, units, resolved.DefaultPerRequestPrice, multiplier), true
	}
	cost, err := s.forGatewayRequest(ctx).CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model, Mode: mode, Group: input.Group, GroupID: input.GroupID,
		UsageUnits: units, RateMultiplier: multiplier, Resolver: resolver, Resolved: resolved,
	})
	if err != nil {
		return pricingFailure(err), true
	}
	return cost, true
}
