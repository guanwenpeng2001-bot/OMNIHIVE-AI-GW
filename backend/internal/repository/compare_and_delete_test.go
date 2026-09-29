//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCompareAndDelete(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	const key = "domain:lock:42"
	require.NoError(t, client.Set(ctx, key, "current-owner", time.Minute).Err())
	require.NoError(t, client.Set(ctx, key+":other", "current-owner", time.Minute).Err())

	require.NoError(t, compareAndDelete(ctx, client, key, "stale-owner"))
	owner, err := server.Get(key)
	require.NoError(t, err)
	require.Equal(t, "current-owner", owner)
	require.Equal(t, time.Minute, server.TTL(key))

	require.NoError(t, compareAndDelete(ctx, client, key, "current-owner"))
	require.False(t, server.Exists(key))
	require.True(t, server.Exists(key+":other"), "only the full key supplied may be deleted")
	require.NoError(t, compareAndDelete(ctx, client, key, "current-owner"), "missing keys are no-ops")
}

func TestCompareAndDeleteMatched(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	const key = "affinity:v2:gemini:v2:binding:hmac:k1:session"
	const current = "v2:42:new-nonce"
	for _, tc := range []struct {
		name, expected   string
		present, matched bool
	}{
		{"matched", current, true, true},
		{"same account different nonce", "v2:42:old-nonce", true, false},
		{"missing", current, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.present {
				require.NoError(t, client.Set(ctx, key, current, time.Minute).Err())
			}
			matched, err := compareAndDeleteMatched(ctx, client, key, tc.expected)
			require.NoError(t, err)
			require.Equal(t, tc.matched, matched)
			if tc.present && !tc.matched {
				value, err := server.Get(key)
				require.NoError(t, err)
				require.Equal(t, current, value)
				require.Equal(t, time.Minute, server.TTL(key))
			} else {
				require.False(t, server.Exists(key))
			}
			server.Del(key)
		})
	}
}
