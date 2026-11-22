package legacy

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSaleProjectionKeepsFieldOriginAndMissingDimensions(t *testing.T) {
	input, output, tier := 1e-6, 2e-6, 3e-6
	model := service.PlazaModel{PriceSource: service.PricingSourceGroup, PriceRevision: "sha256:fixture", PriceProvenance: map[string]string{"input_cost_per_token": "group", "output_cost_per_token": "fallback"}, Pricing: &service.ChannelModelPricing{InputPrice: &input, OutputPrice: &output}}
	price := priceFor(model, service.PlazaGroup{RateMultiplier: 1}, nil, true)
	require.Equal(t, "sale", price.Kind)
	require.Equal(t, "group", price.Provenance["input_rate"].SourceKind)
	require.Equal(t, "fallback", price.Provenance["output_rate"].SourceKind)
	require.Equal(t, "sha256:fixture", price.Provenance["input_rate"].SourceRevision)
	model.Pricing.OutputPrice = nil
	model.Pricing.Intervals = []service.PricingInterval{{MinTokens: 100, InputPrice: &tier}}
	price = priceFor(model, service.PlazaGroup{RateMultiplier: 1}, nil, true)
	require.Equal(t, "partial_rate", price.BillingState, "a time/tier rule cannot conceal a missing output tariff")
	model.PriceSource = "pi-ai"
	price = priceFor(model, service.PlazaGroup{RateMultiplier: 1}, nil, true)
	require.Equal(t, "unpriced", price.State)
	require.Nil(t, price.Input)
	require.Nil(t, price.Output)
	require.Empty(t, price.Additional)
}
