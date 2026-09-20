package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func (s *OpenAIGatewayService) validateQianwenRequestConditions(ctx context.Context, c *gin.Context, model string, body []byte, target string, thinking *bool) error {
	explicit, err := qianwenCacheMode(body)
	if err != nil {
		return err
	}
	batch := qianwenBatchChatURL(target)
	if !explicit && !batch {
		return nil
	}
	if batch && (explicit || gjson.GetBytes(body, "stream").Bool()) {
		return fmt.Errorf("unsupported Qianwen Batch Chat: caching and streaming are unavailable")
	}
	if s.resolver == nil || c == nil {
		return fmt.Errorf("unsupported Qianwen condition: native pricing resolver required")
	}
	v, _ := c.Get("api_key")
	key, ok := v.(*APIKey)
	if !ok || key.Group == nil {
		return fmt.Errorf("unsupported Qianwen condition: group rate card required")
	}
	in := CostInput{Ctx: WithResolvedTargetPlatform(ctx, PlatformQianwen), Model: model, Group: key.Group, GroupID: key.GroupID, Resolver: s.resolver, ThinkingEnabled: thinking, Tokens: UsageTokens{QianwenExplicitCache: explicit, QianwenBatchChat: batch}}
	resolved := s.resolver.Resolve(in.Ctx, PricingInput{Model: model, Group: key.Group, GroupID: key.GroupID})
	if resolved.channelPricing == nil || resolved.channelPricing.NativePricingSource != QianwenPricingSource {
		return fmt.Errorf("unsupported Qianwen condition: native rate card required")
	}
	resolved, err = resolveQianwenThinkingPricing(resolved, in)
	if err != nil {
		return err
	}
	_, err = resolveQianwenConditions(resolved, in)
	return err
}

// Only these native models have a documented fixed thinking mode. A suffix is
// not evidence for an arbitrary future model or for a multimodal rate card.
func qianwenThinkingOnly(model string) bool {
	switch model {
	case "qwen3-next-80b-a3b-thinking", "qwen3-235b-a22b-thinking-2507", "qwen3-30b-a3b-thinking-2507":
		return true
	}
	return false
}

func cloneQianwenMode(p *QianwenModePricing) *QianwenModePricing {
	if p == nil {
		return nil
	}
	v := *p
	v.InputPrice = clonePriceNumber(p.InputPrice)
	v.OutputPrice = clonePriceNumber(p.OutputPrice)
	v.CacheReadPrice = clonePriceNumber(p.CacheReadPrice)
	v.CacheWritePrice = clonePriceNumber(p.CacheWritePrice)
	v.Intervals = append([]PricingInterval(nil), p.Intervals...)
	for index, interval := range p.Intervals {
		v.Intervals[index] = clonePricingIntervalPrices(interval)
	}
	v.ExplicitCachePricing = cloneQianwenMode(p.ExplicitCachePricing)
	v.BatchChatPricing = cloneQianwenMode(p.BatchChatPricing)
	return &v
}

func validateQianwenCacheCard(p *QianwenModePricing) error {
	if p == nil {
		return nil
	}
	valid := func(write, read *float64) bool {
		return write != nil && read != nil && positiveFinite(*write) && positiveFinite(*read)
	}
	if len(p.Intervals) == 0 {
		if !valid(p.CacheWritePrice, p.CacheReadPrice) {
			return fmt.Errorf("complete positive Qianwen explicit cache write/read prices required")
		}
	}
	for _, iv := range p.Intervals {
		if !valid(iv.CacheWritePrice, iv.CacheReadPrice) {
			return fmt.Errorf("complete positive Qianwen explicit cache interval prices required")
		}
	}
	return nil
}

func qianwenModeFromCard(p *ChannelModelPricing) *QianwenModePricing {
	return &QianwenModePricing{InputPrice: p.InputPrice, OutputPrice: p.OutputPrice, CacheReadPrice: p.CacheReadPrice, CacheWritePrice: p.CacheWritePrice, Intervals: p.Intervals, ExplicitCachePricing: p.ExplicitCachePricing, BatchChatPricing: p.BatchChatPricing}
}

func applyQianwenMode(p *ChannelModelPricing, v *QianwenModePricing) {
	p.InputPrice, p.OutputPrice, p.CacheReadPrice, p.CacheWritePrice, p.Intervals = v.InputPrice, v.OutputPrice, v.CacheReadPrice, v.CacheWritePrice, v.Intervals
	p.ExplicitCachePricing, p.BatchChatPricing = v.ExplicitCachePricing, v.BatchChatPricing
}

