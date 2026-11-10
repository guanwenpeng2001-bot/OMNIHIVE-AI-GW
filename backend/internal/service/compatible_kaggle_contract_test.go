//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKaggleCompatibleAccountWireContract(t *testing.T) {
	for _, base := range []string{
		"https://example-tunnel.trycloudflare.com",
		"https://example-tunnel.trycloudflare.com/",
		"https://example-tunnel.trycloudflare.com/v1",
	} {
		t.Run(base, func(t *testing.T) {
			account := &Account{Platform: "kaggle", Type: AccountTypeAPIKey, Credentials: map[string]any{
				"account_mode": "payg",
				"api_protocol": "chat_completions",
				"api_key":      "opaque-kaggle-fixture",
				"base_url":     base,
			}}
			require.NoError(t, validateCompatibleAccount(account))
			require.True(t, IsCompatibleProvider(account.Platform))
			bound, err := bindCompatibleRequest(context.Background(), account, "chat", "qwen3.5-9b")
			require.NoError(t, err)
			require.Equal(t, "https://example-tunnel.trycloudflare.com/v1/chat/completions", bound.compatibleBinding.Base+"/chat/completions")
			request, err := http.NewRequest(http.MethodPost, bound.compatibleBinding.Base+"/chat/completions", nil)
			require.NoError(t, err)
			require.NoError(t, applyCompatibleRequestAuth(bound, request))
			require.Equal(t, "Bearer opaque-kaggle-fixture", request.Header.Get("Authorization"))
			require.Equal(t, "custom:kaggle", resolveCapabilityProfile(account))
		})
	}
	missing := &Account{Platform: "kaggle", Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "payg", "api_key": "x"}}
	_, err := bindCompatibleRequest(context.Background(), missing, "chat", "qwen3.5-9b")
	require.Error(t, err, "account-sourced provider must require a resource base_url")
}
