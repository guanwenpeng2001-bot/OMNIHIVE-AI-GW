package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSalePricingBundlePinsRouteCaptureAndSettlement(t *testing.T) {
	oldInput, oldOutput, groupOutput, personal := 0.01, 0.02, 0.02, 0.5
	group := &Group{ID: 1, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"native"}, OutputPrice: &groupOutput}}}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"native": {InputCostPerToken: oldInput, OutputCostPerToken: oldOutput}}}
	versions := &frozenChannelVersions{generation: 1}
	channelPrice := 0.03
	repository := &catalogChannelFixture{channels: []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"channel-native"}, InputPrice: &channelPrice, OutputPrice: &channelPrice}}}}}
	channels := &ChannelService{repo: repository, groupRepo: versions}
	rates := &userGroupRateResolverRepoStub{rate: &personal}
	billing := &BillingService{pricingService: prices}
	gateway := &GatewayService{billingService: billing, channelService: channels, userGroupRateRepo: rates}
	key := &APIKey{ID: 2, WorkspaceID: 1, UserID: 3, Group: group}
	at := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	ctx, captured, err := gateway.CaptureSalePricing(context.Background(), key, at)
	require.NoError(t, err)
	require.Nil(t, GatewayRequestPlanFromContext(ctx), "a pricing snapshot is not an admission")
	capturedChannel := channels.GetChannelModelPricing(ctx, group.ID, "channel-native")
	require.NotNil(t, capturedChannel)
	require.NotNil(t, capturedChannel.InputPrice)
	require.Equal(t, channelPrice, *capturedChannel.InputPrice)
	resolver := NewModelPricingResolver(channels, billing)
	quoted := resolver.Resolve(ctx, PricingInput{Provider: PlatformOpenAI, Model: "native", Group: captured.Group, GroupID: &captured.Group.ID})
	_, contentVersion := pricingAuditIdentity(quoted)
	require.NotEmpty(t, contentVersion)
	ctx, err = ConfirmSalePricing(ctx, PlatformOpenAI, "native", contentVersion)
	require.NoError(t, err)

	// Deterministic route/capture interleaving. No timing-dependent sleeps: every
	// source is refreshed after the selected quote and before request capture.
	refresh, refreshed := make(chan struct{}), make(chan struct{})
	go func() {
		<-refresh
		prices.mu.Lock()
		prices.pricingData = map[string]*LiteLLMModelPricing{"native": {InputCostPerToken: 20, OutputCostPerToken: 30}}
		prices.mu.Unlock()
		groupOutput = 40
		personal = 9
		fresh := 50.0
		repository.channels = []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"channel-native"}, InputPrice: &fresh, OutputPrice: &fresh}}}}
		versions.generation++
		close(refreshed)
	}()
	close(refresh)
	<-refreshed
	spec := GatewayRequestSpec{WorkspaceID: 1, UserID: key.UserID, APIKeyID: key.ID, BillingGroupID: group.ID, Model: "native", PriceVersion: contentVersion}
	ctx, plan := captureGatewayRequestPlan(ctx, spec, nil, billing, channels)
	require.NoError(t, plan.admissionErr)
	require.Equal(t, contentVersion, plan.Snapshot().PriceVersion)
	require.Equal(t, at, plan.Snapshot().PricingAt)
	actual := resolver.Resolve(ctx, PricingInput{Provider: PlatformOpenAI, Model: "native", Group: captured.Group, GroupID: &captured.Group.ID})
	_, actualVersion := pricingAuditIdentity(actual)
	require.Equal(t, contentVersion, actualVersion, "a captured version must identify the rates actually charged")
	require.Equal(t, oldInput, actual.BasePricing.InputPricePerToken)
	require.Equal(t, oldOutput, actual.BasePricing.OutputPricePerToken)
	actualChannel := channels.GetChannelModelPricing(ctx, group.ID, "channel-native")
	require.NotNil(t, actualChannel)
	require.NotNil(t, actualChannel.InputPrice)
	require.Equal(t, channelPrice, *actualChannel.InputPrice)
	require.Equal(t, 0.5, gateway.ResolveUserGroupRateMultiplier(ctx, key.UserID, group.ID, 1))
	// A cache/DB fault after capture cannot cause fallback or repricing.
	rates.err, versions.failure = errors.New("unavailable"), errors.New("unavailable")
	require.Equal(t, 0.5, gateway.ResolveUserGroupRateMultiplier(ctx, key.UserID, group.ID, 1))
	cost, err := plan.billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "native", Group: captured.Group, GroupID: &captured.Group.ID,
		Resolver: resolver, Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10}, RateMultiplier: 0.5, PricingAt: plan.Snapshot().PricingAt})
	require.NoError(t, err)
	require.InDelta(t, 0.6, cost.ActualCost, 1e-12)
	require.Equal(t, contentVersion, cost.PriceCardVersion)
	require.Contains(t, cost.PricingSnapshot, `"pricing_at":"2026-11-02T01:00:00Z"`)

	schedule, err := billing.ResolveContextPricingSchedule(ctx, resolver, ContextPricingScheduleInput{Model: "native", Group: captured.Group, Platform: PlatformOpenAI})
	require.NoError(t, err)
	require.NotEmpty(t, schedule.Tiers)
	require.InDelta(t, oldInput, *schedule.Tiers[0].Input, 1e-12)
	require.InDelta(t, oldOutput, *schedule.Tiers[0].Output, 1e-12)
}

func TestSalePricingBundleRejectsMismatchedVersionBeforeAdmission(t *testing.T) {
	price := 1.0
	gateway := &GatewayService{billingService: &BillingService{fallbackPrices: map[string]*ModelPricing{}}}
	key := &APIKey{ID: 2, WorkspaceID: 1, UserID: 3, Group: &Group{ID: 7, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"native"}, InputPrice: &price, OutputPrice: &price}}}}
	ctx, _, err := gateway.CaptureSalePricing(context.Background(), key, time.Now())
	require.NoError(t, err)
	ctx, err = ConfirmSalePricing(ctx, PlatformOpenAI, "native", "captured")
	require.NoError(t, err)
	for _, spec := range []GatewayRequestSpec{
		{WorkspaceID: 1, UserID: 3, APIKeyID: 2, BillingGroupID: 7, Model: "native", PriceVersion: "changed"},
		{WorkspaceID: 1, UserID: 3, APIKeyID: 4, BillingGroupID: 7, Model: "native", PriceVersion: "captured"},
		{WorkspaceID: 1, UserID: 8, APIKeyID: 2, BillingGroupID: 7, Model: "native", PriceVersion: "captured"},
	} {
		_, plan := (gatewayPlanFactory{billing: gateway.billingService}).capture(ctx, spec)
		require.ErrorIs(t, plan.admissionErr, ErrSalePriceChanged)
		require.Nil(t, plan.store)
		require.False(t, plan.budgetAdmitted)
	}
}
