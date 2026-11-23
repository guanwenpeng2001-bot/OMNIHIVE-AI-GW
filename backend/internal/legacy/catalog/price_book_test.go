//go:build unit

package legacy

import (
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCatalogSaleBindingCarriesAuthorizedProviderAndWireModel(t *testing.T) {
	identity := catalog.NewModelIdentity("bedrock", "public-alias")
	offer := service.PlazaCatalogOffer{BillingModelSource: service.BillingModelSourceChannelMapped, ChannelMappedModel: "channel-alias"}
	account := service.PlazaCatalogBinding{}
	account.Identity.Platform = service.PlatformAnthropic
	account.UpstreamModel = "wire-model"

	binding := saleBinding(catalog.Perspective{}, bindingFor(identity, 3, offer, account))
	require.Equal(t, service.PlatformAnthropic, binding.Provider)
	require.Equal(t, "channel-alias", binding.Model)
	require.Equal(t, "wire-model", binding.WireModel)

	offer.BillingModelSource = service.BillingModelSourceResponse
	binding = saleBinding(catalog.Perspective{}, bindingFor(identity, 3, offer, account))
	require.Empty(t, binding.Model)
	require.Empty(t, binding.WireModel)
}

func TestPriceBookDisplayUsesEffectiveRatesAndExactVersion(t *testing.T) {
	input, err := money.NewRate("USD", "1.25", "input_token", "1000000")
	require.NoError(t, err)
	output, err := money.NewRate("USD", "3", "output_token", "1000000")
	require.NoError(t, err)
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	d := pricing.Description{
		Outcome: pricing.Conditional, Source: "channel", VersionID: "tariff-1", TermsID: "terms",
		EffectiveFrom: at, RateMultiplier: 0.4,
		Rates: []pricing.RateFact{{Kind: "input", Rate: input}, {Kind: "output", Rate: output}},
	}
	display, err := displayDescription(d)
	require.NoError(t, err)
	require.Equal(t, "conditional", display.State)
	require.Equal(t, d.VersionID, *display.Version)
	require.Equal(t, d.TermsID, display.Semantics)
	require.Equal(t, at, *display.ValidFrom)
	require.Equal(t, "0.5", display.Input.Amount().String())
	require.Equal(t, "1.2", display.Output.Amount().String())
	require.Equal(t, "1000000", display.Input.Per().String())

	d.Outcome, d.RateMultiplier = pricing.Available, 0
	free, err := displayDescription(d)
	require.NoError(t, err)
	require.Equal(t, "free", free.State)
	require.Equal(t, "0", free.Input.Amount().String())
}
