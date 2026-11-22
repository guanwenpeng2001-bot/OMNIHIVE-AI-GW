//go:build unit

package service

import (
	"context"
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

	hardcoded := &BillingService{fallbackPrices: map[string]*ModelPricing{"exact": {InputPricePerToken: 1, OutputPricePerToken: 2}}}
	fallback, err = hardcoded.GetModelPricing("exact")
	require.NoError(t, err)
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
