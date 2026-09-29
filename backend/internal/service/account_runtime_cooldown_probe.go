package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// 冷却恢复时的跨实例单次试探（台账 T3-1.3：恢复时分布式单次试探，避免每请求撞坏账号）。
//
// 卡口只有一个：(*AntigravityGatewayService).AdmitRuntimeCooldownRecovery。
// 判定顺序：
//  1. 没有冷却记录 → 放行且不持有租约（常规请求，与冷却无关）。
//  2. 冷却未到期 → 不放行（保持既有语义：直接返回上游错误，不再去撞账号）。
//  3. 冷却已到期（记录仍在恢复窗口内）→ 抢一个跨实例短租约：胜出者放行并负责
//     续期/释放，其余实例不放行，也不干等，按既有语义直接返回上游错误。
//  4. 没有跨实例存储（未接入 Redis）或租约存储报错 → 退化为既有行为：放行且不持有租约。
//
// 成功与失败的语义在调用方：试探成功清除冷却记录，试探失败重新进入冷却；
// 两条路径都由 defer 释放租约，租约本身不承担冷却状态。
const (
	// runtimeCooldownProbeLeaseTTL 覆盖一次容量试探的最长重试预算（60 次 × 1s），
	// 试探期间按需续期；实例崩溃时租约最多占用这么久，之后别的实例可以接手。
	runtimeCooldownProbeLeaseTTL = 90 * time.Second
	// runtimeCooldownProbeRenewBelow 剩余租约低于该值才续期，避免每次重试都无条件刷 TTL。
	runtimeCooldownProbeRenewBelow = runtimeCooldownProbeLeaseTTL / 2
	// runtimeCooldownProbeReleaseTimeout 释放租约走独立的短超时：客户端断连取消请求时，
	// 租约不能一直被占着等到 TTL 到期。
	runtimeCooldownProbeReleaseTimeout = 2 * time.Second
)

// RuntimeCooldownProbe 是一次已获批的跨实例恢复试探租约。
// 非 nil 表示本实例是本次恢复的唯一胜出者：试探结束必须 Release，
// 试探过程中按需 Renew（只有仍持有租约时才能延长）。
// Renew 与 Release 对 nil 接收者安全，调用方可以直接对"没有租约"的情况调用。
type RuntimeCooldownProbe struct {
	cache RuntimeCooldownCache
	key   string
	owner string
}

// AdmitRuntimeCooldownRecovery 是"冷却恢复是否放行试探"的唯一判定点。
//
// 返回值：
//   - (nil, false)：不放行。冷却中，或冷却已到期但别的实例正在试探。
//   - (nil, true)：放行且无需租约。没有冷却记录，或没有跨实例存储。
//   - (probe, true)：放行且本实例持有跨实例试探租约，调用方必须 Release。
func (service *AntigravityGatewayService) AdmitRuntimeCooldownRecovery(ctx context.Context, key string) (*RuntimeCooldownProbe, bool) {
	until, recorded := service.runtimeCooldown(ctx, key)
	if !recorded {
		return nil, true
	}
	if time.Now().Before(until) {
		return nil, false
	}
	cache := service.runtimeCooldownCache()
	if cache == nil {
		// 没有跨实例存储：不假装有协调，保持"到期后各自试探"的既有行为。
		return nil, true
	}
	owner := uuid.NewString()
	acquired, err := cache.AcquireRuntimeCooldownProbe(ctx, key, owner, runtimeCooldownProbeLeaseTTL)
	if err != nil {
		slog.Warn("runtime_cooldown_probe_acquire_failed", "key", key, "error", err)
		return nil, true
	}
	if !acquired {
		return nil, false
	}
	return &RuntimeCooldownProbe{cache: cache, key: key, owner: owner}, true
}

// Renew 按需延长租约：剩余租约充足时不发写命令。
// 租约丢失或续期失败只记录日志，不中断正在进行中的试探——最坏情况是租约到期后
// 别的实例也会试探一次，而不会让恢复彻底停住。
func (probe *RuntimeCooldownProbe) Renew(ctx context.Context) {
	if probe == nil || probe.cache == nil {
		return
	}
	held, err := probe.cache.RenewRuntimeCooldownProbe(ctx, probe.key, probe.owner, runtimeCooldownProbeLeaseTTL, runtimeCooldownProbeRenewBelow)
	if err != nil {
		slog.Warn("runtime_cooldown_probe_renew_failed", "key", probe.key, "error", err)
		return
	}
	if !held {
		slog.Warn("runtime_cooldown_probe_lease_lost", "key", probe.key)
	}
}

// Release 释放租约（只在自己仍是 owner 时删除）。
// 用独立短超时而不是请求 context：客户端断连不能把租约留到 TTL 到期。
func (probe *RuntimeCooldownProbe) Release() {
	if probe == nil || probe.cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeCooldownProbeReleaseTimeout)
	defer cancel()
	if err := probe.cache.ReleaseRuntimeCooldownProbe(ctx, probe.key, probe.owner); err != nil {
		slog.Warn("runtime_cooldown_probe_release_failed", "key", probe.key, "error", err)
	}
}

// markRuntimeCooldownRecovered 试探成功：清掉冷却记录，账号回到常规调度。
// 租约由调用方的 defer Release 释放。
func (service *AntigravityGatewayService) markRuntimeCooldownRecovered(ctx context.Context, cooldownKey string) {
	service.setRuntimeCooldown(ctx, cooldownKey, time.Time{})
}

// markRuntimeCooldownFailed 试探失败：账号重新进入冷却，冷却记录重新保留恢复窗口，
// 下一个窗口再放行一个新的跨实例胜出者。租约由调用方的 defer Release 释放。
func (service *AntigravityGatewayService) markRuntimeCooldownFailed(ctx context.Context, cooldownKey string) {
	service.setRuntimeCooldown(ctx, cooldownKey, time.Now().Add(antigravityModelCapacityCooldown))
}
