package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type visiblePolicyAccountRepo struct {
	AccountRepository
	accounts         []Account
	generation       int64
	changeDuringRead bool
	generationErr    error
}

func (r *visiblePolicyAccountRepo) ListModelCatalogAccounts(context.Context, *int64, []string, bool) ([]Account, error) {
	if r.changeDuringRead {
		r.generation++
	}
	return append([]Account(nil), r.accounts...), nil
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
