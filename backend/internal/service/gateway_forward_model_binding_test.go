package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type forwardCatalogRepo struct {
	AccountRepository
	current     *Account
	parent      *Account
	err         error
	reads       int
	assertScope func(*int64, []string, bool)
}

func TestCatalogSelectionHydratesSchedulerProjection(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformKimi, PlatformDeepseek, PlatformQianwen, PlatformGrok, PlatformCommandCode} {
		t.Run(platform, func(t *testing.T) {
			full := legacyCatalogSeedAccount(platform)
			if platform == PlatformGrok {
				full = catalogEvidenceAccount(51, platform, "actual-model")
				full.Credentials["model_mapping"] = map[string]any{"public-model": "actual-model"}
			}
			full.Extra["openai_responses_mode"] = "chat"
			projected := full
			projected.Extra = map[string]any{"openai_responses_mode": "chat"}
			repo := &forwardCatalogRepo{current: &full}
			plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "public-model", Protocol: "chat_completions"})
			plan.catalogRepo = repo
			ctx := WithGatewayRequestPlan(context.Background(), plan)
			gateway := &OpenAIGatewayService{accountRepo: repo}
			require.True(t, gateway.supportsOpenAIAccountModel(ctx, &full, "public-model", OpenAIEndpointCapabilityChatCompletions))
			require.Zero(t, repo.reads, "complete evidence needs no fallback read")
			require.True(t, gateway.supportsOpenAIAccountModel(ctx, &projected, "public-model", OpenAIEndpointCapabilityChatCompletions))
			require.Empty(t, openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, &projected, platform, "public-model", false, OpenAIEndpointCapabilityChatCompletions))
			require.Equal(t, 2, repo.reads, "advanced and legacy selectors both read complete evidence")
			require.NotContains(t, projected.Extra, UpstreamModelCatalogExtraKey, "do not mutate shared scheduler metadata")
			require.False(t, gateway.supportsOpenAIAccountModel(ctx, &projected, "unseeded", OpenAIEndpointCapabilityChatCompletions))
		})
	}
}

func TestCatalogSelectionHydrationPreservesDenials(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account, *forwardCatalogRepo)
	}{
		{"read error", func(_ *Account, r *forwardCatalogRepo) { r.err = errors.New("unavailable") }},
		{"deleted", func(_ *Account, r *forwardCatalogRepo) { r.current = nil }},
		{"disabled", func(a *Account, _ *forwardCatalogRepo) { a.Schedulable = false }},
		{"identity revoked", func(a *Account, _ *forwardCatalogRepo) {
			a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		}},
		{"rules revoked", func(a *Account, _ *forwardCatalogRepo) {
			a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
		}},
		{"allowlist", func(a *Account, _ *forwardCatalogRepo) {
			a.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"other"}}
		}},
		{"no evidence", func(a *Account, _ *forwardCatalogRepo) { a.Extra = nil }},
		{"platform changed", func(a *Account, _ *forwardCatalogRepo) { a.Platform = PlatformGrok }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			full := legacyCatalogSeedAccount(PlatformDeepseek)
			projected := full
			projected.Extra = nil
			repo := &forwardCatalogRepo{current: &full}
			scenario.change(&full, repo)
			plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "public-model", Protocol: "chat_completions"})
			plan.catalogRepo = repo
			ctx := WithGatewayRequestPlan(context.Background(), plan)
			require.False(t, (&OpenAIGatewayService{accountRepo: repo}).supportsOpenAIAccountModel(ctx, &projected, "public-model", OpenAIEndpointCapabilityChatCompletions))
			require.False(t, requestAccountCatalogSupports(ctx, &projected, "public-model", "chat_completions"))
		})
	}
}

