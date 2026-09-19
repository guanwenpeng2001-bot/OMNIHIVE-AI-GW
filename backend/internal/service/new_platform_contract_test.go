package service

import (
	"context"
	"testing"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/userplatformquota"
	"github.com/stretchr/testify/require"
)

func TestConcretePlatformContracts(t *testing.T) {
	platforms := ConcretePlatforms()
	require.Len(t, platforms, 15)
	require.ElementsMatch(t, platforms, AllowedQuotaPlatforms)
	require.Equal(t, platforms, schedulerSnapshotPlatforms())
	require.ElementsMatch(t, platforms, matchingPlatforms(PlatformComposite))
	seen := map[string]bool{}
	for _, platform := range platforms {
		require.False(t, seen[platform], "duplicate platform %s", platform)
		seen[platform] = true
		require.True(t, isConcreteRequestPlatform(platform))
		require.NoError(t, userplatformquota.PlatformValidator(platform))
	}
	for _, platform := range []string{PlatformComposite, "Qianwen", "command_code", "bogus"} {
		require.False(t, isConcreteRequestPlatform(platform))
		require.False(t, IsAllowedQuotaPlatform(platform))
		require.Error(t, userplatformquota.PlatformValidator(platform))
	}
	require.True(t, IsCNProvider(PlatformQianwen))
	require.False(t, IsCNProvider(PlatformCommandCode))
	require.True(t, IsMultiProtocolAPIKeyProvider(PlatformQianwen))
	require.True(t, IsMultiProtocolAPIKeyProvider(PlatformCommandCode))
	require.False(t, IsMultiProtocolAPIKeyProvider(PlatformCursor))
	require.Equal(t, 32, schedulerCanonicalBucketCount())
	for _, platform := range []string{PlatformQianwen, PlatformCommandCode} {
		require.Contains(t, schedulerCanonicalBuckets(7), SchedulerBucket{GroupID: 7, Platform: platform, Mode: SchedulerModeSingle})
		require.Contains(t, schedulerCanonicalBuckets(7), SchedulerBucket{GroupID: 7, Platform: platform, Mode: SchedulerModeForced})
	}
}

func TestNewPlatformExplicitRoutesPreserveSharedModelIDs(t *testing.T) {
	for _, platform := range []string{PlatformQianwen, PlatformCommandCode} {
		for _, model := range []string{"deepseek-v4-pro", "glm-5", "kimi-k3", "qwen-max", "Qwen/Qwen3-Max"} {
			t.Run(platform+"/"+model, func(t *testing.T) {
				detected, _ := DetectModelPlatform(model)
				require.NotEqual(t, PlatformQianwen, detected)
				require.NotEqual(t, PlatformCommandCode, detected)
				resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{{
					ID: 1, GroupID: 7, PublicModel: model, UpstreamModel: model,
					MatchType: CompositeRouteMatchExact, TargetPlatform: platform,
					Endpoint: CompositeRouteEndpointAny, Enabled: true,
				}}})
				decision, err := resolver.Resolve(context.Background(), 7, model, CompositeRouteEndpointChatCompletions)
				require.NoError(t, err)
				require.Equal(t, CompositeRouteSourceExplicit, decision.Source)
				require.Equal(t, platform, decision.TargetPlatform)
				require.Equal(t, model, decision.UpstreamModel)
			})
		}
	}
}
