package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
)

func pricingPlatform(ctx context.Context, input PricingInput) string {
	if input.Provider != "" {
		return input.Provider
	}
	platform := ""
	if input.Group != nil {
		platform = input.Group.Platform
	}
	platform = channelLookupPlatform(ctx, platform)
	if target, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		return target
	}
	return platform
}

func pricingInstant(ctx context.Context, at time.Time) time.Time {
	if !at.IsZero() {
		return at
	}
	if at = pinnedPricingInstant(ctx); !at.IsZero() {
		return at
	}
	return timezone.Now()
}

func pinnedPricingInstant(ctx context.Context) time.Time {
	if pinned, ok := gatewayTokenRequestPricingAtFromContext(ctx); ok {
		return pinned
	}
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil && !plan.Snapshot().PricingAt.IsZero() {
		return plan.Snapshot().PricingAt
	}
	return time.Time{}
}

// PlatformPriceBook adapts a caller-authorized sale binding to the legacy
// gateway evaluator and channel snapshot. Group reads use the existing store.
type PlatformPriceBook struct {
	resolver *ModelPricingResolver
}

func NewPlatformPriceBook(resolver *ModelPricingResolver) *PlatformPriceBook {
	return &PlatformPriceBook{resolver: resolver}
}

var _ pricing.PriceBook = (*PlatformPriceBook)(nil)

func freezeBillingPricing(billing *BillingService, registry *ModelRegistrySnapshot) *BillingService {
	if billing == nil || billing.pricingService == nil {
		return billing
	}
	prices := billing.pricingService
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	frozen := &PricingService{cfg: prices.cfg, pricingData: prices.pricingData, registrySnapshot: registry, localHash: prices.localHash,
		customFilesHash: prices.customFilesHash, lastUpdated: prices.lastUpdated, pricingRevision: prices.pricingRevision, providerPrices: prices.providerPrices}
	frozen.baseNameIndex.Store(prices.baseNameIndex.Load())
	return &BillingService{cfg: billing.cfg, pricingService: frozen, fallbackPrices: billing.fallbackPrices}
}

// Snapshot captures tariff data for a batch of already authorized bindings.
func (b *PlatformPriceBook) Snapshot(ctx context.Context, at time.Time) context.Context {
	if b == nil || b.resolver == nil || GatewayRequestPlanFromContext(ctx) != nil {
		return ctx
	}
	r := b.resolver
	p := &GatewayRequestPlan{spec: GatewayRequestSpec{PricingAt: at}, channels: r.channelService, billing: freezeBillingPricing(r.billingService, ModelRegistrySnapshotFromContext(ctx))}
	if p.channels != nil {
		p.channelSnapshot, p.channelError = p.channels.loadCache(ctx)
	}
	return context.WithValue(ctx, gatewayRequestPlanKey{}, p)
}

// PriceRevision identifies the live inputs for a catalog read fence.
func (b *PlatformPriceBook) PriceRevision(ctx context.Context) (string, error) {
	r := b.resolver
	var generation int64
	if r.channelService != nil {
		var err error
		generation, _, err = catalogEvidenceGeneration(ctx, r.channelService.groupRepo)
		if err != nil {
			return "", err
		}
	}
	var source, overrides string
	if r.billingService != nil && r.billingService.pricingService != nil {
		prices := r.billingService.pricingService
		prices.mu.RLock()
		source, overrides = prices.localHash, prices.customFilesHash
		prices.mu.RUnlock()
	}
	return fmt.Sprintf("%d:%s:%s:%s", generation, source, overrides, ModelRegistrySnapshotFromContext(ctx).Version()), nil
}

func (b *PlatformPriceBook) Resolve(ctx context.Context, binding pricing.Binding) (pricing.Tariff, error) {
	var group *Group
	if binding.GroupID > 0 && b != nil && b.resolver != nil && b.resolver.channelService != nil && b.resolver.channelService.groupRepo != nil {
		var err error
		group, err = b.resolver.channelService.groupRepo.GetByID(ctx, binding.GroupID)
		if err != nil {
			return nil, err
		}
	}
	return b.ResolveForGroup(ctx, binding, group)
}

