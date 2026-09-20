package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestDingTalkTokenCacheOwnershipAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	cache := provideDingTalkTokenCache(client)
	ctx := context.Background()
	token, err := cache.Get(ctx, "token")
	require.NoError(t, err)
	require.Empty(t, token)
	acquired, err := cache.AcquireRefresh(ctx, "lock", "first", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = cache.AcquireRefresh(ctx, "lock", "second", 30*time.Second)
	require.NoError(t, err)
	require.False(t, acquired)
	published, err := cache.Publish(ctx, "lock", "token", "first", "first-token", 7000*time.Second)
	require.NoError(t, err)
	require.True(t, published)
	require.Equal(t, 7000*time.Second, server.TTL("token"))
	server.FastForward(31 * time.Second)
	acquired, err = cache.AcquireRefresh(ctx, "lock", "second", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	published, err = cache.Publish(ctx, "lock", "token", "second", "new-token", time.Hour)
	require.NoError(t, err)
	require.True(t, published)
	published, err = cache.Publish(ctx, "lock", "token", "first", "stale-token", time.Hour)
	require.NoError(t, err)
	require.False(t, published)
	require.NoError(t, cache.ReleaseRefresh(ctx, "lock", "first"))
	owner, err := server.Get("lock")
	require.NoError(t, err)
	require.Equal(t, "second", owner)
	token, err = cache.Get(ctx, "token")
	require.NoError(t, err)
	require.Equal(t, "new-token", token)
	require.NoError(t, cache.ReleaseRefresh(ctx, "lock", "second"))
	require.False(t, server.Exists("lock"))
	server.FastForward(time.Hour)
	token, err = cache.Get(ctx, "token")
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestDingTalkTokenCacheErrors(t *testing.T) {
	require.Nil(t, provideDingTalkTokenCache(nil))
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	cache := provideDingTalkTokenCache(client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cache.Get(ctx, "token")
	require.ErrorIs(t, err, context.Canceled)
	_, err = cache.AcquireRefresh(ctx, "lock", "owner", time.Second)
	require.ErrorIs(t, err, context.Canceled)
	_, err = cache.Publish(ctx, "lock", "token", "owner", "value", time.Second)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, cache.ReleaseRefresh(ctx, "lock", "owner"), context.Canceled)
	server.SetError("ERR unavailable")
	_, err = cache.Get(context.Background(), "token")
	require.EqualError(t, err, "ERR unavailable")
}
