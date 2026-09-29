//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAntigravityTokenProvider_GetAccessToken_Upstream(t *testing.T) {
	provider := &AntigravityTokenProvider{}

	t.Run("upstream account with valid api_key", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
			Credentials: map[string]any{
				"api_key": "sk-test-key-12345",
			},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.NoError(t, err)
		require.Equal(t, "sk-test-key-12345", token)
	})

	t.Run("upstream account missing api_key", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Type:        AccountTypeUpstream,
			Credentials: map[string]any{},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})

	t.Run("upstream account with empty api_key", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
			Credentials: map[string]any{
				"api_key": "",
			},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})

	t.Run("upstream account with nil credentials", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})
}

func TestAntigravityTokenProvider_GetAccessToken_Guards(t *testing.T) {
	provider := &AntigravityTokenProvider{}

	t.Run("nil account", func(t *testing.T) {
		token, err := provider.GetAccessToken(context.Background(), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "account is nil")
		require.Empty(t, token)
	})

	t.Run("non-antigravity platform", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity account")
		require.Empty(t, token)
	})

	t.Run("unsupported account type", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeAPIKey,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity oauth account")
		require.Empty(t, token)
	})
}

type isolatedAGRepo struct {
	AccountRepository
	mu       sync.Mutex
	accounts map[int64]*Account
	err      error
}

func (r *isolatedAGRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotOAuthRefreshAccount(r.accounts[id]), r.err
}

func (r *isolatedAGRepo) UpdateCredentials(_ context.Context, id int64, creds map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[id].Credentials = shallowCopyMap(creds)
	return nil
}

func (r *isolatedAGRepo) put(account *Account) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[account.ID] = snapshotOAuthRefreshAccount(account)
}

type isolatedAGCache struct {
	mu        sync.Mutex
	tokens    map[string]string
	beforeSet func(string)
}