// ResolveForGroup lets batch catalog reads and Gateway use their already
// hydrated, authorized group. The caller supplies its default or user rate.
func (b *PlatformPriceBook) ResolveForGroup(ctx context.Context, binding pricing.Binding, group *Group) (pricing.Tariff, error) {
	t := &platformTariff{description: pricing.Description{Outcome: pricing.Missing, Currency: "USD", Evaluator: "gateway"}}
	if b == nil || b.resolver == nil || b.resolver.billingService == nil || binding.Provider == "" ||
		binding.Provider == PlatformComposite || binding.Model == "" || binding.At.IsZero() ||
		(binding.GroupID != 0 && (group == nil || group.ID != binding.GroupID)) {
		return t, nil
	}
	ctx = b.Snapshot(ctx, binding.At)
	r := b.resolver
	var groupID *int64
	if group != nil && group.ID != 0 {
		groupID = &group.ID
	}
	if binding.ChannelID != 0 {
		if groupID == nil || r.channelService == nil {
			return t, nil
		}
		channel, err := r.channelService.lookupGroupChannel(ctx, *groupID)
		if err != nil {
			return nil, err
		}
		if channel == nil || channel.channel.ID != binding.ChannelID {
			return t, nil
		}
	}
	resolved := r.Resolve(ctx, PricingInput{Provider: binding.Provider, Model: binding.Model, Mode: BillingMode(binding.Mode), At: binding.At, Group: group, GroupID: groupID})
	t.billing = r.billingService.forGatewayRequest(ctx)
	if resolved.pricingErr == nil && resolved.Source != PricingSourceGroup && resolved.descriptor == nil {
		t.media = t.billing.resolveSaleMediaRates(&APIKey{Group: group, GroupID: groupID}, binding, resolved)
	}
	multiplier := binding.RateMultiplier
	key := &APIKey{Group: group, GroupID: groupID}
	switch resolved.Mode {
	case BillingModeToken:
		multiplier, _ = computePeakAwareMultipliers(key, multiplier, binding.At)
	case BillingModeImage:
		multiplier = resolveImageRateMultiplier(key, multiplier)
	case BillingModeVideo:
		multiplier = resolveVideoRateMultiplier(key, multiplier)
	}
	t.input = CostInput{Ctx: ctx, Model: binding.Model, Provider: binding.Provider, Mode: resolved.Mode, Group: group, GroupID: groupID,
		PricingAt: binding.At, RateMultiplier: multiplier, Resolver: r, Resolved: resolved}
	t.baseMultiplier = binding.RateMultiplier
	t.description = describePlatformTariff(t.input)
	t.description.Mode, t.description.RateMultiplier = string(resolved.Mode), multiplier
	if len(t.media) != 0 {
		t.description.Outcome = pricing.Conditional
	}
	return t, nil
}

func (b *PlatformPriceBook) Describe(ctx context.Context, binding pricing.Binding) (pricing.Description, error) {
	tariff, err := b.Resolve(ctx, binding)
	if err != nil {
		return pricing.Description{}, err
	}
	return tariff.Describe(), nil
}

func (b *PlatformPriceBook) DescribeForGroup(ctx context.Context, binding pricing.Binding, group *Group) (pricing.Description, error) {
	tariff, err := b.ResolveForGroup(ctx, binding, group)
	if err != nil {
		return pricing.Description{}, err
	}
	return tariff.Describe(), nil
}

type platformTariff struct {
	description    pricing.Description
	billing        *BillingService
	input          CostInput
	media          map[string]saleMediaRate
	baseMultiplier float64
}

func (t *platformTariff) Describe() pricing.Description {
	description := t.description
	description.Rates = append([]pricing.RateFact(nil), description.Rates...)
	return description
}

