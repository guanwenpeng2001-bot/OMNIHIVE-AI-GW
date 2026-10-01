//go:build unit

package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestProxyGroupCacheDedicatedRedis(t *testing.T) {
	if os.Getenv("IP_GROUP_DEDICATED_TESTS") != "1" {
		t.Skip("requires dedicated Redis 127.0.0.1:56389")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:56389", DB: 0})
	defer rdb.Close()
	require.NoError(t, rdb.Ping(ctx).Err())
	c := &proxyGroupCache{rdb: rdb, prefix: "test:ip-group:" + uuid.NewString() + ":"}
	keys := []string{c.cooledSetKey(), c.cooldownKey(11), c.cooldownKey(12)}
	for _, a := range []int64{1, 2, 3, 4} {
		key, err := c.bindingKey(7, a)
		require.NoError(t, err)
		keys = append(keys, key)
	}
	defer func() { require.NoError(t, rdb.Del(ctx, keys...).Err()) }()

	t.Run("atomic_winner_across_clients", func(t *testing.T) {
		second := redis.NewClient(&redis.Options{Addr: "127.0.0.1:56389", DB: 0})
		defer second.Close()
		other := &proxyGroupCache{rdb: second, prefix: c.prefix}
		var wg sync.WaitGroup
		winners := make(chan int64, 32)
		errs := make(chan error, 32)
		for i := int64(1); i <= 32; i++ {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				cache := c
				if id%2 == 0 {
					cache = other
				}
				winner, err := cache.TrySetBindingNX(ctx, 7, 1, id, time.Hour)
				winners <- winner
				errs <- err
			}(i)
		}
		wg.Wait()
		close(winners)
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		stored, err := c.GetBinding(ctx, 7, 1)
		require.NoError(t, err)
		require.Positive(t, stored)
		for winner := range winners {
			require.Equal(t, stored, winner)
		}
		ttl, err := c.GetBindingTTL(ctx, 7, 1)
		require.NoError(t, err)
		require.Greater(t, ttl, 59*time.Minute)
	})

	t.Run("compare_and_swap_delete_and_sliding_touch", func(t *testing.T) {
		winner, err := c.TrySetBindingNX(ctx, 7, 2, 11, 800*time.Millisecond)
		require.NoError(t, err)
		require.EqualValues(t, 11, winner)
		before, err := c.GetBindingTTL(ctx, 7, 2)
		require.NoError(t, err)
		touched, err := c.TouchBindingIfValue(ctx, 7, 2, 12, time.Hour)
		require.NoError(t, err)
		require.False(t, touched)
		after, err := c.GetBindingTTL(ctx, 7, 2)
		require.NoError(t, err)
		require.LessOrEqual(t, after, before)
		deleted, err := c.DeleteBindingIfValue(ctx, 7, 2, 12)
		require.NoError(t, err)
		require.False(t, deleted)
		touched, err = c.TouchBindingIfValue(ctx, 7, 2, 11, 2*time.Second)
		require.NoError(t, err)
		require.True(t, touched)
		ttl, err := c.GetBindingTTL(ctx, 7, 2)
		require.NoError(t, err)
		require.Greater(t, ttl, time.Second)
		deleted, err = c.DeleteBindingIfValue(ctx, 7, 2, 11)
		require.NoError(t, err)
		require.True(t, deleted)
		winner, err = c.TrySetBindingNX(ctx, 7, 2, 12, time.Hour)
		require.NoError(t, err)
		require.EqualValues(t, 12, winner)
		deleted, err = c.DeleteBindingIfValue(ctx, 7, 2, 11)
		require.NoError(t, err)
		require.False(t, deleted)
		touched, err = c.TouchBindingIfValue(ctx, 7, 2, 11, time.Millisecond)
		require.NoError(t, err)
		require.False(t, touched)
		winner, err = c.GetBinding(ctx, 7, 2)
		require.NoError(t, err)
		require.EqualValues(t, 12, winner)
	})

	t.Run("expiry_and_invalid_bindings", func(t *testing.T) {
		_, err := c.TrySetBindingNX(ctx, 7, 3, 11, 50*time.Millisecond)
		require.NoError(t, err)
		require.Eventually(t, func() bool { v, err := c.GetBinding(ctx, 7, 3); return err == nil && v == 0 }, time.Second, 10*time.Millisecond)
		ttl, err := c.GetBindingTTL(ctx, 7, 3)
		require.NoError(t, err)
		require.Zero(t, ttl)
		_, err = c.TrySetBindingNX(ctx, 7, 0, 11, time.Hour)
		require.Error(t, err)
		_, err = c.TouchBindingIfValue(ctx, 7, 0, 11, time.Hour)
		require.Error(t, err)
		_, err = c.DeleteBindingIfValue(ctx, 7, 0, 11)
		require.Error(t, err)
		_, err = c.TrySetBindingNX(ctx, 7, 4, 0, time.Hour)
		require.Error(t, err)
		_, err = c.TrySetBindingNX(ctx, 7, 4, 11, 0)
		require.Error(t, err)
		zeroKey := fmt.Sprintf("%sbind:{7}:0", c.prefix)
		exists, err := rdb.Exists(ctx, zeroKey).Result()
		require.NoError(t, err)
		require.Zero(t, exists)
		key, _ := c.bindingKey(7, 4)
		require.NoError(t, rdb.Set(ctx, key, "11", 0).Err())
		_, err = c.GetBindingTTL(ctx, 7, 4)
		require.Error(t, err)
	})

	t.Run("fixed_cooldown_and_expired_index_cleanup", func(t *testing.T) {
		observed := time.Now().UTC().Truncate(time.Millisecond)
		first := service.ProxyCooldown{ProxyID: 11, Reason: "proxy_connect_failed", ObservedAt: observed}
		inserted, err := c.SetCooldownIfAbsent(ctx, first, 400*time.Millisecond)
		require.NoError(t, err)
		require.True(t, inserted)
		initial, err := c.GetCooldown(ctx, 11)
		require.NoError(t, err)
		require.Equal(t, first.Reason, initial.Reason)
		require.True(t, observed.Equal(initial.ObservedAt))
		inserted, err = c.SetCooldownIfAbsent(ctx, service.ProxyCooldown{ProxyID: 11, Reason: "second"}, time.Hour)
		require.NoError(t, err)
		require.False(t, inserted)
		unchanged, err := c.GetCooldown(ctx, 11)
		require.NoError(t, err)
		require.Equal(t, initial, unchanged)
		ttl, err := rdb.PTTL(ctx, c.cooldownKey(11)).Result()
		require.NoError(t, err)
		require.LessOrEqual(t, ttl, 400*time.Millisecond)
		inserted, err = c.SetCooldownIfAbsent(ctx, service.ProxyCooldown{ProxyID: 12, Reason: "proxy_dial_timeout"}, time.Hour)
		require.NoError(t, err)
		require.True(t, inserted)
		values, err := c.ListCooled(ctx)
		require.NoError(t, err)
		require.Len(t, values, 2)
		require.Eventually(t, func() bool {
			values, err := c.ListCooled(ctx)
			return err == nil && len(values) == 1 && values[0].ProxyID == 12
		}, 2*time.Second, 10*time.Millisecond)
		indexed, err := rdb.SIsMember(ctx, c.cooledSetKey(), "11").Result()
		require.NoError(t, err)
		require.False(t, indexed)
		require.NoError(t, c.ClearCooldown(ctx, 12))
		values, err = c.ListCooled(ctx)
		require.NoError(t, err)
		require.Empty(t, values)
		inserted, err = c.SetCooldownIfAbsent(ctx, first, time.Hour)
		require.NoError(t, err)
		require.True(t, inserted)
	})
}