func TestRequestPlanMediaUsesActualCapabilityAndPreservesTextBridge(t *testing.T) {
	for _, scenario := range []struct{ protocol, platform, endpoint string }{
		{"images", PlatformOpenAI, "images"},
		{"media", PlatformOpenAI, "images"},
		{"media", PlatformGrok, string(OpenAIEndpointCapabilityGrokMediaGeneration)},
		{"embeddings", PlatformOpenAI, "embeddings"},
		{"search", PlatformOpenAI, "alpha_search"},
		{"live", PlatformOpenAI, "live"},
		{"chat_completions", PlatformOpenAI, "responses"},
	} {
		t.Run(scenario.platform+"/"+scenario.protocol, func(t *testing.T) {
			account := catalogEvidenceAccount(1, scenario.platform, "fixture")
			snapshot, _ := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
			snapshot.EligibleModels[0].Endpoints = []string{scenario.endpoint}
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Protocol: scenario.protocol, Model: "fixture"}))
			require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "fixture", OpenAIEndpointCapabilityChatCompletions))
			snapshot.EligibleModels[0].Endpoints = []string{"unrelated"}
			require.False(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "fixture", OpenAIEndpointCapabilityChatCompletions))
		})
	}
}

func (r *forwardCatalogRepo) GetModelCatalogAccount(_ context.Context, id int64) (*Account, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	if r.parent != nil && r.parent.ID == id {
		return r.parent, nil
	}
	return r.current, nil
}
func (r *forwardCatalogRepo) ListModelCatalogAccounts(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]Account, error) {
	if r.assertScope != nil {
		r.assertScope(groupID, platforms, includeGrouped)
	}
	if r.err != nil {
		return nil, r.err
	}
	return []Account{*r.current}, nil
}

func TestForwardModelFinalBindingAndRevocation(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Credentials["model_mapping"] = map[string]any{"alias": "native", "native": "wrong"}
	current := account
	repo := &forwardCatalogRepo{current: &current}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "alias"})
	ctx := WithGatewayRequestPlan(WithAffinityBindingState(context.Background()), plan)
	admitted := 0
	deferGatewayRequestBinding(ctx, 1, func(ctx context.Context) error { require.True(t, AffinityAdmitted(ctx)); admitted++; return nil })
	require.NoError(t, validateAccountForwardModel(ctx, repo, &account, "native", "responses"))
	require.Equal(t, 1, admitted, "final model must not be remapped to wrong")
	current.Extra = map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{IdentityRevision: 2}}
	require.ErrorIs(t, validateAccountForwardModel(ctx, repo, &account, "native", "responses"), ErrAccountModelUnsupported)
	require.Equal(t, 1, admitted, "revoked identity never admits or renews affinity")
	repo.err = errors.New("catalog unavailable")
	require.ErrorContains(t, validateAccountForwardModel(ctx, repo, &account, "native", "responses"), "catalog unavailable")
}
func TestForwardModelRequestBodyPreservedAndEndpointExact(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	repo := &forwardCatalogRepo{current: &account}
	body := `{"model":"native","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"function_call_output","call_id":"call_original","output":"ok"}]}`
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/responses", strings.NewReader(body))
	require.NoError(t, err)
	require.NoError(t, validateOpenAIForwardRequest(repo, &account, req))
	sent, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(sent))
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/embeddings", strings.NewReader(body))
	require.NoError(t, err)
	require.ErrorIs(t, validateOpenAIForwardRequest(repo, &account, req), ErrAccountModelUnsupported)
}
func TestForwardModelMissingCatalogNeverAdmits(t *testing.T) {
	account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{}))
	admitted := false
	deferGatewayRequestBinding(ctx, 1, func(context.Context) error { admitted = true; return nil })
	require.ErrorIs(t, validateAccountForwardModel(ctx, &forwardCatalogRepo{current: &account}, &account, "gpt-5.1", "responses"), ErrAccountModelUnsupported)
	require.False(t, admitted)
}
func TestForwardModelPublicCatalogUsesSameEligibility(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Credentials["model_mapping"] = map[string]any{"alias": "native", "unsupported": "missing"}
	repo := &forwardCatalogRepo{current: &account, assertScope: func(groupID *int64, platforms []string, includeGrouped bool) {
		require.Nil(t, groupID)
		require.Equal(t, []string{PlatformOpenAI}, platforms)
		require.False(t, includeGrouped)
	}}
	svc := &GatewayService{accountRepo: repo}
	ids, err := svc.CatalogModelIDs(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Contains(t, ids, "alias")
	require.NotContains(t, ids, "unsupported")
	account.Extra = nil
	ids, err = svc.CatalogModelIDs(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.NotNil(t, ids)
	require.Empty(t, ids)
}

func (r *forwardCatalogRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.GetModelCatalogAccount(ctx, id)
}
func TestAffinityRequestPlanPreservesTransientBinding(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "m")
	repo := &forwardCatalogRepo{current: &account}
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "m", Protocol: "responses"}))
	later := time.Now().Add(time.Hour)
	account.RateLimitResetAt = &later
	account.OverloadUntil = &later
	account.TempUnschedulableUntil = &later
	require.True(t, shouldClearStickySession(&account, "m"), "legacy semantics remain unchanged")
	require.False(t, requestAffinityPermanentlyInvalid(ctx, repo, 1), "429/timeout must preserve recovery binding")
	account.Schedulable = false
	require.True(t, requestAffinityPermanentlyInvalid(ctx, repo, 1))
	account.Schedulable = true
	account.Extra = nil
	require.True(t, requestAffinityPermanentlyInvalid(ctx, repo, 1), "revoked evidence is permanent")
	repo.err = errors.New("database unavailable")
	require.False(t, requestAffinityPermanentlyInvalid(ctx, repo, 1), "read failure cannot establish revocation")
}

