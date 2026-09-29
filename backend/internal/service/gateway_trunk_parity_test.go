//go:build unit

package service

// 两条主干（Claude GatewayService / OpenAI OpenAIGatewayService）差分测试：
// 同一渠道映射与调度协作者下，资格判定与准入决策必须给出相同结果。

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// restrictedPricingChannel 只给 claude-opus-4-6 定价，并把 claude-sonnet-4-5 映射为
// claude-sonnet-4-6，因此映射后的模型受限；claude-opus-4-6 自身不受限。
func restrictedPricingChannel() Channel {
	return Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-opus-4-6"}},
		},
		ModelMapping: map[string]map[string]string{
			"anthropic": {"claude-sonnet-4-5": "claude-sonnet-4-6"},
		},
	}
}

func TestGatewayTrunksAgreeOnChannelPricingRestriction(t *testing.T) {
	channelService := newTestChannelService(makeStandardRepo(restrictedPricingChannel(), map[int64]string{10: "anthropic"}))
	claudeTrunk := &GatewayService{channelService: channelService}
	openAITrunk := &OpenAIGatewayService{channelService: channelService}
	groupID := int64(10)
	ctx := context.Background()

	require.True(t, claudeTrunk.checkChannelPricingRestriction(ctx, &groupID, "claude-sonnet-4-5"),
		"the mapped model claude-sonnet-4-6 is not priced, so the request must be restricted")
	require.False(t, claudeTrunk.checkChannelPricingRestriction(ctx, &groupID, "claude-opus-4-6"),
		"the priced model must stay allowed")

	for _, requestedModel := range []string{"claude-sonnet-4-5", "claude-opus-4-6", "unknown-model", ""} {
		claudeRestricted := claudeTrunk.checkChannelPricingRestriction(ctx, &groupID, requestedModel)
		openAIRestricted := openAITrunk.checkChannelPricingRestriction(ctx, &groupID, requestedModel)
		require.Equal(t, claudeRestricted, openAIRestricted,
			"model %q must get the same pricing restriction decision from both trunks", requestedModel)
	}

	require.False(t, claudeTrunk.checkChannelPricingRestriction(ctx, nil, "claude-sonnet-4-5"))
	require.False(t, openAITrunk.checkChannelPricingRestriction(ctx, nil, "claude-sonnet-4-5"))
}

func TestGatewayTrunksAgreeOnChannelMappingHelpers(t *testing.T) {
	channelService := newTestChannelService(makeStandardRepo(restrictedPricingChannel(), map[int64]string{10: "anthropic"}))
	claudeTrunk := &GatewayService{channelService: channelService}
	openAITrunk := &OpenAIGatewayService{channelService: channelService}
	groupID := int64(10)
	ctx := context.Background()

	for _, requestedModel := range []string{"claude-sonnet-4-5", "claude-opus-4-6", "unknown-model"} {
		require.Equal(t,
			claudeTrunk.ResolveChannelMapping(ctx, groupID, requestedModel),
			openAITrunk.ResolveChannelMapping(ctx, groupID, requestedModel),
			"channel mapping for %q must be identical on both trunks", requestedModel)

		claudeMapping, claudeRestricted := claudeTrunk.ResolveChannelMappingAndRestrict(ctx, &groupID, requestedModel)
		openAIMapping, openAIRestricted := openAITrunk.ResolveChannelMappingAndRestrict(ctx, &groupID, requestedModel)
		require.Equal(t, claudeMapping, openAIMapping,
			"channel mapping + restriction for %q must be identical on both trunks", requestedModel)
		require.Equal(t, claudeRestricted, openAIRestricted,
			"channel mapping restriction flag for %q must be identical on both trunks", requestedModel)
	}

	for _, model := range []string{"claude-sonnet-4-6", "claude-opus-4-6"} {
		require.Equal(t,
			claudeTrunk.IsModelRestricted(ctx, groupID, model),
			openAITrunk.IsModelRestricted(ctx, groupID, model),
			"model restriction for %q must be identical on both trunks", model)
	}
	require.True(t, claudeTrunk.IsModelRestricted(ctx, groupID, "claude-sonnet-4-6"),
		"the unpriced model must be reported as restricted so the parity assertion above is meaningful")

	// 未接线渠道服务时两条主干都必须放行。
	require.Equal(t,
		(&GatewayService{}).ResolveChannelMapping(ctx, groupID, "claude-sonnet-4-5"),
		(&OpenAIGatewayService{}).ResolveChannelMapping(ctx, groupID, "claude-sonnet-4-5"))
	require.False(t, (&GatewayService{}).IsModelRestricted(ctx, groupID, "claude-sonnet-4-5"))
	require.False(t, (&OpenAIGatewayService{}).IsModelRestricted(ctx, groupID, "claude-sonnet-4-5"))
}

