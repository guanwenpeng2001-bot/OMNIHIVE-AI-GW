//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

type stagedEgressSessionCache struct {
	*cursorMemoryCache
	reads, writes, locks atomic.Int32
}

func (c *stagedEgressSessionCache) Get(ctx context.Context, key string) (cursor.Session, error) {
	c.reads.Add(1)
	return c.cursorMemoryCache.Get(ctx, key)
}
func (c *stagedEgressSessionCache) Set(ctx context.Context, key string, value cursor.Session) error {
	c.writes.Add(1)
	return c.cursorMemoryCache.Set(ctx, key, value)
}
func (c *stagedEgressSessionCache) Lock(ctx context.Context, key string) (func(), bool, error) {
	c.locks.Add(1)
	return c.cursorMemoryCache.Lock(ctx, key)
}

func TestStagedProxyCursorSessionAndUsageBoundaries(t *testing.T) {
	for _, dependency := range []string{"required", "unknown", "none", ""} {
		t.Run(dependency, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformCursor, Type: AccountTypeAPIKey, Status: "inactive", Credentials: map[string]any{"api_key": "synthetic-key"}, Extra: map[string]any{StagedProxyDependencyExtraKey: dependency}}
			cache := &stagedEgressSessionCache{cursorMemoryCache: newCursorMemoryCache()}
			provider := NewCursorSessionProvider(cache)
			var exchanges, sends atomic.Int32
			provider.exchange = func(context.Context, string) (cursor.Session, error) {
				exchanges.Add(1)
				return cursor.Session{Token: "synthetic-session", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			usage := &AccountUsageService{cache: NewUsageCache(), cursorSessions: provider, cursorUsageClient: cursorUsageParityClient(func(request *http.Request) (*http.Response, error) {
				sends.Add(1)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})}
			token, sessionErr := provider.GetSessionToken(context.Background(), account, "")
			_, usageErr := usage.GetUsageForAccount(context.Background(), account, true)
			direct := usage.fetchCursorUsageWithToken(context.Background(), account, "synthetic-token")
			if dependency == "required" || dependency == "unknown" {
				require.ErrorIs(t, sessionErr, ErrStagedProxyPending)
				require.ErrorIs(t, usageErr, ErrStagedProxyPending)
				require.Empty(t, token)
				require.NotEmpty(t, direct.ErrorCode)
				require.Zero(t, exchanges.Load())
				require.Zero(t, sends.Load())
				require.Zero(t, cache.reads.Load())
				require.Zero(t, cache.writes.Load())
				require.Zero(t, cache.locks.Load())
			} else {
				require.NoError(t, sessionErr)
				require.NoError(t, usageErr)
				require.Empty(t, direct.ErrorCode)
				require.EqualValues(t, 1, exchanges.Load())
				require.EqualValues(t, 1, cache.writes.Load())
				require.EqualValues(t, 6, sends.Load(), "none and ordinary inactive usage preserve existing behavior")
			}
		})
	}
}

func TestStagedProxySendBoundaryBlocksHTTPAndTLSWithoutGroup(t *testing.T) {
	for _, dependency := range []string{"required", "unknown", "none", ""} {
		account := &Account{ID: 7, Status: "inactive", Extra: map[string]any{StagedProxyDependencyExtraKey: dependency}}
		base := &egressRecordingUpstream{}
		transport := accountHTTPUpstream(base, account)
		request, err := http.NewRequest(http.MethodGet, "https://unused.example.invalid", nil)
		require.NoError(t, err)
		for _, tls := range []bool{false, true} {
			var response *http.Response
			if tls {
				response, err = transport.DoWithTLS(request, "http://untrusted-hint.invalid", 7, 1, nil)
			} else {
				response, err = transport.Do(request, "", 7, 1)
			}
			if dependency == "required" || dependency == "unknown" {
				require.ErrorIs(t, err, ErrStagedProxyPending)
				require.Nil(t, response)
				require.Empty(t, base.urls)
			} else {
				require.Same(t, base, transport)
				require.NoError(t, err)
				_ = response.Body.Close()
			}
		}
	}
}

func TestStagedProxyBalanceQueryCannotUseDirectFallback(t *testing.T) {
	for _, dependency := range []string{"required", "unknown", "none", ""} {
		account := newDeepSeekBalanceProbeAccount()
		account.Status = "inactive"
		account.Extra = map[string]any{StagedProxyDependencyExtraKey: dependency}
		repo := &cnBalanceProbeRepo{account: account}
		upstream := &cnBalanceResponseUpstream{statusCode: 200, body: `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"12.50"}]}`}
		balance := NewCNProviderBalanceService(repo, nil, upstream, nil)
		result, err := balance.QueryBalance(context.Background(), account.ID)
		if dependency == "required" || dependency == "unknown" {
			require.ErrorIs(t, err, ErrStagedProxyPending)
			require.Nil(t, result)
			require.Zero(t, upstream.calls)
			require.Empty(t, repo.extraWrites)
		} else {
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, 1, upstream.calls)
		}
	}
}
