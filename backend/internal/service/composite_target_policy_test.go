package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeTargetCandidatesUseSelectedConfiguration(t *testing.T) {
	for _, trunk := range []string{"gateway", "openai"} {
		for _, scenario := range []struct {
			name         string
			mapping      map[string]string
			excluded     bool
			schedulable  bool
			wantPosition int
		}{
			{"selected without metadata", map[string]string{"wire-first": "wire-first"}, false, true, 0},
			{"no selected model", nil, false, true, 1},
			{"different selected model", map[string]string{"unrelated": "unrelated"}, false, true, 1},
			{"excluded selected account", map[string]string{"wire-first": "wire-first"}, true, true, 1},
			{"unschedulable selected account", map[string]string{"wire-first": "wire-first"}, false, false, 1},
		} {
			t.Run(trunk+"/"+scenario.name, func(t *testing.T) {
				first := compositeOrderedTargetTestAccount(801, PlatformOpenAI, AccountTypeAPIKey)
				first.Credentials = map[string]any{"model_mapping": scenario.mapping}
				first.Schedulable = scenario.schedulable
				second := compositeOrderedTargetTestAccount(802, PlatformGrok, AccountTypeAPIKey)
				second.Credentials = map[string]any{"model_mapping": map[string]string{"wire-second": "wire-second"}}
				repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{PlatformOpenAI: {first}, PlatformGrok: {second}}}
				targets := []CompositeRouteTarget{
					{Position: 0, TargetPlatform: PlatformOpenAI, UpstreamModel: "wire-first", Enabled: true},
					{Position: 1, TargetPlatform: PlatformGrok, UpstreamModel: "wire-second", Enabled: true},
				}
				ctx := WithCompositeTargetAdvancement(WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: "Exact.Public-Alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "wire-first", Targets: targets}))
				if scenario.excluded {
					ctx = WithCompositeExcludedAccounts(ctx, map[int64]struct{}{801: {}})
				}
				groupID := int64(21)
				var target CompositeRouteTarget
				var ok bool
				var err error
				if trunk == "gateway" {
					target, ok, err = (&GatewayService{accountRepo: repo}).CompositeTargetForAttempt(ctx, &groupID)
				} else {
					target, ok, err = (&OpenAIGatewayService{accountRepo: repo}).CompositeTargetForAttempt(ctx, &groupID)
				}
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, scenario.wantPosition, target.Position)
				selected := WithCompositeTargetSelectionContext(ctx, target)
				model, _ := RequestedPublicModelFromContext(selected)
				require.Equal(t, "Exact.Public-Alias", model)
				platform, _ := ResolvedTargetPlatformFromContext(selected)
				require.Equal(t, target.TargetPlatform, platform)
				upstream, _ := ResolvedUpstreamModelFromContext(selected)
				require.Equal(t, target.UpstreamModel, upstream)

				// Exhaustion retains the last target and never invents a different model.
				exhausted := WithCompositeExcludedAccounts(ctx, map[int64]struct{}{801: {}, 802: {}})
				if trunk == "gateway" {
					target, ok, err = (&GatewayService{accountRepo: repo}).CompositeTargetForAttempt(exhausted, &groupID)
				} else {
					target, ok, err = (&OpenAIGatewayService{accountRepo: repo}).CompositeTargetForAttempt(exhausted, &groupID)
				}
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, targets[1], target)
			})
		}
	}
}

func TestCompositeTargetEmptyUpstreamDoesNotRetainPreviousModel(t *testing.T) {
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: "Exact.Public-Alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "old-wire"})
	ctx = WithCompositeTargetSelectionContext(ctx, CompositeRouteTarget{TargetPlatform: PlatformGrok})
	model, ok := ResolvedUpstreamModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "Exact.Public-Alias", model)
}