func (t *platformTariff) Evaluate(scenario pricing.Scenario) (pricing.Result, error) {
	if len(t.media) > 0 {
		if scenario.Label == "" {
			return pricing.Unavailable(pricing.Conditional), nil
		}
		if rate, found := t.media[scenario.Label]; found {
			return t.evaluateMedia(rate, scenario)
		}
	}
	switch t.description.Outcome {
	case pricing.Missing, pricing.Conflict:
		return pricing.Unavailable(t.description.Outcome), nil
	}
	input, quantities, count, reason := platformScenario(t.input, scenario)
	if reason != "" {
		return pricing.Unavailable(reason), nil
	}
	cost, err := t.billing.CalculateCostUnified(input)
	if errors.Is(err, ErrQianwenThinkingUndetermined) {
		return pricing.Unavailable(pricing.Conditional), nil
	}
	if errors.Is(err, ErrModelPricingUnavailable) {
		return pricing.Unavailable(pricing.Partial), nil
	}
	if errors.Is(err, pricing.ErrAmbiguous) {
		return pricing.Unavailable(pricing.Conflict), nil
	}
	if err != nil {
		return pricing.Result{}, err
	}
	return platformResult(cost, input, quantities, count)
}

func describePlatformTariff(input CostInput) pricing.Description {
	resolved := input.Resolved
	d := pricing.Description{Outcome: pricing.Missing, Currency: "USD", Evaluator: "gateway", BaseSource: resolved.baseSource}
	if resolved.pricingErr != nil {
		if errors.Is(resolved.pricingErr, pricing.ErrAmbiguous) {
			d.Outcome = pricing.Conflict
		}
		return d
	}
	d.Source, d.VersionID = pricingEvaluationIdentity(resolved, input)
	terms := input
	terms.Group, terms.GroupID = nil, nil
	_, d.TermsID = pricingEvaluationIdentity(resolved, terms)
	d.Outcome = pricing.Available
	appendRate := func(kind, source, value string, unit money.Unit) {
		if rate, err := money.NewRate("USD", value, unit, "1"); err == nil {
			d.Rates = append(d.Rates, pricing.RateFact{Kind: kind, Origin: source, Rate: rate})
		}
	}
	if resolved.descriptor != nil {
		for _, variant := range resolved.descriptor.Variants {
			if !priceVariantActive(variant, input.PricingAt) {
				continue
			}
			if variant.ExpiresAt != nil && (d.EffectiveUntil == nil || variant.ExpiresAt.Before(*d.EffectiveUntil)) {
				expiry := *variant.ExpiresAt
				d.EffectiveUntil = &expiry
			}
		}
	}
	if resolved.Mode == BillingModeToken && resolved.BasePricing != nil {
		base, card := resolved.BasePricing, resolved.channelPricing
		for _, rate := range []struct {
			kind       string
			unit       money.Unit
			value      float64
			overridden bool
		}{
			{"input", "input_token", base.InputPricePerToken, card != nil && card.InputPrice != nil},
			{"output", "output_token", base.OutputPricePerToken, card != nil && card.OutputPrice != nil},
			{"cache_read", "cache_read_token", base.CacheReadPricePerToken, card != nil && card.CacheReadPrice != nil},
			{"cache_write", "cache_write_token", base.CacheCreationPricePerToken, card != nil && card.CacheWritePrice != nil},
			{"cache_write_1h", "cache_write_token", base.CacheCreation1hPrice, card != nil && (card.CacheWrite1hPrice != nil || card.CacheWritePrice != nil)},
			{"image_input", "image_input_token", base.ImageInputPricePerToken, card != nil && card.ImageInputPrice != nil},
			{"image_output", "image_output_token", base.ImageOutputPricePerToken, card != nil && card.ImageOutputPrice != nil},
		} {
			if !base.hasTokenPrice(rate.kind, rate.value) {
				if rate.kind == "input" || rate.kind == "output" {
					d.Outcome = pricing.Partial
				}
				continue
			}
			origin := d.BaseSource
			if rate.overridden || origin == "" {
				origin = d.Source
			}
			value := base.ExactTokenRates[rate.kind]
			if value == "" {
				value = strconv.FormatFloat(rate.value, 'f', -1, 64)
			}
			appendRate(rate.kind, origin, value, rate.unit)
		}
	} else if resolved.DefaultPerRequestPriceFound {
		kind, unit := "request", money.Unit("request")
		if resolved.Mode == BillingModeImage {
			kind, unit = "image", "image"
		}
		appendRate(kind, d.Source, strconv.FormatFloat(resolved.DefaultPerRequestPrice, 'f', -1, 64), unit)
	} else if len(resolved.Intervals) == 0 && len(resolved.RequestTiers) == 0 {
		d.Outcome = pricing.Missing
	}
	if len(resolved.Intervals) > 0 || len(resolved.RequestTiers) > 0 ||
		(resolved.BasePricing != nil && resolved.BasePricing.LongContextInputThreshold > 0) ||
		(resolved.channelPricing != nil && (resolved.channelPricing.TimePricing != nil || needsConditionScenario(resolved))) {
		d.Outcome = pricing.Conditional
	}
	return d
}

