package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const QianwenPricingSource = "dashscope-cn-standard"

// QianwenPricingOptions deliberately has no default exchange rate. USDPerCNY
// means USD for one CNY; FXSource and FXEffectiveAt are operator provenance.
type QianwenPricingOptions struct {
	Region        string  `json:"region"`
	USDPerCNY     float64 `json:"usd_per_cny"`
	FXSource      string  `json:"fx_source"`
	FXEffectiveAt string  `json:"fx_effective_at"`
}

type QianwenPrice struct {
	Type     string  `json:"type"`
	Price    string  `json:"price"`
	Unit     string  `json:"price_unit"`
	Name     string  `json:"price_name"`
	TimeBand *string `json:"time_band"`
}

type QianwenPriceRange struct {
	Name   string         `json:"range_name"`
	Prices []QianwenPrice `json:"prices"`
}

type QianwenModelPrices struct {
	Model  string              `json:"model"`
	Prices []QianwenPriceRange `json:"prices"`
}

type QianwenPricingModelResult struct {
	QianwenModelPrices
	Status                string                        `json:"status"`
	Reason                string                        `json:"reason,omitempty"`
	SkippedConditions     int                           `json:"skipped_conditions"`
	UnsupportedConditions []QianwenUnsupportedCondition `json:"unsupported_conditions,omitempty"`
	Pricing               *ChannelModelPricing          `json:"pricing,omitempty"`
}

// QianwenPricingResult preserves the native IDs and all conditions, including
// unsupported models. Currency is established only for the verified CN site;
// published_time is NOT a price effective date.
type QianwenPricingResult struct {
	QianwenPricingOptions
	Source            string                      `json:"source"`
	SourceURL         string                      `json:"source_url,omitempty"`
	Currency          string                      `json:"currency"`
	PriceUnit         string                      `json:"price_unit"`
	FetchedAt         time.Time                   `json:"fetched_at"`
	Status            string                      `json:"status"`
	Reason            string                      `json:"reason,omitempty"`
	Pages             int                         `json:"pages"`
	Fetched           int                         `json:"fetched"`
	Duplicates        int                         `json:"duplicates"`
	Matched           int                         `json:"matched"`
	Imported          int                         `json:"imported"`
	ImportedRanges    int                         `json:"imported_ranges"`
	SkippedRanges     int                         `json:"skipped_ranges"`
	SkippedConditions int                         `json:"skipped_conditions"`
	Models            []QianwenPricingModelResult `json:"models"`
}

// FetchQianwenPricing uses the selected account's credential and proxy. No
// platform-wide LiteLLM provider alias or shared global price cache is involved.
func (s *AccountTestService) FetchQianwenPricing(ctx context.Context, account *Account, opts QianwenPricingOptions) (*QianwenPricingResult, error) {
	return fetchQianwenPricing(ctx, account, opts, func(req *http.Request) (*http.Response, error) {
		return s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	})
}

