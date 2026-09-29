package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type visiblePolicyAccountRepo struct {
	AccountRepository
	accounts         []Account
	generation       int64
	changeDuringRead bool
	generationErr    error
	assertScope      func(*int64, []string, bool)
}

func (r *visiblePolicyAccountRepo) ListModelCatalogAccounts(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]Account, error) {
	if r.assertScope != nil {
		r.assertScope(groupID, platforms, includeGrouped)
	}
	if r.changeDuringRead {
		r.generation++
	}
	accounts := []Account{}
	for _, account := range r.accounts {
		if !slices.Contains(platforms, account.Platform) || account.Status != StatusActive || !account.Schedulable {
			continue
		}
		if groupID != nil && !slices.Contains(account.GroupIDs, *groupID) {
			continue
		}
		if groupID == nil && !includeGrouped && len(account.GroupIDs) > 0 {
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}
func (r *visiblePolicyAccountRepo) GetModelCatalogAccount(_ context.Context, accountID int64) (*Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == accountID {
			return &r.accounts[index], nil
		}
	}
	return nil, ErrAccountNotFound
}
func (r *visiblePolicyAccountRepo) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return r.generation, r.generationErr
}

type visiblePolicyGroupRepo struct {
	GroupRepository
	groups []Group
}

func (r *visiblePolicyGroupRepo) GetByIDLite(_ context.Context, groupID int64) (*Group, error) {
	for index := range r.groups {
		if r.groups[index].ID == groupID {
			return &r.groups[index], nil
		}
	}
	return nil, nil
}
func (r *visiblePolicyGroupRepo) ListActive(context.Context) ([]Group, error) { return r.groups, nil }

type visiblePolicyChannelRepo struct {
	ChannelRepository
	channels []Channel
}

func (r *visiblePolicyChannelRepo) ListAll(context.Context) ([]Channel, error) {
	return r.channels, nil
}

func TestVisibleModelsKeyInheritanceIntersectionAndPublicAuthorization(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "deployment-private", "other")
	account.GroupIDs = []int64{9}
	account.Credentials["model_mapping"] = map[string]any{"public": "deployment-private", "other": "other", "missing": "unobserved"}
	group := Group{ID: 9, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public", "missing"}}}
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}, generation: 12}, groupRepo: &visiblePolicyGroupRepo{groups: []Group{group}}}
	for _, scenario := range []struct {
		name   string
		policy []string
		want   []string
	}{
		{name: "inherit", want: []string{"public"}},
		{name: "explicit empty", policy: []string{}, want: []string{}},
		{name: "intersection", policy: []string{"public", "other", "candidate-only"}, want: []string{"public"}},
		{name: "upstream is not authorization", policy: []string{"deployment-private"}, want: []string{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := WithVisibleModelKeyPolicy(context.Background(), scenario.policy)
			view, err := gateway.VisibleModels(ctx, &group.ID, group.Platform)
			require.NoError(t, err)
			require.Equal(t, scenario.want, view.Models)
			binding := ResolveAccountModelBinding(ctx, &account, "public", AccountModelBindingOptions{Endpoint: "responses"})
			require.Equal(t, len(scenario.want) > 0, binding.Supported)
			encoded, err := json.Marshal(view)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "deployment-private")
			if len(view.Models) > 0 {
				require.Equal(t, int64(7), view.Bindings["public"][0].SnapshotRevision)
				require.NotEmpty(t, view.Bindings["public"][0].EvidenceObservedAt)
			}
		})
	}
	policy := []string{"public"}
	ctx := WithVisibleModelKeyPolicy(context.Background(), policy)
	policy[0] = "other"
	require.True(t, visibleModelPolicyAllows(ctx, "public"), "request owns its policy copy")
}

func TestVisibleModelsWildcardBindingAndRevisionFence(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "deployment-private")
	account.Credentials["model_mapping"] = map[string]any{"public": "deployment-private", "tenant-*": "deployment-private"}
	repository := &visiblePolicyAccountRepo{accounts: []Account{account}, generation: 31}
	gateway := &GatewayService{accountRepo: repository}
	view, err := gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, view.Models)
	require.False(t, view.FullyEnumerable)
	require.True(t, ResolveAccountModelBinding(context.Background(), &account, "tenant-arbitrary", AccountModelBindingOptions{Endpoint: "responses"}).Supported)
	account.Credentials["model_mapping"] = map[string]any{"public": "absent"}
	require.False(t, ResolveAccountModelBinding(context.Background(), &account, "public", AccountModelBindingOptions{Endpoint: "responses"}).Supported)
	repository.changeDuringRead = true
	_, err = gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
	require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
	repository.generationErr = errors.New("authority unavailable")
	_, err = gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
	require.ErrorContains(t, err, "authority unavailable")
}

