//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type affinityRetryAccountRepo struct {
	stubAntigravityAccountRepo
	account    *Account
	beforeRead func()
}

func (r *affinityRetryAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	if r.beforeRead != nil {
		r.beforeRead()
	}
	return r.account, nil
}

// Model the cache's first-observation CAS contract; repository tests exercise Lua.
type affinityRetryCache struct {
	GatewayCache
	version     string
	deleteCalls int
}

func (c *affinityRetryCache) GetSessionAccountID(ctx context.Context, _ int64, hash string) (int64, error) {
	AffinityBindingStateFromContext(ctx).Versions.LoadOrStore(hash, c.version)
	return 42, nil
}

func (c *affinityRetryCache) DeleteSessionAccountID(ctx context.Context, _ int64, hash string) error {
	c.deleteCalls++
	if !IsV2AffinityKey(hash) {
		c.version = ""
		return nil
	}
	expected, _ := AffinityBindingStateFromContext(ctx).Versions.Load(hash)
	if expected == c.version {
		c.version = ""
	}
	return nil
}

func TestAntigravityRetryAffinityLifecycle(t *testing.T) {
	for _, path := range []string{"long delay", "exhausted", "model rate limit"} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			for _, state := range []string{"temporary", "disabled", "concurrent winner", "legacy"} {
				t.Run(fmt.Sprintf("%s/%d/%s", path, status, state), func(t *testing.T) {
					account := &Account{ID: 42, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
					repo := &affinityRetryAccountRepo{account: account}
					cache := &affinityRetryCache{version: "v2:42:observed-nonce"}
					svc := &AntigravityGatewayService{cache: cache, accountRepo: repo}
					ctx := WithAffinityBindingState(context.Background())
					hash := "gemini:v2:binding:hmac:k1:0123456789abcdef"
					if state == "legacy" {
						hash = "legacy-session"
					}
					_, err := cache.GetSessionAccountID(ctx, 7, hash)
					require.NoError(t, err)
					if state == "disabled" || state == "concurrent winner" {
						account.Schedulable = false
					}
					if state == "concurrent winner" {
						repo.beforeRead = func() { cache.version = "v2:42:winner-nonce" }
					}
					delay := "15s"
					if path == "exhausted" {
						delay = "0.001s"
					}
					body := []byte(fmt.Sprintf(`{"error":{"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","metadata":{"model":"claude-sonnet-4-5"},"reason":"RATE_LIMIT_EXCEEDED"},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":%q}]}}`, delay))
					if path == "model rate limit" {
						result := svc.handleModelRateLimit(&handleModelRateLimitParams{ctx: ctx, account: account, statusCode: status, body: body, cache: cache, groupID: 7, sessionHash: hash, isStickySession: true})
						require.NotNil(t, result.SwitchError)
					} else {
						upstream := &mockSmartRetryUpstream{responses: []*http.Response{nil}, errors: []error{nil}, repeatLast: true}
						result := svc.handleSmartRetry(antigravityRetryLoopParams{ctx: ctx, account: account, accountRepo: repo, httpUpstream: upstream, action: "generateContent", body: []byte(`{"input":"test"}`), groupID: 7, sessionHash: hash, isStickySession: true}, &http.Response{StatusCode: status, Header: http.Header{}}, body, "https://ag.test", 0, []string{"https://ag.test"})
						require.NotNil(t, result.switchError)
						require.Equal(t, int64(42), result.switchError.OriginalAccountID)
						if path == "exhausted" {
							require.NotEmpty(t, upstream.calls)
						}
					}
					require.NotEmpty(t, repo.modelRateLimitCalls)
					require.True(t, repo.modelRateLimitCalls[0].resetAt.After(time.Now()))
					switch state {
					case "temporary":
						require.Equal(t, "v2:42:observed-nonce", cache.version)
						require.Zero(t, cache.deleteCalls)
					case "concurrent winner":
						require.Equal(t, "v2:42:winner-nonce", cache.version)
						require.Equal(t, 1, cache.deleteCalls)
					default:
						require.Empty(t, cache.version)
						require.Equal(t, 1, cache.deleteCalls)
					}
				})
			}
		}
	}
}
