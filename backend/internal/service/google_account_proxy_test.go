//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
	"github.com/stretchr/testify/require"
)

func TestGoogleAccountProxyResolution(t *testing.T) {
	id := int64(7)
	proxy := &Proxy{ID: id, Protocol: "http", Host: "proxy.test", Port: 3128}
	lookupErr := errors.New("repository unavailable")
	for _, tc := range []struct {
		name    string
		account Account
		repo    ProxyRepository
		want    string
		wantErr bool
	}{
		{name: "unbound", account: Account{}, want: ""},
		{name: "direct expiry fallback", account: Account{ProxyFallbackOriginID: &id, Proxy: proxy}, want: ""},
		{name: "loaded binding", account: Account{ProxyID: &id, Proxy: proxy}, want: proxy.URL()},
		{name: "missing binding", account: Account{ProxyID: &id}, wantErr: true},
		{name: "mismatched binding", account: Account{ProxyID: &id, Proxy: &Proxy{ID: 9}}, wantErr: true},
		{name: "repository binding", account: Account{ProxyID: &id}, repo: &mockGeminiProxyRepo{getByIDFunc: func(context.Context, int64) (*Proxy, error) { return proxy, nil }}, want: proxy.URL()},
		{name: "missing repository row", account: Account{ProxyID: &id}, repo: &mockGeminiProxyRepo{getByIDFunc: func(context.Context, int64) (*Proxy, error) { return nil, nil }}, wantErr: true},
		{name: "lookup error does not use stale binding", account: Account{ProxyID: &id, Proxy: proxy}, repo: &mockGeminiProxyRepo{getByIDFunc: func(context.Context, int64) (*Proxy, error) { return nil, lookupErr }}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveGoogleAccountProxyURL(context.Background(), &tc.account, tc.repo)
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestGoogleAccountRefreshRejectsUnresolvedProxy(t *testing.T) {
	id := int64(7)
	lookupErr := errors.New("repository unavailable")
	repo := &mockGeminiProxyRepo{getByIDFunc: func(context.Context, int64) (*Proxy, error) { return nil, lookupErr }}
	account := &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, ProxyID: &id, Credentials: map[string]any{"refresh_token": "rt"}}
	ag := NewAntigravityOAuthService(repo)
	defer ag.Stop()
	_, err := ag.RefreshAccountToken(context.Background(), account)
	require.ErrorIs(t, err, lookupErr)
	_, err = ag.FillProjectID(context.Background(), account, "token")
	require.ErrorIs(t, err, lookupErr)
	account.Platform = PlatformGemini
	// A nil OAuth client makes an accidental upstream attempt fail this test.
	gemini := NewGeminiOAuthService(repo, nil, nil, nil, &config.Config{})
	defer gemini.Stop()
	_, err = gemini.RefreshAccountToken(context.Background(), account)
	require.ErrorIs(t, err, lookupErr)
}

func TestGeminiAccountRefreshPreservesDirectRoute(t *testing.T) {
	called := false
	client := &mockGeminiOAuthClient{refreshTokenFunc: func(_ context.Context, _, _, proxyURL string) (*geminicli.TokenResponse, error) {
		called = true
		require.Empty(t, proxyURL)
		return &geminicli.TokenResponse{AccessToken: "refreshed", ExpiresIn: 3600}, nil
	}}
	svc := NewGeminiOAuthService(nil, client, nil, nil, &config.Config{})
	defer svc.Stop()
	_, err := svc.RefreshAccountToken(context.Background(), &Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"refresh_token": "rt", "project_id": "project"}})
	require.NoError(t, err)
	require.True(t, called)
}

func TestAntigravityQuotaRoutesThroughAccountProxy(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	respond := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1internal:fetchAvailableModels" {
			_, _ = w.Write([]byte(`{"models":{"model-a":{"quotaInfo":{"remainingFraction":1}}}}`))
		} else {
			_, _ = w.Write([]byte(`{"currentTier":{"id":"free-tier"}}`))
		}
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalls.Add(1)
		respond(w, r)
	}))
	defer origin.Close()
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if !r.URL.IsAbs() {
			t.Error("expected an HTTP proxy request with an absolute target URL")
		}
		respond(w, r)
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	id := int64(7)
	proxy := &Proxy{ID: id, Protocol: "http", Host: parsed.Hostname(), Port: port}
	repo := &mockGeminiProxyRepo{getByIDFunc: func(context.Context, int64) (*Proxy, error) { return proxy, nil }}
	oldURLs, oldAvailability := antigravity.BaseURLs, antigravity.DefaultURLAvailability
	t.Cleanup(func() { antigravity.BaseURLs, antigravity.DefaultURLAvailability = oldURLs, oldAvailability })
	antigravity.BaseURLs = []string{origin.URL}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)
	fetcher := NewAntigravityQuotaFetcher(repo, &config.Config{})
	account := &Account{Platform: PlatformAntigravity, ProxyID: &id, Credentials: map[string]any{"access_token": "token", "project_id": "project"}}
	_, err = fetcher.FetchQuota(context.Background(), account, "")
	require.NoError(t, err)
	require.EqualValues(t, 2, proxyCalls.Load())
	require.Zero(t, directCalls.Load())

	account.ProxyID = nil
	_, err = fetcher.FetchQuota(context.Background(), account, "")
	require.NoError(t, err)
	require.EqualValues(t, 2, directCalls.Load())
	require.EqualValues(t, 2, proxyCalls.Load())

	account.ProxyID = &id
	repo.getByIDFunc = func(context.Context, int64) (*Proxy, error) { return nil, errors.New("lookup failed") }
	_, err = fetcher.FetchQuota(context.Background(), account, "")
	require.ErrorContains(t, err, "lookup failed")
	require.EqualValues(t, 2, directCalls.Load())
	require.EqualValues(t, 2, proxyCalls.Load())
}
