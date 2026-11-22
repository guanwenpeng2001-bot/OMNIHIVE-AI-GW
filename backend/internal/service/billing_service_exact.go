package service

import (
	"encoding/json"
	"math"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/shopspring/decimal"
)

func readExactTokenRates(raw json.RawMessage) map[string]string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	if raw, exists := fields["currency"]; exists {
		var currency string
		if json.Unmarshal(raw, &currency) != nil || currency != "USD" {
			return nil
		}
	}
	rates := map[string]string{}
	for dimension, field := range map[string]string{
		"input": "input_cost_per_token", "input_priority": "input_cost_per_token_priority",
		"output": "output_cost_per_token", "output_priority": "output_cost_per_token_priority",
		"cache_read": "cache_read_input_token_cost", "cache_read_priority": "cache_read_input_token_cost_priority",
		"cache_write": "cache_creation_input_token_cost", "cache_write_priority": "cache_creation_input_token_cost_priority",
		"cache_write_1h": "cache_creation_input_token_cost_above_1hr",
		"image_input":    "input_cost_per_image_token", "image_output": "output_cost_per_image_token",
		"image_cache_read": "cache_read_input_image_token_cost",
	} {
		if len(fields[field]) == 0 {
			continue
		}
		value, err := decimal.NewFromString(string(fields[field]))
		if err != nil || value.Exponent() < -128 || value.Exponent() > 128 {
			continue
		}
		rate, err := money.NewRate("USD", value.String(), "token", "1")
		if err == nil {
			rates[dimension] = rate.Amount().String()
		}
	}
	return rates
}

func exactFrozenTokenPricing(snapshot PricingSnapshot) (pricing.Frozen, error) {
	frozen, err := snapshot.exactLeaf()
	p := snapshot.TokenPrices
	if err != nil || p == nil || len(p.ExactTokenRates) == 0 {
		return frozen, err
	}
	for i := range frozen.Version.Rules {
		rule := &frozen.Version.Rules[i]
		kind := rule.Kind
		switch kind {
		case "image_input":
			if !p.hasTokenPrice(kind, p.ImageInputPricePerToken) {
				kind = "input"
			}
		case "image_output":
			if !p.hasTokenPrice(kind, p.ImageOutputPricePerToken) && !p.ImageOutputPriceExplicit {
				kind = "output"
			}
		case "cache_write_5m":
			kind = "cache_write"
		}
		if usePriorityServiceTierPricing(snapshot.ServiceTier, p) && rule.Kind != "cache_write_5m" {
			priority := map[string]float64{"input": p.InputPricePerTokenPriority, "output": p.OutputPricePerTokenPriority,
				"cache_read": p.CacheReadPricePerTokenPriority, "cache_write": p.CacheCreationPricePerTokenPriority}
			if value, ok := priority[kind]; ok && p.hasTokenPrice(kind+"_priority", value) {
				kind += "_priority"
			}
		}
		if text := p.ExactTokenRates[kind]; text != "" {
			effective := decimal.RequireFromString(rule.Rate.Amount().String()).InexactFloat64()
			raw, err := decimal.NewFromString(text)
			if err != nil {
				return pricing.Frozen{}, err
			}
			if raw.InexactFloat64() != effective {
				continue // a runtime policy changed the configured rate
			}
			rate, err := money.NewRate("USD", text, rule.Rate.Unit(), "1")
			if err != nil {
				return pricing.Frozen{}, err
			}
			rule.Rate = rate
		}
	}
	return frozen, nil
}

func applyExactTokenCosts(cost *CostBreakdown, prices *ModelPricing, tokens UsageTokens, multiplier float64, serviceTier string, longContext bool) {
	if len(prices.ExactTokenRates) == 0 {
		return
	}
	frozen, err := exactFrozenTokenPricing(PricingSnapshot{TokenPrices: prices, Tokens: tokens,
		ServiceTier: serviceTier, RateMultiplier: multiplier, LongContextApplied: longContext, TimeMultiplier: 1, ReasoningMultiplier: 1})
	if err != nil || len(frozen.Version.Rules) == 0 {
		return
	}
	total, _ := (money.Amount{}).Exact("USD")
	for i, rule := range frozen.Version.Rules {
		line, err := rule.Rate.Cost(frozen.Usage.Dimensions[i].Quantity)
		if err != nil {
			return
		}
		for _, factor := range rule.Multipliers {
			line, err = line.Multiply(factor)
			if err != nil {
				return
			}
		}
		total, err = total.Add(line)
		if err != nil {
			return
		}
	}
	actual, err := total.Multiply(frozen.Version.Multipliers[0])
	if err != nil {
		return
	}
	cost.exactTotal, cost.exactActual = &total, &actual
	cost.TotalCost, cost.ActualCost = billingFloatFromExact(total), billingFloatFromExact(actual)
}

func applyExactTokenMultiplier(cost *CostBreakdown, multiplier float64) {
	if cost.exactActual == nil {
		return
	}
	factor, err := exactLegacyMultiplier(multiplier)
	if err != nil {
		cost.exactTotal, cost.exactActual = nil, nil
		return
	}
	total, totalErr := cost.exactTotal.Multiply(factor)
	actual, actualErr := cost.exactActual.Multiply(factor)
	if totalErr != nil || actualErr != nil {
		cost.exactTotal, cost.exactActual = nil, nil
		return
	}
	cost.exactTotal, cost.exactActual = &total, &actual
	cost.TotalCost, cost.ActualCost = billingFloatFromExact(total), billingFloatFromExact(actual)
}

// Existing receipt fields carry a float projection. Keep that projection on
// the correct side of a half-unit rounding boundary for the ledger.
func billingFloatFromExact(value money.Exact) float64 {
	projected, _ := value.Ratio().Float64()
	rounded, err := value.RoundCharge()
	if err != nil {
		return projected
	}
	expected := decimal.RequireFromString(rounded.String()).InexactFloat64()
	got := QuantizeUsageBillingAmount(projected)
	if got < expected {
		return math.Nextafter(projected, math.Inf(1))
	}
	if got > expected {
		return math.Nextafter(projected, math.Inf(-1))
	}
	return projected
}
