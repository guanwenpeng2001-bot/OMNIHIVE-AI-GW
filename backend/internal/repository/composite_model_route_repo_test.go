package repository

import (
	"encoding/json/jsontext"
	"testing"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// These tests exercise the ordered_targets JSONB marshaling without an ent
// client: every harness in this package opens a real database (in-memory SQLite
// or PostgreSQL), so the pure column conversion is covered here instead.
func TestCompositeModelRouteOrderedTargetsRoundTrip(t *testing.T) {
	targets := []service.CompositeRouteTarget{
		{
			Position:          0,
			TargetPlatform:    service.PlatformAnthropic,
			AccountType:       service.AccountTypeOAuth,
			UpstreamModel:     "claude-sonnet-4-6",
			Region:            "us-east-1",
			Provider:          "aws",
			LatencyPreference: service.CompositeTargetLatencyDefault,
			SchedulingWeight:  100,
			Enabled:           true,
		},
		{
			Position:          1,
			TargetPlatform:    service.PlatformOpenAI,
			UpstreamModel:     "gpt-5",
			LatencyPreference: service.CompositeTargetLatencyLowLatency,
			SchedulingWeight:  200,
			Enabled:           true,
		},
	}

	encoded, err := marshalCompositeRouteTargets(targets)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	route := compositeModelRouteEntityToService(&dbent.CompositeModelRoute{
		ID:             1,
		GroupID:        7,
		PublicModel:    "public-model",
		MatchType:      service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformAnthropic,
		AccountType:    service.AccountTypeOAuth,
		UpstreamModel:  "claude-sonnet-4-6",
		Endpoint:       service.CompositeRouteEndpointAny,
		Priority:       100,
		Enabled:        true,
		OrderedTargets: jsontext.Value(encoded),
	})

	require.NotNil(t, route)
	require.Equal(t, targets, route.Targets)
	require.Equal(t, targets, route.EffectiveTargets())
}

func TestCompositeModelRouteOrderedTargetsClearedOrInvalid(t *testing.T) {
	cleared, err := marshalCompositeRouteTargets(nil)
	require.NoError(t, err)
	require.Nil(t, cleared)

	cleared, err = marshalCompositeRouteTargets([]service.CompositeRouteTarget{})
	require.NoError(t, err)
	require.Nil(t, cleared)

	route := compositeModelRouteEntityToService(&dbent.CompositeModelRoute{
		ID:             2,
		GroupID:        7,
		PublicModel:    "legacy-model",
		MatchType:      service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformOpenAI,
		UpstreamModel:  "gpt-5",
		Endpoint:       service.CompositeRouteEndpointAny,
		Enabled:        true,
		OrderedTargets: jsontext.Value([]byte(`{"unexpected":"shape"}`)),
	})

	require.NotNil(t, route)
	require.Empty(t, route.Targets)
	require.Equal(t, []service.CompositeRouteTarget{{
		Position:          0,
		TargetPlatform:    service.PlatformOpenAI,
		UpstreamModel:     "gpt-5",
		LatencyPreference: service.CompositeTargetLatencyDefault,
		SchedulingWeight:  service.CompositeTargetDefaultWeight,
		Enabled:           true,
	}}, route.EffectiveTargets())
}
