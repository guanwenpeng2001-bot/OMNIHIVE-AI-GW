//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func billingWithTokenPriceJSON(t *testing.T, fields string) *BillingService {
	t.Helper()
	prices := &PricingService{}
	var err error
	prices.pricingData, err = prices.parsePricingData([]byte(`{"presence-model":{` + fields + `}}`))
	require.NoError(t, err)
	return &BillingService{pricingService: prices}
}

// Exercise the real Submit/Prepare path, recording whether settlement was invoked.
func assertTokenPriceSettlement(t *testing.T, cost *CostBreakdown, tokens UsageTokens, tier string, hold bool) {
	t.Helper()
	repo := &settlementCapture{}
	svc := NewUsageSettlementService(repo, nil, nil)
	var tierPointer *string
	if tier != "" {
		tierPointer = &tier
	}
	receipt := &UsageSettlementReceipt{
		Key: UsageSettlementKey{RequestID: "presence", UserID: 1, APIKeyID: 2},
		Command: UsageBillingCommand{RequestID: "presence", UserID: 1, APIKeyID: 2, AccountID: 3,
			Model: "presence-model", InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens,
			CacheCreationTokens: tokens.CacheCreationTokens, CacheReadTokens: tokens.CacheReadTokens,
			ServiceTier: tier, BalanceCost: cost.ActualCost},
		Usage: UsageLog{RequestID: "presence", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "presence-model",
			InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens, CacheCreationTokens: tokens.CacheCreationTokens,
			CacheReadTokens: tokens.CacheReadTokens, CacheCreation5mTokens: tokens.CacheCreation5mTokens,
			CacheCreation1hTokens: tokens.CacheCreation1hTokens, ImageInputTokens: tokens.ImageInputTokens,
			ImageOutputTokens: tokens.ImageOutputTokens, ServiceTier: tierPointer,
			PricingState: cost.PricingState, PricingSnapshot: cost.PricingSnapshot, TotalCost: cost.TotalCost, ActualCost: cost.ActualCost},
		ResolutionState: "resolved", CompletionState: "complete",
	}
	err := svc.Submit(context.Background(), receipt, nil)
	if hold {
		require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
		require.Equal(t, UsageSettlementNeedsReview, repo.receipt.State)
		require.Equal(t, PricingStateUnpriced, repo.receipt.Usage.PricingState)
		require.Zero(t, repo.settles)
	} else {
		require.NoError(t, err)
		require.Equal(t, 1, repo.settles)
	}
}

