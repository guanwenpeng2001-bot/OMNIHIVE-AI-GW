package handler

import (
	"errors"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// writeAdmissionRejectionHeaders 写出本次准入拒绝可以给客户端的真实事实。
//
// 只暴露用户/组维度的有效限额：
//   - Retry-After / X-RateLimit-Reset-Requests 只在错误携带真实窗口重置时刻时写出，
//     没有窗口证据（如余额不足、上游账户额度）就什么都不写，绝不用本机时钟编造；
//   - X-RateLimit-Limit / Remaining 只来自按 RPM 判定的拒绝。
//
// 上游账号的额度、remaining、reset 都不是本函数的输入，因此不可能从这里泄漏。
func writeAdmissionRejectionHeaders(c *gin.Context, err error) {
	if c == nil || err == nil {
		return
	}
	// 过载退避策略：闸门容量持续归还，没有窗口可等，用固定最小提示。
	if errors.Is(err, service.ErrGatewayOverloaded) || errors.Is(err, service.ErrGatewayAdmissionTimeout) {
		c.Header("Retry-After", strconv.Itoa(service.GatewayOverloadRetryAfterSeconds))
		return
	}
	values, ok := service.AdmissionRateLimitFacts(err)
	if !ok {
		return
	}
	service.ApplyRateLimitHeaders(c.Writer.Header(), values, time.Now())
}
