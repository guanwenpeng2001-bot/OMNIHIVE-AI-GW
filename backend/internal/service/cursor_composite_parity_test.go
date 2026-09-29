//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCursorCompositeRoutesParity(t *testing.T) {
	for _, endpoint := range []string{CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages, CompositeRouteEndpointResponses} {
		t.Run(endpoint, func(t *testing.T) {
			resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{{GroupID: 7, PublicModel: "cursor-model", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformCursor, UpstreamModel: "claude-sonnet-4-6-thinking", Endpoint: CompositeRouteEndpointAny, Enabled: true}}})
			decision, err := resolver.Resolve(context.Background(), 7, "cursor-model", endpoint)
			require.NoError(t, err)
			require.True(t, decision.Matched)
			require.Equal(t, PlatformCursor, decision.TargetPlatform)
			require.Equal(t, "claude-sonnet-4-6-thinking", decision.UpstreamModel)
			gateway := &GatewayService{accountRepo: &compositeOwnershipAccountRepo{accounts: []Account{{Platform: PlatformCursor, Credentials: map[string]any{"model_mapping": map[string]any{"default": "default"}}}}}}
			resolver = NewCompositeRouteResolver(nil)
			resolver.SetModelOwnershipResolver(gateway.resolveCompositeModelOwnership)
			decision, err = resolver.Resolve(context.Background(), 7, "default", endpoint)
			require.NoError(t, err)
			require.True(t, decision.Matched)
			require.Equal(t, PlatformCursor, decision.TargetPlatform)
			require.Equal(t, CompositeRouteSourceAccount, decision.Source)
		})
	}
	require.NotContains(t, NewModelRegistry().Snapshot().CandidateIDs(PlatformCursor, ""), "default", "automatic router is a protocol selector, not a reviewed model ID")
	require.Contains(t, schedulerSnapshotPlatforms(), PlatformCursor)
	require.True(t, groupSupportsOAuthOnlyFilter(PlatformCursor))
	require.True(t, canCopyAccountsFromGroupPlatform(PlatformComposite, PlatformCursor))
	for _, concurrency := range []int{0, -2, 1, 7} {
		account := &Account{Platform: PlatformCursor, Type: AccountTypeAPIKey, Concurrency: concurrency, Credentials: map[string]any{"api_key": "test"}}
		require.NoError(t, NormalizeCursorAccount(account))
		require.Equal(t, max(1, concurrency), account.Concurrency)
	}
}
