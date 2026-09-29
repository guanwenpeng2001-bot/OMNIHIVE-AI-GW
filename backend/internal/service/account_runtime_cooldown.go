package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RuntimeCooldownCache extends the existing Redis temporary scheduling cache.
type RuntimeCooldownCache interface {
	GetRuntimeCooldown(context.Context, string) (time.Time, error)
	SetRuntimeCooldown(context.Context, string, time.Time) error
	DeleteRuntimeCooldown(context.Context, string) error
}

var runtimeCooldownFallback = struct {
	sync.Mutex
	entries map[string]time.Time
}{entries: make(map[string]time.Time)}

const runtimeCooldownFallbackLimit = 4096

func (service *AntigravityGatewayService) runtimeCooldownCache() RuntimeCooldownCache {
	if service == nil || service.rateLimitService == nil {
		return nil
	}
	cache, _ := service.rateLimitService.tempUnschedCache.(RuntimeCooldownCache)
	return cache
}

func (service *AntigravityGatewayService) runtimeCooldown(ctx context.Context, key string) time.Time {
	if cache := service.runtimeCooldownCache(); cache != nil {
		until, err := cache.GetRuntimeCooldown(ctx, key)
		if err == nil {
			return until
		}
		slog.Warn("runtime_cooldown_read_failed", "error", err)
	}
	runtimeCooldownFallback.Lock()
	defer runtimeCooldownFallback.Unlock()
	until := runtimeCooldownFallback.entries[key]
	if !time.Now().Before(until) {
		delete(runtimeCooldownFallback.entries, key)
	}
	return until
}

func (service *AntigravityGatewayService) setRuntimeCooldown(ctx context.Context, key string, until time.Time) {
	if cache := service.runtimeCooldownCache(); cache != nil {
		var err error
		if until.IsZero() {
			err = cache.DeleteRuntimeCooldown(ctx, key)
		} else {
			err = cache.SetRuntimeCooldown(ctx, key, until)
		}
		if err != nil {
			slog.Warn("runtime_cooldown_write_failed", "error", err)
		}
	}
	runtimeCooldownFallback.Lock()
	defer runtimeCooldownFallback.Unlock()
	if until.IsZero() {
		delete(runtimeCooldownFallback.entries, key)
		return
	}
	for entry, expiry := range runtimeCooldownFallback.entries {
		if !time.Now().Before(expiry) {
			delete(runtimeCooldownFallback.entries, entry)
		}
	}
	if len(runtimeCooldownFallback.entries) >= runtimeCooldownFallbackLimit {
		var oldestKey string
		var oldest time.Time
		for entry, expiry := range runtimeCooldownFallback.entries {
			if oldest.IsZero() || expiry.Before(oldest) {
				oldestKey, oldest = entry, expiry
			}
		}
		delete(runtimeCooldownFallback.entries, oldestKey)
	}
	runtimeCooldownFallback.entries[key] = until
}
