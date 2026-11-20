//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCursorRedisSessionTTLAndLockOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	cache := NewCursorSessionCache(client)
	ctx := context.Background()
	require.NoError(t, cache.Set(ctx, "session", cursor.Session{Token: "jwt", ExpiresAt: time.Now().Add(time.Minute)}))
	session, err := cache.Get(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, "jwt", session.Token)
	release, acquired, err := cache.Lock(ctx, "session")
	require.NoError(t, err)
	require.True(t, acquired)
	_, acquired, err = cache.Lock(ctx, "session")
	require.NoError(t, err)
	require.False(t, acquired)
	server.FastForward(31 * time.Second)
	newRelease, acquired, err := cache.Lock(ctx, "session")
	require.NoError(t, err)
	require.True(t, acquired)
	release()
	_, acquired, err = cache.Lock(ctx, "session")
	require.NoError(t, err)
	require.False(t, acquired)
	newRelease()
	server.FastForward(time.Minute)
	_, err = cache.Get(ctx, "session")
	require.Error(t, err)
}
