//go:build unit

package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This opt-in gate uses an existing Redis server, never starts a container and
// only removes keys containing this invocation's random test namespace.
func TestAffinityRealRedisCAS(t *testing.T) {
	address := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set AFFINITY_REDIS_TEST_ADDR to the existing isolated Redis endpoint")
	}
	client := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("AFFINITY_REDIS_TEST_PASSWORD"), MaxRetries: -1})
	peer := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("AFFINITY_REDIS_TEST_PASSWORD"), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, client.Ping(ctx).Err())
	var nonce [16]byte
	_, err := rand.Read(nonce[:])
	require.NoError(t, err)
	session := "v2:binding:t6-test:" + hex.EncodeToString(nonce[:])
	key := buildSessionKey(1, session)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, peer.Del(cleanup, key).Err())
	})
	first, second := NewGatewayCache(client), NewGatewayCache(peer)
	request := func() context.Context { return service.WithAffinityBindingState(ctx) }
	left, right := request(), request()
	_, err = first.GetSessionAccountID(left, 1, session)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
	_, err = second.GetSessionAccountID(right, 1, session)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
	require.NoError(t, first.SetSessionAccountID(left, 1, session, 11, time.Minute))
	require.EqualValues(t, 0, client.Exists(ctx, key).Val(), "selection may not write")

	t.Run("concurrent_first_binding", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var ready sync.WaitGroup
		ready.Add(2)
		go func() {
			ready.Done()
			<-start
			results <- first.SetSessionAccountID(service.WithAffinityAdmission(left), 1, session, 11, time.Minute)
		}()
		go func() {
			ready.Done()
			<-start
			results <- second.SetSessionAccountID(service.WithAffinityAdmission(right), 1, session, 22, time.Minute)
		}()
		ready.Wait()
		close(start)
		require.NoError(t, <-results)
		require.NoError(t, <-results)
		raw := client.Get(ctx, key).Val()
		require.NotEmpty(t, raw)
		leftVersion, _ := service.AffinityBindingStateFromContext(left).Versions.Load(key)
		rightVersion, _ := service.AffinityBindingStateFromContext(right).Versions.Load(key)
		require.True(t, (leftVersion == raw && rightVersion == "") || (rightVersion == raw && leftVersion == ""), "exactly one contender must have committed")
		winner, err := first.GetSessionAccountID(ctx, 1, session)
		require.NoError(t, err)
		loser := left
		loserID := int64(11)
		if winner == 11 {
			loser = right
			loserID = 22
		}
		ttl := client.PTTL(ctx, key).Val()
		require.Positive(t, ttl)
		require.LessOrEqual(t, ttl, time.Minute)
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(loser), 1, session, loserID, time.Hour))
		require.NoError(t, second.DeleteSessionAccountID(loser, 1, session))
		require.Equal(t, raw, client.Get(ctx, key).Val())
		require.LessOrEqual(t, client.PTTL(ctx, key).Val(), ttl)
	})

	t.Run("old_failure_refresh_and_rebind", func(t *testing.T) {
		old, current := request(), request()
		account, err := first.GetSessionAccountID(old, 1, session)
		require.NoError(t, err)
		_, err = second.GetSessionAccountID(current, 1, session)
		require.NoError(t, err)
		raw := client.Get(ctx, key).Val()
		require.NoError(t, second.RefreshSessionTTL(current, 1, session, time.Hour))
		require.LessOrEqual(t, client.PTTL(ctx, key).Val(), time.Minute)
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(current), 1, session, account, time.Hour))
		renewed := client.Get(ctx, key).Val()
		require.NotEqual(t, raw, renewed)
		require.Greater(t, client.PTTL(ctx, key).Val(), 59*time.Minute)
		_, err = first.GetSessionAccountID(old, 1, session)
		require.NoError(t, err)
		require.NoError(t, first.DeleteSessionAccountID(old, 1, session))
		require.NoError(t, first.SetSessionAccountID(service.WithAffinityAdmission(old), 1, session, account, time.Minute))
		require.Equal(t, renewed, client.Get(ctx, key).Val())
		// Temporary rejection uses another account for this attempt only.
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(current), 1, session, 99, time.Minute))
		require.Equal(t, renewed, client.Get(ctx, key).Val())
		// Permanent revocation clears the matching version before replacement.
		require.NoError(t, second.DeleteSessionAccountID(current, 1, session))
		require.EqualValues(t, 0, client.Exists(ctx, key).Val())
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(current), 1, session, 99, time.Minute))
		replacement := client.Get(ctx, key).Val()
		require.NoError(t, first.DeleteSessionAccountID(old, 1, session))
		require.Equal(t, replacement, client.Get(ctx, key).Val())
	})

	t.Run("disconnect_reconnect_and_expiry", func(t *testing.T) {
		require.NoError(t, client.Close())
		_, err := first.GetSessionAccountID(request(), 1, session)
		require.Error(t, err)
		reconnected := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("AFFINITY_REDIS_TEST_PASSWORD"), MaxRetries: -1})
		t.Cleanup(func() { _ = reconnected.Close() })
		require.NoError(t, reconnected.Ping(ctx).Err())
		second = NewGatewayCache(reconnected)
		resumed := request()
		account, err := second.GetSessionAccountID(resumed, 1, session)
		require.NoError(t, err)
		require.EqualValues(t, 99, account)
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(resumed), 1, session, account, 20*time.Millisecond))
		require.Eventually(t, func() bool { return peer.Exists(ctx, key).Val() == 0 }, 2*time.Second, 10*time.Millisecond)
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(resumed), 1, session, account, time.Minute))
		require.EqualValues(t, 0, peer.Exists(ctx, key).Val(), "expired version cannot resurrect its binding")
		fresh := request()
		_, err = second.GetSessionAccountID(fresh, 1, session)
		require.ErrorIs(t, err, service.ErrStickySessionNotFound)
		require.NoError(t, second.SetSessionAccountID(service.WithAffinityAdmission(fresh), 1, session, 22, time.Minute))
		account, err = second.GetSessionAccountID(ctx, 1, session)
		require.NoError(t, err)
		require.EqualValues(t, 22, account)
	})
}
