package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func orderedCompositeTargetsFixture() []CompositeRouteTarget {
	return []CompositeRouteTarget{
		{
			Position:          0,
			TargetPlatform:    PlatformAnthropic,
			AccountType:       AccountTypeOAuth,
			UpstreamModel:     "claude-sonnet-4-6",
			Region:            "us-east-1",
			LatencyPreference: CompositeTargetLatencyDefault,
			SchedulingWeight:  100,
			Enabled:           true,
		},
		{
			Position:          1,
			TargetPlatform:    PlatformOpenAI,
			UpstreamModel:     "gpt-5",
			LatencyPreference: CompositeTargetLatencyDefault,
			SchedulingWeight:  200,
			Enabled:           true,
		},
	}
}

func TestOrderedCompositeRouteTargetsContextRoundTrip(t *testing.T) {
	targets := orderedCompositeTargetsFixture()
	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformOpenAI,
		Targets:        targets,
	}
	ctx := WithCompositeRouteDecision(context.Background(), decision)

	stored, ok := CompositeRouteTargetsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, targets, stored)

	// The decision owns a defensive copy: mutating the caller's slice afterwards
	// must not change what the request context reports.
	targets[0].TargetPlatform = "mutated-platform"
	targets[1].SchedulingWeight = 9999
	again, ok := CompositeRouteTargetsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, orderedCompositeTargetsFixture(), again)

	// The active target is the one matching the platform resolved for the request.
	active, ok := ActiveCompositeTargetFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, PlatformOpenAI, active.TargetPlatform)

	// Without a matching resolved platform the first declared target is used.
	fallbackCtx := WithCompositeRouteTargets(context.Background(), orderedCompositeTargetsFixture())
	fallbackCtx = WithResolvedTargetPlatform(fallbackCtx, PlatformGrok)
	active, ok = ActiveCompositeTargetFromContext(fallbackCtx)
	require.True(t, ok)
	require.Equal(t, PlatformAnthropic, active.TargetPlatform)

	// A request without ordered targets has no active target.
	_, ok = ActiveCompositeTargetFromContext(context.Background())
	require.False(t, ok)
}

func TestOrderedCompositeTargetSelectionContextRepointsTarget(t *testing.T) {
	targets := orderedCompositeTargetsFixture()
	base := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		Targets:        targets,
	})

	ctx := WithCompositeTargetSelectionContext(base, targets[1])

	platform, ok := ResolvedTargetPlatformFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, PlatformOpenAI, platform)

	upstreamModel, ok := ResolvedUpstreamModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "gpt-5", upstreamModel)

	publicModel, ok := RequestedPublicModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "public-model", publicModel)

	kept, ok := CompositeRouteTargetsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, targets, kept)

	active, ok := ActiveCompositeTargetFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, targets[1], active)
}

func TestOrderedCompositeTargetSelectionContextKeepsRequestModelWithoutUpstream(t *testing.T) {
	target := CompositeRouteTarget{
		Position:          0,
		TargetPlatform:    PlatformGemini,
		LatencyPreference: CompositeTargetLatencyDefault,
		SchedulingWeight:  CompositeTargetDefaultWeight,
		Enabled:           true,
	}
	base := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		Targets:        []CompositeRouteTarget{target},
	})

	ctx := WithCompositeTargetSelectionContext(base, target)

	_, ok := ResolvedUpstreamModelFromContext(ctx)
	require.False(t, ok)

	publicModel, ok := RequestedPublicModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "public-model", publicModel)
}

