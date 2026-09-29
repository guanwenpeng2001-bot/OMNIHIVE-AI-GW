package service

import (
	"context"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 实例级网关准入闸门。
//
// 目标（T3-2.3）：给出一个跨用户的 inflight 上限，让任何一个用户/分组的突发流量
// 都不能把本实例的 goroutine、上游连接与内存打爆；超出的请求进入**有界**等待队列，
// 队列也满时立即 503 过载，而不是无限堆积。
//
// 边界说明（如实标注）：
//   - 这是**单实例**资源保护。多实例部署下每个实例各自保护自己，总量是 实例数 × 上限。
//     这是第一性原理的选择：被保护的是本进程的资源，跨实例的份额分配不属于本闸门的职责。
//   - 默认关闭（MaxInflight=0）。未配置时行为与加闸门前完全一致，不引入新门禁。
//   - 槽位随请求 context 结束释放：客户端取消、超时、正常返回都会归还。

// GatewayOverloadRetryAfterSeconds 是过载/排队超时给客户端的退避提示。
// 闸门槽位随请求结束持续归还，没有可等的“窗口”，1 秒是“稍后重试”的最小可信提示，
// 不是伪造的窗口重置时间。
const GatewayOverloadRetryAfterSeconds = 1

// ErrGatewayOverloaded 表示网关实例已到 inflight 上限且等待队列已满。
var ErrGatewayOverloaded = infraerrors.ServiceUnavailable(
	"GATEWAY_OVERLOADED",
	"Gateway is at capacity, please retry later",
)

// ErrGatewayAdmissionTimeout 表示排队等待超过配置的等位窗口仍未拿到槽位。
// 对客户端同样表现为过载（503），但原因可区分，便于运维定位。
var ErrGatewayAdmissionTimeout = infraerrors.ServiceUnavailable(
	"GATEWAY_ADMISSION_TIMEOUT",
	"Gateway admission wait timed out, please retry later",
)

// GatewayAdmissionConfig 是闸门的运维配置。
type GatewayAdmissionConfig struct {
	// MaxInflight 是本实例同时进行的网关请求上限，<=0 表示不设闸门。
	MaxInflight int
	// MaxWaiting 是有界等待队列长度，<=0 表示不允许等待（超出即过载）。
	MaxWaiting int
	// WaitTimeout 是单个请求在队列里的最长等待时间，<=0 表示只做一次立即尝试。
	WaitTimeout time.Duration
}

// Enabled 报告闸门是否配置为生效。
func (c GatewayAdmissionConfig) Enabled() bool { return c.MaxInflight > 0 }

// GatewayAdmissionGate 实现有界 inflight + 有界等待队列。
type GatewayAdmissionGate struct {
	cfg GatewayAdmissionConfig

	slots chan struct{}

	mu      sync.Mutex
	waiting int
}

// NewGatewayAdmissionGate 按配置构造闸门；MaxInflight<=0 时返回 nil（等于关闭）。
func NewGatewayAdmissionGate(cfg GatewayAdmissionConfig) *GatewayAdmissionGate {
	if !cfg.Enabled() {
		return nil
	}
	if cfg.MaxWaiting < 0 {
		cfg.MaxWaiting = 0
	}
	if cfg.WaitTimeout < 0 {
		cfg.WaitTimeout = 0
	}
	return &GatewayAdmissionGate{cfg: cfg, slots: make(chan struct{}, cfg.MaxInflight)}
}

// Config 返回闸门的生效配置（便于运维自检与测试断言）。
func (g *GatewayAdmissionGate) Config() GatewayAdmissionConfig {
	if g == nil {
		return GatewayAdmissionConfig{}
	}
	return g.cfg
}

// Inflight 返回当前占用的槽位数。
func (g *GatewayAdmissionGate) Inflight() int {
	if g == nil {
		return 0
	}
	return len(g.slots)
}

// Waiting 返回当前在队列中等位的请求数。
func (g *GatewayAdmissionGate) Waiting() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.waiting
}

// Acquire 取得一个 inflight 槽位。
//
// 返回的 release 必须被调用且只调用一次；ctx 结束不会自动归还槽位，
// 调用方（请求计划 lease 或 context.AfterFunc）负责释放。
// 队列已满或等位超时返回 ErrGatewayOverloaded / ErrGatewayAdmissionTimeout。
func (g *GatewayAdmissionGate) Acquire(ctx context.Context) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	release := func() {
		select {
		case <-g.slots:
		default:
		}
	}
	// 快路径：还有余量时不进队列。
	select {
	case g.slots <- struct{}{}:
		return release, nil
	default:
	}
	if g.cfg.MaxWaiting <= 0 || g.cfg.WaitTimeout <= 0 {
		return nil, ErrGatewayOverloaded
	}
	if !g.reserveWaiter() {
		return nil, ErrGatewayOverloaded
	}
	defer g.releaseWaiter()

	timer := time.NewTimer(g.cfg.WaitTimeout)
	defer timer.Stop()
	select {
	case g.slots <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrGatewayAdmissionTimeout
	}
}

// AcquireForPlan 把槽位挂到请求计划上，让既有的 lease 生命周期负责归还：
//   - 计划已结束（取消/完成）→ 立即归还，不占额度；
//   - 有请求计划 → 计划 finalize/cancel 时统一释放，重试不会重复占用。
//
// 没有请求计划时退回 ctx 驱动释放，保证取消同样归还。
func (g *GatewayAdmissionGate) AcquireForPlan(ctx context.Context, plan *GatewayRequestPlan) error {
	if g == nil {
		return nil
	}
	release, err := g.Acquire(ctx)
	if err != nil {
		return err
	}
	if plan == nil {
		if ctx != nil {
			context.AfterFunc(ctx, release)
		}
		return nil
	}
	plan.Acquire(release)
	return nil
}

func (g *GatewayAdmissionGate) reserveWaiter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.waiting >= g.cfg.MaxWaiting {
		return false
	}
	g.waiting++
	return true
}

func (g *GatewayAdmissionGate) releaseWaiter() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.waiting > 0 {
		g.waiting--
	}
}