func TestTokenPricePresenceSettlement(t *testing.T) {
	const text = `"input_cost_per_token":0.001,"output_cost_per_token":0.002`
	const orphan = text + `,"cache_read_input_token_cost":0.0001,"cache_creation_input_token_cost_above_200k_tokens":0.00025`
	for _, test := range []struct {
		name, fields, tier string
		tokens             UsageTokens
		hold               bool
		cost               float64
	}{
		{"orphan cache only", orphan, "", UsageTokens{CacheCreationTokens: 10}, true, 0},
		{"orphan cache details only", orphan, "", UsageTokens{CacheCreation1hTokens: 10}, true, 0},
		{"orphan mixed", orphan, "", UsageTokens{InputTokens: 10, OutputTokens: 5, CacheCreationTokens: 10}, true, 0},
		{"unused missing cache", orphan, "", UsageTokens{InputTokens: 10}, false, .01},
		{"missing read", text, "", UsageTokens{CacheReadTokens: 10}, true, 0},
		{"missing output", `"input_cost_per_token":0.001`, "", UsageTokens{OutputTokens: 10}, true, 0},
		{"unused output", `"input_cost_per_token":0.001`, "", UsageTokens{InputTokens: 10}, false, .01},
		{"missing input", `"output_cost_per_token":0.002`, "", UsageTokens{InputTokens: 10}, true, 0},
		{"unused input", `"output_cost_per_token":0.002`, "", UsageTokens{OutputTokens: 10}, false, .02},
		{"explicit free write", text + `,"cache_creation_input_token_cost":0`, "", UsageTokens{CacheCreationTokens: 10}, false, 0},
		{"explicit free text", `"input_cost_per_token":0,"output_cost_per_token":0`, "", UsageTokens{InputTokens: 10, OutputTokens: 10}, false, 0},
		{"explicit free read", text + `,"cache_read_input_token_cost":0`, "", UsageTokens{CacheReadTokens: 10}, false, 0},
		{"image fallback", text, "", UsageTokens{InputTokens: 10, ImageInputTokens: 10, OutputTokens: 5, ImageOutputTokens: 5}, false, .02},
		{"image fallback missing", `"output_cost_per_token":0.002`, "", UsageTokens{InputTokens: 10, ImageInputTokens: 10}, true, 0},
		{"image explicit free", text + `,"input_cost_per_image_token":0,"output_cost_per_image_token":0`, "", UsageTokens{InputTokens: 10, ImageInputTokens: 10, OutputTokens: 5, ImageOutputTokens: 5}, false, 0},
		{"image cache fallback", text + `,"cache_read_input_token_cost":0.0001`, "", UsageTokens{CacheReadTokens: 10, ImageCacheReadTokens: 10}, false, .001},
		{"image cache free", text + `,"cache_read_input_image_token_cost":0`, "", UsageTokens{CacheReadTokens: 10, ImageCacheReadTokens: 10}, false, 0},
		{"image cache mixed missing", text + `,"cache_read_input_image_token_cost":0`, "", UsageTokens{CacheReadTokens: 10, ImageCacheReadTokens: 5}, true, 0},
		{"priority fallback", text, "priority", UsageTokens{InputTokens: 10}, false, .02},
		{"priority only", `"input_cost_per_token_priority":0.003`, "priority", UsageTokens{InputTokens: 10}, false, .03},
		{"priority only standard missing", `"input_cost_per_token_priority":0.003`, "", UsageTokens{InputTokens: 10}, true, 0},
		{"priority free", text + `,"input_cost_per_token_priority":0`, "priority", UsageTokens{InputTokens: 10}, false, 0},
		{"priority cache free", text + `,"cache_creation_input_token_cost_priority":0`, "priority", UsageTokens{CacheCreationTokens: 10}, false, 0},
		{"1h only used", text + `,"cache_creation_input_token_cost_above_1hr":0.004`, "", UsageTokens{CacheCreationTokens: 10, CacheCreation1hTokens: 10}, false, .04},
		{"1h only missing 5m", text + `,"cache_creation_input_token_cost_above_1hr":0.004`, "", UsageTokens{CacheCreationTokens: 10, CacheCreation5mTokens: 10}, true, 0},
		{"1h only aggregate", text + `,"cache_creation_input_token_cost_above_1hr":0.004`, "", UsageTokens{CacheCreationTokens: 10}, true, 0},
		{"1h falls back to base", text + `,"cache_creation_input_token_cost":0.003`, "", UsageTokens{CacheCreationTokens: 10, CacheCreation1hTokens: 10}, false, .03},
		{"1h explicit free", text + `,"cache_creation_input_token_cost":0.003,"cache_creation_input_token_cost_above_1hr":0`, "", UsageTokens{CacheCreationTokens: 10, CacheCreation1hTokens: 10}, false, 0},
		{"5m explicit free", text + `,"cache_creation_input_token_cost":0,"cache_creation_input_token_cost_above_1hr":0.004`, "", UsageTokens{CacheCreationTokens: 10, CacheCreation5mTokens: 10}, false, 0},
		{"long context missing write", orphan + `,"long_context_input_token_threshold":1,"long_context_input_cost_multiplier":2`, "", UsageTokens{InputTokens: 10, CacheCreationTokens: 10}, true, 0},
		{"long context priced", text + `,"long_context_input_token_threshold":1,"long_context_input_cost_multiplier":2`, "", UsageTokens{InputTokens: 10}, false, .02},
		{"cache only tariff", `"cache_read_input_token_cost":0.0001`, "", UsageTokens{CacheReadTokens: 10}, false, .001},
		{"image only token tariff", `"input_cost_per_image_token":0.001`, "", UsageTokens{InputTokens: 10, ImageInputTokens: 10}, false, .01},
	} {
		t.Run(test.name, func(t *testing.T) {
			billing := billingWithTokenPriceJSON(t, test.fields)
			resolver := NewModelPricingResolver(nil, billing)
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "presence-model", Tokens: test.tokens, ServiceTier: test.tier, RateMultiplier: 1, Resolver: resolver})
			if test.hold {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Equal(t, PricingStateUnpriced, cost.PricingState)
			} else {
				require.NoError(t, err)
				require.InDelta(t, test.cost, cost.TotalCost, 1e-12)
				if test.cost == 0 {
					require.Equal(t, PricingStateFree, cost.PricingState)
				}
			}
			assertTokenPriceSettlement(t, cost, test.tokens, test.tier, test.hold)
			legacy, legacyErr := billing.calculateCostInternal("presence-model", test.tokens, 1, test.tier, nil)
			if test.hold {
				require.ErrorIs(t, legacyErr, ErrModelPricingUnavailable)
			} else {
				require.NoError(t, legacyErr)
			}
			require.Equal(t, cost.PricingState, legacy.PricingState)
			require.InDelta(t, cost.TotalCost, legacy.TotalCost, 1e-12)
		})
	}
}

