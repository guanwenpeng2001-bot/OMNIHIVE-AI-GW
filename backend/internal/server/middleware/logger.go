package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/logredact"
	"go.uber.org/zap"
)

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		startTime := time.Now()

		// 请求路径
		path := accessLogPath(c)
		if _, private := playgroundAuditActions[c.Request.Method+" "+c.FullPath()]; private {
			path = c.FullPath()
		}

		// 处理请求
		c.Next()

		// 跳过健康检查等高频探针路径的日志
		if path == "/health" || path == "/setup/status" {
			return
		}

		endTime := time.Now()
		latency := endTime.Sub(startTime)

		method := c.Request.Method
		statusCode := c.Writer.Status()
		if isWorkspaceV2(c) {
			// Request paths, arbitrary Gin errors and client-supplied context labels
			// can contain tickets or request content. The matched route is static.
			logger.FromContext(c.Request.Context()).Info("http request completed",
				zap.String("component", "http.access"), zap.String("path", path),
				zap.Int("status_code", statusCode), zap.Int64("latency_ms", latency.Milliseconds()))
			return
		}
		clientIP := ip.GetClientIP(c)
		protocol := c.Request.Proto
		accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
		platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
		model, _ := c.Request.Context().Value(ctxkey.Model).(string)
		reason, rejected := GetIngressRejectReason(c)
		if rejected {
			recordIngressReject(c, reason)
			allowed, droppedSummary := globalIngressRejectAccessSampler.allow(endTime)
			if droppedSummary > 0 {
				logger.FromContext(c.Request.Context()).Info("ingress rejection access logs dropped",
					zap.String("component", "http.access"),
					zap.Uint64("dropped_count", droppedSummary),
					zap.Bool(logger.OpsSystemLogSkipField, true),
				)
			}
			if !allowed {
				return
			}
		}

		fields := []zap.Field{
			zap.String("component", "http.access"),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("client_ip", clientIP),
			zap.String("protocol", protocol),
			zap.String("method", method),
			zap.String("path", path),
		}
		if rejected {
			fields = append(fields,
				zap.String("ingress_reject_reason", string(reason)),
				zap.Bool(logger.OpsSystemLogSkipField, true),
			)
		}
		if hasAccountID && accountID > 0 {
			fields = append(fields, zap.Int64("account_id", accountID))
		}
		if platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}
		if model != "" {
			fields = append(fields, zap.String("model", model))
		}

		l := logger.FromContext(c.Request.Context()).With(fields...)
		l.Info("http request completed", zap.Time("completed_at", endTime))

		if len(c.Errors) > 0 {
			l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
		}
	}
}

func isWorkspaceV2(c *gin.Context) bool {
	return strings.HasPrefix(c.Request.URL.Path, "/api/v2/workspaces/")
}

func accessLogPath(c *gin.Context) string {
	if isWorkspaceV2(c) {
		if path := c.FullPath(); path != "" {
			return path
		}
		return "/api/v2/workspaces/"
	}
	return redactPluginUIPath(c.Request.URL.Path)
}

// redactPluginUIPath removes the capability token from log fields without
// changing the request URL or the resource path used to serve the plugin UI.
func redactPluginUIPath(path string) string {
	const prefix = "/api/v1/plugin-ui/"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	token, resourcePath, hasResourcePath := strings.Cut(strings.TrimPrefix(path, prefix), "/")
	if token == "" {
		return path
	}
	redacted := logredact.RedactMap(map[string]any{"token": token}, "token")
	redactedToken, _ := redacted["token"].(string)
	path = prefix + redactedToken
	if hasResourcePath {
		path += "/" + resourcePath
	}
	return path
}
