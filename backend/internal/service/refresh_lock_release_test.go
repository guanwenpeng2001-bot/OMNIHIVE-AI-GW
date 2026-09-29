//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type refreshLockReleaseCache struct {
	GeminiTokenCache
	release func(context.Context, string, string) error
}

func (c *refreshLockReleaseCache) ReleaseRefreshLock(ctx context.Context, key, owner string) error {
	return c.release(ctx, key, owner)
}

func TestReleaseRefreshLockDetachedAndBounded(t *testing.T) {
	type contextKey struct{}
	for _, parentState := range []string{"canceled", "expired", "nil"} {
		t.Run(parentState, func(t *testing.T) {
			var parent context.Context
			if parentState != "nil" {
				parent = context.WithValue(context.Background(), contextKey{}, "request-value")
				var cancel context.CancelFunc
				if parentState == "expired" {
					parent, cancel = context.WithDeadline(parent, time.Now().Add(-time.Second))
				} else {
					parent, cancel = context.WithCancel(parent)
				}
				cancel()
				require.Error(t, parent.Err())
			}
			started := time.Now()
			var cleanup context.Context
			cache := &refreshLockReleaseCache{release: func(ctx context.Context, key, owner string) error {
				cleanup = ctx
				require.NoError(t, ctx.Err(), "cleanup must survive parent cancellation")
				require.Equal(t, "domain:account:42", key)
				require.Equal(t, "original-owner", owner)
				deadline, ok := ctx.Deadline()
				require.True(t, ok, "cleanup must have a deadline")
				require.False(t, deadline.Before(started.Add(defaultRefreshLockReleaseTimeout)))
				require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
				if parent != nil {
					require.Equal(t, "request-value", ctx.Value(contextKey{}))
				}
				return nil
			}}
			releaseRefreshLock(parent, cache, "domain:account:42", "original-owner")
			require.NotNil(t, cleanup)
			require.ErrorIs(t, cleanup.Err(), context.Canceled, "release must dispose of the cleanup context")
		})
	}
}

func TestReleaseRefreshLockLogsFailure(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cache := &refreshLockReleaseCache{release: func(context.Context, string, string) error {
		return errors.New("cache unavailable")
	}}
	releaseRefreshLock(context.Background(), cache, "domain:account:42", "secret-owner")
	require.Contains(t, logs.String(), "oauth_refresh_lock_release_failed")
	require.Contains(t, logs.String(), "domain:account:42")
	require.Contains(t, logs.String(), "cache unavailable")
	require.NotContains(t, logs.String(), "secret-owner")
}