func TestOrderedCompositeStickyTTLSelection(t *testing.T) {
	t.Run("override wins over the legacy default", func(t *testing.T) {
		ctx := WithStickySessionTTL(context.Background(), 5*time.Minute)
		require.Equal(t, 5*time.Minute, stickySessionTTLForRequest(ctx))
	})

	t.Run("zero and negative overrides keep the legacy default", func(t *testing.T) {
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(context.Background()))
		require.Equal(t, time.Hour, stickySessionTTL)
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(WithStickySessionTTL(context.Background(), 0)))
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(WithStickySessionTTL(context.Background(), -time.Minute)))
	})

	t.Run("selection without a composite target is a no-op", func(t *testing.T) {
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(WithSelectionStickyTTL(context.Background(), nil)))
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(WithSelectionStickyTTL(context.Background(), &AccountSelectionResult{})))
	})

	t.Run("selection without a target affinity ttl is a no-op", func(t *testing.T) {
		ctx := WithSelectionStickyTTL(context.Background(), &AccountSelectionResult{
			CompositeTarget: &CompositeRouteTarget{TargetPlatform: PlatformOpenAI},
		})
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(ctx))
	})

	t.Run("selection affinity ttl is applied", func(t *testing.T) {
		ctx := WithSelectionStickyTTL(context.Background(), &AccountSelectionResult{
			CompositeTarget: &CompositeRouteTarget{
				TargetPlatform:     PlatformOpenAI,
				AffinityTTLSeconds: 300,
			},
		})
		require.Equal(t, 300*time.Second, stickySessionTTLForRequest(ctx))
	})

	t.Run("active ordered target affinity ttl is applied", func(t *testing.T) {
		base := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
			Matched:        true,
			TargetPlatform: PlatformOpenAI,
			Targets: []CompositeRouteTarget{{
				TargetPlatform:     PlatformOpenAI,
				SchedulingWeight:   CompositeTargetDefaultWeight,
				AffinityTTLSeconds: 300,
				Enabled:            true,
			}},
		})
		require.Equal(t, 300*time.Second, stickySessionTTLForRequest(WithCompositeTargetStickyTTL(base)))
	})

	t.Run("legacy target keeps the legacy default", func(t *testing.T) {
		base := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
			Matched:        true,
			TargetPlatform: PlatformOpenAI,
			Targets: []CompositeRouteTarget{{
				TargetPlatform:   PlatformOpenAI,
				SchedulingWeight: CompositeTargetDefaultWeight,
				Enabled:          true,
			}},
		})
		require.Equal(t, stickySessionTTL, stickySessionTTLForRequest(WithCompositeTargetStickyTTL(base)))
	})
}

