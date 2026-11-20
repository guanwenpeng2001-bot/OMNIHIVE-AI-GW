package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/shopspring/decimal"
)

// BudgetMeter describes provider units before a request is sent. Numerator and
// Denominator retain fractional seconds without a floating-point division.
type BudgetMeter struct {
	Unit                   string
	Count                  int64
	Numerator, Denominator int64
	Size, Quality          string
}

func (s *UsageSettlementService) reserveBudgetAmount(ctx context.Context, cmd *KeyBudgetReservationCommand, amount money.Money) (*KeyBudgetReservationOutcome, error) {
	if repo, ok := s.repo.(interface {
		ReserveKeyBudgetAmount(context.Context, *KeyBudgetReservationCommand, money.Money) (*KeyBudgetReservationOutcome, error)
	}); ok {
		return repo.ReserveKeyBudgetAmount(ctx, cmd, amount)
	}
	cmd.HoldAmount = decimal.RequireFromString(amount.String()).InexactFloat64()
	return s.ReserveKeyBudget(ctx, cmd)
}

// Round the rational bound once, towards positive infinity, at the ledger scale.
func budgetAmount(n, d decimal.Decimal) *money.Money {
	if n.IsNegative() || !d.IsPositive() {
		return nil
	}
	q, rem := n.QuoRem(d, UsageBillingMonetaryScale)
	if rem.IsPositive() {
		q = q.Add(decimal.New(1, -UsageBillingMonetaryScale))
	}
	usd, err := money.NewMoney("USD", q.StringFixed(UsageBillingMonetaryScale))
	if err != nil {
		return nil
	}
	return &usd
}

func (p *GatewayRequestPlan) estimateBudgetHold(ctx context.Context, e RequestBudgetEstimate) *money.Money {
	if p.billing == nil {
		return nil
	}
	input, output, base := NormalizeBudgetEstimateBounds(e)
	if e.Group == nil && p.billing.cfg != nil {
		base = math.Max(base, p.billing.cfg.Default.RateMultiplier)
	}
	key := &APIKey{Group: e.Group}
	if e.Group != nil {
		key.GroupID = &e.Group.ID
		if p.budgetRate != nil {
			base = p.budgetRate(ctx, p.spec.UserID, e.Group.ID, e.Group.RateMultiplier)
		}
	}
	resolver := NewModelPricingResolver(p.channels, p.billing)
	in := PricingInput{Model: e.Model, Group: e.Group, GroupID: key.GroupID}
	if e.Meter != nil {
		return p.mediaBudgetAmount(ctx, resolver, in, key, e.Meter, base)
	}
	resolved := resolver.Resolve(ctx, in)
	if resolved.pricingErr != nil || (!p.billing.HasIdentifiedTokenPricing(e.Model) && resolved.descriptor == nil && resolved.Source != PricingSourceChannel && resolved.Source != PricingSourceGroup && !strings.HasPrefix(resolved.Source, "official_free:")) {
		return nil
	}
	multiplier, _ := computePeakAwareMultipliers(key, base, p.spec.PricingAt)
	cost, err := p.billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: e.Model, Group: e.Group, GroupID: key.GroupID, Resolver: resolver, Resolved: resolved, RateMultiplier: multiplier,
		Tokens: UsageTokens{InputTokens: input, OutputTokens: output}, RequestCount: 1, PricingAt: p.spec.PricingAt, ServiceTier: e.ServiceTier, ReasoningEffort: e.ReasoningEffort})
	if err != nil || cost == nil || (cost.PricingState != PricingStatePriced && cost.PricingState != PricingStateFree) || !validBillingNumber(cost.ActualCost) {
		return nil
	}
	var snap PricingSnapshot
	if err := json.Unmarshal([]byte(cost.PricingSnapshot), &snap); err != nil {
		return nil
	}
	bound := decimal.NewFromFloat(cost.ActualCost)
	if rate := snap.TokenPrices; rate != nil {
		// Use the largest applicable rate if the request spans a priority or
		// long-context tier. Discounts cannot reduce the admission upper bound.
		inputRate := math.Max(rate.InputPricePerToken, rate.InputPricePerTokenPriority)
		outputRate := math.Max(rate.OutputPricePerToken, rate.OutputPricePerTokenPriority)
		exact := decimal.NewFromInt(int64(input)).Mul(decimal.NewFromFloat(inputRate)).Add(decimal.NewFromInt(int64(output)).Mul(decimal.NewFromFloat(outputRate)))
		for _, factor := range []float64{multiplier, math.Max(1, snap.TimeMultiplier), math.Max(1, snap.ReasoningMultiplier)} {
			exact = exact.Mul(decimal.NewFromFloat(factor))
		}
		bound = decimal.Max(bound, exact)
	}
	return budgetAmount(bound, decimal.NewFromInt(1))
}

