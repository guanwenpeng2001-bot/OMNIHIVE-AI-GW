package service

import (
	"context"
	"log/slog"
	"time"
)

const defaultRefreshLockReleaseTimeout = 2 * time.Second

// releaseRefreshLock preserves request values while giving cleanup its own bounded
// lifetime, even when the request has already been canceled.
func releaseRefreshLock(parent context.Context, cache GeminiTokenCache, cacheKey string, lockOwner string) {
	cleanupParent := context.Background()
	if parent != nil {
		cleanupParent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(cleanupParent, defaultRefreshLockReleaseTimeout)
	defer cancel()
	if err := cache.ReleaseRefreshLock(ctx, cacheKey, lockOwner); err != nil {
		slog.Warn("oauth_refresh_lock_release_failed", "cache_key", cacheKey, "error", err)
	}
}