func TestTokenPricePresenceOverridesAreIsolated(t *testing.T) {
	billing := billingWithTokenPriceJSON(t, `"input_cost_per_token":0.001,"output_cost_per_token":0.002`)
	resolver := NewModelPricingResolver(nil, billing)
	base, err := billing.GetModelPricing("presence-model")
	require.NoError(t, err)
	before, err := json.Marshal(base)
	require.NoError(t, err)
	zero, paid := 0.0, .004
	for _, override := range []*ChannelModelPricing{
		{CacheWritePrice: &zero}, {CacheWritePrice: &paid},
		{CacheWrite1hPrice: &zero}, {CacheWrite1hPrice: &paid},
	} {
		resolved := &ResolvedPricing{Mode: BillingModeToken, BasePricing: base}
		resolver.applyTokenOverrides(override, resolved)
		tokens := UsageTokens{CacheCreationTokens: 10, CacheCreation1hTokens: 10}
		cost, err := billing.CalculateCostUnified(CostInput{Model: "presence-model", Tokens: tokens, RateMultiplier: 1, Resolver: resolver, Resolved: resolved})
		require.NoError(t, err)
		assertTokenPriceSettlement(t, cost, tokens, "", false)
		interval := intervalToModelPricing(&PricingInterval{CacheWritePrice: override.CacheWritePrice, CacheWrite1hPrice: override.CacheWrite1hPrice}, base, nil)
		require.NoError(t, validateTokenDimensions(interval, tokens))
		after, err := json.Marshal(base)
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(after))
		require.ErrorIs(t, validateTokenDimensions(base, tokens), ErrModelPricingUnavailable)
	}
	// Multipliers, even zero multipliers, are not a replacement for a tariff.
	interval := intervalToModelPricing(&PricingInterval{CacheWriteMultiplier: &zero}, base, nil)
	require.ErrorIs(t, validateTokenDimensions(interval, UsageTokens{CacheCreationTokens: 10}), ErrModelPricingUnavailable)
	// Strict variant dimension maps must also be copied before overrides.
	variant := &ModelPricing{TokenDimensions: map[string]bool{"input": true}}
	interval = intervalToModelPricing(&PricingInterval{CacheWritePrice: &zero}, variant, nil)
	require.True(t, interval.TokenDimensions["cache_write"])
	require.False(t, variant.TokenDimensions["cache_write"])
}

