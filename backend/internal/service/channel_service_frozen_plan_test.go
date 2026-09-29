package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type frozenChannelVersions struct {
	GroupRepository
	generation int64
	failure    error
}

func (fixture *frozenChannelVersions) ModelCatalogGeneration(ctx context.Context, _ string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return fixture.generation, fixture.failure
}

func TestReviewCatalogVersionFailurePreservesFrozenChannelPlan(t *testing.T) {
	groups := &frozenChannelVersions{generation: 1}
	originalPrice, replacementPrice := 1.0, 9.0
	repository := &catalogChannelFixture{channels: []Channel{{ID: 1, Name: "captured-price", Status: StatusActive, GroupIDs: []int64{1}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"native"}, InputPrice: &originalPrice}}}}}
	channels := &ChannelService{repo: repository, groupRepo: groups}
	ctx, plan := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{}, nil, nil, channels)
	require.Equal(t, "captured-price", plan.channelSnapshot.byID[1].Name)
	require.Equal(t, originalPrice, *channels.GetChannelModelPricing(ctx, 1, "native").InputPrice)
	repository.channels = []Channel{{ID: 1, Name: "replacement-price", Status: StatusActive, GroupIDs: []int64{1}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"native"}, InputPrice: &replacementPrice}}}}
	groups.generation++
	cached, err := channels.loadCache(ctx)
	require.NoError(t, err)
	require.Same(t, plan.channelSnapshot, cached, "normal requests retain their pricing snapshot")
	groups.failure = errors.New("transient generation read failure")
	cached, err = channels.loadCache(ctx)
	require.NoError(t, err)
	t.Logf("captured=%s returned=%s", plan.channelSnapshot.byID[1].Name, cached.byID[1].Name)
	actualPrice := channels.GetChannelModelPricing(ctx, 1, "native")
	require.NotNil(t, actualPrice)
	t.Logf("same request input price: captured=%v actual=%v", originalPrice, *actualPrice.InputPrice)
	require.Equal(t, originalPrice, *actualPrice.InputPrice)
	require.Same(t, plan.channelSnapshot, cached, "a DB read error must not change prices inside the request")
}

func TestCatalogVersionFailurePreservesFrozenChannelPlan(t *testing.T) {
	for _, scenario := range []string{"version_changed", "version_failed", "request_canceled"} {
		t.Run(scenario, func(t *testing.T) {
			groups := &frozenChannelVersions{generation: 1}
			originalPrice, replacementPrice := 1.0, 9.0
			channel := func(name, mapped string, price *float64) Channel {
				return Channel{ID: 1, Name: name, Status: StatusActive, GroupIDs: []int64{1},
					ModelMapping: map[string]map[string]string{PlatformOpenAI: {"native": mapped}},
					ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"native"}, InputPrice: price}}}
			}
			repository := &catalogChannelFixture{channels: []Channel{channel("captured", "old-model", &originalPrice)}}
			channels := &ChannelService{repo: repository, groupRepo: groups}
			root, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, plan := captureGatewayRequestPlan(root, GatewayRequestSpec{}, nil, nil, channels)
			require.NoError(t, plan.channelError)
			require.Equal(t, "old-model", channels.ResolveChannelMapping(ctx, 1, "native").MappedModel)
			repository.channels = []Channel{channel("replacement", "new-model", &replacementPrice)}
			groups.generation++
			if scenario == "version_failed" {
				groups.failure = errors.New("generation unavailable")
			}
			if scenario == "request_canceled" {
				cancel()
			}
			cached, err := channels.loadCache(ctx)
			require.NoError(t, err)
			require.Same(t, plan.channelSnapshot, cached)
			price := channels.GetChannelModelPricing(ctx, 1, "native")
			require.NotNil(t, price)
			require.Equal(t, originalPrice, *price.InputPrice)
			require.Equal(t, "old-model", channels.ResolveChannelMapping(ctx, 1, "native").MappedModel)
			fresh := channels.GetChannelModelPricing(context.Background(), 1, "native")
			require.NotNil(t, fresh)
			require.Equal(t, replacementPrice, *fresh.InputPrice)
		})
	}
}

func TestFrozenChannelPlanPreservesCapturedError(t *testing.T) {
	channels := &ChannelService{groupRepo: &frozenChannelVersions{failure: errors.New("generation unavailable")}}
	frozenError := errors.New("captured channel failure")
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	plan.channels, plan.channelError = channels, frozenError
	cached, err := channels.loadCache(WithGatewayRequestPlan(context.Background(), plan))
	require.Nil(t, cached)
	require.ErrorIs(t, err, frozenError)
}
