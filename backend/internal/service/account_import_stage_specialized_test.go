//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type stagedTokenActivity struct{ reads, writes, deletes, locks, unlocks atomic.Int32 }

func (c *stagedTokenActivity) GetAccessToken(context.Context, string) (string, error) {
	c.reads.Add(1)
	return "synthetic-cached-token", nil
}
func (c *stagedTokenActivity) SetAccessToken(context.Context, string, string, time.Duration) error {
	c.writes.Add(1)
	return nil
}
func (c *stagedTokenActivity) DeleteAccessToken(context.Context, string) error {
	c.deletes.Add(1)
	return nil
}
func (c *stagedTokenActivity) AcquireRefreshLock(context.Context, string, string, time.Duration) (bool, error) {
	c.locks.Add(1)
	return true, nil
}
func (c *stagedTokenActivity) ReleaseRefreshLock(context.Context, string, string) error {
	c.unlocks.Add(1)
	return nil
}
func (c *stagedTokenActivity) assertIdle(t *testing.T) {
	t.Helper()
	require.Zero(t, c.reads.Load())
	require.Zero(t, c.writes.Load())
	require.Zero(t, c.deletes.Load())
	require.Zero(t, c.locks.Load())
	require.Zero(t, c.unlocks.Load())
}

func stagedOAuthFixture(platform, dependency string) *Account {
	return &Account{ID: 47, Platform: platform, Type: AccountTypeOAuth, Status: "inactive", Extra: map[string]any{StagedProxyDependencyExtraKey: dependency}, Credentials: map[string]any{
		"access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "chatgpt_account_id": "synthetic-tenant", "project_id": "synthetic-project", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}}
}

type stagedRefreshActivity struct {
	OAuthRefreshExecutor
	keys, refreshes atomic.Int32
}

func (r *stagedRefreshActivity) CacheKey(*Account) string { r.keys.Add(1); return "fixture" }
func (r *stagedRefreshActivity) Refresh(context.Context, *Account) (map[string]any, error) {
	r.refreshes.Add(1)
	return nil, errors.New("unexpected refresh")
}

func TestStagedSpecializedTokenEntriesRejectBeforeCacheAndRefresh(t *testing.T) {
	for _, dependency := range []string{"required", "unknown"} {
		for _, platform := range []string{PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformAnthropic, PlatformGrok} {
			t.Run(dependency+"/"+platform, func(t *testing.T) {
				account, cache := stagedOAuthFixture(platform, dependency), &stagedTokenActivity{}
				var token string
				var err error
				switch platform {
				case PlatformOpenAI:
					token, err = (&OpenAITokenProvider{tokenCache: cache}).GetAccessToken(context.Background(), account)
				case PlatformGemini:
					token, err = (&GeminiTokenProvider{tokenCache: cache}).GetAccessToken(context.Background(), account)
				case PlatformAntigravity:
					token, err = (&AntigravityTokenProvider{tokenCache: cache}).GetAccessToken(context.Background(), account)
				case PlatformAnthropic:
					token, err = (&ClaudeTokenProvider{tokenCache: cache}).GetAccessToken(context.Background(), account)
				case PlatformGrok:
					provider := &GrokTokenProvider{tokenCache: cache}
					token, err = provider.GetAccessToken(context.Background(), account)
					require.ErrorIs(t, err, ErrStagedProxyPending)
					token, err = provider.GetAccessTokenForManualTest(context.Background(), account)
				}
				require.ErrorIs(t, err, ErrStagedProxyPending)
				require.Empty(t, token)
				cache.assertIdle(t)
				executor := &stagedRefreshActivity{}
				api := NewOAuthRefreshAPI(&stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}, cache)
				_, err = api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)
				require.ErrorIs(t, err, ErrStagedProxyPending)
				require.Zero(t, executor.keys.Load())
				require.Zero(t, executor.refreshes.Load())
				cache.assertIdle(t)
			})
		}
	}
}

type stagedGrokOAuthClient struct {
	*grokOAuthClientStub
	refreshes atomic.Int32
}

func (c *stagedGrokOAuthClient) RefreshToken(context.Context, string, string, string) (*xai.TokenResponse, error) {
	c.refreshes.Add(1)
	return &xai.TokenResponse{AccessToken: "synthetic-new-token", ExpiresIn: 3600}, nil
}