func (c *isolatedAGCache) GetAccessToken(_ context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens[key], nil
}
func (c *isolatedAGCache) SetAccessToken(_ context.Context, key, token string, _ time.Duration) error {
	if c.beforeSet != nil {
		c.beforeSet(key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[key] = token
	return nil
}
func (c *isolatedAGCache) DeleteAccessToken(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, key)
	return nil
}
func (c *isolatedAGCache) AcquireRefreshLock(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (c *isolatedAGCache) ReleaseRefreshLock(context.Context, string, string) error { return nil }

func isolatedAGAccount(id int64, token string, version int64) *Account {
	return &Account{ID: id, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"project_id": "shared-project", "access_token": token, "refresh_token": "refresh-" + token,
			"_token_version": version, "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}
}

func TestAntigravityTokenProvider_OwnerVersionIsolation(t *testing.T) {
	ctx := context.Background()
	a, b := isolatedAGAccount(1, "token-a", 1), isolatedAGAccount(2, "token-b", 1)
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: a, 2: b}}
	cache := &isolatedAGCache{tokens: map[string]string{"ag:shared-project": "foreign-shared-token", "ag:account:1": "legacy-token"}}
	p := NewAntigravityTokenProvider(repo, cache, nil)
	for _, account := range []*Account{a, b, a, b} {
		token, err := p.GetAccessToken(ctx, account)
		require.NoError(t, err)
		require.Equal(t, account.GetCredential("access_token"), token)
	}
	require.NotEqual(t, AntigravityTokenCacheKey(a), AntigravityTokenCacheKey(b))
	require.Empty(t, cache.tokens["ag:shared-project"])
	require.Empty(t, cache.tokens["ag:account:1"])

	// A credential replacement with a lower version must still supersede a
	// cached scheduler snapshot; manual edits need not bump the version.
	rotated := isolatedAGAccount(1, "rotated", 0)
	repo.put(rotated)
	token, err := p.GetAccessToken(ctx, a)
	require.NoError(t, err)
	require.Equal(t, "rotated", token)
	require.NotEqual(t, antigravityCredentialTokenCacheKey(a), antigravityCredentialTokenCacheKey(rotated))
	require.Equal(t, AntigravityTokenCacheKey(a), AntigravityTokenCacheKey(rotated))
}

func TestAntigravityTokenProvider_RevocationBeforeCacheHit(t *testing.T) {
	for _, mutation := range []string{"deleted", "disabled", "empty", "platform", "type", "unavailable"} {
		t.Run(mutation, func(t *testing.T) {
			a := isolatedAGAccount(1, "old-token", 1)
			repo := &isolatedAGRepo{accounts: map[int64]*Account{1: snapshotOAuthRefreshAccount(a)}}
			cache := &isolatedAGCache{tokens: map[string]string{antigravityCredentialTokenCacheKey(a): "old-token"}}
			switch mutation {
			case "deleted":
				delete(repo.accounts, 1)
			case "disabled":
				repo.accounts[1].Status = "disabled"
			case "empty":
				repo.accounts[1].Credentials["access_token"] = ""
			case "platform":
				repo.accounts[1].Platform = PlatformGemini
			case "type":
				repo.accounts[1].Type = AccountTypeAPIKey
			case "unavailable":
				repo.err = errors.New("database unavailable")
			}
			token, err := NewAntigravityTokenProvider(repo, cache, nil).GetAccessToken(context.Background(), a)
			require.Error(t, err)
			require.Empty(t, token)
		})
	}
}

func TestAntigravityTokenProvider_LateCacheWrite(t *testing.T) {
	ctx := context.Background()
	old, rotated := isolatedAGAccount(1, "old", 1), isolatedAGAccount(1, "new", 2)
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: old}}
	entered, release := make(chan struct{}), make(chan struct{})
	cache := &isolatedAGCache{tokens: map[string]string{}, beforeSet: func(key string) {
		if key == antigravityCredentialTokenCacheKey(old) {
			close(entered)
			<-release
		}
	}}
	p := NewAntigravityTokenProvider(repo, cache, nil)
	done := make(chan error, 1)
	go func() { _, err := p.GetAccessToken(ctx, old); done <- err }()
	<-entered
	repo.put(rotated)
	token, err := p.GetAccessToken(ctx, old)
	require.NoError(t, err)
	require.Equal(t, "new", token)
	close(release)
	require.NoError(t, <-done)
	token, err = p.GetAccessToken(ctx, old)
	require.NoError(t, err)
	require.Equal(t, "new", token)
}

type isolatedAGExecutor struct {
	AntigravityTokenRefresher
	calls atomic.Int64
}

func (e *isolatedAGExecutor) Refresh(_ context.Context, a *Account) (map[string]any, error) {
	e.calls.Add(1)
	creds := shallowCopyMap(a.Credentials)
	creds["access_token"] = "refreshed"
	creds["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	return creds, nil
}

func TestAntigravityTokenProvider_RefreshSingleFlight(t *testing.T) {
	a := isolatedAGAccount(1, "expired", 1)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: snapshotOAuthRefreshAccount(a)}}
	cache := &isolatedAGCache{tokens: map[string]string{}}
	p := NewAntigravityTokenProvider(repo, cache, nil)
	executor := &isolatedAGExecutor{}
	p.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := p.GetAccessToken(context.Background(), a)
			assert.NoError(t, err)
			assert.Equal(t, "refreshed", token)
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1), executor.calls.Load())
}

func TestAntigravityTokenProvider_ExpiredCachedToken(t *testing.T) {
	a := isolatedAGAccount(1, "expired", 1)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: a}}
	cache := &isolatedAGCache{tokens: map[string]string{antigravityCredentialTokenCacheKey(a): "expired"}}
	token, err := NewAntigravityTokenProvider(repo, cache, nil).GetAccessToken(context.Background(), a)
	require.Error(t, err)
	require.Empty(t, token)
}

func TestAntigravityTokenProvider_LockHeldRejectsExpiredCache(t *testing.T) {
	a := isolatedAGAccount(1, "expired", 1)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: a}}
	cache := &isolatedAGCache{tokens: map[string]string{antigravityCredentialTokenCacheKey(a): "expired"}}
	p := NewAntigravityTokenProvider(repo, cache, nil)
	p.SetRefreshAPI(NewOAuthRefreshAPI(repo, &refreshAPICacheStub{lockResult: false}), &isolatedAGExecutor{})
	policy := AntigravityProviderRefreshPolicy()
	policy.OnLockHeld = ProviderLockHeldWaitForCache
	p.SetRefreshPolicy(policy)
	token, err := p.GetAccessToken(context.Background(), a)
	require.ErrorContains(t, err, "expired")
	require.Empty(t, token)
}

