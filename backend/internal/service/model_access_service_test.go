package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelAccessNullEmptyAndNamedIntersection(t *testing.T) {
	identifier := int64(8)
	for _, scenario := range []struct {
		name    string
		encoded string
		want    []string
	}{
		{"inherit", `{"models":null}`, nil},
		{"deny", `{"models":[]}`, []string{}},
		{"limit", `{"models":["public"]}`, []string{"public"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var rule ModelAccessRule
			require.NoError(t, json.Unmarshal([]byte(scenario.encoded), &rule))
			encoded, err := json.Marshal(rule)
			require.NoError(t, err)
			var restored ModelAccessRule
			require.NoError(t, json.Unmarshal(encoded, &restored))
			require.Equal(t, scenario.want, ResolveModelAccessRule(restored, nil))
		})
	}
	require.Equal(t, []string{}, ResolveModelAccessRule(ModelAccessRule{AccessGroupID: &identifier, Models: []string{}}, []string{"public"}))
	require.Equal(t, []string{"public"}, ResolveModelAccessRule(ModelAccessRule{AccessGroupID: &identifier, Models: []string{"public", "private"}}, []string{"public"}))
	ctx := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{KeyExclude: []string{"private"}, AccountExcludes: map[int64][]string{1: {"private"}}})
	require.True(t, visibleModelPolicyAllows(ctx, "public"))
	require.False(t, visibleModelPolicyAllows(ctx, "private"))
	require.False(t, modelAccessAccountAllows(ctx, 1, "private"))
}

func TestModelAccessAccountAndKeySharePublicNameBoundary(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "deployment")
	account.Credentials["model_mapping"] = map[string]any{"public": "deployment"}
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}, generation: 3}}
	for _, scenario := range []struct {
		name    string
		key     []string
		account []string
		allowed bool
	}{
		{"inherit", nil, nil, true}, {"key empty", []string{}, nil, false}, {"account empty", nil, []string{}, false},
		{"both public", []string{"public"}, []string{"public"}, true}, {"key wire name", []string{"deployment"}, nil, false},
		{"account wire name", nil, []string{"deployment"}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{Key: scenario.key, Accounts: map[int64][]string{1: scenario.account}})
			view, err := gateway.VisibleModels(ctx, nil, PlatformOpenAI)
			require.NoError(t, err)
			require.Equal(t, scenario.allowed, len(view.Models) > 0)
			for _, endpoint := range []string{"responses", "chat_completions"} {
				binding := ResolveAccountModelBinding(ctx, &account, "public", AccountModelBindingOptions{Endpoint: endpoint})
				require.Equal(t, scenario.allowed, binding.Supported, endpoint)
			}
		})
	}
	ctx := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{Generation: 2, RevisionKnown: true})
	_, err := gateway.VisibleModels(ctx, nil, PlatformOpenAI)
	require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
}

func TestModelAccessCompositeAccountPolicyUsesAlias(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "deployment")
	account.GroupIDs = []int64{9}
	group := Group{ID: 9, Platform: PlatformComposite, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public"}}}
	routes := &visiblePolicyRouteRepo{routes: []CompositeModelRoute{{PublicModel: "public", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "deployment", Endpoint: "responses", Enabled: true}}}
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}}, groupRepo: &visiblePolicyGroupRepo{groups: []Group{group}}, compositeResolver: NewCompositeRouteResolver(routes)}
	for _, policy := range [][]string{nil, {}, {"public"}, {"deployment"}} {
		ctx := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{Key: policy, Accounts: map[int64][]string{account.ID: policy}})
		view, err := gateway.VisibleModels(ctx, &group.ID, PlatformComposite)
		require.NoError(t, err)
		allowed := policy == nil || len(policy) == 1 && policy[0] == "public"
		require.Equal(t, allowed, len(view.Models) > 0)
	}
}

type modelAccessJobRepository struct {
	UpstreamJobRepository
	job *UpstreamJob
}

func (r *modelAccessJobRepository) Get(context.Context, string, int64, int64) (*UpstreamJob, error) {
	return r.job, nil
}
func TestModelAccessTaskReadsRecheckPublicPolicy(t *testing.T) {
	job := &UpstreamJob{AccountID: 1, Billing: UpstreamJobBilling{RequestedModel: "public", Model: "deployment"}}
	jobs := &UpstreamJobService{repo: &modelAccessJobRepository{job: job}}
	for _, policy := range [][]string{nil, {}, {"public"}, {"deployment"}} {
		ctx := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{Key: policy})
		_, err := jobs.Get(ctx, "job", 1, 1)
		if policy == nil || len(policy) == 1 && policy[0] == "public" {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
		}
	}
	ctx := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{Accounts: map[int64][]string{1: {}}})
	_, err := jobs.Get(ctx, "job", 1, 1)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
}
