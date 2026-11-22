package legacy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func TestPublicPlazaPriceUsesStoredPerTokenAmounts(t *testing.T) {
	input, output, cache, tierInput, request := 0.000008, 0.00004, 0.0000004, 0.000012, 0.25
	price := priceFor(service.PlazaModel{PriceSource: service.PricingSourceChannel, Pricing: &service.ChannelModelPricing{
		BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output,
		CacheReadPrice: &cache, PerRequestPrice: &request,
		Intervals: []service.PricingInterval{{MinTokens: 128000, InputPrice: &tierInput}},
	}}, service.PlazaGroup{RateMultiplier: 1}, nil, true)
	require.Equal(t, service.PricingSourceChannel, price.Source)
	require.Equal(t, "sale", price.Kind)
	require.Len(t, price.Additional, 3)
	for _, tc := range []struct {
		rate        *money.Rate
		units, cost string
	}{
		{price.Input, "1000000", "8.00000000"},
		{price.Output, "1000000", "40.00000000"},
		{&price.Additional[0].Rate, "1000000", "0.40000000"},
		{&price.Additional[1].Rate, "1", "0.25000000"},
		{&price.Additional[2].Rate, "1000000", "12.00000000"},
	} {
		require.NotNil(t, tc.rate)
		require.Equal(t, "1", tc.rate.Per().String())
		q, err := money.NewQuantity(tc.rate.Unit(), tc.units)
		require.NoError(t, err)
		cost, err := tc.rate.Cost(q)
		require.NoError(t, err)
		charge, err := cost.RoundCharge()
		require.NoError(t, err)
		require.Equal(t, tc.cost, charge.String())
	}
}

func TestLegacyRateKeepsDeclaredPerUnitScale(t *testing.T) {
	one, err := money.Parse("1")
	require.NoError(t, err)

	rate, err := legacyRate(1.25, money.Unit("input_token"), "1000000", one)
	require.NoError(t, err)

	million, err := money.NewQuantity("input_token", "1000000")
	require.NoError(t, err)
	charged, err := rate.Cost(million)
	require.NoError(t, err)
	settled, err := charged.RoundCharge()
	require.NoError(t, err)
	require.Equal(t, "1.25000000", settled.String())

	single, err := money.NewQuantity("input_token", "1")
	require.NoError(t, err)
	perToken, err := rate.Cost(single)
	require.NoError(t, err)
	require.Equal(t, "0.00000125", trimDiagnostic(perToken.Diagnostic()))
}

func TestLegacyRateAppliesMultiplierOnce(t *testing.T) {
	onePointFive, err := money.Parse("1.5")
	require.NoError(t, err)

	rate, err := legacyRate(2, money.Unit("output_token"), "1000000", onePointFive)
	require.NoError(t, err)

	million, err := money.NewQuantity("output_token", "1000000")
	require.NoError(t, err)
	charged, err := rate.Cost(million)
	require.NoError(t, err)
	settled, err := charged.RoundCharge()
	require.NoError(t, err)
	require.Equal(t, "3.00000000", settled.String())
}

func trimDiagnostic(value string) string {
	for len(value) > 0 && value[len(value)-1] == '0' {
		value = value[:len(value)-1]
	}
	if len(value) > 0 && value[len(value)-1] == '.' {
		value = value[:len(value)-1]
	}
	return value
}
