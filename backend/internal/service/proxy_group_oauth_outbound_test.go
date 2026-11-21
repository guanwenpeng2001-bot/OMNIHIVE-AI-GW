//go:build unit

package service

import (
	"context"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/oauth"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/xai"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func (c *egressOpenAIOAuthClient) RefreshToken(ctx context.Context, _ string, proxy string) (*openai.TokenResponse, error) {
	return c.ExchangeCode(ctx, "", "", "", proxy, "")
}
func (c *egressOpenAIOAuthClient) RefreshTokenWithClientID(ctx context.Context, _ string, proxy, _ string) (*openai.TokenResponse, error) {
	return c.RefreshToken(ctx, "", proxy)
}

type egressClaudeOAuthClient struct {
	ClaudeOAuthClient
	urls []string
}

func (c *egressClaudeOAuthClient) RefreshToken(_ context.Context, _, proxy string) (*oauth.TokenResponse, error) {
	c.urls = append(c.urls, proxy)
	return &oauth.TokenResponse{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresIn: 3600}, nil
}

type egressGrokOAuthClient struct {
	GrokOAuthClient
	urls []string
}

func (c *egressGrokOAuthClient) RefreshToken(_ context.Context, _, proxy, _ string) (*xai.TokenResponse, error) {
	c.urls = append(c.urls, proxy)
	return &xai.TokenResponse{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresIn: 3600}, nil
}

func TestProxyGroupOAuthRefreshFamiliesResolveBeforeClient(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGrok, PlatformGemini} {
		t.Run(platform, func(t *testing.T) {
			r, g, cache, a := runtimeFixture()
			a.Platform = platform
			a.Type = AccountTypeOAuth
			a.Credentials = map[string]any{"refresh_token": "fixture", "project_id": "fixture-project"}
			var urls *[]string
			var refresh func(*Account) error
			ctx := context.Background()
			switch platform {
			case PlatformAnthropic:
				client := &egressClaudeOAuthClient{}
				svc := NewOAuthService(nil, client)
				defer svc.Stop()
				urls = &client.urls
				refresh = func(a *Account) error { _, err := svc.RefreshAccountToken(ctx, a); return err }
			case PlatformOpenAI:
				client := &egressOpenAIOAuthClient{}
				svc := NewOpenAIOAuthService(nil, client)
				defer svc.Stop()
				urls = &client.urls
				refresh = func(a *Account) error { _, err := svc.RefreshAccountToken(ctx, a); return err }
			case PlatformGrok:
				client := &egressGrokOAuthClient{}
				svc := NewGrokOAuthService(nil, client)
				defer svc.Stop()
				urls = &client.urls
				refresh = func(a *Account) error { _, err := svc.RefreshAccountToken(ctx, a); return err }
			case PlatformGemini:
				recorded := []string{}
				urls = &recorded
				client := &mockGeminiOAuthClient{refreshTokenFunc: func(_ context.Context, _, _, url string) (*geminicli.TokenResponse, error) {
					recorded = append(recorded, url)
					return &geminicli.TokenResponse{AccessToken: "fixture-access", ExpiresIn: 3600}, nil
				}}
				svc := NewGeminiOAuthService(nil, client, nil, nil, &config.Config{})
				defer svc.Stop()
				refresh = func(a *Account) error { _, err := svc.RefreshAccountToken(ctx, a); return err }
			}
			require.ErrorIs(t, refresh(a), ErrProxyGroupUnavailable)
			require.Empty(t, *urls)
			a.AttachEgress(r)
			want, err := a.EgressProxyURL(ctx)
			require.NoError(t, err)
			require.NoError(t, refresh(a))
			require.Equal(t, []string{want}, *urls)
			a.ProxyGroupID = nil
			reads, calls := g.reads, cache.calls
			require.NoError(t, refresh(a))
			require.Equal(t, "", (*urls)[1])
			require.Equal(t, reads, g.reads)
			require.Equal(t, calls, cache.calls)
		})
	}
}

func TestProxyGroupAntigravityRefreshUsesSelectedProxy(t *testing.T) {
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects.Add(1)
		require.Equal(t, http.MethodConnect, r.Method)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	r, g, _, a := runtimeFixture()
	useFixtureProxy(t, r, g, proxy.URL)
	a.Platform = PlatformAntigravity
	a.Type = AccountTypeOAuth
	a.Credentials = map[string]any{"refresh_token": "fixture"}
	svc := NewAntigravityOAuthService(nil)
	defer svc.Stop()
	_, err := svc.RefreshAccountToken(context.Background(), a)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	require.Zero(t, connects.Load())
	a.AttachEgress(r)
	_, err = svc.RefreshAccountToken(context.Background(), a)
	require.Error(t, err)
	require.Positive(t, connects.Load())
}