func TestTokenPricePresenceGroupAndIntervalUsage(t *testing.T) {
	billing := &BillingService{}
	resolver := NewModelPricingResolver(nil, billing)
	zero, price := 0.0, .001
	for _, card := range []*ChannelModelPricing{
		{Models: []string{"presence-model"}, InputPrice: &zero},
		{Models: []string{"presence-model"}, Intervals: []PricingInterval{{MinTokens: 0, InputPrice: &zero}}},
	} {
		resolved := resolver.resolveConfiguredPricingFor(card, PricingInput{Model: "presence-model", At: time.Now()}, PricingSourceGroup)
		for _, tokens := range []UsageTokens{{InputTokens: 10}, {InputTokens: 10, OutputTokens: 1}, {CacheCreationTokens: 10}} {
			cost, err := billing.CalculateCostUnified(CostInput{Model: "presence-model", Tokens: tokens, Resolver: resolver, Resolved: resolved, RateMultiplier: 1})
			hold := tokens.OutputTokens > 0 || tokens.CacheCreationTokens > 0
			if hold {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
			} else {
				require.NoError(t, err)
			}
			assertTokenPriceSettlement(t, cost, tokens, "", hold)
		}
	}
	baseBilling := billingWithTokenPriceJSON(t, `"input_cost_per_token":0.001,"cache_creation_input_token_cost":0.003`)
	base, err := baseBilling.GetModelPricing("presence-model")
	require.NoError(t, err)
	resolved := &ResolvedPricing{Mode: BillingModeToken, BasePricing: base, longContextPricingEnabled: true,
		Intervals: []PricingInterval{{MinTokens: 100, InputPrice: &price, CacheReadPrice: &zero}}}
	for _, count := range []int{10, 110} {
		tokens := UsageTokens{InputTokens: count, CacheReadTokens: 1}
		cost, err := billing.CalculateCostUnified(CostInput{Model: "presence-model", Tokens: tokens, Resolver: resolver, Resolved: resolved, RateMultiplier: 1})
		if count == 10 {
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		} else {
			require.NoError(t, err)
		}
		assertTokenPriceSettlement(t, cost, tokens, "", count == 10)
	}
	// A real tariff multiplied by zero is free; a multiplier alone is not a tariff.
	free := intervalToModelPricing(&PricingInterval{CacheWriteMultiplier: &zero}, base, nil)
	tokens := UsageTokens{CacheCreationTokens: 10}
	cost, err := billing.CalculateCostUnified(CostInput{Model: "presence-model", Tokens: tokens, Resolver: resolver,
		Resolved: &ResolvedPricing{Mode: BillingModeToken, BasePricing: free}, RateMultiplier: 1})
	require.NoError(t, err)
	require.Equal(t, PricingStateFree, cost.PricingState)
	assertTokenPriceSettlement(t, cost, tokens, "", false)
	// Empty presence survives serialization so a future batch cannot gain a free tariff.
	encoded, err := json.Marshal(&ModelPricing{TokenPricePresence: map[string]bool{}})
	require.NoError(t, err)
	var restored ModelPricing
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.ErrorIs(t, validateTokenDimensions(&restored, tokens), ErrModelPricingUnavailable)
}

func TestTokenPricePresenceVariantUnusedDimensions(t *testing.T) {
	zero := 0.0
	descriptor := &ModelPricingDescriptor{Variants: []ModelPriceVariant{{ID: "presence-model", Lifecycle: "active",
		Prices: []ModelUnitPrice{{Unit: "token", Dimension: "input", Currency: "USD", Sales: &zero}},
	}}}
	pricing := descriptor.tokenPricing()
	require.NotNil(t, pricing)
	require.NoError(t, validateTokenDimensions(pricing, UsageTokens{InputTokens: 10}))
	require.ErrorIs(t, validateTokenDimensions(pricing, UsageTokens{InputTokens: 10, OutputTokens: 1}), ErrModelPricingUnavailable)
}

