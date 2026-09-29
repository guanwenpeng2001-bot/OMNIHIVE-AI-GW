package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGeminiTokenCache_InvalidationPreservesRefreshLocks(t *testing.T) {
	for _, platform := range []string{service.PlatformAntigravity, service.PlatformGemini} {
		t.Run(platform, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			cache := NewGeminiTokenCache(client)
			ctx := context.Background()
			account := &service.Account{ID: 42, Platform: platform, Type: service.AccountTypeOAuth, Credentials: map[string]any{"project_id": "shared"}}
			prefix, currentKey := "gemini:", service.GeminiTokenCacheKey(account)
			if platform == service.PlatformAntigravity {
				prefix, currentKey = "ag:", service.AntigravityTokenCacheKey(account)
			}
			for _, key := range []string{prefix + "shared", currentKey} {
				require.NoError(t, cache.SetAccessToken(ctx, key, "stale-token", time.Hour))
				acquired, err := cache.AcquireRefreshLock(ctx, key, "current-owner", time.Minute)
				require.NoError(t, err)
				require.True(t, acquired)
			}
			require.NoError(t, service.NewCompositeTokenCacheInvalidator(cache).InvalidateToken(ctx, account))
			for _, key := range []string{prefix + "shared", currentKey} {
				token, err := cache.GetAccessToken(ctx, key)
				require.ErrorIs(t, err, redis.Nil)
				require.Empty(t, token)
				owner, err := server.Get(oauthRefreshLockKeyPrefix + key)
				require.NoError(t, err)
				require.Equal(t, "current-owner", owner)
			}
		})
	}
}

func TestGeminiTokenCache_ExpiredRefreshLeaseCannotReleaseNewOwner(t *testing.T) {
	for _, sharedCache := range []bool{true, false} {
		name := "separate_cache_instances"
		if sharedCache {
			name = "shared_cache_instance"
		}
		t.Run(name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			cache := NewGeminiTokenCache(client)
			nextCache := cache
			if !sharedCache {
				nextCache = NewGeminiTokenCache(client)
			}
			ctx := context.Background()
			const cacheKey = "gemini:account:42"
			key := oauthRefreshLockKeyPrefix + cacheKey

			acquired, err := cache.AcquireRefreshLock(ctx, cacheKey, "old-owner", time.Second)
			require.NoError(t, err)
			require.True(t, acquired)
			value, err := server.Get(key)
			require.NoError(t, err)
			require.Equal(t, "old-owner", value)

			acquired, err = cache.AcquireRefreshLock(ctx, cacheKey, "contender", time.Minute)
			require.NoError(t, err)
			require.False(t, acquired)
			require.NoError(t, cache.ReleaseRefreshLock(ctx, cacheKey, "contender"))
			require.True(t, server.Exists(key))

			server.FastForward(time.Second)
			require.False(t, server.Exists(key))
			acquired, err = nextCache.AcquireRefreshLock(ctx, cacheKey, "new-owner", time.Minute)
			require.NoError(t, err)
			require.True(t, acquired)

			require.NoError(t, cache.ReleaseRefreshLock(ctx, cacheKey, "old-owner"))
			value, err = server.Get(key)
			require.NoError(t, err, "the expired holder must not delete the new holder's lock")
			require.Equal(t, "new-owner", value)
			require.Equal(t, time.Minute, server.TTL(key))
			acquired, err = cache.AcquireRefreshLock(ctx, cacheKey, "third-owner", time.Minute)
			require.NoError(t, err)
			require.False(t, acquired, "the new holder must still exclude other refreshers")

			require.NoError(t, nextCache.ReleaseRefreshLock(ctx, cacheKey, "new-owner"))
			require.False(t, server.Exists(key))
			require.NoError(t, nextCache.ReleaseRefreshLock(ctx, cacheKey, "new-owner"))
			acquired, err = cache.AcquireRefreshLock(ctx, cacheKey, "third-owner", time.Minute)
			require.NoError(t, err)
			require.True(t, acquired)
		})
	}
}

func TestGeminiTokenCache_RefreshLockRejectsEmptyOwner(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGeminiTokenCache(client)
	ctx := context.Background()

	acquired, err := cache.AcquireRefreshLock(ctx, "account", "", time.Minute)
	require.Error(t, err)
	require.False(t, acquired)
	require.Empty(t, server.Keys())
	acquired, err = cache.AcquireRefreshLock(ctx, "account", "owner", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Error(t, cache.ReleaseRefreshLock(ctx, "account", ""))
	require.True(t, server.Exists(oauthRefreshLockKeyPrefix+"account"))
}
