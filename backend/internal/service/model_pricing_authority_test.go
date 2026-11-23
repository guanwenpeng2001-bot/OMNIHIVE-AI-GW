//go:build unit

package service

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/stretchr/testify/require"
)

func TestUnifiedPricingProviderIdentityAndAmbiguity(t *testing.T) {
	a := &LiteLLMModelPricing{InputCostPerToken: 1, OutputCostPerToken: 2, sourceOrigin: PricingSourceLiteLLM}
	b := &LiteLLMModelPricing{InputCostPerToken: 4, OutputCostPerToken: 8, sourceOrigin: PricingSourceLiteLLM}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"provider-a/same": a, "provider-b/same": b}}
	billing := &BillingService{pricingService: prices}
	resolver := NewModelPricingResolver(nil, billing)
	for _, test := range []struct {
		provider string
		expected float64
	}{{"provider-a", 1}, {"provider-b", 4}} {
		bound, err := prices.BoundPricingModel(test.provider, "same")
		require.NoError(t, err)
		require.Equal(t, test.provider+"/same", bound)
		resolved := resolver.Resolve(context.Background(), PricingInput{Provider: test.provider, Model: "same"})
		require.NoError(t, resolved.pricingErr)
		require.NotNil(t, resolved.BasePricing)
		require.Equal(t, test.expected, resolved.BasePricing.InputPricePerToken)
		require.Equal(t, test.provider, resolved.Context.Provider)
	}
	_, err := prices.BoundPricingModel("", "same")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, prices.GetModelPricing("same"))
	_, err = billing.GetModelPricing("same")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = prices.BoundPricingModel("provider-c", "same")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// A partial operator card may only inherit from its own provider baseline.
	input := 9.0
	group := &Group{ID: 3, Platform: "provider-b", ModelPricing: []ChannelModelPricing{{Models: []string{"same"}, InputPrice: &input}}}
	resolved := resolver.Resolve(context.Background(), PricingInput{Provider: "provider-b", Model: "same", Group: group, GroupID: &group.ID})
	require.Equal(t, input, resolved.BasePricing.InputPricePerToken)
	require.Equal(t, float64(8), resolved.BasePricing.OutputPricePerToken)
	require.Equal(t, PricingSourceGroup, resolved.BasePricing.FieldSources["input_cost_per_token"])
	require.Equal(t, PricingSourceLiteLLM, resolved.BasePricing.FieldSources["output_cost_per_token"])
}