func fetchQianwenPricing(ctx context.Context, account *Account, opts QianwenPricingOptions, do func(*http.Request) (*http.Response, error)) (*QianwenPricingResult, error) {
	r := &QianwenPricingResult{QianwenPricingOptions: opts, Source: QianwenPricingSource, Currency: "CNY", PriceUnit: "USD/token", Status: "unsupported", Models: []QianwenPricingModelResult{}}
	if account == nil || account.Platform != PlatformQianwen || account.Type != AccountTypeAPIKey {
		r.Reason = "qianwen API-key account required"
		return r, nil
	}
	if !positiveFinite(opts.USDPerCNY) {
		r.Reason = "未配置汇率（unsupported）: usd_per_cny must be positive and finite"
		return r, nil
	}
	if strings.TrimSpace(opts.FXSource) == "" {
		r.Reason = "fx_source is required"
		return r, nil
	}
	if _, err := time.Parse(time.RFC3339, opts.FXEffectiveAt); err != nil {
		r.Reason = "fx_effective_at must be an RFC3339 timestamp"
		return r, nil
	}
	// Other regions may use a different currency, host, or workspace. Do not
	// extrapolate the verified legacy Beijing contract to those accounts.
	if opts.Region != "asia-pacific-china" {
		r.Reason = "unsupported region: only asia-pacific-china has verified CNY pricing"
		return r, nil
	}
	u, err := url.Parse(account.GetCredential("base_url"))
	if err != nil || u.Scheme != "https" || (u.Host != "dashscope.aliyuncs.com" && u.Host != "batch.dashscope.aliyuncs.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimRight(u.Path, "/") != "/compatible-mode/v1" {
		r.Reason = "unsupported account base URL: verified Beijing compatible-mode account required"
		return r, nil
	}
	key := account.GetCredential("api_key")
	if strings.TrimSpace(key) == "" {
		r.Reason = "account API key is missing"
		return r, nil
	}
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		r.Reason = "configured exact model mapping required"
		return r, nil
	}
	u.Host = "dashscope.aliyuncs.com"
	u.Path = "/api/v1/models"
	r.SourceURL = u.String()
	models := map[string]QianwenModelPrices{}
	conflicts := map[string]bool{}
	total, seen := -1, 0
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for page := 1; page <= 100; page++ {
		q := url.Values{"page_no": {strconv.Itoa(page)}, "page_size": {"100"}, "service_site": {opts.Region}}
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("build DashScope pricing request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		resp, err := do(req)
		if err != nil {
			return nil, fmt.Errorf("DashScope pricing page %d request failed", page)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024+1))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("DashScope pricing page %d HTTP %d", page, resp.StatusCode)
		}
		if readErr != nil || len(body) > 16*1024*1024 {
			return nil, fmt.Errorf("DashScope pricing page %d unreadable or too large", page)
		}
		var p struct {
			Success bool `json:"success"`
			Output  struct {
				Total  *int                 `json:"total"`
				Page   int                  `json:"page_no"`
				Models []QianwenModelPrices `json:"models"`
			} `json:"output"`
		}
		if json.Unmarshal(body, &p) != nil || !p.Success || p.Output.Total == nil || *p.Output.Total < 0 || p.Output.Page != page {
			return nil, fmt.Errorf("DashScope pricing page %d invalid schema or unsuccessful response", page)
		}
		if total == -1 {
			total = *p.Output.Total
		}
		if total != *p.Output.Total {
			return nil, fmt.Errorf("DashScope pricing catalog changed during pagination; retry sync")
		}
		r.Pages++
		for _, m := range p.Output.Models {
			if strings.TrimSpace(m.Model) == "" {
				return nil, fmt.Errorf("DashScope pricing model ID missing")
			}
			if old, ok := models[m.Model]; ok {
				r.Duplicates++
				a, _ := json.Marshal(old.Prices)
				b, _ := json.Marshal(m.Prices)
				if string(a) != string(b) {
					conflicts[m.Model] = true
					old.Prices = append(old.Prices, m.Prices...)
					models[m.Model] = old
				}
			} else {
				models[m.Model] = m
			}
		}
		seen += len(p.Output.Models)
		if seen >= total {
			break
		}
		if len(p.Output.Models) == 0 || page == 100 {
			return nil, fmt.Errorf("DashScope pricing pagination incomplete")
		}
	}
	r.FetchedAt = time.Now().UTC()
	r.Fetched = len(models)
	// Aliases/wildcards need request-vs-upstream billing semantics and are not
	// imported automatically. Raw upstream IDs always remain intact.
	ids := make([]string, 0, len(mapping))
	for id := range mapping {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m, found := models[id]
		entry := QianwenPricingModelResult{QianwenModelPrices: m, Status: "unsupported"}
		if !found {
			entry.Model = id
			entry.Status = "unmatched"
			entry.Reason = "no exact native model ID"
		} else {
			r.Matched++
			entry = parseQianwenModelPrices(m, opts.USDPerCNY)
			if mapping[id] != id || strings.ContainsAny(id, "*?") {
				entry.Pricing = nil
				entry.Status = "unsupported"
				entry.Reason = "aliases and wildcard model mappings require manual pricing"
			}
			if conflicts[id] {
				entry.Pricing = nil
				entry.Status = "unsupported"
				entry.Reason = "conflicting duplicate model prices"
			}
			if entry.Pricing == nil {
				entry.SkippedConditions = 0
				for _, tier := range entry.Prices {
					entry.SkippedConditions += len(tier.Prices)
				}
			}
		}
		annotateQianwenConditions(&entry)
		if entry.Pricing != nil {
			r.Imported++
			r.ImportedRanges += len(entry.Prices)
		}
		if entry.Pricing == nil {
			r.SkippedRanges += len(entry.Prices)
		}
		r.SkippedConditions += entry.SkippedConditions
		r.Models = append(r.Models, entry)
	}
	r.Status = "ready"
	if r.Imported == 0 {
		r.Status = "unsupported"
		r.Reason = "no supported model prices"
	}
	return r, nil
}