func TestAntigravityTokenProvider_ProjectDiscoveryPreservesOwner(t *testing.T) {
	ctx := context.Background()
	a := isolatedAGAccount(1, "token", 1)
	delete(a.Credentials, "project_id")
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: a}}
	cache := &isolatedAGCache{tokens: map[string]string{}}
	p := NewAntigravityTokenProvider(repo, cache, nil)
	token, err := p.GetAccessToken(ctx, a)
	require.NoError(t, err)
	require.Equal(t, "token", token)
	lockKey, tokenKey := AntigravityTokenCacheKey(a), antigravityCredentialTokenCacheKey(a)
	discovered := snapshotOAuthRefreshAccount(a)
	discovered.Credentials["project_id"] = "discovered"
	repo.put(discovered)
	require.Equal(t, lockKey, AntigravityTokenCacheKey(discovered))
	require.NotEqual(t, tokenKey, antigravityCredentialTokenCacheKey(discovered))
	token, err = p.GetAccessToken(ctx, a)
	require.NoError(t, err)
	require.Equal(t, "token", token)
	require.Len(t, cache.tokens, 2)
}

func TestAntigravityTokenProvider_RefreshAndRevocationInvalidateCache(t *testing.T) {
	ctx := context.Background()
	a := isolatedAGAccount(1, "expired", 1)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &isolatedAGRepo{accounts: map[int64]*Account{1: snapshotOAuthRefreshAccount(a)}}
	cache := &isolatedAGCache{tokens: map[string]string{antigravityCredentialTokenCacheKey(a): "expired", "ag:shared-project": "legacy"}}
	p := NewAntigravityTokenProvider(repo, cache, nil)
	p.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &isolatedAGExecutor{})
	token, err := p.GetAccessToken(ctx, a)
	require.NoError(t, err)
	require.Equal(t, "refreshed", token)
	fresh, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.NotEqual(t, antigravityCredentialTokenCacheKey(a), antigravityCredentialTokenCacheKey(fresh))
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	require.NoError(t, invalidator.InvalidateToken(ctx, fresh))
	require.Empty(t, cache.tokens[antigravityCredentialTokenCacheKey(fresh)])
	require.Empty(t, cache.tokens["ag:shared-project"])
	fresh.Credentials["access_token"] = ""
	repo.put(fresh)
	require.NoError(t, invalidator.InvalidateToken(ctx, fresh))
	token, err = p.GetAccessToken(ctx, a)
	require.Error(t, err)
	require.Empty(t, token, "an older fingerprint still in cache cannot resurrect revoked credentials")
}

type failingAGSecondReadRepo struct {
	AccountRepository
	account *Account
	reads   int
}

func (r *failingAGSecondReadRepo) GetByID(context.Context, int64) (*Account, error) {
	r.reads++
	if r.reads > 1 {
		return nil, errors.New("credentials unavailable")
	}
	return snapshotOAuthRefreshAccount(r.account), nil
}

func TestAntigravityTokenProvider_FinalReadFailureStillReleasesLock(t *testing.T) {
	a := isolatedAGAccount(1, "near-expiry", 1)
	a.Credentials["expires_at"] = time.Now().Add(time.Minute).Format(time.RFC3339)
	repo := &failingAGSecondReadRepo{account: a}
	cache := &refreshAPICacheStub{lockResult: true}
	token, err := NewAntigravityTokenProvider(repo, cache, nil).GetAccessToken(context.Background(), a)
	require.ErrorContains(t, err, "credentials unavailable")
	require.Empty(t, token)
	require.Equal(t, 1, cache.releaseCalls)
	require.NotEmpty(t, cache.lockOwner)
	require.Equal(t, cache.lockOwner, cache.releasedOwner)
}
