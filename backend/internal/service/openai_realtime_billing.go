package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func OpenAIRealtimeBillingSnapshot(ctx context.Context, group *Group, account *Account, key *APIKey, model string, subscription *UserSubscription) (UpstreamJobBilling, error) {
	billing := UpstreamJobBilling{RequestedModel: model, PricingState: PricingStateUnpriced}
	if group == nil || account == nil || key == nil || account.Platform != PlatformOpenAI || strings.TrimSpace(model) == "" {
		return billing, ErrUpstreamJobTariff
	}
	billing.Model = account.GetMappedModel(model)
	pricingAt := time.Now()
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
		pricingAt = plan.Snapshot().PricingAt
	}
	billing.RateMultiplier = group.RateMultiplier * group.PeakMultiplierAt(pricingAt)
	billing.AccountRateMultiplier = account.BillingRateMultiplier()
	billing.KeyQuota, billing.KeyRateLimit, billing.AccountQuota = key.Quota > 0, key.HasRateLimits(), account.HasAnyQuotaLimit()
	if subscription != nil {
		billing.SubscriptionID = &subscription.ID
	}
	billing.Realtime = &QianwenRealtimeSnapshot{PriceVersion: "openai-realtime-operator-tokens"}
	// Only operator group facts may price realtime. Generic text defaults or
	// elapsed-session minute rates cannot stand in for audio/image token prices.
	descriptor, found, err := NewModelPricingResolver(nil, nil).configuredPriceDescriptor(ctx, PricingInput{Model: model, Group: group})
	if err == nil && found {
		// Normalize timestamps and admission-only resolver flags exactly as the
		// database does, so a persisted replay compares equal to its evidence.
		encoded, encodeErr := json.Marshal(descriptor)
		var frozen ModelPricingDescriptor
		if encodeErr == nil && json.Unmarshal(encoded, &frozen) == nil {
			billing.Realtime.TokenPricing = &frozen
		}
	}
	if !finitePositive(billing.RateMultiplier) || !finitePositive(billing.AccountRateMultiplier) {
		return billing, ErrUpstreamJobTariff
	}
	return billing, nil
}

type openAIRealtimeTokenUsage struct {
	input, output, cached int
	dimensions            map[string]int
}