func countQianwenPrices(m QianwenModelPrices) int {
	n := 0
	for _, r := range m.Prices {
		n += len(r.Prices)
	}
	return n
}

// Cache mode is derived from actual outbound content blocks, never arbitrary
// nested user JSON, tools, or a client-provided billing flag. DashScope supports
// ephemeral 5m only (context-cache documentation, verified 2026-09-13).
func qianwenCacheMode(body []byte) (bool, error) {
	explicit := false
	var err error
	for _, path := range []string{"messages", "input.messages"} {
		gjson.GetBytes(body, path).ForEach(func(_, msg gjson.Result) bool {
			msg.Get("content").ForEach(func(_, block gjson.Result) bool {
				v := block.Get("cache_control")
				if !v.Exists() {
					return true
				}
				if !v.IsObject() || v.Get("type").String() != "ephemeral" || (v.Get("ttl").Exists() && v.Get("ttl").String() != "5m") {
					err = fmt.Errorf("unsupported Qianwen cache_control: only ephemeral 5m is supported")
					return false
				}
				explicit = true
				return true
			})
			return err == nil
		})
	}
	return explicit, err
}

func qianwenBatchChatURL(target string) bool {
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "https" && u.Host == "batch.dashscope.aliyuncs.com" && u.Path == "/compatible-mode/v1/chat/completions"
}

// Native Batch Chat is a distinct synchronous endpoint. File Batch is not a
// chat request and requires a separate task ownership/idempotent settlement API.
func parseQianwenConditionalPrices(m QianwenModelPrices, rate float64) QianwenPricingModelResult {
	r := parseQianwenUnconditionalPrices(m, rate)
	if r.Pricing == nil {
		return r
	}
	for _, explicit := range []bool{true, false} {
		mode := QianwenModelPrices{Model: m.Model}
		writes := make([]*float64, 0, len(m.Prices))
		valid := true
		for _, tier := range m.Prices {
			out := QianwenPriceRange{Name: tier.Name}
			var write *float64
			inputBand, writeBand := "", ""
			for _, p := range tier.Prices {
				if p.TimeBand != nil && *p.TimeBand != "" && *p.TimeBand != "standard" {
					continue
				}
				if explicit {
					switch p.Type {
					case "input_token", "output_token":
						if p.Type == "input_token" && p.TimeBand != nil {
							inputBand = *p.TimeBand
						}
						out.Prices = append(out.Prices, p)
					case "input_token_cache_read":
						p.Type = "input_token_cache"
						out.Prices = append(out.Prices, p)
					case "input_token_cache_creation_5m":
						if p.TimeBand != nil {
							writeBand = *p.TimeBand
						}
						v, err := strconv.ParseFloat(p.Price, 64)
						v *= rate / 1e6
						if err != nil || !positiveFinite(v) || p.Unit != "每百万tokens" || write != nil {
							valid = false
						}
						write = &v
					}
				} else if p.Type == "input_token_batch_chat" || p.Type == "output_token_batch_chat" {
					p.Type = strings.TrimSuffix(p.Type, "_batch_chat")
					out.Prices = append(out.Prices, p)
				}
			}
			if explicit && (write == nil || inputBand != writeBand) {
				valid = false
			}
			writes = append(writes, write)
			mode.Prices = append(mode.Prices, out)
		}
		if !valid {
			continue
		}
		parsed := parseQianwenUnconditionalPrices(mode, rate)
		if parsed.Pricing == nil {
			continue
		}
		card := parsed.Pricing
		if explicit {
			if len(card.Intervals) == 0 {
				if card.CacheReadPrice == nil {
					continue
				}
				card.CacheWritePrice = writes[0]
			} else {
				for i := range card.Intervals {
					if card.Intervals[i].CacheReadPrice == nil {
						valid = false
					}
					// Native input ranges may arrive out of order.
					for j, tier := range m.Prices {
						if tier.Name == card.Intervals[i].TierLabel {
							card.Intervals[i].CacheWritePrice = writes[j]
						}
					}
				}
			}
			if !valid {
				continue
			}
			r.Pricing.ExplicitCachePricing = qianwenModeFromCard(card)
		} else {
			r.Pricing.BatchChatPricing = qianwenModeFromCard(card)
		}
		r.SkippedConditions -= 2 * len(m.Prices)
	}
	return r
}

