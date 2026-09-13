package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// QianwenModePricing is a complete independent rate card, in USD/token.
// Batch and explicit cache creation remain unsupported native conditions.
type QianwenModePricing struct {
	InputPrice     *float64          `json:"input_price"`
	OutputPrice    *float64          `json:"output_price"`
	CacheReadPrice *float64          `json:"cache_read_price"`
	Intervals      []PricingInterval `json:"intervals,omitempty"`
}

func qianwenExplicitThinking(body []byte, path string) *bool {
	v := gjson.GetBytes(body, path)
	if v.Type != gjson.True && v.Type != gjson.False {
		return nil
	}
	b := v.Bool()
	return &b
}

func parseQianwenModelPrices(m QianwenModelPrices, rate float64) QianwenPricingModelResult {
	plain, thinking := QianwenModelPrices{Model: m.Model}, QianwenModelPrices{Model: m.Model}
	hasThinking, total := false, 0
	for _, tier := range m.Prices {
		p, t := QianwenPriceRange{Name: tier.Name}, QianwenPriceRange{Name: tier.Name}
		for _, price := range tier.Prices {
			total++
			if strings.HasPrefix(price.Type, "thinking_") {
				hasThinking = true
				price.Type = strings.TrimPrefix(price.Type, "thinking_")
				t.Prices = append(t.Prices, price)
			} else {
				p.Prices = append(p.Prices, price)
			}
		}
		plain.Prices = append(plain.Prices, p)
		thinking.Prices = append(thinking.Prices, t)
	}
	if !hasThinking {
		return parseQianwenUnconditionalPrices(m, rate)
	}
	p, t := parseQianwenUnconditionalPrices(plain, rate), parseQianwenUnconditionalPrices(thinking, rate)
	r := QianwenPricingModelResult{QianwenModelPrices: m, Status: "unsupported", SkippedConditions: total}
	if p.Pricing == nil || t.Pricing == nil {
		r.Reason = "complete non-thinking and thinking cards required: " + p.Reason + "; " + t.Reason
		return r
	}
	p.Pricing.ThinkingPricing = &QianwenModePricing{InputPrice: t.Pricing.InputPrice, OutputPrice: t.Pricing.OutputPrice, CacheReadPrice: t.Pricing.CacheReadPrice, Intervals: t.Pricing.Intervals}
	r.Pricing, r.Status, r.SkippedConditions = p.Pricing, "supported", p.SkippedConditions+t.SkippedConditions
	return r
}

func resolveQianwenThinkingPricing(resolved *ResolvedPricing, input CostInput) (*ResolvedPricing, error) {
	card := resolved.channelPricing
	if card == nil || card.ThinkingPricing == nil {
		return resolved, nil
	}
	if input.ThinkingEnabled == nil {
		return nil, fmt.Errorf("explicit enable_thinking required for conditional price of %s: %w", input.Model, ErrModelPricingUnavailable)
	}
	if !*input.ThinkingEnabled {
		return resolved, nil
	}
	selected := card.Clone()
	v := selected.ThinkingPricing
	selected.InputPrice, selected.OutputPrice, selected.CacheReadPrice, selected.Intervals = v.InputPrice, v.OutputPrice, v.CacheReadPrice, v.Intervals
	selected.ThinkingPricing = nil
	r := input.Resolver.resolveConfiguredPricing(&selected, input.Model, resolved.Source)
	r.longContextPricingEnabled = resolved.longContextPricingEnabled
	return r, nil
}

func validateQianwenThinkingPrices(cards []ChannelModelPricing) error {
	for _, card := range cards {
		if card.ThinkingPricing == nil {
			continue
		}
		if card.NativePricingSource != QianwenPricingSource || card.Platform != PlatformQianwen || (card.BillingMode != "" && card.BillingMode != BillingModeToken) {
			return fmt.Errorf("thinking_pricing requires native Qianwen token pricing")
		}
		v := card.ThinkingPricing
		for _, mode := range []QianwenModePricing{{InputPrice: card.InputPrice, OutputPrice: card.OutputPrice, CacheReadPrice: card.CacheReadPrice, Intervals: card.Intervals}, *v} {
			valid := func(in, out, cache *float64) bool {
				return in != nil && out != nil && positiveFinite(*in) && positiveFinite(*out) && (cache == nil || positiveFinite(*cache))
			}
			if len(mode.Intervals) == 0 {
				if !valid(mode.InputPrice, mode.OutputPrice, mode.CacheReadPrice) {
					return fmt.Errorf("complete positive thinking/non-thinking prices required")
				}
			} else {
				if err := ValidateIntervals(mode.Intervals, BillingModeToken); err != nil {
					return err
				}
				expected := 0
				for _, iv := range mode.Intervals {
					if iv.MinTokens != expected || iv.MaxTokens == nil || *iv.MaxTokens <= expected || !valid(iv.InputPrice, iv.OutputPrice, iv.CacheReadPrice) {
						return fmt.Errorf("complete contiguous native mode intervals required")
					}
					expected = *iv.MaxTokens
				}
			}
		}
	}
	return nil
}