func TestVisibleModelInputMetadataMatchesFullProjection(t *testing.T) {
	for _, scenario := range []struct {
		platform string
		endpoint string
		protocol string
		action   string
	}{
		{PlatformOpenAI, "responses", "responses", "responses"},
		{PlatformMistral, "chat_completions", "chat_completions", "chat"},
		{PlatformOpenAI, "images", "images", "image.generate"},
		{PlatformOpenAI, "embeddings", "embeddings", "embeddings"},
		{PlatformOpenAI, "alpha_search", "search", ""},
		{PlatformGrok, string(OpenAIEndpointCapabilityGrokMediaGeneration), "media", "image.generate"},
	} {
		t.Run(scenario.platform+"/"+scenario.endpoint, func(t *testing.T) {
			account := catalogEvidenceAccount(1, scenario.platform, "deployment-private", "other")
			catalog, ok := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
			require.True(t, ok)
			for index := range catalog.EligibleModels {
				catalog.EligibleModels[index].Endpoints = []string{scenario.endpoint}
			}
			account.Credentials["model_mapping"] = map[string]any{"public": "deployment-private", "tenant-*": "deployment-private", "other": "other"}
			account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
				"deployment-private": {ProviderProfile: resolveCapabilityProfile(&account), MaxOutputTokens: 123},
			}})
			gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}, generation: 42}}
			for _, policy := range [][]string{nil, {}, {"public"}, {"deployment-private"}} {
				ctx := WithVisibleModelKeyPolicy(context.Background(), policy)
				ctx = WithGatewayRequestPlan(ctx, NewGatewayRequestPlan(GatewayRequestSpec{Model: "public", Protocol: scenario.protocol, Action: scenario.action}))
				_, full, err := gateway.VisibleModelsWithMetadata(ctx, nil, scenario.platform)
				require.NoError(t, err)
				if policy == nil {
					require.NotEmpty(t, full["public"], "fixture must exercise supported endpoint evidence")
				}
				for _, model := range []string{"public", "other", "deployment-private", "tenant-arbitrary", "absent", ""} {
					actual, err := gateway.visibleModelInputMetadata(ctx, nil, scenario.platform, model)
					require.NoError(t, err)
					require.Equal(t, full[model], actual, "policy=%v model=%s", policy, model)
				}
			}
		})
	}
}

func TestVisibleModelInputMetadataPreservesRevisionFence(t *testing.T) {
	for _, mode := range []string{"retry", "pre-frozen", "authenticated"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			repository := &visiblePolicyAccountRepo{accounts: []Account{catalogEvidenceAccount(1, PlatformOpenAI, "public")}, generation: 31, changeDuringRead: true}
			repository.assertScope = func(*int64, []string, bool) { reads++ }
			gateway := &GatewayService{accountRepo: repository}
			ctx := context.Background()
			if mode == "pre-frozen" {
				var err error
				ctx, err = (&VisibleModelResolver{gateway: gateway}).freeze(ctx)
				require.NoError(t, err)
			}
			if mode == "authenticated" {
				ctx = WithVisibleModelKeyPolicy(ctx, nil)
			}
			metadata, err := gateway.visibleModelInputMetadata(ctx, nil, PlatformOpenAI, "public")
			require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
			require.Nil(t, metadata, "never return partially resolved evidence")
			if mode == "retry" {
				require.Equal(t, visibleModelResolveMaxAttempts, reads)
			} else {
				require.Equal(t, 1, reads)
			}
		})
	}
}

func TestVisibleModelsDoNotPublishCandidatesOrUnsupportedActions(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformMistral, "native")
	snapshot, _ := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	snapshot.EligibleModels[0].Endpoints = []string{"images"}
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}}}
	view, err := gateway.VisibleModels(context.Background(), nil, PlatformMistral)
	require.NoError(t, err)
	require.Empty(t, view.Models)
	require.False(t, ResolveAccountModelBinding(context.Background(), &account, "native", AccountModelBindingOptions{Endpoint: "images"}).Supported)
	account.Extra = map[string]any{}
	account.Credentials["model_mapping"] = map[string]any{"gpt-5": "gpt-5"}
	gateway.accountRepo = &visiblePolicyAccountRepo{accounts: []Account{account}}
	view, err = gateway.VisibleModels(context.Background(), nil, PlatformMistral)
	require.NoError(t, err)
	require.Empty(t, view.Models)
}

