package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestBuildCompositeRouteDecisionPreservesPublicAlias(t *testing.T) {
	group := &Group{ID: 21, Platform: PlatformComposite}
	for _, platform := range []string{PlatformCursor, PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformBedrock} {
		t.Run(platform, func(t *testing.T) {
			accountType := AccountTypeAPIKey
			if platform == PlatformBedrock {
				accountType = AccountTypeBedrock
			}
			targets := []CompositeRouteTarget{{TargetPlatform: platform, AccountType: accountType, UpstreamModel: "wire-model", Enabled: true}}
			ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
				Matched: true, Source: CompositeRouteSourceExplicit, PublicModel: "Cumora.Public-Alias",
				TargetPlatform: platform, AccountType: accountType, UpstreamModel: "wire-model", Targets: targets,
			})
			decision, matched, err := (&GatewayService{}).buildCompositeRouteDecision(ctx, group, "rewritten-body-model", CompositeRouteEndpointResponses)
			require.NoError(t, err)
			require.True(t, matched)
			require.Equal(t, CompositeRouteDecision{
				Matched: true, Source: CompositeRouteSourceExplicit, GroupID: group.ID, PublicModel: "Cumora.Public-Alias",
				TargetPlatform: platform, AccountType: accountType, UpstreamModel: "wire-model",
				Endpoint: CompositeRouteEndpointResponses, Targets: targets,
			}, decision)
			publicModel, ok := RequestedPublicModelFromContext(WithCompositeRouteDecision(ctx, decision))
			require.True(t, ok)
			require.Equal(t, "Cumora.Public-Alias", publicModel)
		})
	}
}

func TestBuildCompositeRouteDecisionPublicModelFallback(t *testing.T) {
	group := &Group{ID: 21, Platform: PlatformComposite}
	for _, publicModel := range []any{nil, "", " \t ", 42} {
		t.Run(fmt.Sprintf("context_%v", publicModel), func(t *testing.T) {
			ctx := WithResolvedTargetPlatform(context.Background(), PlatformCursor)
			if publicModel != nil {
				ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, publicModel)
			}
			decision, matched, err := (&GatewayService{}).buildCompositeRouteDecision(ctx, group, "default", CompositeRouteEndpointChatCompletions)
			require.NoError(t, err)
			require.True(t, matched)
			require.Equal(t, "default", decision.PublicModel)
			require.Equal(t, "default", decision.UpstreamModel)
			require.Equal(t, PlatformCursor, decision.TargetPlatform)
			require.Equal(t, CompositeRouteSourceDetector, decision.Source)
		})
	}

	t.Run("no resolved context still uses resolver", func(t *testing.T) {
		svc := &GatewayService{compositeResolver: NewCompositeRouteResolver(nil)}
		decision, matched, err := svc.buildCompositeRouteDecision(context.Background(), group, "gpt-5", CompositeRouteEndpointResponses)
		require.NoError(t, err)
		require.True(t, matched)
		require.Equal(t, "gpt-5", decision.PublicModel)
		require.Equal(t, "gpt-5", decision.UpstreamModel)
		require.Equal(t, PlatformOpenAI, decision.TargetPlatform)
		require.Equal(t, CompositeRouteSourceDetector, decision.Source)

		_, matched, err = svc.buildCompositeRouteDecision(context.Background(), group, "unknown-alias", CompositeRouteEndpointResponses)
		require.NoError(t, err)
		require.False(t, matched)
	})
}

func TestGatewayCompositePublicAliasScheduling(t *testing.T) {
	const publicModel, upstreamModel = "Cumora.Public-Alias", "default"
	for _, protocol := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions} {
		for _, loadBatch := range []bool{false, true} {
			for _, scenario := range []string{"alias", "direct_upstream", "missing_public_context", "unlisted_alias", "wrong_platform", "wrong_account_type", "unselected_upstream"} {
				t.Run(fmt.Sprintf("%s/load_batch_%t/%s", protocol, loadBatch, scenario), func(t *testing.T) {
					group := &Group{ID: 21, Hydrated: true, Platform: PlatformComposite, Status: StatusActive,
						ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{publicModel}}}
					account := catalogEvidenceAccount(201, PlatformCursor, upstreamModel)
					account.Concurrency = 1
					account.AccountGroups = []AccountGroup{{AccountID: account.ID, GroupID: group.ID}}
					if scenario == "wrong_platform" {
						account.Platform = PlatformOpenAI
					}
					if scenario == "wrong_account_type" {
						account.Type = AccountTypeOAuth
					}
					if scenario == "unselected_upstream" {
						account.Credentials["model_mapping"] = map[string]string{"other-model": "other-model"}
					}
					cfg := &config.Config{RunMode: config.RunModeSimple}
					cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatch
					slots := &compositeTargetRecordingConcurrencyCache{}
					svc := &GatewayService{cfg: cfg,
						accountRepo:        &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{PlatformCursor: {account}}},
						concurrencyService: NewConcurrencyService(slots)}
					// The body has already been rewritten; authorization still uses the
					// authenticated group's public-only allowlist, never the wire name.
					ctx := svc.withGroupContext(context.Background(), group)
					ctx = withModelAccessSnapshot(ctx, ModelAccessSnapshot{KeyGroup: group.ModelAllowlist})
					ctx = WithGatewayRequestPlan(ctx, NewGatewayRequestPlan(GatewayRequestSpec{Protocol: protocol, Model: upstreamModel}))
					requestedPublicModel := publicModel
					switch scenario {
					case "direct_upstream":
						requestedPublicModel = upstreamModel
					case "missing_public_context":
						requestedPublicModel = ""
					case "unlisted_alias":
						requestedPublicModel = "Unlisted.Public-Alias"
					}
					target := CompositeRouteTarget{TargetPlatform: PlatformCursor, AccountType: AccountTypeAPIKey, UpstreamModel: upstreamModel, Enabled: true}
					ctx = WithCompositeTargetAdvancement(WithCompositeRouteDecision(ctx, CompositeRouteDecision{
						Matched: true, Source: CompositeRouteSourceExplicit, PublicModel: requestedPublicModel,
						TargetPlatform: target.TargetPlatform, AccountType: target.AccountType, UpstreamModel: upstreamModel,
						Targets: []CompositeRouteTarget{target},
					}))
					selection, err := svc.SelectAccountWithLoadAwareness(ctx, &group.ID, "", upstreamModel, nil, "", 0)
					if scenario != "alias" {
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
						require.Nil(t, selection)
						require.Empty(t, slots.acquiredHardCap)
						return
					}
					require.NoError(t, err)
					require.NotNil(t, selection)
					require.Equal(t, account.ID, selection.Account.ID)
					require.Equal(t, PlatformCursor, selection.Account.Platform)
					require.Equal(t, &target, selection.CompositeTarget)
					require.True(t, selection.Acquired)
					selection.ReleaseFunc()
				})
			}
		}
	}
}