func TestGatewayTrunksAgreeOnSchedulingConfig(t *testing.T) {
	claudeDefaults := (&GatewayService{}).schedulingConfig()
	openAIDefaults := (&OpenAIGatewayService{}).schedulingConfig()
	require.Equal(t, claudeDefaults, openAIDefaults, "built-in scheduling defaults must not drift between trunks")
	require.Equal(t, 45*time.Second, claudeDefaults.StickySessionWaitTimeout, "default sticky wait must stay 45s")
	require.Equal(t, 30*time.Second, claudeDefaults.FallbackWaitTimeout, "default fallback wait must stay 30s")

	configured := &config.Config{}
	configured.Gateway.Scheduling.StickySessionWaitTimeout = 12 * time.Second
	configured.Gateway.Scheduling.FallbackMaxWaiting = 7
	require.Equal(t,
		(&GatewayService{cfg: configured}).schedulingConfig(),
		(&OpenAIGatewayService{cfg: configured}).schedulingConfig(),
		"a configured scheduling block must be read identically by both trunks")
	require.Equal(t, time.Duration(12*time.Second), (&GatewayService{cfg: configured}).schedulingConfig().StickySessionWaitTimeout)
}

func TestGatewayTrunksAgreeOnAccountSlotAdmission(t *testing.T) {
	ctx := context.Background()

	claudeResult, claudeErr := (&GatewayService{}).tryAcquireAccountSlot(ctx, 7, 3)
	openAIResult, openAIErr := (&OpenAIGatewayService{}).tryAcquireAccountSlot(ctx, 7, 3)
	require.NoError(t, claudeErr)
	require.NoError(t, openAIErr)
	require.Equal(t, claudeResult.Acquired, openAIResult.Acquired,
		"an unwired concurrency service must not block one trunk but not the other")
	require.NotNil(t, claudeResult.ReleaseFunc)
	require.NotNil(t, openAIResult.ReleaseFunc)
	claudeResult.ReleaseFunc()
	openAIResult.ReleaseFunc()
}

func TestGatewayTrunksAgreeOnSchedulingThresholdDecision(t *testing.T) {
	sharedRateLimitService := &RateLimitService{}
	accounts := []*Account{
		nil,
		{ID: 0},
		{ID: 7, Status: StatusActive, Schedulable: true},
	}
	ctx := context.Background()
	for _, account := range accounts {
		claudeBlocked := (&GatewayService{rateLimitService: sharedRateLimitService}).isAccountBlockedBySchedulingThreshold(ctx, account)
		openAIBlocked := (&OpenAIGatewayService{rateLimitService: sharedRateLimitService}).isOpenAIAccountBlockedBySchedulingThreshold(ctx, account)
		require.Equal(t, claudeBlocked, openAIBlocked, "the same account must get the same scheduling threshold decision")
	}

	// 未接线限流服务的部署不能在两条主干之间出现“一边放行一边拦截”。
	require.False(t, (&GatewayService{}).isAccountBlockedBySchedulingThreshold(ctx, &Account{ID: 1, Status: StatusActive}))
	require.False(t, (&OpenAIGatewayService{}).isOpenAIAccountBlockedBySchedulingThreshold(ctx, &Account{ID: 1, Status: StatusActive}))
}