func TestUnifiedPricingSingleForeignProviderCannotSupplyBaseline(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		name := "direct_fixture"
		if indexed {
			name = "published_index"
		}
		t.Run(name, func(t *testing.T) {
			prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				"provider-a/same": {InputCostPerToken: 1, OutputCostPerToken: 2, sourceOrigin: PricingSourceLiteLLM},
			}}
			if indexed {
				prices.rebuildBaseNameIndex()
			}
			billing := NewBillingService(nil, prices)
			resolver := NewModelPricingResolver(nil, billing)
			bound, err := prices.BoundPricingModel("provider-a", "same")
			require.NoError(t, err)
			require.Equal(t, "provider-a/same", bound)
			bound, err = prices.BoundPricingModel("", "same")
			require.NoError(t, err)
			require.Equal(t, "provider-a/same", bound, "a unique unscoped identity remains compatible")
			_, err = prices.BoundPricingModel("provider-c", "same")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)

			input := CostInput{
				Ctx: context.Background(), Mode: BillingModeToken, Model: "same",
				Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1,
				PricingAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Resolver: resolver,
			}
			for _, provider := range []string{"provider-a", ""} {
				input.Provider = provider
				cost, err := billing.CalculateCostUnified(input)
				require.NoError(t, err)
				require.Equal(t, float64(3), cost.TotalCost)
			}
			input.Provider = "provider-c"
			cost, err := billing.CalculateCostUnified(input)
			require.ErrorIs(t, err, ErrModelPricingUnavailable, "the unified entry point must preserve the declared provider")
			require.Zero(t, cost.TotalCost)

			input.Resolved = resolver.Resolve(input.Ctx, PricingInput{
				Provider: "provider-a", Model: input.Model, PricingAt: input.PricingAt,
			})
			cost, err = billing.CalculateCostUnified(input)
			require.ErrorIs(t, err, ErrModelPricingUnavailable, "a same-time foreign pre-resolved card must be resolved for the actual provider")
			require.Zero(t, cost.TotalCost)
			input.Resolved = nil

			pinnedContext, pinnedAt := WithGatewayTokenRequestPricing(context.Background())
			pinnedInput := input
			pinnedInput.Ctx, pinnedInput.PricingAt = pinnedContext, pinnedAt
			pinnedInput.Resolved = resolver.Resolve(pinnedContext, PricingInput{
				Provider: "provider-a", Model: input.Model, PricingAt: pinnedAt,
			})
			cost, err = billing.CalculateCostUnified(pinnedInput)
			require.ErrorIs(t, err, ErrSalePriceChanged, "a frozen request cannot reuse another provider's same-time card")
			require.Zero(t, cost.TotalCost)

			legacyInput := input
			legacyInput.Mode, legacyInput.Resolver = "", nil
			cost, err = billing.CalculateCostUnified(legacyInput)
			require.ErrorIs(t, err, ErrModelPricingUnavailable, "omitting a resolver must not erase the declared provider")
			require.Zero(t, cost.TotalCost)

			inputRate, outputRate := 9.0, 10.0
			group := &Group{ID: 4, Platform: "provider-c", ModelPricing: []ChannelModelPricing{
				{Models: []string{"same"}, InputPrice: &inputRate, OutputPrice: &outputRate},
			}}
			input.Group, input.GroupID = group, &group.ID
			cost, err = billing.CalculateCostUnified(input)
			require.NoError(t, err, "a complete operator card needs no foreign baseline")
			require.Equal(t, float64(19), cost.TotalCost)
			resolved := resolver.Resolve(input.Ctx, PricingInput{Provider: input.Provider, Model: input.Model, Group: group, GroupID: &group.ID})
			require.Equal(t, PricingSourceGroup, resolved.BasePricing.FieldSources["input_cost_per_token"])
			require.Equal(t, PricingSourceGroup, resolved.BasePricing.FieldSources["output_cost_per_token"])

			group.ModelPricing[0].OutputPrice = nil
			cost, err = billing.CalculateCostUnified(input)
			require.ErrorIs(t, err, ErrModelPricingUnavailable, "a partial operator card must not borrow provider-a's output rate")
			require.Zero(t, cost.TotalCost)
			resolved = resolver.Resolve(input.Ctx, PricingInput{Provider: input.Provider, Model: input.Model, Group: group, GroupID: &group.ID})
			require.Equal(t, inputRate, resolved.BasePricing.InputPricePerToken)
			require.Zero(t, resolved.BasePricing.OutputPricePerToken)
			require.NotContains(t, resolved.BasePricing.FieldSources, "output_cost_per_token")
		})
	}
}

func TestUnifiedPricingQualifiedInputCannotOverrideProvider(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		name := "direct_fixture"
		if indexed {
			name = "published_index"
		}
		t.Run(name, func(t *testing.T) {
			prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				"provider-a/same":           {InputCostPerToken: 1, OutputCostPerToken: 2, sourceOrigin: PricingSourceLiteLLM},
				"provider-a/dated-20260101": {InputCostPerToken: 3, OutputCostPerToken: 4, sourceOrigin: PricingSourceLiteLLM},
			}}
			if indexed {
				prices.rebuildBaseNameIndex()
			}
			billing := NewBillingService(nil, prices)
			resolver := NewModelPricingResolver(nil, billing)
			for _, model := range []string{"provider-a/same", "provider-a/dated"} {
				_, err := prices.BoundPricingModel("provider-c", model)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				bound, err := prices.BoundPricingModel("provider-a", model)
				require.NoError(t, err)
				require.Contains(t, bound, "provider-a/")
				_, err = prices.BoundPricingModel("", model)
				require.NoError(t, err, "an unscoped unique qualified identity stays compatible")
				cost, err := billing.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Mode: BillingModeToken, Provider: "provider-c", Model: model,
					Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1, Resolver: resolver,
				})
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Zero(t, cost.TotalCost)
			}

			// A provider's native model may contain '/'. Its own explicit
			// namespace remains authoritative, ahead of a foreign exact key.
			updated := maps.Clone(prices.pricingData)
			updated["provider-c/provider-a/same"] = &LiteLLMModelPricing{
				InputCostPerToken: 9, OutputCostPerToken: 10, sourceOrigin: PricingSourceLiteLLM,
			}
			updated["provider-c/provider-a/dated-20260201"] = &LiteLLMModelPricing{
				InputCostPerToken: 11, OutputCostPerToken: 12, sourceOrigin: PricingSourceLiteLLM,
			}
			if indexed {
				prices.replacePricingData(updated)
			} else {
				prices.pricingData = updated
			}
			for _, test := range []struct {
				model string
				key   string
				total float64
			}{
				{"provider-a/same", "provider-c/provider-a/same", 19},
				{"provider-a/dated", "provider-c/provider-a/dated-20260201", 23},
			} {
				bound, err := prices.BoundPricingModel("provider-c", test.model)
				require.NoError(t, err)
				require.Equal(t, test.key, bound)
				cost, err := billing.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Mode: BillingModeToken, Provider: "provider-c", Model: test.model,
					Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1, Resolver: resolver,
				})
				require.NoError(t, err)
				require.Equal(t, test.total, cost.TotalCost)
			}
		})
	}
}

