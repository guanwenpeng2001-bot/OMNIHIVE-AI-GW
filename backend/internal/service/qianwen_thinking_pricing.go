package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var ErrQianwenThinkingUndetermined = errors.New("enable_thinking is required for this conditional rate card: no documented default")

// Validate before upstream consumption. An unknown mode is a request condition
// error, not a missing price that usage recording may silently replace with zero.
func (s *OpenAIGatewayService) validateQianwenThinkingMode(ctx context.Context, c *gin.Context, model string, mode *bool) error {
	if s.resolver == nil || c == nil {
		return nil
	}
	v, _ := c.Get("api_key")
	key, ok := v.(*APIKey)
	if !ok || key.Group == nil {
		return nil
	}
	resolved := s.resolver.Resolve(WithResolvedTargetPlatform(ctx, PlatformQianwen), PricingInput{Model: model, Group: key.Group, GroupID: key.GroupID})

	// Qianwen account-native IDs must have an identified configured card. A
	// similarly named global provider price is not evidence of DashScope pricing.
	if resolved == nil || (resolved.Source != PricingSourceGroup && resolved.Source != PricingSourceChannel) {
		return fmt.Errorf("%w: no verified Qianwen price for %s", ErrUnverifiedModelPrice, model)
	}
	if mode == nil && resolved.channelPricing != nil && resolved.channelPricing.ThinkingPricing != nil {
		return ErrQianwenThinkingUndetermined
	}
	return nil
}

// QianwenModePricing is a complete independent rate card, in USD/token.
// Alternate cache and Batch Chat cards retain independent mode/input tiers.
type QianwenModePricing struct {
	CacheWritePrice      *float64            `json:"cache_write_price,omitempty"`
	ExplicitCachePricing *QianwenModePricing `json:"explicit_cache_pricing,omitempty"`
	BatchChatPricing     *QianwenModePricing `json:"batch_chat_pricing,omitempty"`
	InputPrice           *float64            `json:"input_price"`
	OutputPrice          *float64            `json:"output_price"`
	CacheReadPrice       *float64            `json:"cache_read_price"`
	Intervals            []PricingInterval   `json:"intervals,omitempty"`
}

func qianwenExplicitThinking(body []byte, path string) *bool {
	v := gjson.GetBytes(body, path)
	if v.Type != gjson.True && v.Type != gjson.False {
		return nil
	}
	b := v.Bool()
	return &b
}

// Defaults documented by DashScope, checked 2026-09-13:
// https://help.aliyun.com/zh/model-studio/deep-thinking
// Keep this bounded to known conditional-price families; never guess for new IDs.
func qianwenDefaultThinking(model string) *bool {
	switch model {
	case "qwen3-14b", "qwen3-235b-a22b", "qwen3-30b-a3b", "qwen3-32b", "qwen3-8b":
		v := true
		return &v
	}
	for _, family := range []string{"qwen-turbo", "qwen-plus", "qwen-flash"} {
		if model == family || strings.HasPrefix(model, family+"-") {
			v := false
			return &v
		}
	}
	return nil
}

// Preserve the same effective mode in the wire request and billing result.
func prepareQianwenThinking(source, outbound []byte, model, path string) ([]byte, *bool, error) {
	explicit, err := qianwenCacheMode(outbound)
	if err != nil {
		return nil, nil, err
	}
	sourceExplicit, err := qianwenCacheMode(source)
	if err != nil {
		return nil, nil, err
	}
	if sourceExplicit && !explicit {
		return nil, nil, fmt.Errorf("unsupported Qianwen explicit cache: protocol conversion removed cache_control")
	}
	systemCache := false
	gjson.GetBytes(source, "system").ForEach(func(_, block gjson.Result) bool {
		systemCache = systemCache || block.Get("cache_control").Exists()
		return true
	})
	if !explicit && systemCache {
		return nil, nil, fmt.Errorf("unsupported Qianwen explicit cache: system cache markers are not preserved by this protocol adapter")
	}
	mode := qianwenExplicitThinking(source, path)
	if v := gjson.GetBytes(source, path); v.Exists() && mode == nil {
		return nil, nil, fmt.Errorf("enable_thinking must be a boolean")
	}
	if mode == nil && path == "enable_thinking" {
		switch gjson.GetBytes(source, "thinking.type").String() {
		case "enabled", "adaptive":
			v := true
			mode = &v
		case "disabled":
			v := false
			mode = &v
		}
	}
	if mode == nil {
		mode = qianwenDefaultThinking(model)
	}
	if qianwenThinkingOnly(model) {
		if mode != nil && !*mode {
			return nil, nil, fmt.Errorf("%s only supports thinking mode", model)
		}
		v := true
		return outbound, &v, nil
	}
	if mode == nil {
		return outbound, nil, nil
	}
	updated, err := sjson.SetBytes(outbound, path, *mode)
	return updated, mode, err
}

