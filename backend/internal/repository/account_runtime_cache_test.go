package repository

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCursorUnlockEvalErrorIsVisibleAndLeavesTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewCursorSessionCache(client)
	release, acquired, err := cache.Lock(context.Background(), "runtime-test-session")
	require.NoError(t, err)
	require.True(t, acquired)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	server.SetError("ERR forced eval failure")
	release()
	require.Contains(t, output.String(), "cursor_refresh_lock_release_failed")
	server.SetError("")
	require.True(t, server.Exists("runtime-test-session:refresh"))
	server.FastForward(31 * time.Second)
	require.False(t, server.Exists("runtime-test-session:refresh"))
}

func TestRuntimeCooldownSharedAcrossInstancesAndScopedSuccess(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	first, _ := NewTempUnschedCache(client).(service.RuntimeCooldownCache)
	second, _ := NewTempUnschedCache(client).(service.RuntimeCooldownCache)
	account := &service.Account{ID: 8, Credentials: map[string]any{"api_key": "secret"}}
	key := service.AccountRuntimeCooldownKey(10, account, "actual-model")
	other := service.AccountRuntimeCooldownKey(11, account, "actual-model")
	ctx := context.Background()
	require.NoError(t, first.SetRuntimeCooldown(ctx, key, time.Now().Add(time.Minute)))
	until, err := second.GetRuntimeCooldown(ctx, key)
	require.NoError(t, err)
	require.True(t, time.Now().Before(until))
	require.NoError(t, second.DeleteRuntimeCooldown(ctx, other))
	until, err = first.GetRuntimeCooldown(ctx, key)
	require.NoError(t, err)
	require.False(t, until.IsZero())
	require.NoError(t, second.DeleteRuntimeCooldown(ctx, key))
	until, err = first.GetRuntimeCooldown(ctx, key)
	require.NoError(t, err)
	require.True(t, until.IsZero())
}

func TestFailureCountersRejectCrossIdentityAccumulationAndLateReset(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	old := &service.Account{ID: 4, Credentials: map[string]any{"api_key": "old"}}
	current := &service.Account{ID: 4, Credentials: map[string]any{"api_key": "new"}}
	oldCtx := service.WithAccountRuntimeIdentity(context.Background(), old)
	currentCtx := service.WithAccountRuntimeIdentity(context.Background(), current)
	for _, counter := range []struct {
		name      string
		increment func(context.Context) (int64, error)
		reset     func(context.Context) error
	}{
		{"timeout", func(ctx context.Context) (int64, error) {
			return NewTimeoutCounterCache(client).IncrementTimeoutCount(ctx, 4, 1)
		}, func(ctx context.Context) error { return NewTimeoutCounterCache(client).ResetTimeoutCount(ctx, 4) }},
		{"forbidden", func(ctx context.Context) (int64, error) {
			return NewOpenAI403CounterCache(client).IncrementOpenAI403Count(ctx, 4, 1)
		}, func(ctx context.Context) error { return NewOpenAI403CounterCache(client).ResetOpenAI403Count(ctx, 4) }},
		{"internal", func(ctx context.Context) (int64, error) {
			return NewInternal500CounterCache(client).IncrementInternal500Count(ctx, 4)
		}, func(ctx context.Context) error {
			return NewInternal500CounterCache(client).ResetInternal500Count(ctx, 4)
		}},
	} {
		t.Run(counter.name, func(t *testing.T) {
			count, err := counter.increment(oldCtx)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			count, err = counter.increment(currentCtx)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			require.NoError(t, counter.reset(oldCtx))
			count, err = counter.increment(currentCtx)
			require.NoError(t, err)
			require.EqualValues(t, 2, count)
		})
	}
}
