//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeTargetForwardModelPrecedence(t *testing.T) {
	publicModel := "Exact.Public-Alias"
	first := CompositeRouteTarget{Position: 0, TargetPlatform: PlatformOpenAI, UpstreamModel: "previous-wire"}
	later := CompositeRouteTarget{Position: 1, TargetPlatform: PlatformOpenAI}
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: publicModel, UpstreamModel: first.UpstreamModel, TargetPlatform: first.TargetPlatform, Targets: []CompositeRouteTarget{first, later}, SelectedTarget: &first})
	ctx = WithCompositeTargetSelectionContext(ctx, later)
	resolved, _ := ResolvedUpstreamModelFromContext(ctx)
	require.Equal(t, publicModel, resolved) // Filled context is a fallback, not configured target authority.
	mapping := ChannelMappingResult{Mapped: true, MappedModel: "channel-wire", ChannelID: 42, BillingModelSource: BillingModelSourceChannelMapped}
	for _, scenario := range []struct {
		name     string
		target   *CompositeRouteTarget
		mapping  ChannelMappingResult
		expected string
	}{
		{"explicit", &first, mapping, "previous-wire"},
		{"explicit_equal_inbound", &CompositeRouteTarget{UpstreamModel: "previous-wire"}, mapping, "previous-wire"},
		{"empty_channel", &later, mapping, "channel-wire"},
		{"whitespace_channel", &CompositeRouteTarget{UpstreamModel: "  "}, mapping, "channel-wire"},
		{"empty_public", &later, ChannelMappingResult{MappedModel: "previous-wire"}, publicModel},
		{"non_composite_channel", nil, mapping, "channel-wire"},
		{"non_composite_unmapped", nil, ChannelMappingResult{}, "previous-wire"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.expected, CompositeTargetForwardModel(ctx, scenario.target, scenario.mapping, "previous-wire"))
			requested, _ := RequestedPublicModelFromContext(ctx)
			require.Equal(t, publicModel, requested)
			require.Equal(t, "channel-wire", mapping.MappedModel)
			require.Equal(t, BillingModelSourceChannelMapped, mapping.BillingModelSource)
		})
	}
}