func positiveFinite(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func parseQianwenFlatPrices(m QianwenModelPrices, rate float64) QianwenPricingModelResult {
	r := QianwenPricingModelResult{QianwenModelPrices: m, Status: "unsupported"}
	count := 0
	for _, tier := range m.Prices {
		count += len(tier.Prices)
	}
	r.SkippedConditions = count
	if !positiveFinite(rate) {
		r.Reason = "exchange rate missing or invalid"
		return r
	}
	if len(m.Prices) != 1 || m.Prices[0].Name != "Default" {
		r.Reason = "tiered or missing default range requires conditional billing"
		return r
	}
	values := map[string]float64{}
	bands := map[string]string{}
	for _, p := range m.Prices[0].Prices {
		if strings.HasPrefix(p.Type, "thinking_") {
			r.Reason = "thinking-dependent pricing requires mode-aware billing"
			return r
		}
		band := ""
		if p.TimeBand != nil {
			band = *p.TimeBand
		}
		if band != "" && band != "standard" {
			continue
		}
		// Only implicit caching is identifiable in compatible chat usage. Explicit
		// cache reads, TTL writes and Batch remain in the raw result, not mixed in.
		if p.Type != "input_token" && p.Type != "output_token" && p.Type != "input_token_cache" {
			continue
		}
		if p.Unit != "每百万tokens" {
			r.Reason = "unsupported token price unit"
			return r
		}
		v, err := strconv.ParseFloat(p.Price, 64)
		v = v * rate / 1_000_000
		if err != nil || !positiveFinite(v) {
			r.Reason = "missing, zero, or invalid token price"
			return r
		}
		if _, exists := values[p.Type]; exists {
			r.Reason = "ambiguous duplicate standard token price"
			return r
		}
		values[p.Type] = v
		bands[p.Type] = band
	}
	in, out := values["input_token"], values["output_token"]
	if !positiveFinite(in) || !positiveFinite(out) {
		r.Reason = "complete standard input/output token pair missing"
		return r
	}
	if bands["input_token"] != bands["output_token"] {
		r.Reason = "input/output time bands differ"
		return r
	}
	card := &ChannelModelPricing{Platform: PlatformQianwen, Models: []string{m.Model}, BillingMode: BillingModeToken, InputPrice: &in, OutputPrice: &out, NativePricingSource: QianwenPricingSource}
	r.SkippedConditions -= 2
	if cache, ok := values["input_token_cache"]; ok && bands["input_token_cache"] == bands["input_token"] {
		card.CacheReadPrice = &cache
		r.SkippedConditions--
	}
	r.Pricing = card
	r.Status = "supported"
	return r
}

// Native bounds use decimal K/M, per the CN pricing documentation. Accept only
// the observed input-length grammar, never infer bounds from array position.
var qianwenInputRange = regexp.MustCompile(`^(?:([0-9]+)([km])<)?输入<=([0-9]+)([km])$`)

func parseQianwenUnconditionalPrices(m QianwenModelPrices, rate float64) QianwenPricingModelResult {
	if len(m.Prices) < 2 {
		return parseQianwenFlatPrices(m, rate)
	}
	r := QianwenPricingModelResult{QianwenModelPrices: m, Status: "unsupported"}
	for _, tier := range m.Prices {
		r.SkippedConditions += len(tier.Prices)
	}
	card := &ChannelModelPricing{Platform: PlatformQianwen, Models: []string{m.Model}, BillingMode: BillingModeToken, NativePricingSource: QianwenPricingSource}
	skipped := 0
	for _, tier := range m.Prices {
		parts := qianwenInputRange.FindStringSubmatch(tier.Name)
		if parts == nil {
			r.Reason = "unsupported input-length range label"
			return r
		}
		bound := func(n, unit string) (int, error) {
			if n == "" {
				return 0, nil
			}
			v, err := strconv.ParseUint(n, 10, 31)
			scale := uint64(1000)
			if unit == "m" {
				scale = 1000000
			}
			if err != nil || v == 0 || v > uint64(math.MaxInt32)/scale {
				return 0, fmt.Errorf("invalid input bound")
			}
			return int(v * scale), nil
		}
		lo, errLo := bound(parts[1], parts[2])
		hi, errHi := bound(parts[3], parts[4])
		if errLo != nil || errHi != nil || hi <= lo {
			r.Reason = "invalid input-length bounds"
			return r
		}
		flat := parseQianwenFlatPrices(QianwenModelPrices{Model: m.Model, Prices: []QianwenPriceRange{{Name: "Default", Prices: tier.Prices}}}, rate)
		if flat.Pricing == nil {
			r.Reason = tier.Name + ": " + flat.Reason
			return r
		}
		skipped += flat.SkippedConditions
		card.Intervals = append(card.Intervals, PricingInterval{MinTokens: lo, MaxTokens: &hi, TierLabel: tier.Name, InputPrice: flat.Pricing.InputPrice, OutputPrice: flat.Pricing.OutputPrice, CacheReadPrice: flat.Pricing.CacheReadPrice})
	}
	sort.Slice(card.Intervals, func(i, j int) bool { return card.Intervals[i].MinTokens < card.Intervals[j].MinTokens })
	expected := 0
	for i := range card.Intervals {
		iv := &card.Intervals[i]
		if iv.MinTokens != expected {
			r.Reason = "input-length ranges have a gap or overlap"
			return r
		}
		expected = *iv.MaxTokens
		iv.SortOrder = i
	}
	r.Pricing = card
	r.Status = "supported"
	r.SkippedConditions = skipped
	return r
}
