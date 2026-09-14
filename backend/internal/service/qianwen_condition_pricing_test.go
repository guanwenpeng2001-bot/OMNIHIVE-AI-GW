//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenCacheForwardingAndPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://batch.dashscope.aliyuncs.com/compatible-mode/v1"} {
		for _, explicit := range []bool{false, true} {
			body := []byte(`{"model":"qwen-plus","stream":false,"messages":[{"role":"user","content":"OK"}]}`)
			if explicit {
				body = []byte(`{"model":"qwen-plus","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"OK","cache_control":{"type":"ephemeral"}}]}]}`)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			card := parseQianwenModelPrices(qianwenConditionTestModel(false), 1.0/7).Pricing
			c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{*card}}})
			u := &httpUpstreamRecorder{resp: qianwenModelPage(`{"id":"cache","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1040,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0,"cache_creation_input_tokens":1024}}}`, 200)}
			s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u, resolver: NewModelPricingResolver(nil, newTestBillingServiceForResolver())}
			a := rawChatCompletionsTestAccount()
			a.Platform = PlatformQianwen
			a.Credentials["base_url"] = target
			result, err := s.forwardAsRawChatCompletions(context.Background(), c, a, body, "")
			batch := target != "https://dashscope.aliyuncs.com/compatible-mode/v1"
			if batch && explicit {
				require.Error(t, err)
				require.Empty(t, u.requests)
				continue
			}
			require.NoError(t, err)
			require.Equal(t, batch, result.QianwenBatchChat)
			require.Equal(t, explicit, result.QianwenExplicitCache)
			require.Equal(t, 1024, result.Usage.CacheCreationInputTokens)
		}
	}
}

func TestQianwenConditionValidationAndUnpricedFailure(t *testing.T) {
	p := parseQianwenModelPrices(qianwenConditionTestModel(false), 1.0/7).Pricing
	p.ExplicitCachePricing.CacheWritePrice = nil
	require.Error(t, validatePricingEntries([]ChannelModelPricing{*p}))
	p.ExplicitCachePricing = nil
	p.BatchChatPricing = nil
	bs := newTestBillingServiceForResolver()
	resolver := NewModelPricingResolver(nil, bs)
	for _, tokens := range []UsageTokens{{QianwenExplicitCache: true, CacheReadTokens: 20}, {QianwenBatchChat: true}, {CacheCreationTokens: 10}} {
		_, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "qwen-plus", Group: &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{*p}}, Resolver: resolver, Tokens: tokens, RateMultiplier: 1})
		require.Error(t, err)
		require.False(t, isUsagePricingUnavailableError(err))
	}
}

func qianwenConditionTestModel(tiered bool) QianwenModelPrices {
	m := qianwenTestModel()
	if tiered {
		m = qianwenTieredTestModel()
	}
	m.Model = "qwen-plus"
	for i := range m.Prices {
		rows := []QianwenPrice{}
		for _, p := range m.Prices[i].Prices {
			if p.Type == "input_token" || p.Type == "output_token" || p.Type == "input_token_cache" {
				rows = append(rows, p)
			}
		}
		for _, v := range []struct{ typ, price string }{{"input_token_cache_read", "0.5"}, {"input_token_cache_creation_5m", "15"}, {"input_token_batch_chat", "5"}, {"output_token_batch_chat", "10"}, {"input_token_batch", "3"}, {"output_token_batch", "6"}} {
			rows = append(rows, QianwenPrice{Type: v.typ, Price: v.price, Unit: "每百万tokens"})
		}
		m.Prices[i].Prices = rows
	}
	return m
}

