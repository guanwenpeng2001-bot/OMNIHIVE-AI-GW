//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func qianwenTestModel() QianwenModelPrices {
	return QianwenModelPrices{Model: "qwen3.8-max", Prices: []QianwenPriceRange{{Name: "Default", Prices: []QianwenPrice{
		{Type: "input_token", Price: "12", Unit: "每百万tokens"},
		{Type: "output_token", Price: "36", Unit: "每百万tokens"},
		{Type: "input_token_cache", Price: "1.5", Unit: "每百万tokens"},
		{Type: "input_token_batch", Price: "6", Unit: "每百万tokens"},
		{Type: "input_token_cache_read", Price: "1", Unit: "每百万tokens"},
	}}}}
}

func TestQianwenPriceConditionsAndConversion(t *testing.T) {
	m := qianwenTestModel()
	peak, standard := "peak", "standard"
	for i := range m.Prices[0].Prices {
		m.Prices[0].Prices[i].TimeBand = &standard
	}
	m.Prices[0].Prices = append([]QianwenPrice{{Type: "input_token", Price: "24", Unit: "每百万tokens", TimeBand: &peak}}, m.Prices[0].Prices...)
	r := parseQianwenModelPrices(m, 0.125)
	require.Equal(t, "supported", r.Status)
	require.Equal(t, 3, r.SkippedConditions)
	require.Len(t, r.Prices[0].Prices, 6)
	require.InDelta(t, 1.5e-6, *r.Pricing.InputPrice, 1e-16)
	require.InDelta(t, 4.5e-6, *r.Pricing.OutputPrice, 1e-16)
	require.InDelta(t, 0.1875e-6, *r.Pricing.CacheReadPrice, 1e-16)
	require.Nil(t, r.Pricing.CacheWritePrice)
}

func TestQianwenPricesNeverInventZeroOrFlattenConditions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*QianwenModelPrices)
	}{
		{"missing prices", func(m *QianwenModelPrices) { m.Prices = nil }},
		{"missing output", func(m *QianwenModelPrices) { m.Prices[0].Prices = m.Prices[0].Prices[:1] }},
		{"missing unit", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Unit = "" }},
		{"wrong unit", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Unit = "每秒" }},
		{"missing value", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Price = "" }},
		{"zero", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Price = "0" }},
		{"negative", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Price = "-1" }},
		{"NaN", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Price = "NaN" }},
		{"Inf", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Price = "+Inf" }},
		{"underflow", func(m *QianwenModelPrices) { m.Prices[0].Prices[0].Price = "1e-320" }},
		{"tier name", func(m *QianwenModelPrices) { m.Prices[0].Name = "输入<=128k" }},
		{"multiple tiers", func(m *QianwenModelPrices) { m.Prices = append(m.Prices, QianwenPriceRange{Name: "128k<输入<=256k"}) }},
		{"thinking", func(m *QianwenModelPrices) {
			m.Prices[0].Prices = append(m.Prices[0].Prices, QianwenPrice{Type: "thinking_output_token", Price: "48"})
		}},
		{"duplicate", func(m *QianwenModelPrices) { m.Prices[0].Prices = append(m.Prices[0].Prices, m.Prices[0].Prices[0]) }},
		{"peak only output", func(m *QianwenModelPrices) { p := "peak"; m.Prices[0].Prices[1].TimeBand = &p }},
		{"mixed bands", func(m *QianwenModelPrices) { p := "standard"; m.Prices[0].Prices[1].TimeBand = &p }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := qianwenTestModel()
			tc.mutate(&m)
			r := parseQianwenModelPrices(m, 0.125)
			require.Equal(t, "unsupported", r.Status)
			require.NotEmpty(t, r.Reason)
			require.Nil(t, r.Pricing)
		})
	}
}

func qianwenPricingTestAccount() *Account {
	return &Account{ID: 19, Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1", "api_key": "test-only-key", "model_mapping": map[string]any{"qwen3.8-max": "qwen3.8-max", "missing": "missing"},
	}}
}

func qianwenTestOptions() QianwenPricingOptions {
	return QianwenPricingOptions{Region: "asia-pacific-china", USDPerCNY: 0.125, FXSource: "unit test fixture", FXEffectiveAt: "2026-09-13T00:00:00Z"}
}

