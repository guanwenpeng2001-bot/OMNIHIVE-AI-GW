//go:build redis_integration && !integration

package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This opt-in test launches its own Redis with a private Unix socket and no TCP
// listener. It never consumes an external Redis address or flushes an existing
// database. Run with: go test -tags=redis_integration ./internal/repository
// -run TestSchedulerCacheAccountWriteRealRedis -count=1 -v
func TestSchedulerCacheAccountWriteRealRedis(t *testing.T) {
	t.Run("contract", func(t *testing.T) {
		rdb := newSchedulerWriteTestRedis(t)
		testSchedulerAccountWriteContract(t, rdb)
	})
	t.Run("expired values and rejected writes", func(t *testing.T) {
		rdb := newSchedulerWriteTestRedis(t)
		ctx := context.Background()
		cache := NewSchedulerCache(rdb)
		account := schedulerWriteTestAccount(9805, "expired")
		keys, payloads := schedulerWriteTestPayloads(t, account)
		for i, key := range keys {
			require.NoError(t, rdb.Set(ctx, key, payloads[i], 20*time.Millisecond).Err())
		}
		time.Sleep(30 * time.Millisecond)
		require.NoError(t, cache.SetAccount(ctx, &account))
		for i, key := range keys {
			require.Equal(t, payloads[i], rdb.Get(ctx, key).Val())
			require.Equal(t, time.Duration(-1), rdb.PTTL(ctx, key).Val())
		}

		// Verify a rejected EVAL is returned to the caller and cannot publish
		// a snapshot whose account data was never written.
		require.NoError(t, rdb.Do(ctx, "ACL", "SETUSER", "scheduler-test", "on", ">scheduler-test-password", "~*", "+@all", "-set").Err())
		restricted := redis.NewClient(&redis.Options{
			Network: "unix", Addr: rdb.Options().Addr,
			Username: "scheduler-test", Password: "scheduler-test-password", MaxRetries: -1,
		})
		t.Cleanup(func() { _ = restricted.Close() })
		require.Equal(t, "scheduler-test", restricted.Do(ctx, "ACL", "WHOAMI").Val())
		writer := NewSchedulerCache(restricted)
		account.Name = "rejected"
		assertSchedulerKeysUnchanged(t, rdb, keys, func() {
			require.Error(t, writer.SetAccount(ctx, &account))
		})
		bucket := service.SchedulerBucket{GroupID: 8, Platform: account.Platform, Mode: service.SchedulerModeSingle}
		token, err := cache.CaptureBucketWriteToken(ctx, bucket)
		require.NoError(t, err)
		require.Error(t, writer.SetSnapshot(ctx, bucket, token, []service.Account{account}))
		require.Zero(t, rdb.Exists(ctx, schedulerBucketKey(schedulerActivePrefix, bucket), schedulerBucketKey(schedulerReadyPrefix, bucket)).Val())
	})
	t.Run("AOF with six MiB account", func(t *testing.T) {
		rdb := newSchedulerWriteTestRedis(t)
		ctx := context.Background()
		cache := NewSchedulerCache(rdb)
		account := schedulerWriteTestAccount(9804, "large")
		// Both existing associations remain serialized, matching the observed
		// source of large production values without dropping business data.
		account.Groups[0].Name = strings.Repeat("g", 3*1024*1024)
		keys, payloads := schedulerWriteTestPayloads(t, account)
		require.Greater(t, len(payloads[0]), 6*1024*1024)
		for i, key := range keys {
			require.NoError(t, rdb.Set(ctx, key, payloads[i], 0).Err())
		}
		before := schedulerWriteTestAOFSize(t, rdb)
		for i := 0; i < 10; i++ {
			require.NoError(t, cache.SetAccount(ctx, &account))
		}
		after := schedulerWriteTestAOFSize(t, rdb)
		require.Equal(t, before, after, "identical values must append zero AOF bytes")
		t.Logf("full=%d bytes meta=%d bytes; 10 identical writes: AOF delta=%d", len(payloads[0]), len(payloads[1]), after-before)

		for _, key := range keys {
			require.NoError(t, rdb.PExpire(ctx, key, time.Hour).Err())
		}
		before = schedulerWriteTestAOFSize(t, rdb)
		require.NoError(t, cache.SetAccount(ctx, &account))
		delta := schedulerWriteTestAOFSize(t, rdb) - before
		require.Positive(t, delta)
		require.Less(t, delta, int64(512), "TTL repair must persist only small PERSIST commands")
		t.Logf("equal values with TTL: AOF delta=%d", delta)

		account.Credentials["access_token"] = "changed-secret"
		_, changed := schedulerWriteTestPayloads(t, account)
		before = schedulerWriteTestAOFSize(t, rdb)
		assertSchedulerKeysUnchanged(t, rdb, keys[1:], func() {
			require.NoError(t, cache.SetAccount(ctx, &account))
		})
		delta = schedulerWriteTestAOFSize(t, rdb) - before
		require.GreaterOrEqual(t, delta, int64(len(changed[0])))
		require.Less(t, delta, int64(len(changed[0])+512), "only the changed full payload should be appended")
		t.Logf("changed secret: AOF delta=%d", delta)

		require.NoError(t, rdb.Del(ctx, keys[1]).Err())
		before = schedulerWriteTestAOFSize(t, rdb)
		assertSchedulerKeysUnchanged(t, rdb, keys[:1], func() {
			require.NoError(t, cache.SetAccount(ctx, &account))
		})
		delta = schedulerWriteTestAOFSize(t, rdb) - before
		require.GreaterOrEqual(t, delta, int64(len(changed[1])))
		require.Less(t, delta, int64(len(changed[1])+512), "a missing meta key must not rewrite the large full value")
		t.Logf("missing metadata only: AOF delta=%d", delta)
	})
}

func newSchedulerWriteTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("isolated Redis test needs Unix sockets; run in Linux/WSL")
	}
	server := os.Getenv("SCHEDULER_TEST_REDIS_SERVER")
	if server == "" {
		var err error
		server, err = exec.LookPath("redis-server")
		if err != nil {
			t.Skip("redis-server not installed; set SCHEDULER_TEST_REDIS_SERVER to a local executable")
		}
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "redis.sock")
	logPath := filepath.Join(dir, "redis.log")
	cmd := exec.Command(server,
		"--port", "0", "--unixsocket", socket, "--unixsocketperm", "700",
		"--dir", dir, "--logfile", logPath, "--save", "",
		"--appendonly", "yes", "--appendfsync", "always",
		"--auto-aof-rewrite-percentage", "0",
	)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	rdb := redis.NewClient(&redis.Options{
		Network: "unix", Addr: socket, MaxRetries: -1,
		DialTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for rdb.Ping(context.Background()).Err() != nil {
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(logPath)
			t.Fatalf("local Redis did not start: %s", log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := rdb.Info(context.Background(), "server").Result()
	require.NoError(t, err)
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			t.Log(strings.TrimSpace(line))
		}
	}
	return rdb
}

func schedulerWriteTestAOFSize(t *testing.T, rdb *redis.Client) int64 {
	t.Helper()
	info, err := rdb.Info(context.Background(), "persistence").Result()
	require.NoError(t, err)
	for _, line := range strings.Split(info, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "aof_current_size:"); ok {
			size, err := strconv.ParseInt(value, 10, 64)
			require.NoError(t, err)
			return size
		}
	}
	t.Fatal("missing aof_current_size in INFO persistence")
	return 0
}
