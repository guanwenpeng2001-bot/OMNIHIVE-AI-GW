//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperatorCardMissingCatalogPrice(testingContext *testing.T) {
	inputPrice, outputPrice, freePrice, invalidPrice := 2e-6, 6e-6, 0.0, -1.0
	for _, scenario := range []struct {
		name  string
		card  *ChannelModelPricing
		state PricingState
	}{
		{"absent", nil, PricingStateUnpriced},
		{"empty", &ChannelModelPricing{}, PricingStateUnpriced},
		{"partial", &ChannelModelPricing{InputPrice: &inputPrice}, PricingStateUnpriced},
		{"paid", &ChannelModelPricing{InputPrice: &inputPrice, OutputPrice: &outputPrice}, PricingStatePriced},
		{"explicit free", &ChannelModelPricing{InputPrice: &freePrice, OutputPrice: &freePrice}, PricingStateFree},
		{"invalid", &ChannelModelPricing{InputPrice: &invalidPrice, OutputPrice: &outputPrice}, PricingStateError},
	} {
		testingContext.Run(scenario.name, func(testingContext *testing.T) {
			billing := newTestBillingServiceForResolver()
			group := &Group{Platform: PlatformOpenAI}
			if scenario.card != nil {
				card := scenario.card.Clone()
				card.Models = []string{"operator-only-model"}
				group.ModelPricing = []ChannelModelPricing{card}
			}
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: "operator-only-model", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 100}, RateMultiplier: 1, Resolver: NewModelPricingResolver(nil, billing)})
			require.Equal(testingContext, scenario.state, cost.PricingState)
			switch scenario.state {
			case PricingStateUnpriced:
				require.ErrorIs(testingContext, err, ErrModelPricingUnavailable)
				require.Empty(testingContext, cost.PricingSnapshot)
			case PricingStateError:
				require.ErrorIs(testingContext, err, ErrInvalidBillingAmount)
			default:
				require.NoError(testingContext, err)
				require.Equal(testingContext, PricingSourceGroup, cost.PriceSource)
				require.Contains(testingContext, cost.PricingSnapshot, `"source":"group"`)
				if scenario.state == PricingStatePriced {
					require.InDelta(testingContext, .0026, cost.ActualCost, 1e-12)
				}
			}
		})
	}
}

func TestOperatorIntervalRequiresExplicitMissingCatalogRates(testingContext *testing.T) {
	billing := newTestBillingServiceForResolver()
	resolver := NewModelPricingResolver(nil, billing)
	price := 2e-6
	for _, complete := range []bool{false, true} {
		interval := PricingInterval{InputPrice: &price}
		if complete {
			interval.OutputPrice = &price
		}
		resolved := resolver.resolveConfiguredPricing(&ChannelModelPricing{Intervals: []PricingInterval{interval}}, "operator-only-model", PricingSourceChannel)
		pricing := resolver.GetIntervalPricing(resolved, 100)
		if complete {
			require.NotNil(testingContext, pricing)
		} else {
			require.Nil(testingContext, pricing)
		}
	}
}

func TestOperatorChannelCardPriority(testingContext *testing.T) {
	inputPrice, outputPrice := 2e-6, 6e-6
	groupID := int64(717)
	for _, model := range []string{"operator-only-model", "claude-sonnet-4"} {
		billing := newTestBillingServiceForResolver()
		channel := newChannelServiceWithPricings(groupID, []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{model}, InputPrice: &inputPrice, OutputPrice: &outputPrice}})
		resolver := NewModelPricingResolver(channel, billing)
		request := TokenCostRequest{Ctx: context.Background(), Model: model, Group: &Group{ID: groupID, Platform: PlatformOpenAI}, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 100}, RateMultiplier: 1, Resolver: resolver}
		cost, err := billing.CalculateTokenCostForRequest(request)
		require.NoError(testingContext, err)
		require.Equal(testingContext, PricingSourceChannel, cost.PriceSource)
		require.InDelta(testingContext, .0026, cost.ActualCost, 1e-12)
		request.Group.ModelPricing = []ChannelModelPricing{{Models: []string{model}, InputPrice: &outputPrice, OutputPrice: &outputPrice}}
		cost, err = billing.CalculateTokenCostForRequest(request)
		require.NoError(testingContext, err)
		require.Equal(testingContext, PricingSourceGroup, cost.PriceSource)
		require.InDelta(testingContext, .0066, cost.ActualCost, 1e-12)
	}
}
