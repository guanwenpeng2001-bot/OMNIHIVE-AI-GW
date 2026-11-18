package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This opt-in gate uses an existing isolated Redis server, never starts a
// container, and only touches keys inside this invocation's random namespace.
// Run it with an isolated endpoint, never the default port:
//
//	AFFINITY_REDIS_TEST_ADDR=127.0.0.1:6380 SUB2API_TEST_EXTERNAL_SERVICES=1 \
//	  go test -count=1 -run TestRuntimeCooldownRecoverySingleProbeOnRealRedis -v ./internal/repository/
func TestRuntimeCooldownRecoverySingleProbeOnRealRedis(t *testing.T) {
	address := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set AFFINITY_REDIS_TEST_ADDR to the existing isolated Redis endpoint")
	}

	// Every instance gets its own client, standing in for a separate gateway process.
	const instanceCount = 12
	namespace := "test:isolated-resource-adaptation:" + uuid.NewString() + ":"
	options := &redis.Options{Addr: address, MaxRetries: -1}
	clients := make([]*redis.Client, instanceCount)
	caches := make([]service.RuntimeCooldownCache, instanceCount)
	instances := make([]*service.AntigravityGatewayService, instanceCount)
	for index := range clients {
		clients[index] = testresources.RedisClientWithNamespace(t, options, namespace)
		tempUnsched := NewTempUnschedCache(clients[index])
		cache, ok := tempUnsched.(service.RuntimeCooldownCache)
		require.True(t, ok, "temp unsched cache must implement the runtime cooldown contract")
		caches[index] = cache
		instances[index] = service.NewAntigravityGatewayService(nil, nil, nil, nil,
			service.NewRateLimitService(nil, nil, nil, nil, tempUnsched), nil, nil, nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, clients[0].Ping(ctx).Err())

	account := &service.Account{ID: 6601, Credentials: map[string]any{"access_token": "real-redis-probe"}}
	cooldownKey := service.AccountRuntimeCooldownKey(31, account, "gemini-3-pro-high")
	cooldownRedisKey := namespace + "runtime_cooldown:" + cooldownKey
	probeRedisKey := namespace + "runtime_cooldown_probe:" + cooldownKey

	// 冷却到期但记录仍在恢复窗口内：写入一段很短的冷却，等它自然到期。
	require.NoError(t, caches[0].SetRuntimeCooldown(ctx, cooldownKey, time.Now().Add(300*time.Millisecond)))
	time.Sleep(450 * time.Millisecond)
	until, recorded, err := caches[0].GetRuntimeCooldown(ctx, cooldownKey)
	require.NoError(t, err)
	require.True(t, recorded, "the record must survive the cooldown deadline inside the recovery window")
	require.True(t, time.Now().After(until), "the cooldown itself must already be over")
	require.Positive(t, clients[0].PTTL(ctx, cooldownRedisKey).Val(), "the recovery window must still be alive in Redis")

	// 判据 5：12 个并发实例里恰好 1 个拿到恢复试探租约。
	probes := make([]*service.RuntimeCooldownProbe, instanceCount)
	admitted := make([]bool, instanceCount)
	var ready, finished sync.WaitGroup
	ready.Add(instanceCount)
	finished.Add(instanceCount)
	start := make(chan struct{})
	for index := range instances {
		go func(instanceIndex int) {
			defer finished.Done()
			ready.Done()
			<-start
			probes[instanceIndex], admitted[instanceIndex] = instances[instanceIndex].AdmitRuntimeCooldownRecovery(ctx, cooldownKey)
		}(index)
	}
	ready.Wait()
	close(start)
	finished.Wait()

	admittedCount := 0
	winnerIndex := -1
	for index, ok := range admitted {
		if ok {
			admittedCount++
			winnerIndex = index
		}
	}
	require.Equal(t, 1, admittedCount, "exactly one instance may probe after the cooldown expires")
	require.GreaterOrEqual(t, instanceCount, 8)
	require.NotNil(t, probes[winnerIndex], "the winner must hold the real Redis lease")
	for index, probe := range probes {
		if index != winnerIndex {
			require.Nil(t, probe, "losers must not hold a lease")
		}
	}
	t.Logf("instances=%d admitted=%d refused=%d winner_index=%d", instanceCount, admittedCount, instanceCount-admittedCount, winnerIndex)

	// 租约确实落在 Redis 上，且只属于胜出者。
	leaseOwner, err := clients[0].Get(ctx, probeRedisKey).Result()
	require.NoError(t, err)
	require.NotEmpty(t, leaseOwner)
	t.Logf("redis_lease_owner=%s", leaseOwner)
	require.EqualValues(t, 1, clients[0].Exists(ctx, probeRedisKey).Val())

	// 归属校验：非 owner 释放无效，胜出者释放才真正删键。
	require.NoError(t, caches[1].ReleaseRuntimeCooldownProbe(ctx, cooldownKey, "foreign-owner"))
	require.EqualValues(t, 1, clients[0].Exists(ctx, probeRedisKey).Val(), "a foreign owner must not release the lease")
	probes[winnerIndex].Release()
	require.EqualValues(t, 0, clients[0].Exists(ctx, probeRedisKey).Val(), "the winner must release the lease")

	// 租约释放后，下一个恢复窗口的请求可以再次被放行。
	nextProbe, nextAdmitted := instances[0].AdmitRuntimeCooldownRecovery(ctx, cooldownKey)
	require.True(t, nextAdmitted)
	require.NotNil(t, nextProbe)
	nextProbe.Release()
	require.EqualValues(t, 0, clients[0].Exists(ctx, probeRedisKey).Val())

	// 没有冷却记录时不占用租约：常规请求不该被恢复卡口拦住。
	require.NoError(t, caches[0].DeleteRuntimeCooldown(ctx, cooldownKey))
	normalProbe, normalAdmitted := instances[0].AdmitRuntimeCooldownRecovery(ctx, cooldownKey)
	require.True(t, normalAdmitted)
	require.Nil(t, normalProbe)
	require.EqualValues(t, 0, clients[0].Exists(ctx, probeRedisKey).Val())
}