func TestUnifiedPricingProtocolResourceAliasesKeepProviderBinding(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		name := "direct_fixture"
		if indexed {
			name = "published_index"
		}
		t.Run(name, func(t *testing.T) {
			prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				"gemini-2.5-pro":  {LiteLLMProvider: PlatformGemini, InputCostPerToken: 0.01, OutputCostPerToken: 0.02, sourceOrigin: PricingSourceLiteLLM},
				"provider-a/same": {InputCostPerToken: 1, OutputCostPerToken: 2, sourceOrigin: PricingSourceLiteLLM},
			}}
			if indexed {
				prices.rebuildBaseNameIndex()
			}
			billing := NewBillingService(nil, prices)
			resolver := NewModelPricingResolver(nil, billing)
			for _, model := range []string{
				"models/gemini-2.5-pro",
				"publishers/google/models/gemini-2.5-pro",
				"projects/project-one/locations/us-central1/publishers/google/models/gemini-2.5-pro",
			} {
				bound, err := prices.BoundPricingModel(PlatformGemini, model)
				require.NoError(t, err)
				require.Equal(t, "gemini-2.5-pro", bound)
				cost, err := billing.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Mode: BillingModeToken, Provider: PlatformGemini, Model: model,
					Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1, Resolver: resolver,
				})
				require.NoError(t, err)
				require.InDelta(t, 0.03, cost.TotalCost, 1e-12)
			}
			for _, model := range []string{
				"models/same",
				"publishers/google/models/same",
				"projects/project-one/locations/us-central1/publishers/google/models/same",
				"models/provider-a/same",
				"publishers/google/models/provider-a/same",
				"publishers/provider-a/models/same",
				"projects/project-one/locations/us-central1/publishers/provider-a/models/same",
				"projects/project-one/locations/us-central1/publishers/google/models/provider-a/same",
			} {
				_, err := prices.BoundPricingModel("provider-c", model)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				cost, err := billing.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Mode: BillingModeToken, Provider: "provider-c", Model: model,
					Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1, Resolver: resolver,
				})
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Zero(t, cost.TotalCost)
			}

			updated := maps.Clone(prices.pricingData)
			updated[PlatformGemini+"/models/gemini-2.5-pro"] = &LiteLLMModelPricing{
				InputCostPerToken: 0.04, OutputCostPerToken: 0.06, sourceOrigin: PricingSourceLiteLLM,
			}
			if indexed {
				prices.replacePricingData(updated)
			} else {
				prices.pricingData = updated
			}
			bound, err := prices.BoundPricingModel(PlatformGemini, "models/gemini-2.5-pro")
			require.NoError(t, err)
			require.Equal(t, PlatformGemini+"/models/gemini-2.5-pro", bound, "the provider's explicit native key still takes precedence")
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Mode: BillingModeToken, Provider: PlatformGemini, Model: "models/gemini-2.5-pro",
				Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1, Resolver: resolver,
			})
			require.NoError(t, err)
			require.InDelta(t, 0.1, cost.TotalCost, 1e-12)
		})
	}
}

func TestUnifiedPricingFamilyFallbackNeverBorrowsQualifiedTariff(t *testing.T) {
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"provider-a/claude-opus-4-8": {InputCostPerToken: 1, OutputCostPerToken: 2, sourceOrigin: PricingSourceLiteLLM},
	}}
	prices.rebuildBaseNameIndex()
	const model = "claude-opus-4-8-custom"
	require.Nil(t, prices.GetModelPricing(model), "a family guess must not select a provider-qualified tariff")
	billing := NewBillingService(nil, prices)
	resolved := NewModelPricingResolver(nil, billing).Resolve(context.Background(), PricingInput{
		Provider: "provider-c", Model: model,
	})
	require.NoError(t, resolved.pricingErr)
	require.NotNil(t, resolved.BasePricing)
	require.Equal(t, PricingSourceFallback, resolved.Source)
	legacy, err := NewBillingService(nil, nil).GetModelPricing(model)
	require.NoError(t, err, "the existing hardcoded legacy policy remains available")
	require.Equal(t, legacy.InputPricePerToken, resolved.BasePricing.InputPricePerToken)
	require.Equal(t, legacy.OutputPricePerToken, resolved.BasePricing.OutputPricePerToken)
	require.NotEqual(t, float64(1), resolved.BasePricing.InputPricePerToken)
	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Mode: BillingModeToken, Provider: "provider-c", Model: model,
		Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}, RateMultiplier: 1,
		Resolver: NewModelPricingResolver(nil, billing),
	})
	require.NoError(t, err)
	require.Equal(t, PricingSourceFallback, cost.PriceSource)
	require.InDelta(t, legacy.InputPricePerToken+legacy.OutputPricePerToken, cost.TotalCost, 1e-12)
}