func TestCompositeTargetExposureHeadersWhitelist(t *testing.T) {
	t.Run("nil target has no headers", func(t *testing.T) {
		require.Nil(t, CompositeTargetExposureHeaders(nil))
	})

	t.Run("target without platform or region has no headers", func(t *testing.T) {
		require.Nil(t, CompositeTargetExposureHeaders(&CompositeRouteTarget{AccountType: AccountTypeOAuth}))
	})

	t.Run("platform and region are exposed", func(t *testing.T) {
		headers := CompositeTargetExposureHeaders(&CompositeRouteTarget{
			TargetPlatform: PlatformBedrock,
			Region:         "us-east-1",
			AccountType:    AccountTypeBedrock,
			UpstreamModel:  "anthropic.claude-sonnet-4-6",
		})
		require.Equal(t, map[string]string{
			ExposureProviderHeader: PlatformBedrock,
			ExposureRegionHeader:   "us-east-1",
		}, headers)
		require.ElementsMatch(t, []string{ExposureProviderHeader, ExposureRegionHeader}, mapKeys(headers))
	})

	t.Run("platform without region exposes only the provider", func(t *testing.T) {
		headers := CompositeTargetExposureHeaders(&CompositeRouteTarget{TargetPlatform: PlatformOpenAI})
		require.Equal(t, map[string]string{ExposureProviderHeader: PlatformOpenAI}, headers)
		require.Len(t, headers, 1)
	})

	t.Run("only whitelisted keys are ever returned", func(t *testing.T) {
		headers := CompositeTargetExposureHeaders(&CompositeRouteTarget{
			TargetPlatform: PlatformAnthropic,
			Region:         "eu-west-1",
			AccountType:    AccountTypeOAuth,
			Provider:       "aws",
			UpstreamModel:  "claude-sonnet-4-6",
		})
		require.Len(t, headers, 2)
		for key := range headers {
			require.Contains(t, []string{ExposureProviderHeader, ExposureRegionHeader}, key)
		}
	})

	t.Run("active target headers come from the ordered list", func(t *testing.T) {
		base := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
			Matched:        true,
			TargetPlatform: PlatformOpenAI,
			Targets:        orderedCompositeTargetsFixture(),
		})
		require.Equal(t, map[string]string{ExposureProviderHeader: PlatformOpenAI}, CompositeExposureHeaders(base))
		require.Nil(t, CompositeExposureHeaders(context.Background()))
	})
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestCompositeNormalizeRouteInputMirrorsOrderedTargets(t *testing.T) {
	tests := []struct {
		name            string
		input           CompositeRouteInput
		wantPlatform    string
		wantAccountType string
		wantUpstream    string
		wantTargetCount int
		wantErr         bool
	}{
		{
			name: "first enabled target is mirrored into the legacy columns",
			input: CompositeRouteInput{
				PublicModel:    "public-model",
				MatchType:      CompositeRouteMatchExact,
				TargetPlatform: PlatformOpenAI,
				AccountType:    AccountTypeOAuth,
				UpstreamModel:  "gpt-5",
				Endpoint:       CompositeRouteEndpointChatCompletions,
				Targets: []CompositeRouteTarget{
					// 首个目标被禁用：它的 account_type 不得镜像进旧列，否则回滚二进制会把
					// 第二个目标的流量也限制成 Bedrock。
					{TargetPlatform: PlatformAnthropic, AccountType: AccountTypeBedrock, UpstreamModel: "claude-sonnet-4-6", Enabled: false},
					{TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-pro", Enabled: true},
				},
			},
			wantPlatform:    PlatformGemini,
			wantAccountType: "",
			wantUpstream:    "gemini-2.5-pro",
			wantTargetCount: 2,
		},
		{
			name: "nil targets leave the legacy columns untouched",
			input: CompositeRouteInput{
				PublicModel:    "public-model",
				MatchType:      CompositeRouteMatchExact,
				TargetPlatform: PlatformOpenAI,
				AccountType:    AccountTypeOAuth,
				UpstreamModel:  "gpt-5",
				Targets:        nil,
			},
			wantPlatform:    PlatformOpenAI,
			wantAccountType: AccountTypeOAuth,
			wantUpstream:    "gpt-5",
			wantTargetCount: 0,
		},
		{
			name: "exact route backfills the mirrored empty upstream model",
			input: CompositeRouteInput{
				PublicModel: "public-model",
				MatchType:   CompositeRouteMatchExact,
				Targets: []CompositeRouteTarget{
					{TargetPlatform: PlatformAnthropic, Enabled: true},
				},
			},
			wantPlatform:    PlatformAnthropic,
			wantUpstream:    "public-model",
			wantTargetCount: 1,
		},
		{
			name: "prefix route keeps the mirrored empty upstream model",
			input: CompositeRouteInput{
				PublicModel: "public-model",
				MatchType:   CompositeRouteMatchPrefix,
				Targets: []CompositeRouteTarget{
					{TargetPlatform: PlatformAnthropic, Enabled: true},
				},
			},
			wantPlatform:    PlatformAnthropic,
			wantUpstream:    "",
			wantTargetCount: 1,
		},
		{
			name: "invalid targets propagate the validation error",
			input: CompositeRouteInput{
				PublicModel: "public-model",
				MatchType:   CompositeRouteMatchExact,
				Targets: []CompositeRouteTarget{
					{TargetPlatform: PlatformComposite, Enabled: true},
				},
			},
			wantErr: true,
		},
		{
			name: "a target whose hosted account type conflicts with its platform is rejected",
			input: CompositeRouteInput{
				PublicModel: "public-model",
				MatchType:   CompositeRouteMatchExact,
				Targets: []CompositeRouteTarget{
					{TargetPlatform: PlatformBedrock, AccountType: AccountTypeOAuth, Enabled: true},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCompositeRouteInput(tt.input)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrCompositeTargetInvalid)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPlatform, got.TargetPlatform)
			require.Equal(t, tt.wantAccountType, got.AccountType)
			require.Equal(t, tt.wantUpstream, got.UpstreamModel)
			require.Len(t, got.Targets, tt.wantTargetCount)
			for index, target := range got.Targets {
				require.Equal(t, index, target.Position)
			}
		})
	}
}
