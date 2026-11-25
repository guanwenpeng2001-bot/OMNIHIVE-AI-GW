//go:build unit

package legacy

import (
	"context"
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
	require.Equal(t, "wire-model", binding.WireModel)
}

func TestPriceBookCatalogConflictUsesEffectiveMonetaryTerms(t *testing.T) {
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	port := PriceBookPort{PlatformPriceBook: service.NewPlatformPriceBook(service.NewModelPricingResolver(nil, service.NewBillingService(nil, nil)))}
	ctx := context.Background()
	for _, test := range []struct {
		name, model string
		input       float64
		conflict    bool
	}{
		{name: "long context flag without tier", model: "gpt-5.5", input: 1e-6},
		{name: "applicable long context tier", model: "gpt-6-astra", input: 10e-6, conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, output := test.input, 2e-6
			group := func(id, channelID int64, longContext bool, rate float64) *service.Group {
				return &service.Group{ID: id, Platform: service.PlatformOpenAI, RateMultiplier: rate, LongContextPricingEnabled: longContext,
					ModelPricing: []service.ChannelModelPricing{{ID: id * 10, ChannelID: channelID, Models: []string{test.model}, InputPrice: &input, OutputPrice: &output}}}
			}
			price := func(group *service.Group) catalog.Price {
				binding := catalog.ModelBinding{BillingGroupID: group.ID, RateMultiplier: group.RateMultiplier,
					BillingIdentity: catalog.NewModelIdentity(service.PlatformOpenAI, test.model)}
				result, err := port.DescribeForGroup(ctx, catalog.PriceInput{Binding: binding, Multiplier: group.RateMultiplier, At: at}, group)
				require.NoError(t, err)
				return result
			}
			billingState := func(first, second catalog.Price) string {
				projected := catalog.Project(catalog.Perspective{}, catalog.Input{Models: []catalog.Candidate{{
					Provider: service.PlatformOpenAI, Model: test.model,
					Offers: []catalog.Offer{{Eligible: true, Published: true, Price: first}, {Eligible: true, Published: true, Price: second}},
				}}}, catalog.Window{}, at)
				return projected.Details[0].Model.Billing.State
			}

			withoutLongContext := group(1, 7, false, 1)
			withLongContext := group(2, 2, true, 1)
			first, second := price(withoutLongContext), price(withLongContext)
			require.NotEqual(t, *first.Version, *second.Version)
			if test.conflict {
				require.NotEqual(t, first.Semantics, second.Semantics)
				require.Equal(t, "conflict", billingState(first, second))
			} else {
				require.Equal(t, first.Semantics, second.Semantics)
				require.Equal(t, "priced", billingState(first, second))
			}
			require.Equal(t, "conflict", billingState(first, price(group(3, 3, false, 1.2))))
		})
	}
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
