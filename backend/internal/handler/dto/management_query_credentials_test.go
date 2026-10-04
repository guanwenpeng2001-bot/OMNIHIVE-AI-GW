package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestManagementQueryCredentialsRedactPreserveAndClear(t *testing.T) {
	stored := map[string]any{"api_key": "inference", "tencent_query_region": "ap-shanghai", "tencent_query_team_id": "team", "usage_query_user_id": "user"}
	fields := []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token"}
	for _, field := range fields {
		stored[field] = "query-secret"
	}
	visible, status := RedactCredentials(stored)
	for _, field := range fields {
		require.NotContains(t, visible, field)
		require.True(t, status["has_"+field])
	}
	encoded, err := json.Marshal(visible)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "query-secret")
	for _, field := range []string{"tencent_query_region", "tencent_query_team_id", "usage_query_user_id"} {
		require.Equal(t, stored[field], visible[field])
	}
	merged := service.MergePreservingSensitiveCreds(stored, visible)
	for _, field := range fields {
		require.Equal(t, "query-secret", merged[field])
		visible[field] = " "
	}
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	for _, field := range fields {
		require.Equal(t, "query-secret", merged[field])
		visible[field] = nil
	}
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	_, status = RedactCredentials(merged)
	for _, field := range fields {
		require.Nil(t, merged[field])
		require.False(t, status["has_"+field])
	}
	require.Equal(t, "inference", merged["api_key"])
}

func TestIntegratedAdminDTOQuerySecretsAndModelMapping(t *testing.T) {
	fields := []string{"api_key", "volcengine_access_key_id", "volcengine_secret_access_key", "tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"}
	for _, platform := range []string{"volcengine", "hunyuan", "azure_openai", "github_copilot", "antigravity", "byteplus"} {
		t.Run(platform, func(t *testing.T) {
			account := &service.Account{ID: 17, Platform: platform, Type: "apikey", Credentials: map[string]any{"model_mapping": map[string]any{"alias": "exact-model", "exact-model": "exact-model"}, "usage_query_subscription_id": "subscription", "usage_query_region": "eastus", "usage_query_username": "octocat"}}
			for _, field := range fields {
				account.Credentials[field] = "fixture-" + field
			}
			detail := AccountFromService(account)
			for _, projection := range []any{detail, AccountListItemFromAccount(detail)} {
				encoded, err := json.Marshal(projection)
				require.NoError(t, err)
				for _, field := range fields {
					require.NotContains(t, string(encoded), "fixture-"+field)
				}
				require.Contains(t, string(encoded), `"model_mapping":{"alias":"exact-model","exact-model":"exact-model"}`)
				require.Contains(t, string(encoded), `"has_usage_query_token":true`)
			}
			original := detail.RuntimeIdentity
			for _, field := range fields {
				old := account.Credentials[field]
				account.Credentials[field] = "rotated-" + field
				require.NotEqual(t, original, AccountFromService(account).RuntimeIdentity)
				account.Credentials[field] = old
			}
		})
	}
}

func TestMiniMaxBalanceAdminDTOReadback(t *testing.T) {
	for _, balance := range []float64{1234.56789, 0, -0.25} {
		account := &service.Account{ID: 18, Platform: "minimax", Type: "apikey",
			Credentials: map[string]any{"api_key": "sk-api-private-fixture", "account_mode": "payg", "base_url": "https://api.minimax.cn/v1"},
			Extra:       map[string]any{"minimax_balance": balance, "minimax_balance_currency": "", "minimax_balances": []any{map[string]any{"currency": "", "balance": balance}}, "minimax_balance_updated_at": "2026-10-05T00:00:00Z"},
		}
		detail := AccountFromService(account)
		for _, projection := range []any{detail, AccountListItemFromAccount(detail)} {
			encoded, err := json.Marshal(projection)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "sk-api-private-fixture")
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			credentials, ok := decoded["credentials"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "payg", credentials["account_mode"])
			require.Equal(t, "https://api.minimax.cn/v1", credentials["base_url"])
			require.NotContains(t, credentials, "api_key")
			credentialStatus, ok := decoded["credentials_status"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, true, credentialStatus["has_api_key"])
			extra, ok := decoded["extra"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, balance, extra["minimax_balance"])
			require.Equal(t, "", extra["minimax_balance_currency"])
			balances, ok := extra["minimax_balances"].([]any)
			require.True(t, ok)
			require.Len(t, balances, 1)
			firstBalance, ok := balances[0].(map[string]any)
			require.True(t, ok)
			require.Equal(t, balance, firstBalance["balance"])
			require.NotContains(t, extra, "minimax_balance_low")
		}
	}
}