func TestVisibleModelsPlazaChannelsAndListsSharePolicy(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "public", "disabled")
	account.GroupIDs = []int64{9}
	group := Group{ID: 9, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public"}}}
	accountRepo := &visiblePolicyAccountRepo{accounts: []Account{account}, generation: 19}
	groups := &visiblePolicyGroupRepo{groups: []Group{group}}
	channels := &visiblePolicyChannelRepo{channels: []Channel{{ID: 2, Name: "channel", Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"public", "disabled", "candidate"}}}}}}
	channelService := NewChannelService(channels, groups, nil, nil, nil)
	gateway := &GatewayService{accountRepo: accountRepo, groupRepo: groups}
	channelService.visibleModels = &VisibleModelResolver{gateway: gateway}
	plaza := NewModelPlazaService(channels, groups, nil, nil, NewModelPricingResolver(channelService, nil))
	for _, policy := range [][]string{nil, {}, {"public", "disabled"}} {
		ctx := WithVisibleModelKeyPolicy(context.Background(), policy)
		ids, err := gateway.CatalogModelIDs(ctx, &group.ID, group.Platform)
		require.NoError(t, err)
		available, err := channelService.ListAvailable(ctx)
		require.NoError(t, err)
		require.Len(t, available, 1)
		plazaGroups, err := plaza.ListGroups(ctx)
		require.NoError(t, err)
		channelNames := []string{}
		for _, model := range available[0].SupportedModels {
			channelNames = append(channelNames, model.Name)
		}
		plazaNames := []string{}
		for _, entry := range plazaGroups {
			for _, model := range entry.Models {
				plazaNames = append(plazaNames, model.Name)
			}
		}
		require.Equal(t, ids, channelNames)
		require.Equal(t, ids, plazaNames)
	}
	accountRepo.changeDuringRead = true
	_, err := channelService.ListAvailable(context.Background())
	require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
	_, err = plaza.ListGroups(context.Background())
	require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
}

type visiblePolicyRouteRepo struct {
	CompositeModelRouteRepository
	routes []CompositeModelRoute
}

func (r *visiblePolicyRouteRepo) ListByGroup(context.Context, int64, bool) ([]CompositeModelRoute, error) {
	return r.routes, nil
}

func TestVisibleModelsCompositeAliasRebindsPlatformAndEndpoint(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "private-deployment")
	account.GroupIDs = []int64{9}
	group := Group{ID: 9, Platform: PlatformComposite, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public", "unavailable", "wrong-platform", "wrong-action"}}}
	routes := &visiblePolicyRouteRepo{routes: []CompositeModelRoute{
		{PublicModel: "public", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "private-deployment", Endpoint: "responses", Enabled: true},
		{PublicModel: "unavailable", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "absent", Endpoint: "responses", Enabled: true},
		{PublicModel: "wrong-platform", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformAnthropic, UpstreamModel: "private-deployment", Endpoint: "messages", Enabled: true},
		{PublicModel: "wrong-action", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "private-deployment", Endpoint: "images", Enabled: true},
		{PublicModel: "tenant-", MatchType: CompositeRouteMatchPrefix, TargetPlatform: PlatformOpenAI, UpstreamModel: "private-deployment", Endpoint: "responses", Enabled: true},
	}}
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}}, groupRepo: &visiblePolicyGroupRepo{groups: []Group{group}}, compositeResolver: NewCompositeRouteResolver(routes)}
	view, err := gateway.VisibleModels(WithVisibleModelKeyPolicy(context.Background(), []string{"public", "wrong-platform", "wrong-action", "unavailable"}), &group.ID, PlatformComposite)
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, view.Models)
	require.False(t, view.FullyEnumerable)
	require.Equal(t, "private-deployment", view.Bindings["public"][0].UpstreamModel)
	require.Equal(t, CompositeRouteSourceExplicit, view.Bindings["public"][0].RouteSource)
}