func needsConditionScenario(resolved *ResolvedPricing) bool {
	card := resolved.channelPricing
	return card != nil && (card.ThinkingPricing != nil || card.ExplicitCachePricing != nil || card.BatchChatPricing != nil)
}

func platformScenario(input CostInput, scenario pricing.Scenario) (CostInput, map[string]decimal.Decimal, int64, pricing.Outcome) {
	quantities := make(map[string]decimal.Decimal, len(scenario.Dimensions))
	for _, dimension := range scenario.Dimensions {
		if _, exists := quantities[dimension.Kind]; exists {
			return input, nil, 0, pricing.Conflict
		}
		quantities[dimension.Kind] = decimal.RequireFromString(dimension.Quantity.Amount().String())
	}
	count := int64(1)
	if q, exists := quantities["request"]; exists {
		var err error
		count, err = strconv.ParseInt(q.String(), 10, 64)
		if err != nil || count <= 0 {
			return input, nil, 0, pricing.Conditional
		}
	}
	input.ServiceTier, input.ReasoningEffort = scenario.ServiceTier, scenario.ReasoningEffort
	input.ThinkingEnabled, input.SizeTier = scenario.ThinkingEnabled, scenario.Label
	if card := input.Resolved.channelPricing; card != nil && card.ThinkingPricing != nil && scenario.ThinkingEnabled == nil {
		return input, nil, 0, pricing.Conditional
	}
	if len(input.Resolved.RequestTiers) > 0 && input.Mode == BillingModeImage && input.SizeTier == "" {
		return input, nil, 0, pricing.Conditional
	}

	tokens := make(map[string]int, len(quantities))
	for kind, quantity := range quantities {
		if quantity.IsZero() {
			continue
		}
		switch kind {
		case "request":
		case "image":
			if input.Mode != BillingModeImage {
				return input, nil, 0, pricing.Conditional
			}
			input.UsageUnits = quantity.Div(decimal.NewFromInt(count)).InexactFloat64()
		case "video":
			if input.Mode != BillingModeVideo {
				return input, nil, 0, pricing.Conditional
			}
			input.UsageUnits = quantity.Div(decimal.NewFromInt(count)).InexactFloat64()
		case "input", "output", "cache_read", "cache_write", "cache_write_1h", "image_input", "image_output":
			perRequest, remainder := quantity.QuoRem(decimal.NewFromInt(count), 0)
			value, err := strconv.Atoi(perRequest.String())
			if err != nil || !remainder.IsZero() {
				return input, nil, 0, pricing.Conditional
			}
			tokens[kind] = value
		default:
			return input, nil, 0, pricing.Conditional
		}
	}
	// Gateway reports image and long-TTL counters as subsets of its totals.
	input.Tokens = UsageTokens{InputTokens: tokens["input"] + tokens["image_input"], OutputTokens: tokens["output"] + tokens["image_output"],
		ImageInputTokens: tokens["image_input"], ImageOutputTokens: tokens["image_output"], CacheReadTokens: tokens["cache_read"],
		CacheCreationTokens: tokens["cache_write"] + tokens["cache_write_1h"], CacheCreation5mTokens: tokens["cache_write"],
		CacheCreation1hTokens: tokens["cache_write_1h"]}
	input.Tokens.QianwenExplicitCache, input.Tokens.QianwenBatchChat = scenario.ExplicitCache, scenario.Batch
	if (scenario.ExplicitCache || scenario.Batch || input.Tokens.CacheCreationTokens > 0) && input.Provider == PlatformQianwen {
		if card := input.Resolved.channelPricing; card != nil && card.NativePricingSource == QianwenPricingSource {
			if scenario.Batch && (scenario.ExplicitCache || input.Tokens.CacheCreationTokens > 0 || input.Tokens.CacheReadTokens > 0 || card.BatchChatPricing == nil) ||
				!scenario.Batch && card.ExplicitCachePricing == nil {
				return input, nil, 0, pricing.Conditional
			}
		}
	}
	input.RequestCount = 1
	if input.Mode == BillingModeToken && len(tokens) == 0 || input.Mode == BillingModeVideo && input.UsageUnits == 0 {
		return input, nil, 0, pricing.Conditional
	}
	if input.Mode == BillingModeImage && input.UsageUnits == 0 {
		input.UsageUnits = 1
		quantities["image"] = decimal.NewFromInt(count)
	}
	if input.Mode == BillingModePerRequest {
		quantities["request"] = decimal.NewFromInt(count)
	}
	return input, quantities, count, ""
}

