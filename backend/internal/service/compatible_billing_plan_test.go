//go:build unit

package service

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCompatibleBillingPlanValidationAndTransport(t *testing.T) {
	for _, scenario := range []struct{ platform, plan string }{
		{"aicodemirror", "pro"}, {"ccsub", "subscription"}, {"shengsuanyun", "enterprise"}, {"astron_token", "premium"},
	} {
		t.Run(scenario.platform, func(t *testing.T) {
			provider, _ := domain.LookupCompatibleProvider(scenario.platform)
			credentials := map[string]any{"api_key": "fixture-key", "account_mode": provider.DefaultMode, "api_protocol": "adaptive", "billing_plan": scenario.plan, "model_mapping": map[string]any{"alias": "manual-model"}}
			account, err := buildAccountForCreate(&CreateAccountInput{Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: credentials}, nil)
			require.NoError(t, err)
			require.Equal(t, scenario.plan, account.Credentials["billing_plan"])
			legacy := *account
			legacy.Credentials = maps.Clone(credentials)
			delete(legacy.Credentials, "billing_plan")
			require.NoError(t, validateCompatibleAccount(&legacy))
			mode, _ := provider.ResolveMode("")
			for _, endpoint := range mode.Endpoints {
				plannedBase, err := account.compatibleBase(endpoint)
				require.NoError(t, err)
				legacyBase, err := legacy.compatibleBase(endpoint)
				require.NoError(t, err)
				require.Equal(t, legacyBase, plannedBase)
			}
			inline, secret := SplitCredentialSecrets(credentials)
			require.Equal(t, scenario.plan, inline["billing_plan"])
			require.NotContains(t, secret, "billing_plan")
			require.Equal(t, "fixture-key", secret["api_key"])
			for _, malformed := range []any{"unknown", "PRO", 1, map[string]any{}} {
				bad := maps.Clone(credentials)
				bad["billing_plan"] = malformed
				_, err := buildAccountForCreate(&CreateAccountInput{Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: bad}, nil)
				require.Error(t, err, "create/import service boundary")
				repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
				_, err = (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{account.ID}, Credentials: map[string]any{"billing_plan": malformed}})
				require.Error(t, err)
				require.Zero(t, repo.bulkUpdateCalls)
			}
			for _, clear := range []any{nil, ""} {
				cleared := maps.Clone(credentials)
				cleared["billing_plan"] = clear
				SanitizeStoredCredentials(scenario.platform, cleared)
				require.NotContains(t, cleared, "billing_plan")
			}
			for _, exported := range []map[string]any{credentials, legacy.Credentials} {
				encoded, err := json.Marshal(exported)
				require.NoError(t, err)
				var imported map[string]any
				require.NoError(t, json.Unmarshal(encoded, &imported))
				imported[CredentialSecretReferenceKey] = "local-reference"
				imported[CredentialSecretVersionKey] = 3
				StripCredentialReferenceKeys(imported)
				require.Equal(t, exported, imported)
				_, err = buildAccountForCreate(&CreateAccountInput{Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: imported}, nil)
				require.NoError(t, err)
			}
		})
	}
	for _, scenario := range []struct{ platform, mode string }{{"atlascloud", "coding"}, {"qiniu", "subscription"}} {
		account := &Account{Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "account_mode": scenario.mode}}
		require.NoError(t, validateCompatibleAccount(account))
		account.Credentials["billing_plan"] = "payg"
		require.Error(t, validateCompatibleAccount(account))
	}
}

func TestPaygBalanceAllowed(t *testing.T) {
	for _, provider := range domain.CompatibleProviders() {
		for _, plan := range provider.Plans {
			allowed, err := PaygBalanceAllowed(provider.ID, map[string]any{"billing_plan": plan.ID})
			require.NoError(t, err)
			require.Equal(t, plan.Query == "payg", allowed, provider.ID+"/"+plan.ID)
		}
		for _, credentials := range []map[string]any{nil, {}, {"billing_plan": nil}, {"billing_plan": ""}} {
			allowed, err := PaygBalanceAllowed(provider.ID, credentials)
			require.NoError(t, err)
			require.True(t, allowed)
		}
		for _, bad := range []any{"unknown", "PRO", 1, map[string]any{}} {
			allowed, err := PaygBalanceAllowed(provider.ID, map[string]any{"billing_plan": bad})
			require.Error(t, err)
			require.False(t, allowed)
		}
	}
}

func TestCompatibleBillingPlanServicePersistence(t *testing.T) {
	for _, platform := range []string{"aicodemirror", "ccsub"} {
		plan := "pro"
		if platform == "ccsub" {
			plan = "subscription"
		}
		repository := &accountRepoStubForBulkUpdate{createID: 12}
		admin := &adminServiceImpl{accountRepo: repository}
		created, err := admin.CreateAccount(context.Background(), &CreateAccountInput{
			Name: "fixture", Platform: platform, Type: AccountTypeAPIKey, SkipDefaultGroupBind: true,
			Credentials: map[string]any{"api_key": "fixture-key", "billing_plan": plan, "account_mode": "payg", "api_protocol": "adaptive", "base_url": "https://relay.example/v1"},
		})
		require.NoError(t, err)
		require.Equal(t, plan, repository.createAccount.Credentials["billing_plan"])
		repository.getByIDAccounts = map[int64]*Account{created.ID: created}
		for _, clear := range []any{nil, ""} {
			patch := maps.Clone(created.Credentials)
			patch["billing_plan"] = clear
			updated, err := admin.UpdateAccount(context.Background(), created.ID, &UpdateAccountInput{Credentials: patch})
			require.NoError(t, err)
			require.NotContains(t, updated.Credentials, "billing_plan")
			require.NotContains(t, repository.updatedAccounts[len(repository.updatedAccounts)-1].Credentials, "billing_plan")
			require.Equal(t, "fixture-key", updated.Credentials["api_key"])
			require.Equal(t, "https://relay.example/v1", updated.Credentials["base_url"])
			created.Credentials["billing_plan"] = plan
		}
		repository.getByIDsAccounts = []*Account{created}
		_, err = admin.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{created.ID}, Credentials: map[string]any{"billing_plan": nil}})
		require.NoError(t, err)
		require.Contains(t, repository.lastBulkUpdate.Credentials, "billing_plan", "bulk deletion marker must reach the atomic JSONB merge")
		require.Nil(t, repository.lastBulkUpdate.Credentials["billing_plan"])
		clearedCreate, err := admin.CreateAccount(context.Background(), &CreateAccountInput{
			Name: "unset-fixture", Platform: platform, Type: AccountTypeAPIKey, SkipDefaultGroupBind: true,
			Credentials: map[string]any{"api_key": "fixture-key", "billing_plan": nil},
		})
		require.NoError(t, err)
		require.NotContains(t, clearedCreate.Credentials, "billing_plan")
	}
}