func TestVisibleModelsDeclarationsRetainEvidenceAndRevoke(t *testing.T) {
	for _, kind := range []string{ModelEvidenceAdministrator, ModelEvidenceProbe} {
		t.Run(kind, func(t *testing.T) {
			expires := time.Now().Add(time.Hour)
			declaration := ModelAvailabilityDeclaration{AccountID: 1, Model: "private-deployment", Endpoint: "chat_completions", IdentityRevision: 3, RulesRevision: 4, ConfirmedBy: 2, ConfirmedAt: time.Now().Add(-time.Minute), Reason: "verified", EvidenceKind: kind, ExpiresAt: &expires}
			account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"public": "private-deployment"}}, Extra: map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{IdentityRevision: 3, RulesRevision: 4}, ModelAvailabilityExtraKey: []ModelAvailabilityDeclaration{declaration}}}
			repository := &visiblePolicyAccountRepo{accounts: []Account{account}, generation: 5}
			gateway := &GatewayService{accountRepo: repository}
			before, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			view, err := gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
			require.NoError(t, err)
			require.Equal(t, []string{"public"}, view.Models)
			binding := view.Bindings["public"][0]
			require.Equal(t, kind, binding.EvidenceKind)
			require.Equal(t, int64(3), binding.IdentityRevision)
			require.Equal(t, int64(4), binding.RulesRevision)
			after, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 4, RulesRevision: 4}
			view, err = gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
			require.NoError(t, err)
			require.Empty(t, view.Models)
		})
	}
}

func TestVisibleModelsFinalForwardUsesPublicKeyPolicy(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "private-deployment")
	account.Credentials["model_mapping"] = map[string]any{"public": "private-deployment"}
	repository := &visiblePolicyAccountRepo{accounts: []Account{account}}
	ctx := WithGatewayRequestPlan(WithVisibleModelKeyPolicy(context.Background(), []string{"public"}), NewGatewayRequestPlan(GatewayRequestSpec{Model: "public"}))
	require.NoError(t, validateAccountForwardModel(ctx, repository, &account, "private-deployment", "responses"))
	ctx = WithVisibleModelKeyPolicy(ctx, []string{"private-deployment"})
	require.ErrorIs(t, validateAccountForwardModel(ctx, repository, &account, "private-deployment", "responses"), ErrAccountModelUnsupported)
}

func TestVisibleModelsChannelAliasesNeverPublishPrivateTarget(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "private-deployment")
	account.GroupIDs = []int64{9}
	group := Group{ID: 9, Platform: PlatformOpenAI}
	groups := &visiblePolicyGroupRepo{groups: []Group{group}}
	channels := &visiblePolicyChannelRepo{channels: []Channel{{ID: 2, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelMapping: map[string]map[string]string{PlatformOpenAI: {"public": "private-deployment", "tenant-*": "private-deployment"}}}}}
	channelService := NewChannelService(channels, groups, nil, nil, nil)
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}}, groupRepo: groups, channelService: channelService}
	view, err := gateway.VisibleModels(context.Background(), &group.ID, group.Platform)
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, view.Models)
	require.False(t, view.FullyEnumerable)
	require.Equal(t, "private-deployment", view.Bindings["public"][0].UpstreamModel)
}

func (r *visiblePolicyChannelRepo) GetGroupPlatforms(_ context.Context, groupIDs []int64) (map[int64]string, error) {
	platforms := make(map[int64]string, len(groupIDs))
	for _, groupID := range groupIDs {
		platforms[groupID] = PlatformOpenAI
	}
	return platforms, nil
}

func TestVisibleModelsCatalogScope(t *testing.T) {
	groupID := int64(9)
	for _, platform := range []string{PlatformComposite, "", PlatformOpenAI, "unknown"} {
		for _, group := range []*int64{&groupID, nil} {
			for _, simple := range []bool{false, true} {
				t.Run(fmt.Sprintf("platform=%s/group=%v/simple=%t", platform, group, simple), func(t *testing.T) {
					accounts := []Account{
						catalogEvidenceAccount(1, PlatformOpenAI, "group-model"),
						catalogEvidenceAccount(2, PlatformOpenAI, "other-group-model"),
						catalogEvidenceAccount(3, PlatformOpenAI, "ungrouped-model"),
						catalogEvidenceAccount(4, PlatformMistral, "mistral-model"),
					}
					accounts[0].GroupIDs = []int64{groupID}
					accounts[1].GroupIDs = []int64{10}
					accounts[3].GroupIDs = []int64{groupID}
					reads := 0
					repo := &visiblePolicyAccountRepo{accounts: accounts, assertScope: func(gotGroup *int64, platforms []string, includeGrouped bool) {
						reads++
						require.Equal(t, group, gotGroup)
						wantPlatforms := domain.ConcretePlatforms()
						if platform == PlatformOpenAI {
							wantPlatforms = []string{PlatformOpenAI}
						}
						require.Equal(t, wantPlatforms, platforms)
						require.Equal(t, simple, includeGrouped)
					}}
					cfg := &config.Config{}
					if simple {
						cfg.RunMode = "simple"
					}
					routes := &visiblePolicyRouteRepo{routes: []CompositeModelRoute{
						{PublicModel: "group-model", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "group-model", Endpoint: "responses", Enabled: true},
						{PublicModel: "mistral-model", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformMistral, UpstreamModel: "mistral-model", Endpoint: "chat_completions", Enabled: true},
					}}
					gateway := &GatewayService{accountRepo: repo, cfg: cfg, compositeResolver: NewCompositeRouteResolver(routes)}
					view, err := gateway.VisibleModels(context.Background(), group, platform)
					require.NoError(t, err)
					want := []string{}
					switch {
					case platform == "unknown":
						require.Zero(t, reads, "unknown platforms must not query accounts")
					case platform == PlatformComposite && group == nil:
						// Composite routes require a group even in simple mode.
					case group != nil:
						want = []string{"group-model"}
						if platform != PlatformOpenAI {
							want = append(want, "mistral-model")
						}
					case simple:
						want = []string{"group-model", "other-group-model", "ungrouped-model"}
						if platform != PlatformOpenAI {
							want = append(want, "mistral-model")
						}
					default:
						want = []string{"ungrouped-model"}
					}
					if platform != "unknown" {
						require.Equal(t, 1, reads)
					}
					require.ElementsMatch(t, want, view.Models)
				})
			}
		}
	}
}