func TestUnifiedPricingAmbiguousDateAndFamilyNeverUsesMapOrder(t *testing.T) {
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"same-20260101": {InputCostPerToken: 1}, "same-20260201": {InputCostPerToken: 2},
		"vendor-a/claude-opus-4-8": {InputCostPerToken: 3}, "vendor-b/claude-opus-4-8": {InputCostPerToken: 4},
	}}
	for range 100 {
		_, err := prices.BoundPricingModel("", "same")
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Nil(t, prices.GetModelPricing("same"))
		require.Nil(t, prices.matchByModelFamily("claude-opus-4-8-custom"))
	}
}

func TestUnifiedPricingReportsActualFieldOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"p/model":{"input_cost_per_token":0.5}}`), 0600))
	cfg := &config.Config{}
	cfg.Pricing.OverrideFile = path
	prices := &PricingService{cfg: cfg}
	parsed, err := prices.parsePricingData([]byte(`{"p/model":{"input_cost_per_token":1,"output_cost_per_token":2}}`))
	require.NoError(t, err)
	price := parsed["p/model"]
	require.Equal(t, "mixed_sale", price.sourceOrigin)
	require.Equal(t, "operator_override", price.fieldSources["input_cost_per_token"])
	require.Equal(t, PricingSourceLiteLLM, price.fieldSources["output_cost_per_token"])
	prices.pricingData = parsed
	billing := &BillingService{pricingService: prices}
	resolved := NewModelPricingResolver(nil, billing).Resolve(context.Background(), PricingInput{Provider: "p", Model: "model"})
	require.Equal(t, "mixed_sale", resolved.Source)
	require.Equal(t, price.fieldSources, resolved.BasePricing.FieldSources)

	static := &BillingService{pricingService: &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}}
	fallback, err := static.GetModelPricing("gpt-5.4-mini")
	require.NoError(t, err)
	require.Equal(t, PricingSourceFallback, fallback.Source)
	require.Equal(t, PricingSourceFallback, fallback.FieldSources["input_cost_per_token"])

	hardcoded := NewBillingService(nil, nil)
	fallback, err = hardcoded.GetModelPricing("composer-2.5-fast")
	require.NoError(t, err)
	require.Equal(t, 3e-6, fallback.InputPricePerToken)
	require.Equal(t, 15e-6, fallback.OutputPricePerToken)
	require.Equal(t, 0.5e-6, fallback.CacheReadPricePerToken)
	require.Equal(t, PricingSourceFallback, fallback.Source)
	require.Equal(t, PricingSourceFallback, fallback.FieldSources["input_cost_per_token"])
	require.Equal(t, PricingSourceFallback, fallback.FieldSources["output_cost_per_token"])
}

func TestUnifiedPricingVersionIgnoresAdmissionInstantButSnapshotRetainsIt(t *testing.T) {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	pc := pricing.PriceContext{Provider: "p", Model: "same", Funding: "wallet", PricingAt: at}
	resolved := &ResolvedPricing{Context: &pc, Source: PricingSourceGroup, BasePricing: &ModelPricing{InputPricePerToken: 1, OutputPricePerToken: 2}}
	_, before := pricingAuditIdentity(resolved)
	pc.PricingAt = at.Add(time.Minute)
	_, after := pricingAuditIdentity(resolved)
	require.NotEmpty(t, before)
	require.Equal(t, before, after)
	resolved.BasePricing.OutputPricePerToken = 3
	_, changed := pricingAuditIdentity(resolved)
	require.NotEqual(t, before, changed)
	for _, source := range []string{"pi-ai", "pi_ai", "community_reference"} {
		cost := &CostBreakdown{TotalCost: 1, ActualCost: 1}
		err := freezePricing(cost, PricingSnapshot{Source: source, PriceContext: &pc})
		require.ErrorIs(t, err, pricing.ErrReferencePrice)
		require.Empty(t, cost.PricingState, "a rejected reference must not be marked billable")
	}
}
