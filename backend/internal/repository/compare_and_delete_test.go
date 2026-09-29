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
