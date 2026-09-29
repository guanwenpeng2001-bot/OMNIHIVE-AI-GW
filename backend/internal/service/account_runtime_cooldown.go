package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RuntimeCooldownCache 扩展既有的 Redis 临时调度缓存：既保存账号运行时冷却记录，
// 也保存"冷却到期后谁来试探"的跨实例租约。
//
// 冷却记录在冷却到期后仍会保留 RuntimeCooldownRecoveryWindow 一段时间，
// 否则"冷却中"和"冷却刚到期"在存储里无法区分，恢复阶段就只能每个请求都去撞账号。
type RuntimeCooldownCache interface {
	// GetRuntimeCooldown 返回冷却截止时间，以及是否仍有冷却记录。
	// 记录已到期但仍在恢复窗口内时返回 (过去的时刻, true)。
	GetRuntimeCooldown(context.Context, string) (time.Time, bool, error)
	SetRuntimeCooldown(context.Context, string, time.Time) error
	DeleteRuntimeCooldown(context.Context, string) error

	// AcquireRuntimeCooldownProbe 以 owner 身份占用恢复试探租约（SET NX + TTL）。
	AcquireRuntimeCooldownProbe(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	// RenewRuntimeCooldownProbe 只在 owner 仍持有租约、且剩余租约不足时才延长，
	// 返回 owner 是否仍然持有该租约。
	RenewRuntimeCooldownProbe(ctx context.Context, key, owner string, ttl, renewBelow time.Duration) (bool, error)
	// ReleaseRuntimeCooldownProbe 只在自己仍是 owner 时释放租约（compare-and-delete），
	// 避免删除别人重新抢到的同名租约。
	ReleaseRuntimeCooldownProbe(ctx context.Context, key, owner string) error
}

// RuntimeCooldownRecoveryWindow 是冷却到期后冷却记录仍保留的时间。
// 这段时间内该范围的恢复由一个跨实例胜出者试探，其余请求按既有语义快速失败。
// 必须长于一次试探的租约时长，否则租约还在有效期内、恢复窗口却已经消失，
// 新的请求会跳过卡口直接试探。
const RuntimeCooldownRecoveryWindow = 2 * time.Minute

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

// runtimeCooldown 读取该范围的冷却记录，第二个返回值表示记录是否存在。
// 进程内回退（未接入 Redis，或 Redis 读失败）只覆盖冷却期本身：没有跨实例
// 存储时无法协调"只放行一个"，因此也不会伪造恢复窗口。
func (service *AntigravityGatewayService) runtimeCooldown(ctx context.Context, key string) (time.Time, bool) {
	if cache := service.runtimeCooldownCache(); cache != nil {
		until, recorded, err := cache.GetRuntimeCooldown(ctx, key)
		if err == nil {
			return until, recorded
		}
		slog.Warn("runtime_cooldown_read_failed", "error", err)
	}
	runtimeCooldownFallback.Lock()
	defer runtimeCooldownFallback.Unlock()
	until := runtimeCooldownFallback.entries[key]
	if !time.Now().Before(until) {
		delete(runtimeCooldownFallback.entries, key)
		return time.Time{}, false
	}
	return until, true
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