func TestTokenPricePresenceBuiltinAndImageOverrides(t *testing.T) {
	billing := newTestBillingService()
	zero := 0.0
	pricing, err := billing.GetModelPricingWithChannel("claude-sonnet-4", &ChannelModelPricing{ImageInputPrice: &zero})
	require.NoError(t, err)
	tokens := UsageTokens{InputTokens: 10, ImageInputTokens: 10}
	cost := billing.computeTokenBreakdown(pricing, tokens, 1, "", false)
	require.Zero(t, cost.TotalCost)
	base, err := billing.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)
	free := intervalToModelPricing(&PricingInterval{CacheWriteMultiplier: &zero}, base, nil)
	require.NoError(t, validateTokenDimensions(free, UsageTokens{CacheCreationTokens: 10, CacheCreation1hTokens: 10}))
	// The static LiteLLM fallback must have the same missing-cache protection.
	static := &BillingService{pricingService: &PricingService{pricingData: map[string]*LiteLLMModelPricing{"presence-model": openAIGPT54MiniFallbackPricing}}}
	missing, err := static.CalculateCost("presence-model", UsageTokens{CacheCreationTokens: 10}, 1)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, PricingStateUnpriced, missing.PricingState)
	// A 1h override must not turn the inherited flat/5m tariff into zero.
	pricing, err = billing.GetModelPricingWithChannel("gpt-6-astra", &ChannelModelPricing{CacheWrite1hPrice: &zero})
	require.NoError(t, err)
	tokens = UsageTokens{CacheCreationTokens: 10, CacheCreation5mTokens: 10}
	require.NoError(t, validateTokenDimensions(pricing, tokens))
	require.InDelta(t, 10*12.5e-6, billing.computeTokenBreakdown(pricing, tokens, 1, "", false).CacheCreationCost, 1e-12)
	// The existing GPT-5.6 derived cache tariff also supplies its 5m projection.
	pricesService := &PricingService{}
	pricesService.pricingData, err = pricesService.parsePricingData([]byte(`{"gpt-5.6-sol":{"input_cost_per_token":0.000005,"cache_creation_input_token_cost_above_1hr":0.00002}}`))
	require.NoError(t, err)
	derived := &BillingService{pricingService: pricesService}
	derivedCost, err := derived.CalculateCost("gpt-5.6-sol", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 10*6.25e-6, derivedCost.CacheCreationCost, 1e-12)
}

func TestTokenPricePresenceAuditedCacheOrphans(t *testing.T) {
	for _, model := range []string{
		"gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools",
		"openrouter/google/gemini-3-pro-preview", "vertex_ai/gemini-3-pro-preview",
		"vertex_ai/gemini-3.1-pro-preview", "vertex_ai/gemini-3.1-pro-preview-customtools",
	} {
		t.Run(model, func(t *testing.T) {
			prices := &PricingService{}
			var err error
			prices.pricingData, err = prices.parsePricingData([]byte(`{"` + model + `":{"input_cost_per_token":0.000002,"cache_read_input_token_cost":0.0000002,"cache_creation_input_token_cost_above_200k_tokens":0.00000025}}`))
			require.NoError(t, err)
			billing := &BillingService{pricingService: prices}
			for _, input := range []int{0, 10, 250000} {
				tokens := UsageTokens{InputTokens: input, CacheCreationTokens: 10}
				cost, err := billing.CalculateCostUnified(CostInput{Model: model, Tokens: tokens, RateMultiplier: 1, Resolver: NewModelPricingResolver(nil, billing)})
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				assertTokenPriceSettlement(t, cost, tokens, "", true)
			}
		})
	}
}

func TestTokenPricePresenceBatchChargeRejectsMissingUsedRate(t *testing.T) {
	billing := billingWithTokenPriceJSON(t, `"input_cost_per_token":0.001`)
	prices, err := billing.GetModelPricing("presence-model")
	require.NoError(t, err)
	groupID := int64(3)
	job := &UpstreamJob{PublicRequestID: "presence-batch", Platform: PlatformOpenAI, Family: "batch",
		UserID: 1, APIKeyID: 2, AccountID: 3, GroupID: &groupID,
		Billing: UpstreamJobBilling{Model: "presence-model", RequestedModel: "presence-model", RateMultiplier: 1, AccountRateMultiplier: 1,
			ResourceBatch: &GatewayResourceBatchSnapshot{Model: "presence-model", PricingMode: GatewayResourcePricingModeTokens,
				TokenPrices: prices, RequestCount: 1, CustomIDHashes: []string{"a"}}},
		UsageJSON: mustGatewayResourceUsage(t, GatewayResourceBatchUsage{
			RequestCounts: map[string]int64{"total": 1, "completed": 1, "failed": 0}, ProviderStatus: "completed",
			Lines: 1, Matched: 1, InputTokens: 10, OutputTokens: 1}),
	}
	log, command, err := GatewayResourceJobCharge(job)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, log)
	require.Nil(t, command)
	require.Equal(t, PricingStateUnpriced, job.Billing.PricingState)
	prices.markTokenPrices("output")
	log, command, err = GatewayResourceJobCharge(job)
	require.NoError(t, err)
	require.InDelta(t, .01, log.TotalCost, 1e-12)
	require.InDelta(t, .01, command.BalanceCost, 1e-12)
}