func parseQianwenThinkingModelPrices(m QianwenModelPrices, rate float64) QianwenPricingModelResult {
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
		return parseQianwenConditionalPrices(m, rate)
	}
	p, t := parseQianwenConditionalPrices(plain, rate), parseQianwenConditionalPrices(thinking, rate)
	r := QianwenPricingModelResult{QianwenModelPrices: m, Status: "unsupported", SkippedConditions: total}
	if qianwenThinkingOnly(m.Model) && t.Pricing != nil {
		r.Pricing, r.Status, r.SkippedConditions = t.Pricing, "supported", total-countQianwenPrices(thinking)+t.SkippedConditions
		return r
	}
	if p.Pricing == nil || t.Pricing == nil {
		r.Reason = "complete non-thinking and thinking cards required: " + p.Reason + "; " + t.Reason
		return r
	}
	p.Pricing.ThinkingPricing = qianwenModeFromCard(t.Pricing)
	r.Pricing, r.Status, r.SkippedConditions = p.Pricing, "supported", p.SkippedConditions+t.SkippedConditions
	return r
}

func resolveQianwenThinkingPricing(resolved *ResolvedPricing, input CostInput) (*ResolvedPricing, error) {
	card := resolved.channelPricing
	if card != nil && card.NativePricingSource == QianwenPricingSource && qianwenThinkingOnly(input.Model) && input.ThinkingEnabled != nil && !*input.ThinkingEnabled {
		return nil, fmt.Errorf("%s only supports thinking mode", input.Model)
	}
	if card == nil || card.ThinkingPricing == nil {
		return resolved, nil
	}
	if input.ThinkingEnabled == nil {
		input.ThinkingEnabled = qianwenDefaultThinking(input.Model)
	}
	if input.ThinkingEnabled == nil {
		return nil, fmt.Errorf("%s: %w", input.Model, ErrQianwenThinkingUndetermined)
	}
	if !*input.ThinkingEnabled {
		return resolved, nil
	}
	selected := card.Clone()
	v := selected.ThinkingPricing
	applyQianwenMode(&selected, v)
	selected.ThinkingPricing = nil
	r := input.Resolver.resolveConfiguredPricing(&selected, input.Model, resolved.Source)
	r.longContextPricingEnabled = resolved.longContextPricingEnabled
	return r, nil
}

func validateQianwenThinkingPrices(cards []ChannelModelPricing) error {
	for _, card := range cards {
		if card.ThinkingPricing == nil && card.ExplicitCachePricing == nil && card.BatchChatPricing == nil {
			continue
		}
		if card.NativePricingSource != QianwenPricingSource || card.Platform != PlatformQianwen || (card.BillingMode != "" && card.BillingMode != BillingModeToken) {
			return fmt.Errorf("thinking_pricing requires native Qianwen token pricing")
		}
		modes := []*QianwenModePricing{qianwenModeFromCard(&card)}
		if card.ThinkingPricing != nil {
			modes = append(modes, card.ThinkingPricing)
		}
		for _, parent := range append([]*QianwenModePricing(nil), modes...) {
			for _, child := range []*QianwenModePricing{parent.ExplicitCachePricing, parent.BatchChatPricing} {
				if child == nil {
					continue
				}
				if child.ExplicitCachePricing != nil || child.BatchChatPricing != nil {
					return fmt.Errorf("nested Qianwen pricing conditions are unsupported")
				}
				modes = append(modes, child)
			}
			if err := validateQianwenCacheCard(parent.ExplicitCachePricing); err != nil {
				return err
			}
		}
		for _, mode := range modes {
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