// Realtime uses singular input_token_details/output_token_details, unlike
// DashScope. Cached tokens are a subset of input, including cached modalities.
// https://developers.openai.com/api/reference/resources/realtime/server-events
func openAIRealtimeUsage(raw []byte) (openAIRealtimeTokenUsage, error) {
	usage := openAIRealtimeTokenUsage{dimensions: map[string]int{}}
	if !json.Valid(raw) {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	root := gjson.ParseBytes(raw)
	known := func(object gjson.Result, names ...string) bool {
		if !object.IsObject() {
			return false
		}
		valid := true
		object.ForEach(func(key, _ gjson.Result) bool {
			for _, name := range names {
				if key.String() == name {
					return true
				}
			}
			valid = false
			return false
		})
		return valid
	}
	count := func(value gjson.Result, required bool) (int, bool) {
		if !value.Exists() && !required {
			return 0, true
		}
		if value.Type != gjson.Number || value.Float() < 0 || value.Float() > 1e9 || float64(value.Int()) != value.Float() {
			return 0, false
		}
		return int(value.Int()), true
	}
	if !known(root, "total_tokens", "input_tokens", "output_tokens", "input_token_details", "output_token_details") {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	var valid bool
	if usage.input, valid = count(root.Get("input_tokens"), true); !valid {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	if usage.output, valid = count(root.Get("output_tokens"), true); !valid {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	if total := root.Get("total_tokens"); total.Exists() {
		value, ok := count(total, true)
		if !ok || value != usage.input+usage.output {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
	}
	input, output := root.Get("input_token_details"), root.Get("output_token_details")
	if !known(input, "text_tokens", "audio_tokens", "image_tokens", "cached_tokens", "cached_tokens_details") || !known(output, "text_tokens", "audio_tokens") {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	if usage.cached, valid = count(input.Get("cached_tokens"), false); !valid || usage.cached > usage.input {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	cached := input.Get("cached_tokens_details")
	if cached.Exists() && !known(cached, "text_tokens", "audio_tokens", "image_tokens") {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	inputSum, outputSum, cachedSum := 0, 0, 0
	for _, modality := range []string{"text", "audio", "image"} {
		all, ok := count(input.Get(modality+"_tokens"), false)
		cache, cacheOK := count(cached.Get(modality+"_tokens"), false)
		if !ok || !cacheOK || cache > all {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
		inputSum += all
		cachedSum += cache
		usage.dimensions["input_"+modality] = all - cache
		usage.dimensions["cached_input_"+modality] = cache
		if modality != "image" {
			out, ok := count(output.Get(modality+"_tokens"), false)
			if !ok {
				return usage, ErrUpstreamJobUnsupportedUsage
			}
			outputSum += out
			usage.dimensions["output_"+modality] = out
		}
	}
	if inputSum != usage.input || outputSum != usage.output || cachedSum != usage.cached {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	return usage, nil
}

func OpenAIRealtimeCharge(job *UpstreamJob) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(job, chargeErr) }()
	if job == nil || job.Platform != PlatformOpenAI || job.Family != "realtime_turn" || job.Billing.Realtime == nil || job.Billing.Realtime.SessionID == "" {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	usage, err := openAIRealtimeUsage(job.UsageJSON)
	if err != nil {
		return nil, nil, err
	}
	billing := job.Billing
	descriptor := billing.Realtime.TokenPricing
	if descriptor == nil || !finitePositive(billing.RateMultiplier) || !finitePositive(billing.AccountRateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	var sales, cost, inputCost, outputCost, cacheCost float64
	// A fixed order makes floating point aggregation reproducible on replays.
	for _, dimension := range []string{"input_text", "input_audio", "input_image", "cached_input_text", "cached_input_audio", "cached_input_image", "output_text", "output_audio"} {
		quantity := usage.dimensions[dimension]
		if quantity == 0 {
			continue
		}
		quote, err := descriptor.UnitPrice("token", dimension, "")
		if err != nil || quote.Cost == nil || !validBillingNumber(*quote.Cost) {
			return nil, nil, ErrUpstreamJobTariff
		}
		amount := float64(quantity) * *quote.Sales
		sales += amount
		cost += float64(quantity) * *quote.Cost
		switch {
		case strings.HasPrefix(dimension, "cached_"):
			cacheCost += amount
		case strings.HasPrefix(dimension, "input_"):
			inputCost += amount
		default:
			outputCost += amount
		}
	}
	total, upstreamCost := QuantizeUsageBillingAmount(sales), QuantizeUsageBillingAmount(cost)
	actual, accountCost := QuantizeUsageBillingAmount(total*billing.RateMultiplier), QuantizeUsageBillingAmount(upstreamCost*billing.AccountRateMultiplier)
	for _, amount := range []float64{total, upstreamCost, actual, accountCost} {
		if ValidateUsageBillingAmount(amount) != nil {
			return nil, nil, ErrUpstreamJobTariff
		}
	}
	mode, source, version := "realtime", descriptor.Source, billing.Realtime.PriceVersion
	log := &UsageLog{UserID: job.UserID, APIKeyID: job.APIKeyID, AccountID: job.AccountID, RequestID: job.PublicRequestID,
		Model: billing.Model, RequestedModel: billing.RequestedModel, UpstreamModel: &billing.Model, GroupID: job.GroupID,
		SubscriptionID: billing.SubscriptionID, InputTokens: usage.input - usage.cached, OutputTokens: usage.output, CacheReadTokens: usage.cached,
		InputCost: inputCost, OutputCost: outputCost, CacheReadCost: cacheCost, TotalCost: total, ActualCost: actual,
		RateMultiplier: billing.RateMultiplier, AccountRateMultiplier: &billing.AccountRateMultiplier, AccountStatsCost: &upstreamCost,
		BillingMode: &mode, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	if billing.SubscriptionID != nil {
		log.BillingType = BillingTypeSubscription
	}
	if billing.Realtime.UpstreamResponseModel != "" {
		declared := billing.Realtime.UpstreamResponseModel
		log.UpstreamResponseModel = &declared
	}
	// The exact per-dimension quotes remain immutable in Billing.Realtime.
	// Store their weighted per-token rate for the existing snapshot recalculator.
	units := float64(usage.input + usage.output)
	effectivePrice := 0.0
	if units > 0 {
		effectivePrice = sales / units
	}
	if err := freezeJobPricing(job, log, PricingSnapshot{UnitPrice: &effectivePrice, Units: units, SizeTier: "realtime_tokens"}); err != nil {
		return nil, nil, err
	}
	command = gatewayResourceBillingCommand(job, billing, actual, accountCost)
	command.MediaType = "realtime"
	return log, command, nil
}
