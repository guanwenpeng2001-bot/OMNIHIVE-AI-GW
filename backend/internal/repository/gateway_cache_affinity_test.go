package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAffinityDeleteObservedVersion(t *testing.T) {
	for _, outcome := range []string{"matched", "mismatched", "missing"} {
		t.Run(outcome, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			cache := NewGatewayCache(client)
			ctx := service.WithAffinityBindingState(context.Background())
			const hash = "gemini:v2:binding:hmac:k1:0123456789abcdef"
			key := buildSessionKey(7, hash)
			const observed = "v2:42:old-nonce"
			require.NoError(t, client.Set(ctx, key, observed, time.Minute).Err())
			_, err := cache.GetSessionAccountID(ctx, 7, hash)
			require.NoError(t, err)
			switch outcome {
			case "mismatched":
				require.NoError(t, client.Set(ctx, key, "v2:42:new-nonce", time.Minute).Err())
			case "missing":
				server.Del(key)
			}
			// A reread cannot grant ownership of the concurrent winner's nonce.
			_, _ = cache.GetSessionAccountID(ctx, 7, hash)
			require.NoError(t, cache.DeleteSessionAccountID(ctx, 7, hash))
			version, ok := service.AffinityBindingStateFromContext(ctx).Versions.Load(key)
			require.True(t, ok)
			if outcome == "matched" {
				require.Equal(t, "", version)
			} else {
				require.Equal(t, observed, version)
			}
			if outcome == "mismatched" {
				value, err := server.Get(key)
				require.NoError(t, err)
				require.Equal(t, "v2:42:new-nonce", value)
				require.Equal(t, time.Minute, server.TTL(key))
			} else {
				require.False(t, server.Exists(key))
			}
		})
	}
}

func TestResponsesSessionWindowDeleteMatchedContract(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, ok := NewGatewayCache(client).(service.OpenAIWSSessionPreemptionCache)
	require.True(t, ok)
	ctx := context.Background()
	owner := []byte("owner\x00full-value")
	_, err := cache.ClaimOpenAIResponsesSessionWindow(ctx, 7, "session", owner, time.Minute)
	require.NoError(t, err)
	matched, err := cache.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, 7, "session", []byte("owner"))
	require.NoError(t, err)
	require.False(t, matched)
	matched, err = cache.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, 7, "session", owner)
	require.NoError(t, err)
	require.True(t, matched)
	matched, err = cache.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, 7, "session", owner)
	require.NoError(t, err)
	require.False(t, matched)
}

func TestAffinityConcurrentFirstBinding(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	const key = "v2:binding:hmac:version:owner"
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := int64(1); i <= 20; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			cache := NewGatewayCache(client)
			ctx := service.WithAffinityAdmission(service.WithAffinityBindingState(context.Background()))
			results <- cache.SetSessionAccountID(ctx, 1, key, id, time.Hour)
		}(i)
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	cache := NewGatewayCache(client)
	winner, err := cache.GetSessionAccountID(context.Background(), 1, key)
	require.NoError(t, err)
	require.Greater(t, winner, int64(0))
	require.LessOrEqual(t, winner, int64(20))
	require.Equal(t, time.Hour, server.TTL(buildSessionKey(1, key)))
	_, err = cache.GetSessionAccountID(context.Background(), 2, key)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
}

func TestAffinityCASOldFailureAndRollingTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	first, second := NewGatewayCache(client), NewGatewayCache(client)
	key := "gemini:v2:binding:hmac:version:owner"
	request := func() context.Context { return service.WithAffinityBindingState(context.Background()) }
	seed := request()
	require.NoError(t, first.SetSessionAccountID(seed, 1, key, 11, time.Hour))
	_, err := first.GetSessionAccountID(seed, 1, key)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound, "selection cannot bind before admission")
	require.NoError(t, first.SetSessionAccountID(service.WithAffinityAdmission(seed), 1, key, 11, time.Hour))
	old := request()
	_, err = first.GetSessionAccountID(old, 1, key)
	require.NoError(t, err)
	server.FastForward(20 * time.Minute)
	current := request()
	_, err = second.GetSessionAccountID(current, 1, key)
	require.NoError(t, err)
	require.NoError(t, second.RefreshSessionTTL(current, 1, key, time.Hour))
	require.Equal(t, 40*time.Minute, server.TTL(buildSessionKey(1, key)), "selection must not refresh")
	require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(current), 1, key, 11, time.Hour))
	require.Equal(t, time.Hour, server.TTL(buildSessionKey(1, key)))
	// Even a reread by the old failure cannot acquire the new version.
	_, err = first.GetSessionAccountID(old, 1, key)
	require.NoError(t, err)
	require.NoError(t, first.DeleteSessionAccountID(old, 1, key))
	id, err := second.GetSessionAccountID(context.Background(), 1, key)
	require.NoError(t, err)
	require.Equal(t, int64(11), id)
	// A temporary profit/load rejection preserves the healthy binding.
	require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(current), 1, key, 22, time.Hour))
	id, err = second.GetSessionAccountID(context.Background(), 1, key)
	require.NoError(t, err)
	require.Equal(t, int64(11), id)
	require.NoError(t, second.DeleteSessionAccountID(current, 1, key))
	require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(current), 1, key, 22, time.Hour))
	require.NoError(t, first.DeleteSessionAccountID(old, 1, key))
	id, err = second.GetSessionAccountID(context.Background(), 1, key)
	require.NoError(t, err)
	require.Equal(t, int64(22), id)
	// Old binaries' legacy keys cannot touch the new namespace.
	require.NoError(t, client.Set(context.Background(), "sticky_session:1:"+key, 99, time.Hour).Err())
	require.NoError(t, first.DeleteSessionAccountID(context.Background(), 1, key))
	id, err = second.GetSessionAccountID(context.Background(), 1, key)
	require.NoError(t, err)
	require.Equal(t, int64(22), id)
}

func TestAffinityRedisUnavailable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client)
	server.Close()
	ctx := service.WithAffinityAdmission(service.WithAffinityBindingState(context.Background()))
	_, err := cache.GetSessionAccountID(ctx, 1, "v2:binding:test")
	require.Error(t, err)
	require.Error(t, cache.SetSessionAccountID(ctx, 1, "v2:binding:test", 1, time.Hour))
}

func TestAffinityLegacyIdentityRetainsLegacyCacheContract(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client)
	ctx := context.Background()
	key := "v2:sha256:unmigrated-chat-identity"
	require.NoError(t, cache.SetSessionAccountID(ctx, 1, key, 12, time.Hour))
	id, err := cache.GetSessionAccountID(ctx, 1, key)
	require.NoError(t, err)
	require.Equal(t, int64(12), id)
	require.True(t, server.Exists("sticky_session:1:"+key))
	server.FastForward(20 * time.Minute)
	require.NoError(t, cache.RefreshSessionTTL(ctx, 1, key, time.Hour))
	require.Equal(t, time.Hour, server.TTL("sticky_session:1:"+key))
	require.NoError(t, cache.DeleteSessionAccountID(ctx, 1, key))
	_, err = cache.GetSessionAccountID(ctx, 1, key)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
}

func TestAffinityMissingStateAndInvalidTTLDoNotWrite(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client)
	const session = "v2:binding:guards"
	for _, ctx := range []context.Context{context.Background(), service.WithAffinityAdmission(context.Background()), service.WithAffinityBindingState(context.Background())} {
		require.NoError(t, cache.SetSessionAccountID(ctx, 1, session, 1, time.Minute))
		require.False(t, server.Exists(buildSessionKey(1, session)))
	}
	admitted := service.WithAffinityAdmission(service.WithAffinityBindingState(context.Background()))
	for _, ttl := range []time.Duration{-time.Second, 0, time.Nanosecond} {
		require.NoError(t, cache.SetSessionAccountID(admitted, 1, session, 1, ttl))
		require.False(t, server.Exists(buildSessionKey(1, session)))
	}
	require.NoError(t, cache.SetSessionAccountID(admitted, 1, session, 0, time.Minute))
	require.False(t, server.Exists(buildSessionKey(1, session)))
}