func (p *GatewayRequestPlan) mediaBudgetAmount(ctx context.Context, resolver *ModelPricingResolver, in PricingInput, key *APIKey, m *BudgetMeter, base float64) *money.Money {
	if m.Count <= 0 || m.Numerator <= 0 || m.Denominator <= 0 || !validBillingNumber(base) {
		return nil
	}
	descriptor, found, err := resolver.configuredPriceDescriptor(ctx, in)
	if err != nil {
		return nil
	}
	tier := m.Size
	unit := m.Unit
	multiplier := base
	switch unit {
	case "image":
		tier = NormalizeImageBillingTierOrDefault(tier)
		multiplier = resolveImageRateMultiplier(key, base)
	case "video_second":
		tier = NormalizeVideoBillingResolutionOrDefault(tier)
		unit = "second"
		multiplier = resolveVideoRateMultiplier(key, base)
	case "character", "second":
	default:
		return nil
	}
	if !validBillingNumber(multiplier) {
		return nil
	}
	quantity := decimal.NewFromInt(m.Count).Mul(decimal.NewFromInt(m.Numerator))
	divisor := decimal.NewFromInt(m.Denominator)
	var price float64
	if found {
		var fact ModelUnitPrice
		// A condition can specialize the unconditional rate. Include the latter
		// when settlement can still select it for this same request.
		matched := false
		for _, condition := range []string{m.Quality, tier, ""} {
			fact, err = descriptor.UnitPrice(unit, "output", condition)
			if err == nil {
				price = math.Max(price, *fact.Sales)
				matched = true
			}
		}
		if m.Unit == "image" && (m.Quality == "" || strings.EqualFold(m.Quality, "auto")) {
			// With automatic quality, the provider can choose any quality tier.
			for _, v := range descriptor.Variants {
				if !priceVariantActive(v, descriptor.PricingAt) {
					continue
				}
				for _, fact := range v.Prices {
					if fact.Unit == unit && fact.Dimension == "output" {
						if fact.Sales == nil || !validBillingNumber(*fact.Sales) {
							return nil
						}
						price = math.Max(price, *fact.Sales)
						matched = true
					}
				}
			}
		}
		if !matched {
			return nil
		}
	} else if unit == "character" || (unit == "second" && m.Unit != "video_second") {
		cfg := groupAudioPriceConfigFromAPIKey(key)
		if cfg == nil {
			return nil
		}
		if unit == "character" && cfg.TTSPerMChars != nil {
			price = *cfg.TTSPerMChars
			divisor = divisor.Mul(decimal.NewFromInt(1_000_000))
		} else if unit == "second" && cfg.STTPerHour != nil {
			price = *cfg.STTPerHour
			divisor = divisor.Mul(decimal.NewFromInt(3600))
		} else {
			return nil
		}
	} else {
		resolved := resolver.Resolve(ctx, in)
		if resolved.pricingErr != nil {
			return nil
		}
		if resolved.Mode == BillingModeToken && (resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel) {
			return nil
		}
		configured := resolved.Mode == BillingModeImage || resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeVideo
		switch {
		case resolved.Source != PricingSourceGroup && unit == "image" && apiKeyHasConfiguredImagePrice(key, tier):
			price = *key.Group.GetImagePrice(tier)
		case resolved.Source != PricingSourceGroup && unit == "second" && apiKeyHasConfiguredVideoPrice(key, in.Model, tier):
			price = *key.Group.GetVideoPriceForModel(in.Model, tier)
		case configured:
			var ok bool
			price, ok = resolver.LookupRequestTierPrice(resolved, tier)
			if qualityPrice, found := resolver.LookupRequestTierPrice(resolved, m.Quality); m.Quality != "" && found {
				price, ok = math.Max(price, qualityPrice), true
			}
			if !ok {
				price, ok = resolved.DefaultPerRequestPrice, resolved.DefaultPerRequestPriceFound
			}
			if !ok {
				return nil
			}
			if resolved.Mode != BillingModeVideo && m.Unit == "video_second" {
				quantity = decimal.NewFromInt(m.Count)
				divisor = decimal.NewFromInt(1)
			}
		case unit == "image":
			_, known := getDefaultGrokImagineImagePrice(in.Model, tier)
			if !known && !apiKeyHasConfiguredImagePrice(key, tier) {
				if p.billing.pricingService == nil {
					return nil
				}
				raw := p.billing.pricingService.GetIdentifiedModelPricing(in.Model)
				if raw == nil || !validBillingNumber(raw.OutputCostPerImage) || raw.OutputCostPerImage <= 0 || IsGPTImageGenerationModel(in.Model) {
					return nil
				}
				scale := decimal.NewFromInt(1)
				switch tier {
				case "2K":
					scale = decimal.New(15, -1)
				case "4K":
					scale = decimal.NewFromInt(2)
				}
				return budgetAmount(decimal.NewFromFloat(raw.OutputCostPerImage).Mul(scale).Mul(quantity).Mul(decimal.NewFromFloat(multiplier)), divisor)
			}
			price = p.billing.getImageUnitPrice(in.Model, tier, imagePriceConfigFromAPIKey(key))
		case unit == "second":
			_, known := getDefaultGrokImagineVideoPrice(in.Model, tier)
			if !known && !apiKeyHasConfiguredVideoPrice(key, in.Model, tier) {
				return nil
			}
			price = p.billing.getVideoUnitPrice(in.Model, tier, videoPriceConfigFromAPIKey(key))
		}
	}
	if !validBillingNumber(price) {
		return nil
	}
	return budgetAmount(decimal.NewFromFloat(price).Mul(quantity).Mul(decimal.NewFromFloat(multiplier)), divisor)
}
