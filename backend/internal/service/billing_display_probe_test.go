//go:build unit

package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBillingDisplayProbePreservesSettlementCalculations(tester *testing.T) {
	for _, scenario := range scheduleScenarios() {
		if scenario.wantErr || scenario.wantNil {
			continue
		}
		tester.Run(scenario.name, func(tester *testing.T) {
			billing, resolver := newScheduleTestEnv(tester, scenario)
			request := TokenCostRequest{
				Ctx:   WithResolvedTargetPlatform(context.Background(), scenario.platform),
				Model: scenario.model, Group: scenario.group, Resolver: resolver,
				RateMultiplier: 0.8, PricingAt: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC),
			}
			for _, tokens := range []UsageTokens{{}, {InputTokens: 1000, OutputTokens: 500}, {InputTokens: 300000, OutputTokens: 1000, CacheReadTokens: 10000, CacheCreationTokens: 2000}} {
				request.Tokens = tokens
				request.displayProbe = false
				settlement, err := billing.CalculateTokenCostForRequest(request)
				require.NoError(tester, err)
				require.NotEmpty(tester, settlement.PricingSnapshot)
				request.displayProbe = true
				display, err := billing.CalculateTokenCostForRequest(request)
				require.NoError(tester, err)
				if scenario.group != nil {
					require.Empty(tester, display.PricingSnapshot)
					require.Empty(tester, display.PriceCardVersion)
				}
				// Only audit data is unused by the display; every cost and state must match.
				display.PricingSnapshot = settlement.PricingSnapshot
				display.PriceCardVersion = settlement.PriceCardVersion
				display.PriceSource = settlement.PriceSource
				require.Equal(tester, settlement, display)
			}
		})
	}
}

func TestBillingDisplayProbePreservesValidation(tester *testing.T) {
	scenario := scheduleScenario{model: "claude-sonnet-4", platform: PlatformAnthropic, group: enabledGroup(PlatformAnthropic), channel: sonnetChannel()}
	billing, resolver := newScheduleTestEnv(tester, scenario)
	for _, multiplier := range []float64{-1, math.NaN(), math.Inf(1), 1e20} {
		for _, displayProbe := range []bool{false, true} {
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: scenario.model, Group: scenario.group, Resolver: resolver,
				Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: multiplier, displayProbe: displayProbe,
			})
			require.ErrorIs(tester, err, ErrInvalidBillingAmount)
			require.Equal(tester, PricingStateError, cost.PricingState)
		}
	}
	for _, displayProbe := range []bool{false, true} {
		cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: context.Background(), Model: scenario.model, Group: scenario.group, Resolver: resolver,
			Tokens: UsageTokens{InputTokens: -1}, RateMultiplier: 1, displayProbe: displayProbe,
		})
		require.ErrorIs(tester, err, ErrInvalidBillingAmount)
		require.Equal(tester, PricingStateError, cost.PricingState)
		// Invalid unused rates must still fail the freeze-time price-card validation.
		pricing := &ModelPricing{OutputPricePerToken: math.NaN()}
		require.ErrorIs(tester, freezePricing(&CostBreakdown{}, PricingSnapshot{TokenPrices: pricing, displayProbe: displayProbe}), ErrInvalidBillingAmount)
	}
}
