//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestCompatibleVerifiedPlanWireContracts(t *testing.T) {
	for _, test := range []struct{ platform, mode, protocol, action, url, auth string }{
		{"astron_token", "coding", "chat_completions", "chat", "https://maas-token-api.cn-huabei-1.xf-yun.com/v2/chat/completions", "Authorization"},
		{"astron_token", "coding", "anthropic", "messages", "https://maas-token-api.cn-huabei-1.xf-yun.com/anthropic/v1/messages", "Authorization"},
		{"mimo_token_sg", "coding", "chat_completions", "chat", "https://token-plan-sgp.xiaomimimo.com/v1/chat/completions", "Api-Key"},
		{"mimo_token_sg", "coding", "anthropic", "messages", "https://token-plan-sgp.xiaomimimo.com/anthropic/v1/messages", "Api-Key"},
		{"mimo_token_sg", "coding", "responses", "responses", "https://token-plan-sgp.xiaomimimo.com/v1/responses", "Api-Key"},
		{"mimo_token_eu", "coding", "chat_completions", "chat", "https://token-plan-ams.xiaomimimo.com/v1/chat/completions", "Api-Key"},
		{"mimo_token_eu", "coding", "anthropic", "messages", "https://token-plan-ams.xiaomimimo.com/anthropic/v1/messages", "Api-Key"},
		{"mimo_token_eu", "coding", "responses", "responses", "https://token-plan-ams.xiaomimimo.com/v1/responses", "Api-Key"},
		{"atlascloud", "coding", "anthropic", "messages", "https://api.atlascloud.ai/v1/messages", "Authorization"},
		{"atlascloud", "coding", "chat_completions", "chat", "https://api.atlascloud.ai/v1/chat/completions", "Authorization"},
		{"qiniu", "subscription", "chat_completions", "chat", "https://api.qnaigc.com/v1/chat/completions", "Authorization"},
	} {
		t.Run(test.platform+"/"+test.protocol, func(t *testing.T) {
			account := &Account{Platform: test.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": test.mode, "api_protocol": test.protocol, "api_key": "opaque-plan-fixture", "unrelated": map[string]any{"keep": true}}}
			before, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			bound, err := bindCompatibleRequest(context.Background(), account, test.action, "manual-model")
			require.NoError(t, err)
			suffix := map[string]string{"chat": "/chat/completions", "messages": "/messages", "responses": "/responses"}[test.action]
			require.Equal(t, test.url, bound.compatibleBinding.Base+suffix)
			request, err := http.NewRequest(http.MethodPost, test.url, nil)
			require.NoError(t, err)
			request.Header.Set("X-Api-Key", "stale")
			request.Header.Set("Authorization", "stale")
			require.NoError(t, applyCompatibleRequestAuth(bound, request))
			expected := "opaque-plan-fixture"
			if test.auth == "Authorization" {
				expected = "Bearer " + expected
			}
			require.Equal(t, expected, request.Header.Get(test.auth))
			require.Empty(t, request.Header.Get("X-Api-Key"))
			require.NotEmpty(t, resolveCapabilityProfile(account))
			after, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
			require.Nil(t, account.compatibleBinding)
			account.Credentials["api_base_urls"] = map[string]any{test.protocol: "https://override.example/prefix"}
			overridden, err := bindCompatibleRequest(context.Background(), account, test.action, "manual-model")
			require.NoError(t, err)
			require.Equal(t, "https://override.example/prefix", overridden.compatibleBinding.Base)
			account.Credentials["account_mode"] = "unknown"
			_, err = bindCompatibleRequest(context.Background(), account, test.action, "manual-model")
			require.Error(t, err)
			for _, malformed := range []any{false, 7, map[string]any{"mode": "coding"}} {
				account.Credentials["account_mode"] = malformed
				require.Error(t, validateCompatibleAccount(account))
			}
		})
	}
}
