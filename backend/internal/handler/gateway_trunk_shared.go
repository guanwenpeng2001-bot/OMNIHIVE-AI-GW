package handler

// 本文件集中 Claude 主干（GatewayHandler）与 OpenAI 主干（OpenAIGatewayHandler）
// 共用的计费任务提交与上游错误映射实现。两条主干只差日志组件名/事件名和协议族，
// 决策只在这里实现一次，避免同一语义在两条主干上漂移。

import (
	"context"
	"net/http"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"go.uber.org/zap"
)

// usageRecordTaskLogger 描述一条主干在计费任务降级/panic 路径上的日志标识。
// 组件名与事件名保持各主干既有取值，避免改变现有告警与日志检索口径。
type usageRecordTaskLogger struct {
	component    string
	stoppedEvent string
	panicEvent   string
}

// submitUsageRecordTaskWithFallback 把计费任务交给共享 worker 池。
// 池未注入或已停止（进程关停窗口）时降级为有界内联同步执行：计费任务不能静默丢失，
// 而显式配置的 drop/sample 溢出丢弃仍按配置语义保留。
func submitUsageRecordTaskWithFallback(
	parent context.Context,
	pool *service.UsageRecordWorkerPool,
	task service.UsageRecordTask,
	log usageRecordTaskLogger,
) {
	if task == nil {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	if pool != nil {
		if mode := pool.Submit(task); mode != service.UsageRecordSubmitModeDroppedStopped {
			return
		}
		logger.L().With(zap.String("component", log.component)).Warn(log.stoppedEvent)
	}
	runUsageRecordTaskInline(task, log)
}

// submitMandatoryUsageRecordTaskInline 在队列边界之前冻结并持久化计费任务：
// 队列溢出时不允许静默丢失的强制路径，同样必须有界执行并回收 panic。
func submitMandatoryUsageRecordTaskInline(parent context.Context, task service.UsageRecordTask, log usageRecordTaskLogger) {
	if task == nil || !claimGatewayUsage(parent) {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	runUsageRecordTaskInline(task, log)
}

// runUsageRecordTaskInline 用独立的有界超时执行任务；上游请求上下文可能已经取消，
// 因此这里刻意从 Background 起算，避免客户端断开导致计费证据丢失。
func runUsageRecordTaskInline(task service.UsageRecordTask, log usageRecordTaskLogger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", log.component),
				zap.Any("panic", recovered),
			).Error(log.panicEvent)
		}
	}()
	task(ctx)
}

// upstreamClientErrorMapping 把上游状态码映射为客户端可见的 (status, error type, message)。
// 两条主干共用同一张表，避免同一状态码在不同主干给出不同错误类型。
// 529 的 error type 是唯一按协议族区分的一格：Anthropic 主干报 overloaded_error，
// OpenAI 主干保持 upstream_error；其余分支两条主干完全一致。
func upstreamClientErrorMapping(statusCode int, anthropicFamily bool) (int, string, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"
	case 529:
		if anthropicFamily {
			return http.StatusServiceUnavailable, "overloaded_error", "Upstream service overloaded, please retry later"
		}
		return http.StatusServiceUnavailable, "upstream_error", "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}