func TestForwardModelPublicCompositeAliasesUseExistingResolver(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "gpt-5.1")
	repo := &forwardCatalogRepo{current: &account, assertScope: func(groupID *int64, platforms []string, includeGrouped bool) {
		require.NotNil(t, groupID)
		require.Equal(t, int64(3), *groupID)
		require.Equal(t, domain.ConcretePlatforms(), platforms)
		require.NotContains(t, platforms, PlatformComposite)
		require.False(t, includeGrouped)
	}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 3, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: "company", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5.1", Endpoint: CompositeRouteEndpointAny}}}
	resolver := NewCompositeRouteResolver(routes)
	resolver.SetModelOwnershipResolver(func(context.Context, int64, string) (CompositeModelOwnership, error) {
		return CompositeModelOwnership{Ambiguous: true}, nil
	})
	svc := &GatewayService{accountRepo: repo, compositeResolver: resolver}
	group := int64(3)
	ids, err := svc.CatalogModelIDs(context.Background(), &group, PlatformComposite)
	require.NoError(t, err)
	require.Equal(t, []string{"company"}, ids, "explicit route wins; ambiguous implicit model remains hidden")
}

func TestForwardModelPublicCatalogAllPlatformsScope(t *testing.T) {
	for _, simple := range []bool{false, true} {
		t.Run(fmt.Sprint(simple), func(t *testing.T) {
			account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
			repo := &forwardCatalogRepo{current: &account, assertScope: func(groupID *int64, platforms []string, includeGrouped bool) {
				require.Nil(t, groupID)
				require.Equal(t, domain.ConcretePlatforms(), platforms)
				require.NotContains(t, platforms, PlatformComposite)
				require.Equal(t, simple, includeGrouped)
			}}
			cfg := &config.Config{}
			if simple {
				cfg.RunMode = "simple"
			}
			svc := &GatewayService{accountRepo: repo, cfg: cfg}
			ids, err := svc.CatalogModelIDs(context.Background(), nil, "")
			require.NoError(t, err)
			require.Equal(t, []string{"native"}, ids)
		})
	}
}

func TestForwardBoundaryRejectsDuplicateModelAfterMapping(t *testing.T) {
	for _, endpoint := range []string{"chat/completions", "responses", "messages"} {
		for _, mapped := range []string{"allowed", "mapped-allowed"} {
			t.Run(endpoint+"/"+mapped, func(t *testing.T) {
				account := catalogEvidenceAccount(1, PlatformOpenAI, mapped)
				repo := &forwardCatalogRepo{current: &account}
				ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{}))
				body := ReplaceModelInBody([]byte(`{"model":"allowed","model":"revoked"}`), mapped)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/"+endpoint, strings.NewReader(string(body)))
				require.NoError(t, err)
				require.ErrorContains(t, validateOpenAIForwardRequest(repo, &account, req), "duplicate")
				require.Zero(t, repo.reads)
			})
		}
	}
}