func TestQianwenConditionRatesReachBilling(t *testing.T) {
	for _, tiered := range []bool{false, true} {
		m := qianwenConditionTestModel(tiered)
		for i := range m.Prices {
			for _, p := range append([]QianwenPrice(nil), m.Prices[i].Prices...) {
				p.Type = "thinking_" + p.Type
				if p.Type == "thinking_output_token" {
					p.Price = "72"
				}
				m.Prices[i].Prices = append(m.Prices[i].Prices, p)
			}
		}
		r := parseQianwenModelPrices(m, 1.0/7)
		require.NotNil(t, r.Pricing, r.Reason)
		require.Equal(t, 4*len(m.Prices), r.SkippedConditions)
		require.Len(t, r.UnsupportedConditions, r.SkippedConditions)
		require.NoError(t, validatePricingEntries([]ChannelModelPricing{*r.Pricing}))
		bs := newTestBillingServiceForResolver()
		resolver := NewModelPricingResolver(nil, bs)
		group := &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{*r.Pricing}}
		before, _ := json.Marshal(group)
		for _, think := range []bool{false, true} {
			for _, n := range []int{100, 128001} {
				for _, batch := range []bool{false, true} {
					tokens := UsageTokens{InputTokens: n, OutputTokens: 10, QianwenBatchChat: batch}
					want := float64(n)*5/7e6 + 10*10/7e6
					if !batch {
						tokens.QianwenExplicitCache = true
						tokens.CacheReadTokens = 20
						tokens.CacheCreationTokens = 30
						p := r.Pricing.ExplicitCachePricing
						if think {
							p = r.Pricing.ThinkingPricing.ExplicitCachePricing
						}
						in, out := p.InputPrice, p.OutputPrice
						if tiered {
							iv := FindMatchingInterval(p.Intervals, n+50)
							in, out = iv.InputPrice, iv.OutputPrice
						}
						want = float64(n)**in + 10**out + 20*0.5/7e6 + 30*15/7e6
					}
					cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: m.Model, Group: group, Resolver: resolver, Tokens: tokens, ThinkingEnabled: &think, RateMultiplier: 1})
					require.NoError(t, err)
					require.InDelta(t, want, cost.TotalCost, 1e-12)
					frozen := cost.PricingSnapshot
					replay, err := RecalculatePricingSnapshot(frozen)
					require.NoError(t, err)
					require.InDelta(t, want, replay.TotalCost, 1e-12)
					require.Equal(t, cost.ActualCost, replay.ActualCost)
				}
			}
		}
		after, _ := json.Marshal(group)
		require.Equal(t, string(before), string(after))
	}
}

func TestQianwenConditionsIncompleteNeverInventPrices(t *testing.T) {
	for _, typ := range []string{"input_token_cache_creation_5m", "input_token_cache_read", "input_token_batch_chat", "output_token_batch_chat"} {
		m := qianwenConditionTestModel(true)
		for i := range m.Prices[1].Prices {
			if m.Prices[1].Prices[i].Type == typ {
				m.Prices[1].Prices[i].Price = "NaN"
			}
		}
		r := parseQianwenModelPrices(m, 1.0/7)
		require.NotNil(t, r.Pricing)
		if typ == "input_token_cache_creation_5m" || typ == "input_token_cache_read" {
			require.Nil(t, r.Pricing.ExplicitCachePricing)
		} else {
			require.Nil(t, r.Pricing.BatchChatPricing)
		}
		require.NotEmpty(t, r.UnsupportedConditions)
	}
}

func TestQianwenThinkingOnlyNativeCards(t *testing.T) {
	for _, id := range []string{"qwen3-next-80b-a3b-thinking", "qwen3-235b-a22b-thinking-2507", "qwen3-30b-a3b-thinking-2507"} {
		m := qianwenTestModel()
		m.Model = id
		for i := range m.Prices[0].Prices {
			m.Prices[0].Prices[i].Type = "thinking_" + m.Prices[0].Prices[i].Type
		}
		r := parseQianwenModelPrices(m, 1.0/7)
		require.NotNil(t, r.Pricing, r.Reason)
		body, mode, err := prepareQianwenThinking([]byte(`{}`), []byte(`{}`), id, "enable_thinking")
		require.NoError(t, err)
		require.True(t, *mode)
		require.JSONEq(t, `{}`, string(body))
		_, _, err = prepareQianwenThinking([]byte(`{"enable_thinking":false}`), []byte(`{}`), id, "enable_thinking")
		require.Error(t, err)
	}
}

func TestQianwenExplicitCacheUsageAndTTL(t *testing.T) {
	usage, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"usage":{"prompt_tokens":1040,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":10,"cache_creation_input_tokens":1024}}}`))
	require.True(t, ok)
	require.Equal(t, 1024, usage.CacheCreationInputTokens)
	r := &OpenAIForwardResult{}
	require.NoError(t, observeQianwenNative([]byte(`{"usage":{"input_tokens":1040,"output_tokens":2,"prompt_tokens_details":{"cached_tokens":10,"cache_creation_input_tokens":1024}}}`), r))
	require.Equal(t, 1024, r.Usage.CacheCreationInputTokens)
	for _, ttl := range []string{"5m", "1h", "10m"} {
		explicit, err := qianwenCacheMode([]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"` + ttl + `"}}]}]}`))
		if ttl == "5m" {
			require.NoError(t, err)
			require.True(t, explicit)
		} else {
			require.Error(t, err)
		}
	}
	require.True(t, qianwenBatchChatURL("https://batch.dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"))
	require.False(t, qianwenBatchChatURL("https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"))
	require.False(t, qianwenBatchChatURL("https://batch.dashscope.aliyuncs.com.evil/compatible-mode/v1/chat/completions"))
}
