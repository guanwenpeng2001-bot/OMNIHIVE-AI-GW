//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCompactRetryObservationBillingProtection(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, scenario := range []struct {
			name                                                string
			source                                              string
			conflict, missing, channelBaseline, channelResponse bool
			responsePrice                                       float64
			adopt                                               bool
			declaration                                         string
		}{
			{name: "opt_in_cheaper", source: BillingModelSourceResponse, adopt: true},
			{name: "not_opted_in", source: BillingModelSourceChannelMapped},
			{name: "final_attempt_conflict", source: BillingModelSourceResponse, conflict: true},
			{name: "missing_response", source: BillingModelSourceResponse, missing: true},
			{name: "distinct_unpriced_declaration", source: BillingModelSourceResponse, declaration: "runtime-model-unpriced"},
			{name: "channel_baseline_cannot_use_global_response", source: BillingModelSourceResponse, channelBaseline: true},
			{name: "channel_zero_rejected", source: BillingModelSourceResponse, channelBaseline: true, channelResponse: true},
			{name: "channel_pricier_rejected", source: BillingModelSourceResponse, channelBaseline: true, channelResponse: true, responsePrice: 20e-6},
			{name: "channel_cheaper_adopted", source: BillingModelSourceResponse, channelBaseline: true, channelResponse: true, responsePrice: 2e-6, adopt: true},
		} {
			t.Run("passthrough="+strconv.FormatBool(passthrough)+"/"+scenario.name, func(t *testing.T) {
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}
				cheaper, pricier, cheapCost, baselineCost := orderedResponseBillingModels(t, svc.billingService, tokens, openAICheapFixtureModel, openAIPriceyFixtureModel)
				created, declared := cheaper, cheaper
				if scenario.declaration != "" {
					created, declared = scenario.declaration, scenario.declaration
				}
				if scenario.conflict {
					created = pricier
				}
				if scenario.missing {
					created, declared = "", ""
				}
				result := forwardCompactObservationRetry(t, passthrough, true, false, pricier, cheaper, created, declared)
				require.Equal(t, scenario.conflict, result.UpstreamResponseModelConflict)
				apiKey := &APIKey{ID: 10}
				want := baselineCost.ActualCost
				if scenario.adopt {
					want = cheapCost.ActualCost
				}
				if scenario.channelBaseline {
					groupID := int64(9)
					apiKey.GroupID = &groupID
					apiKey.Group = &Group{ID: groupID, RateMultiplier: 1.1}
					baselinePrice := 10e-6
					cache := newEmptyChannelCache()
					cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: pricier}] = &ChannelModelPricing{BillingMode: BillingModeToken, InputPrice: &baselinePrice, OutputPrice: &baselinePrice}
					if scenario.channelResponse {
						price := scenario.responsePrice
						cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: cheaper}] = &ChannelModelPricing{BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price}
					}
					cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
					cache.groupPlatform[groupID] = ""
					cache.loadedAt = time.Now()
					channels := &ChannelService{}
					channels.cache.Store(cache)
					svc.resolver = NewModelPricingResolver(channels, NewBillingService(&config.Config{}, nil))
					want = 30 * baselinePrice * 1.1
					if scenario.adopt {
						want = 30 * scenario.responsePrice * 1.1
					}
				}
				err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
					Result: result, APIKey: apiKey, User: &User{ID: 20}, Account: &Account{ID: 30},
					ChannelUsageFields: ChannelUsageFields{ChannelID: 9, OriginalModel: pricier, ChannelMappedModel: pricier, BillingModelSource: scenario.source},
				})
				require.NoError(t, err)
				log := usageRepo.lastLog
				require.NotNil(t, log)
				require.Equal(t, pricier, log.RequestedModel)
				require.NotNil(t, log.UpstreamModel)
				require.Equal(t, cheaper, *log.UpstreamModel)
				if scenario.missing {
					require.Nil(t, log.UpstreamResponseModel)
					require.Nil(t, log.UpstreamModelMismatch)
				} else {
					require.Equal(t, declared, *log.UpstreamResponseModel)
					require.NotNil(t, log.UpstreamModelMismatch)
					require.Equal(t, declared != cheaper, *log.UpstreamModelMismatch)
				}
				require.Greater(t, log.ActualCost, 0.0)
				require.InDelta(t, want, log.ActualCost, 1e-12)
				require.InDelta(t, want, userRepo.lastAmount, 1e-12)
			})
		}
	}
}