func resolveQianwenConditions(resolved *ResolvedPricing, input CostInput) (*ResolvedPricing, error) {
	p := resolved.channelPricing
	if p == nil || p.NativePricingSource != QianwenPricingSource {
		return resolved, nil
	}
	var mode *QianwenModePricing
	if input.Tokens.QianwenBatchChat {
		if input.Tokens.QianwenExplicitCache || input.Tokens.CacheCreationTokens > 0 || input.Tokens.CacheReadTokens > 0 {
			return nil, fmt.Errorf("unsupported Qianwen Batch Chat cache usage")
		}
		mode = p.BatchChatPricing
	} else if input.Tokens.QianwenExplicitCache || input.Tokens.CacheCreationTokens > 0 {
		mode = p.ExplicitCachePricing
	} else {
		return resolved, nil
	}
	if mode == nil {
		return nil, fmt.Errorf("unsupported Qianwen pricing condition for %s", input.Model)
	}
	selected := p.Clone()
	applyQianwenMode(&selected, mode)
	r := input.Resolver.resolveConfiguredPricing(&selected, input.Model, resolved.Source)
	r.longContextPricingEnabled = false
	return r, nil
}

// Per-condition reasons accompany every skipped native row, even when another
// condition of the same model is supported. Raw prices remain authoritative.
type QianwenUnsupportedCondition struct {
	Range    string  `json:"range_name"`
	Type     string  `json:"type"`
	TimeBand *string `json:"time_band,omitempty"`
	Status   string  `json:"status"`
	Reason   string  `json:"reason"`
}

func parseQianwenModelPrices(m QianwenModelPrices, rate float64) QianwenPricingModelResult {
	if IsRetiredQianwenModel(m.Model) {
		return QianwenPricingModelResult{QianwenModelPrices: m, Status: "unavailable", Reason: "DashScope retired Qwen1.5 on 2026-03-30; https://help.aliyun.com/zh/model-studio/rate-limit"}
	}
	r := parseQianwenThinkingModelPrices(m, rate)
	annotateQianwenConditions(&r)
	return r
}

func annotateQianwenConditions(r *QianwenPricingModelResult) {
	r.UnsupportedConditions = nil
	for _, tier := range r.Prices {
		for _, price := range tier.Prices {
			p := r.Pricing
			typ := price.Type
			if strings.HasPrefix(typ, "thinking_") {
				typ = strings.TrimPrefix(typ, "thinking_")
				if p != nil && !qianwenThinkingOnly(r.Model) {
					if p.ThinkingPricing == nil {
						p = nil
					} else {
						cp := p.Clone()
						applyQianwenMode(&cp, p.ThinkingPricing)
						p = &cp
					}
				}
			}
			reason := ""
			switch {
			case r.Pricing == nil:
				reason = r.Reason
			case price.TimeBand != nil && *price.TimeBand != "" && *price.TimeBand != "standard":
				reason = "unsupported time band: no verified schedule/timezone in native catalog"
			case p == nil:
				reason = "unsupported thinking rate card"
			case typ == "input_token" || typ == "output_token":
			case typ == "input_token_cache":
				if len(p.Intervals) == 0 && p.CacheReadPrice == nil {
					reason = "unsupported implicit cache price or time band"
				}
			case typ == "input_token_cache_creation_5m" || typ == "input_token_cache_read":
				if p.ExplicitCachePricing == nil {
					reason = "unsupported explicit cache: complete 5m write/read pair required in every input range"
				}
			case typ == "input_token_batch_chat" || typ == "output_token_batch_chat":
				if p.BatchChatPricing == nil {
					reason = "unsupported Batch Chat: complete input/output pair required in every input range"
				}
			case strings.Contains(typ, "batch"):
				reason = "unsupported file Batch: no task ownership and idempotent completion settlement adapter"
			case strings.Contains(typ, "cache_creation"):
				reason = "unsupported cache write TTL: only documented ephemeral 5m is supported"
			default:
				reason = "unsupported native condition: no verified usage meter and billing adapter"
			}
			if reason != "" {
				r.UnsupportedConditions = append(r.UnsupportedConditions, QianwenUnsupportedCondition{Range: tier.Name, Type: price.Type, TimeBand: price.TimeBand, Status: "unsupported", Reason: reason})
			}
		}
	}
	r.SkippedConditions = len(r.UnsupportedConditions)
}