func TestStagedSpecializedRefreshServicesAndResolvers(t *testing.T) {
	for _, dependency := range []string{"required", "unknown", "none", ""} {
		t.Run(dependency, func(t *testing.T) {
			blocked := dependency == "required" || dependency == "unknown"
			var geminiCalls atomic.Int32
			google := NewGeminiOAuthService(nil, &mockGeminiOAuthClient{refreshTokenFunc: func(context.Context, string, string, string) (*geminicli.TokenResponse, error) {
				geminiCalls.Add(1)
				return &geminicli.TokenResponse{AccessToken: "synthetic-new-token", ExpiresIn: 3600}, nil
			}}, nil, nil, &config.Config{})
			defer google.Stop()
			_, err := google.RefreshAccountToken(context.Background(), stagedOAuthFixture(PlatformGemini, dependency))
			if blocked {
				require.ErrorIs(t, err, ErrStagedProxyPending)
				require.Zero(t, geminiCalls.Load())
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, geminiCalls.Load())
			}
			grokClient := &stagedGrokOAuthClient{grokOAuthClientStub: &grokOAuthClientStub{}}
			grok := NewGrokOAuthService(nil, grokClient)
			defer grok.Stop()
			_, err = grok.RefreshAccountToken(context.Background(), stagedOAuthFixture(PlatformGrok, dependency))
			if blocked {
				require.ErrorIs(t, err, ErrStagedProxyPending)
				require.Zero(t, grokClient.refreshes.Load())
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, grokClient.refreshes.Load())
			}
			_, err = resolveGoogleAccountProxyURL(context.Background(), stagedOAuthFixture(PlatformGemini, dependency), nil)
			if blocked {
				require.ErrorIs(t, err, ErrStagedProxyPending)
			} else {
				require.NoError(t, err)
			}
			if blocked {
				// Empty credentials make this a safe sentinel even if a guard regresses:
				// the dependency error must precede missing-token validation.
				account := stagedOAuthFixture(PlatformAntigravity, dependency)
				account.Credentials = nil
				_, err = (&AntigravityOAuthService{}).RefreshAccountToken(context.Background(), account)
				require.ErrorIs(t, err, ErrStagedProxyPending)
				_, err = (&AntigravityQuotaFetcher{}).FetchQuota(context.Background(), account, "")
				require.ErrorIs(t, err, ErrStagedProxyPending)
				account.Platform = PlatformOpenAI
				_, err = (&OpenAIOAuthService{}).RefreshAccountToken(context.Background(), account)
				require.ErrorIs(t, err, ErrStagedProxyPending)
				account.Platform = PlatformAnthropic
				_, err = (&OAuthService{}).RefreshAccountToken(context.Background(), account)
				require.ErrorIs(t, err, ErrStagedProxyPending)
			}
		})
	}
}

func TestStagedOpenAIQuotaStopsBeforeTokenCacheOrClient(t *testing.T) {
	for _, dependency := range []string{"required", "unknown", "none", ""} {
		t.Run(dependency, func(t *testing.T) {
			account, cache := stagedOAuthFixture(PlatformOpenAI, dependency), &stagedTokenActivity{}
			repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
			var clients atomic.Int32
			quota := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), func(string) (*req.Client, error) {
				clients.Add(1)
				return nil, errors.New("fixture stops before network")
			})
			_, err := quota.QueryUsage(context.Background(), account.ID)
			if dependency == "required" || dependency == "unknown" {
				require.ErrorIs(t, err, ErrStagedProxyPending)
				cache.assertIdle(t)
				require.Zero(t, clients.Load())
				require.Zero(t, repo.extraUpdateCalls)
				account.Credentials = nil
				_, err = quota.QueryUsage(context.Background(), account.ID)
				require.ErrorIs(t, err, ErrStagedProxyPending, "dependency is checked before even the account-id credential")
				cache.assertIdle(t)
				require.Zero(t, clients.Load())
			} else {
				require.Error(t, err)
				require.EqualValues(t, 1, clients.Load())
				require.EqualValues(t, 1, cache.reads.Load())
			}
		})
	}
}