func TestVisibleModelsPanelCatalogScope(t *testing.T) {
	for _, platform := range []string{PlatformComposite, ""} {
		for _, simple := range []bool{false, true} {
			t.Run(fmt.Sprintf("platform=%s/simple=%t", platform, simple), func(t *testing.T) {
				group := Group{ID: 9, Platform: platform}
				account := catalogEvidenceAccount(1, PlatformOpenAI, "public")
				account.GroupIDs = []int64{group.ID}
				outsider := catalogEvidenceAccount(2, PlatformOpenAI, "outside")
				outsider.GroupIDs = []int64{10}
				reads := 0
				repo := &visiblePolicyAccountRepo{accounts: []Account{account, outsider}, assertScope: func(groupID *int64, platforms []string, includeGrouped bool) {
					reads++
					require.Equal(t, &group.ID, groupID)
					require.Equal(t, domain.ConcretePlatforms(), platforms)
					require.Equal(t, simple, includeGrouped)
				}}
				groups := &visiblePolicyGroupRepo{groups: []Group{group}}
				channels := &visiblePolicyChannelRepo{channels: []Channel{{ID: 2, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"public", "outside"}}}}}}
				channelService := NewChannelService(channels, groups, nil, nil, nil)
				cfg := &config.Config{}
				if simple {
					cfg.RunMode = "simple"
				}
				routes := &visiblePolicyRouteRepo{routes: []CompositeModelRoute{{PublicModel: "public", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "public", Endpoint: "responses", Enabled: true}}}
				gateway := &GatewayService{accountRepo: repo, groupRepo: groups, cfg: cfg, compositeResolver: NewCompositeRouteResolver(routes)}
				channelService.visibleModels = &VisibleModelResolver{gateway: gateway}
				plaza := NewModelPlazaService(channels, groups, nil, nil, NewModelPricingResolver(channelService, nil))
				available, err := channelService.ListAvailable(context.Background())
				require.NoError(t, err)
				require.Equal(t, 1, reads)
				require.Len(t, available, 1)
				plazaGroups, err := plaza.ListGroups(context.Background())
				require.NoError(t, err)
				require.Equal(t, 2, reads)
				channelNames, plazaNames := []string{}, []string{}
				for _, model := range available[0].SupportedModels {
					channelNames = append(channelNames, model.Name)
				}
				for _, entry := range plazaGroups {
					for _, model := range entry.Models {
						plazaNames = append(plazaNames, model.Name)
					}
				}
				if platform == PlatformComposite {
					require.Equal(t, []string{"public"}, channelNames)
					require.Equal(t, []string{"public"}, plazaNames)
				} else {
					// Panel platform matching still rejects an unconfigured group platform.
					require.Empty(t, channelNames)
					require.Empty(t, plazaNames)
				}
				groups.groups = nil
				_, err = channelService.ListAvailable(context.Background())
				require.NoError(t, err)
				_, err = plaza.ListGroups(context.Background())
				require.NoError(t, err)
				require.Equal(t, 2, reads, "panels without groups must not query ungrouped accounts")
			})
		}
	}
}
