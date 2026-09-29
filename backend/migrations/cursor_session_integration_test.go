//go:build integration

package migrations_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCursorRealRedisSessionCache(t *testing.T) {
	address := os.Getenv("CURSOR_TEST_REDIS_ADDR")
	if testresources.External() {
		require.NoError(t, testresources.ValidateRedis(address))
	}
	if address == "" {
		t.Skip("set CURSOR_TEST_REDIS_ADDR to run Redis integration")
	}
	var client *redis.Client
	if testresources.External() {
		client = testresources.RedisClient(t, &redis.Options{Addr: address})
	} else {
		client = redis.NewClient(&redis.Options{Addr: address, DB: 15})
		defer func() { require.NoError(t, client.Close()) }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := fmt.Sprintf("cursor:integration:%d", time.Now().UnixNano())
	defer client.Del(context.Background(), key, key+":refresh")
	first, second := repository.NewCursorSessionCache(client), repository.NewCursorSessionCache(client)
	session := cursor.Session{Token: "test-derived-jwt", ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, first.Set(ctx, key, session))
	cached, err := second.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, session.Token, cached.Token)
	release, acquired, err := first.Lock(ctx, key)
	require.NoError(t, err)
	require.True(t, acquired)
	_, acquired, err = second.Lock(ctx, key)
	require.NoError(t, err)
	require.False(t, acquired)
	release()
	release, acquired, err = second.Lock(ctx, key)
	require.NoError(t, err)
	require.True(t, acquired)
	release()
	ttl, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 50*time.Second)
	require.LessOrEqual(t, ttl, time.Minute)
}