func platformResult(cost *CostBreakdown, input CostInput, quantities map[string]decimal.Decimal, count int64) (pricing.Result, error) {
	requests := decimal.NewFromInt(count)
	charged := quantizedBillingDecimal(cost.ActualCost).Mul(requests)
	charge, err := money.NewMoney("USD", charged.String())
	if err != nil {
		return pricing.Result{}, err
	}
	result := pricing.Unpriced()
	result.State, result.PriceVersion, result.Source, result.Charge = "estimated", &cost.PriceCardVersion, &cost.PriceSource, &charge
	if cost.PricingState == PricingStateFree {
		result.State = "free"
	}
	total := decimal.NewFromFloat(cost.ActualCost).Mul(requests)
	adjustment := charged.Sub(total).StringFixed(18)
	result.RoundingAdjustment = &adjustment
	add := func(kind string, raw decimal.Decimal) {
		if quantity := quantities[kind]; quantity.IsPositive() {
			ratio := raw.Rat()
			result.Components = append(result.Components, pricing.Line{Kind: kind, Quantity: quantity.String(),
				RawAmount: raw.StringFixed(18), Numerator: ratio.Num().String(), Denominator: ratio.Denom().String()})
		}
	}
	if input.Mode == BillingModeToken {
		factor := decimal.NewFromFloat(input.RateMultiplier).Mul(requests)
		add("input", decimal.NewFromFloat(cost.InputCost).Mul(factor))
		add("output", decimal.NewFromFloat(cost.OutputCost).Mul(factor))
		add("cache_read", decimal.NewFromFloat(cost.CacheReadCost).Mul(factor))
		cacheWrite := decimal.NewFromFloat(cost.CacheCreationCost).Mul(factor)
		// Legacy billing exposes one cache creation component across TTLs.
		if quantity := quantities["cache_write_1h"]; quantity.IsPositive() {
			quantities["cache_write"] = quantities["cache_write"].Add(quantity)
		}
		add("cache_write", cacheWrite)
		add("image_input", decimal.NewFromFloat(cost.ImageInputCost).Mul(factor))
		add("image_output", decimal.NewFromFloat(cost.ImageOutputCost).Mul(factor))
	} else {
		kind := "request"
		if input.Mode == BillingModeImage {
			kind = "image"
		} else if input.Mode == BillingModeVideo {
			kind = "video"
		}
		add(kind, total)
	}
	return result, nil
}

func (s *BillingService) hasProviderTokenPrice(ctx context.Context, provider, model string) bool {
	if s == nil {
		return false
	}
	if provider == "" {
		return s.HasIdentifiedTokenPricing(model)
	}
	_, err := s.getProviderModelPricingAt(provider, model, pricingInstant(ctx, time.Time{}))
	return err == nil
}

func priceBookProviderForKey(ctx context.Context, key *APIKey) string {
	if key == nil {
		return pricingPlatform(ctx, PricingInput{})
	}
	return pricingPlatform(ctx, PricingInput{Group: key.Group})
}

func unitRatePerMillion(charge decimal.Decimal, input int64) (*money.Amount, error) {
	if input <= 0 {
		return nil, nil
	}
	price, err := money.Parse(charge.Mul(decimal.NewFromInt(1_000_000)).DivRound(decimal.NewFromInt(input), 18).String())
	if err != nil {
		return nil, err
	}
	return &price, nil
}

func saleModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}