// TestRuntimeCooldownProbeLeaseTTLGatingOnRealRedis pins the Redis lease
// primitives themselves: renewal is owner-checked and TTL-gated, and release is
// a compare-and-delete.
func TestRuntimeCooldownProbeLeaseTTLGatingOnRealRedis(t *testing.T) {
	address := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set AFFINITY_REDIS_TEST_ADDR to the existing isolated Redis endpoint")
	}
	namespace := "test:isolated-resource-adaptation:" + uuid.NewString() + ":"
	options := &redis.Options{Addr: address, MaxRetries: -1}
	holderClient := testresources.RedisClientWithNamespace(t, options, namespace)
	peerClient := testresources.RedisClientWithNamespace(t, options, namespace)
	holder, ok := NewTempUnschedCache(holderClient).(service.RuntimeCooldownCache)
	require.True(t, ok)
	peer, ok := NewTempUnschedCache(peerClient).(service.RuntimeCooldownCache)
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	probeKey := namespace + "runtime_cooldown_probe:ttl-gating"
	require.True(t, holderClient.SetNX(ctx, probeKey, "holder", time.Minute).Val())

	// 剩余租约充足：续期不发 PEXPIRE，TTL 保持不变。
	expiryBefore, err := holderClient.PTTL(ctx, probeKey).Result()
	require.NoError(t, err)
	held, err := holder.RenewRuntimeCooldownProbe(ctx, "ttl-gating", "holder", time.Minute, 10*time.Second)
	require.NoError(t, err)
	require.True(t, held)
	expiryAfter, err := holderClient.PTTL(ctx, probeKey).Result()
	require.NoError(t, err)
	require.InDelta(t, expiryBefore.Seconds(), expiryAfter.Seconds(), 1, "enough TTL must not be rewritten")

	// 剩余租约不足：续期把 TTL 拉回完整时长。
	require.NoError(t, holderClient.PExpire(ctx, probeKey, 200*time.Millisecond).Err())
	held, err = holder.RenewRuntimeCooldownProbe(ctx, "ttl-gating", "holder", time.Minute, 10*time.Second)
	require.NoError(t, err)
	require.True(t, held)
	extended, err := holderClient.PTTL(ctx, probeKey).Result()
	require.NoError(t, err)
	require.Greater(t, extended, 10*time.Second, "low TTL must be extended")

	// 非 owner 既不能续期也不能释放。
	held, err = peer.RenewRuntimeCooldownProbe(ctx, "ttl-gating", "peer", time.Minute, 10*time.Second)
	require.NoError(t, err)
	require.False(t, held, "a foreign owner must not renew the lease")
	require.NoError(t, peer.ReleaseRuntimeCooldownProbe(ctx, "ttl-gating", "peer"))
	require.EqualValues(t, 1, holderClient.Exists(ctx, probeKey).Val(), "a foreign owner must not release the lease")

	// owner 释放：compare-and-delete。
	require.NoError(t, holder.ReleaseRuntimeCooldownProbe(ctx, "ttl-gating", "holder"))
	require.EqualValues(t, 0, holderClient.Exists(ctx, probeKey).Val())
}
