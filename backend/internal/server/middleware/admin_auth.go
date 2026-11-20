// Package middleware provides HTTP middleware for authentication, authorization, and request processing.
package middleware

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const (
	AdminWebSocketProtocol       = "omnihive-admin"
	LegacyAdminWebSocketProtocol = "sub2api-admin"
)

// NewAdminAuthMiddleware 创建管理员认证中间件
func NewAdminAuthMiddleware(
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) AdminAuthMiddleware {
	return AdminAuthMiddleware(adminAuth(authService, userService, settingService, auditService))
}

// adminAuth 管理员认证中间件实现
// 支持两种认证方式（通过不同的 header 区分）：
// 1. Admin API Key: x-api-key: <admin-api-key>
// 2. JWT Token: Authorization: Bearer <jwt-token> (需要管理员角色)
func adminAuth(
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Browser upgrades carry only a short-lived, scoped, single-use ticket.
		if isWebSocketUpgradeRequest(c) {
			protocol, ticket := sanitizeAdminWebSocketProtocol(c.Request.Header)
			if protocol == "" {
				AbortWithError(c, http.StatusBadRequest, "INVALID_WS_PROTOCOL", "Unsupported WebSocket subprotocol")
				return
			}
			claims, err := authService.ConsumeAdminWebSocketTicket(c.Request.Context(), ticket, c.Request.URL.Path)
			if err != nil {
				AbortWithError(c, 401, "INVALID_WS_TICKET", "Invalid WebSocket ticket")
				return
			}
			if !validateAdminClaims(c, claims, authService, userService, settingService, auditService) {
				return
			}
			c.Next()
			return
		}

		// 检查 x-api-key header（Admin API Key 认证）
		apiKey := c.GetHeader("x-api-key")
		if apiKey != "" {
			if !validateAdminAPIKey(c, apiKey, settingService, userService) {
				return
			}
			c.Next()
			return
		}

		// 检查 Authorization header（JWT 认证）
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token := strings.TrimSpace(parts[1])
				if token == "" {
					AbortWithError(c, 401, "UNAUTHORIZED", "Authorization required")
					return
				}
				if !validateJWTForAdmin(c, token, authService, userService, settingService, auditService) {
					return
				}
				c.Next()
				return
			}
		}

		// 无有效认证信息
		AbortWithError(c, 401, "UNAUTHORIZED", "Authorization required")
	}
}

func isWebSocketUpgradeRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	// RFC6455 handshake uses:
	//   Connection: Upgrade
	//   Upgrade: websocket
	upgrade := strings.ToLower(strings.TrimSpace(c.GetHeader("Upgrade")))
	if upgrade != "websocket" {
		return false
	}
	connection := strings.ToLower(c.GetHeader("Connection"))
	return strings.Contains(connection, "upgrade")
}

func sanitizeAdminWebSocketProtocol(header http.Header) (protocol, ticket string) {
	const key = "Sec-WebSocket-Protocol"

	var offered []string
	for _, line := range header.Values(key) {
		for _, part := range strings.Split(line, ",") {
			offered = append(offered, strings.TrimSpace(part))
		}
	}
	header.Del(key)
	for _, supported := range []string{AdminWebSocketProtocol, LegacyAdminWebSocketProtocol} {
		if slices.Contains(offered, supported) {
			protocol = supported
			header.Set(key, protocol)
			break
		}
	}
	for _, part := range offered {
		if token, ok := strings.CutPrefix(part, "ticket."); ok && token != "" {
			ticket = token
			break
		}
	}
	return protocol, ticket
}

// validateAdminAPIKey 验证管理员 API Key
func validateAdminAPIKey(
	c *gin.Context,
	key string,
	settingService *service.SettingService,
	userService *service.UserService,
) bool {
	storedKey, err := settingService.GetAdminAPIKey(c.Request.Context())
	if err != nil {
		AbortWithError(c, 500, "INTERNAL_ERROR", "Internal server error")
		return false
	}

	// 未配置或不匹配，统一返回相同错误（避免信息泄露）
	if storedKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(storedKey)) != 1 {
		AbortWithError(c, 401, "INVALID_ADMIN_KEY", "Invalid admin API key")
		return false
	}

	// 获取真实的管理员用户
	admin, err := userService.GetFirstAdmin(c.Request.Context())
	if err != nil {
		AbortWithError(c, 500, "INTERNAL_ERROR", "No admin user found")
		return false
	}

	c.Set(string(ContextKeyUser), AuthSubject{
		UserID:      admin.ID,
		Concurrency: admin.Concurrency,
	})
	c.Set(string(ContextKeyUserRole), admin.Role)
	c.Set(ContextKeyAuthEmail, admin.Email)
	c.Set("auth_method", "admin_api_key")
	return true
}

// validateJWTForAdmin 验证 JWT 并检查管理员权限
func validateJWTForAdmin(
	c *gin.Context,
	token string,
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) bool {
	// 验证 JWT token
	claims, err := authService.ValidateTokenContext(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrTokenExpired) {
			AbortWithError(c, 401, "TOKEN_EXPIRED", "Token has expired")
			return false
		}
		AbortWithError(c, 401, "INVALID_TOKEN", "Invalid token")
		return false
	}

	return validateAdminClaims(c, claims, authService, userService, settingService, auditService)
}

func validateAdminClaims(c *gin.Context, claims *service.JWTClaims, authService *service.AuthService, userService *service.UserService, settingService *service.SettingService, auditService *service.AuditLogService) bool {

	// 从数据库获取用户
	user, err := userService.GetByID(c.Request.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			AbortWithError(c, 401, "USER_NOT_FOUND", "User not found")
		} else {
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to load user")
		}
		return false
	}

	// 检查用户状态
	if !user.IsActive() {
		AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
		return false
	}

	// 校验 TokenVersion，确保管理员改密后旧 token 失效
	if claims.TokenVersion != user.TokenVersion {
		AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked (password changed)")
		return false
	}

	// 会话绑定校验：IP/UA 任一变化即撤销会话（功能可在系统设置中关闭）
	if !enforceSessionBinding(c, authService, settingService, auditService, claims) {
		return false
	}

	// 检查管理员权限
	if !user.IsAdmin() {
		AbortWithError(c, 403, "FORBIDDEN", "Admin access required")
		return false
	}

	c.Set(string(ContextKeyUser), AuthSubject{
		UserID:      user.ID,
		Concurrency: user.Concurrency,
	})
	c.Set(string(ContextKeyUserRole), user.Role)
	c.Set(ContextKeyAuthEmail, user.Email)
	c.Set(ContextKeySessionID, claims.SessionID)
	c.Set("auth_method", "jwt")

	return true
}
