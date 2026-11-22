//go:build unit

package redissession

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestStoreRoundTripAndSingleUse(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := New(rdb, "oauth:test", time.Minute)
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "sid", map[string]string{"state": "state"}))
	var got map[string]string
	ok, err := store.Get(ctx, "sid", &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "state", got["state"])

	ok, err = store.TryConsume(ctx, "sid")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = store.TryConsume(ctx, "sid")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, store.Delete(ctx, "sid"))
	ok, err = store.Get(ctx, "sid", &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSingleUseTracksSessionLifetime(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := t.Context()
	s := New(client, "test", 1499*time.Millisecond)
	consume := func(id string, want bool) {
		got, err := s.TryConsume(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	consume("missing", false)
	require.False(t, mr.Exists(s.usedKey("missing")))
	require.NoError(t, s.Set(ctx, "id", 1))
	var wg sync.WaitGroup
	wins := make(chan bool, 12)
	for i := 0; i < cap(wins); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.TryConsume(ctx, "id")
			if err != nil {
				t.Error(err)
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	count := 0
	for ok := range wins {
		if ok {
			count++
		}
	}
	require.Equal(t, 1, count)
	// A claim written by an older worker can have a truncated lifetime.
	mr.SetTTL(s.usedKey("id"), 1200*time.Millisecond)
	mr.FastForward(time.Second)
	consume("id", false)
	require.Equal(t, 499*time.Millisecond, mr.TTL(s.usedKey("id")))
	require.NoError(t, s.Set(ctx, "id", 2))
	mr.FastForward(1498 * time.Millisecond)
	consume("id", false)
	mr.FastForward(time.Millisecond)
	require.False(t, mr.Exists(s.usedKey("id")))
	consume("id", false)
	require.NoError(t, client.Set(ctx, s.dataKey("legacy"), "1", 0).Err())
	consume("legacy", true)
	mr.FastForward(time.Hour)
	consume("legacy", false)
}