func qianwenTestPage(page, total int, ms ...QianwenModelPrices) *http.Response {
	b, _ := json.Marshal(map[string]any{"success": true, "output": map[string]any{"page_no": page, "total": total, "models": ms}})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func TestQianwenPricingPaginationAndDuplicates(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			calls := 0
			r, err := fetchQianwenPricing(context.Background(), qianwenPricingTestAccount(), qianwenTestOptions(), func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, fmt.Sprint(calls), req.URL.Query().Get("page_no"))
				require.Equal(t, "100", req.URL.Query().Get("page_size"))
				require.Equal(t, "asia-pacific-china", req.URL.Query().Get("service_site"))
				require.Equal(t, "Bearer test-only-key", req.Header.Get("Authorization"))
				m := qianwenTestModel()
				if calls == 2 && conflict {
					m.Prices[0].Prices[0].Price = "99"
				}
				return qianwenTestPage(calls, 2, m), nil
			})
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			require.Equal(t, 1, r.Fetched)
			require.Equal(t, 1, r.Duplicates)
			require.Equal(t, 1, r.Matched)
			require.Equal(t, "unmatched", r.Models[0].Status)
			if conflict {
				require.Zero(t, r.Imported)
				require.Nil(t, r.Models[1].Pricing)
			} else {
				require.Equal(t, 1, r.Imported)
			}
		})
	}
}

func TestQianwenPricingMissingFXNeverFetchesOrReturnsCards(t *testing.T) {
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		opts := qianwenTestOptions()
		opts.USDPerCNY = rate
		r, err := fetchQianwenPricing(context.Background(), qianwenPricingTestAccount(), opts, func(*http.Request) (*http.Response, error) { t.Fatal("must not fetch"); return nil, nil })
		require.NoError(t, err)
		require.Equal(t, "unsupported", r.Status)
		require.Contains(t, r.Reason, "未配置汇率")
		require.Zero(t, r.Imported)
		require.Empty(t, r.Models)
	}
}

func TestQianwenPricingPartialFailureReturnsNoSnapshot(t *testing.T) {
	for _, failure := range []string{"http", "schema", "total changed", "empty page", "missing id"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			r, err := fetchQianwenPricing(context.Background(), qianwenPricingTestAccount(), qianwenTestOptions(), func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return qianwenTestPage(1, 2, qianwenTestModel()), nil
				}
				switch failure {
				case "http":
					return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("do not echo secret response"))}, nil
				case "schema":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"output":{}}`))}, nil
				case "total changed":
					return qianwenTestPage(2, 3, qianwenTestModel()), nil
				case "missing id":
					return qianwenTestPage(2, 2, QianwenModelPrices{}), nil
				default:
					return qianwenTestPage(2, 2), nil
				}
			})
			require.Error(t, err)
			require.Nil(t, r)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestQianwenPricingAccountAndRegionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Account, *QianwenPricingOptions)
	}{
		{"international", func(a *Account, o *QianwenPricingOptions) { o.Region = "ap-southeast-1" }},
		{"custom host", func(a *Account, o *QianwenPricingOptions) {
			a.Credentials["base_url"] = "https://example.com/compatible-mode/v1"
		}},
		{"HTTP", func(a *Account, o *QianwenPricingOptions) {
			a.Credentials["base_url"] = "http://dashscope.aliyuncs.com/compatible-mode/v1"
		}},
		{"other platform", func(a *Account, o *QianwenPricingOptions) { a.Platform = PlatformOpenAI }},
		{"missing key", func(a *Account, o *QianwenPricingOptions) { delete(a.Credentials, "api_key") }},
		{"no models", func(a *Account, o *QianwenPricingOptions) { delete(a.Credentials, "model_mapping") }},
		{"no FX source", func(a *Account, o *QianwenPricingOptions) { o.FXSource = "" }},
		{"no FX date", func(a *Account, o *QianwenPricingOptions) { o.FXEffectiveAt = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, opts := qianwenPricingTestAccount(), qianwenTestOptions()
			tc.change(a, &opts)
			r, err := fetchQianwenPricing(context.Background(), a, opts, func(*http.Request) (*http.Response, error) { t.Fatal("must not send credential"); return nil, nil })
			require.NoError(t, err)
			require.Equal(t, "unsupported", r.Status)
			require.NotEmpty(t, r.Reason)
			require.Zero(t, r.Imported)
		})
	}
}

func TestQianwenImportedGroupProducesNonzeroCostWithoutFallbackContamination(t *testing.T) {
	card := parseQianwenModelPrices(qianwenTestModel(), 0.125).Pricing
	bs := newTestBillingServiceForResolver()
	bs.fallbackPrices[card.Models[0]] = &ModelPricing{InputPricePerToken: 99, OutputPricePerToken: 99, CacheCreationPricePerToken: 99, LongContextInputThreshold: 1, LongContextInputMultiplier: 999}
	resolver := NewModelPricingResolver(nil, bs)
	group := &Group{ID: 1, Platform: PlatformQianwen, LongContextPricingEnabled: true, ModelPricing: []ChannelModelPricing{*card}}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: card.Models[0], Group: group})
	require.Zero(t, resolved.BasePricing.CacheCreationPricePerToken)
	require.Zero(t, resolved.BasePricing.LongContextInputThreshold)
	require.False(t, resolved.longContextPricingEnabled)
	cost, err := bs.CalculateCostUnified(CostInput{Model: card.Models[0], Group: group, Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, 0.000195, cost.TotalCost, 1e-12)
}
