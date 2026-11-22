//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type w05PlazaBatchAccounts struct{ *visiblePolicyAccountRepo }

func (r *w05PlazaBatchAccounts) ListModelCatalogAccountsForGroups(context.Context, []int64) ([]Account, error) {
	return r.accounts, nil
}

func w05Plaza(t *testing.T, group Group, accounts []Account, channels []Channel, routes []CompositeModelRoute) *ModelPlazaService {
	t.Helper()
	groups := &visiblePolicyGroupRepo{groups: []Group{group}}
	channelRepo := &visiblePolicyChannelRepo{channels: channels}
	cs := NewChannelService(channelRepo, groups, nil, nil, nil)
	gateway := &GatewayService{accountRepo: &w05PlazaBatchAccounts{&visiblePolicyAccountRepo{accounts: accounts}}, groupRepo: groups}
	if routes != nil {
		gateway.compositeResolver = NewCompositeRouteResolver(&visiblePolicyRouteRepo{routes: routes})
	}
	cs.visibleModels = &VisibleModelResolver{gateway: gateway}
	return NewModelPlazaService(channelRepo, groups, nil, nil, NewModelPricingResolver(cs, nil))
}
func TestW05PlazaHostedProductPlatformBinding(t *testing.T) {
	for _, tc := range []struct{ platform, kind, legacy string }{{PlatformAnthropic, AccountTypeBedrock, PlatformBedrock}, {PlatformGemini, AccountTypeServiceAccount, ""}} {
		platforms := []string{tc.platform}
		if tc.legacy != "" {
			platforms = append(platforms, tc.legacy)
		}
		for _, groupPlatform := range platforms {
			group := Group{ID: 9, Platform: groupPlatform, AccountType: tc.kind}
			model := "hosted-live"
			if tc.kind == AccountTypeBedrock {
				model = "us.anthropic.claude-sonnet-4-6"
			}
			account := catalogEvidenceAccount(1, tc.platform, model)
			snapshot, ok := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
			require.True(t, ok)
			if tc.kind == AccountTypeServiceAccount {
				snapshot.EligibleModels[0].Endpoints = []string{"generate_content"}
			}
			account.Type = tc.kind
			account.Credentials["model_mapping"] = map[string]any{"hosted-live": model}
			account.GroupIDs = []int64{9}
			direct := catalogEvidenceAccount(2, tc.platform, "direct-only")
			direct.Credentials["model_mapping"] = map[string]any{"direct-only": "direct-only", "hosted-live": "direct-only"}
			direct.GroupIDs = []int64{9}
			if tc.kind == AccountTypeServiceAccount {
				directSnapshot, ok := direct.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
				require.True(t, ok)
				directSnapshot.EligibleModels[0].Endpoints = []string{"generate_content"}
			}
			if tc.kind == AccountTypeBedrock {
				account.Credentials["auth_mode"] = "api_key"
				account.Credentials["api_key"] = "bedrock-key"
				account.Credentials["region"] = "us-east-1"
			}
			channel := plazaPricedChannel(2, "published", []int64{9}, tc.platform, "hosted-live", "direct-only")
			plaza := w05Plaza(t, group, []Account{account, direct}, []Channel{channel}, nil)
			out, err := plaza.ListGroups(context.Background())
			require.NoError(t, err)
			require.Len(t, out, 1, "%s/%s", tc.platform, groupPlatform)
			require.Len(t, out[0].Models, 1)
			require.Equal(t, "hosted-live", out[0].Models[0].Name)
			require.False(t, out[0].Models[0].CatalogOnly)
			require.NotNil(t, out[0].Models[0].Pricing)
			require.NotEmpty(t, out[0].Models[0].CatalogBindings)
			for _, binding := range out[0].Models[0].CatalogBindings {
				require.Equal(t, account.ID, binding.AccountID, "a shared alias cannot import a direct account binding")
				require.Equal(t, tc.kind, binding.Identity.HostedAccountType)
			}
			// A same-product account with another credential type cannot satisfy the
			// hosting restriction even though the public model platform is identical.
			account.Type = AccountTypeAPIKey
			plaza = w05Plaza(t, group, []Account{account}, []Channel{channel}, nil)
			out, err = plaza.ListGroups(context.Background())
			require.NoError(t, err)
			require.Empty(t, out)
		}
	}
}
func TestW05PlazaCompositeSameNameRequiresProviderBinding(t *testing.T) {
	group := Group{ID: 9, Platform: PlatformComposite}
	account := catalogEvidenceAccount(1, PlatformOpenAI, "shared")
	account.GroupIDs = []int64{9}
	channels := []Channel{plazaPricedChannel(2, "openai", []int64{9}, PlatformOpenAI, "shared"), plazaPricedChannel(3, "grok", []int64{9}, PlatformGrok, "shared")}
	routes := []CompositeModelRoute{{PublicModel: "shared", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "shared", Endpoint: "responses", Enabled: true}}
	plaza := w05Plaza(t, group, []Account{account}, channels, routes)
	out, err := plaza.ListPublicCatalogGroups(context.Background())
	require.NoError(t, err)
	models := catalogGroupModels(out)[9]
	require.Contains(t, models, PlazaModelIdentity{PlatformOpenAI, "shared"})
	require.False(t, models[PlazaModelIdentity{PlatformOpenAI, "shared"}].CatalogOnly)
	require.Contains(t, models, PlazaModelIdentity{PlatformGrok, "shared"})
	require.True(t, models[PlazaModelIdentity{PlatformGrok, "shared"}].CatalogOnly)
}
