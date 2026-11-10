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

func TestKaggleCompatibleMediaRequestAuth(t *testing.T) {
	account := &Account{Platform: "kaggle", Type: AccountTypeAPIKey, Credentials: map[string]any{
		"account_mode": "payg",
		"api_key":      "opaque-kaggle-fixture",
		"base_url":     "https://example-tunnel.trycloudflare.com",
	}}
	for _, fixture := range []struct{ method, url, action string }{
		{http.MethodPost, "https://example-tunnel.trycloudflare.com/v1/images/generations", "image.generate"},
		{http.MethodPost, "https://example-tunnel.trycloudflare.com/v1/videos", "video.generate"},
		{http.MethodGet, "https://example-tunnel.trycloudflare.com/v1/videos/vid_aaaaaaaaaaaaaaaaaaaa", "video.status"},
		{http.MethodGet, "https://example-tunnel.trycloudflare.com/v1/videos/vid_aaaaaaaaaaaaaaaaaaaa/content", "video.content"},
	} {
		t.Run(fixture.action, func(t *testing.T) {
			request, err := http.NewRequest(fixture.method, fixture.url, nil)
			require.NoError(t, err)
			require.Equal(t, fixture.action, compatibleMediaPathAction(request.URL.Path))
			require.NoError(t, applyCompatibleRequestAuth(account, request))
			require.Equal(t, "Bearer opaque-kaggle-fixture", request.Header.Get("Authorization"))
		})
	}
	rejected, err := http.NewRequest(http.MethodPost, "https://example-tunnel.trycloudflare.com/v1/audio/speech", nil)
	require.NoError(t, err)
	require.ErrorContains(t, applyCompatibleRequestAuth(account, rejected), "unsupported compatible upstream operation")
	wrongHost, err := http.NewRequest(http.MethodPost, "https://evil.example/v1/images/generations", nil)
	require.NoError(t, err)
	require.ErrorContains(t, applyCompatibleRequestAuth(account, wrongHost), "does not match")
}
