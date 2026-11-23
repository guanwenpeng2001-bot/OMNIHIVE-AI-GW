package middleware

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// AuditLogMiddleware 管理面操作审计中间件类型（用于 wire 注入区分）。
type AuditLogMiddleware gin.HandlerFunc

// 审计相关 gin context 覆写键：handler / 认证中间件可通过这些键补充审计信息。
const (
	auditCtxKeyAction     = "audit_action"
	auditCtxKeyActorID    = "audit_actor_id"
	auditCtxKeyActorEmail = "audit_actor_email"
	auditCtxKeySkip       = "audit_skip"
	auditCtxKeyExtra      = "audit_extra"
	// ContextKeyAuthEmail 认证中间件写入的用户邮箱（审计用）。
	ContextKeyAuthEmail = "auth_email"
	// ContextKeySessionID 认证中间件写入的会话 ID（refresh token family）。
	ContextKeySessionID = "session_id"
)

// SetAuditAction 允许 handler / 中间件为当前请求指定审计动作名（覆盖自动推导）。
func SetAuditAction(c *gin.Context, action string) {
	c.Set(auditCtxKeyAction, action)
}

// SetAuditActor 允许 handler 在认证上下文缺失时（如登录接口）补充操作者身份。
func SetAuditActor(c *gin.Context, userID int64, email string) {
	if userID > 0 {
		c.Set(auditCtxKeyActorID, userID)
	}
	if email != "" {
		c.Set(auditCtxKeyActorEmail, email)
	}
}

// SkipAudit 跳过当前请求的审计记录。
func SkipAudit(c *gin.Context) {
	c.Set(auditCtxKeySkip, true)
}

// auditExtraAllowedKeys is deliberately narrow: handlers may only attach
// scalar, non-secret operation summaries. Request bodies and arbitrary maps
// are never accepted through this channel.
var auditExtraAllowedKeys = map[string]struct{}{
	"config_reset_keys": {},
	"result":            {}, "error_code": {}, "enabled": {}, "blocking_enabled": {},
	"config_version": {}, "endpoint_count": {}, "scanner_count": {},
	"all_groups": {}, "group_count": {}, "guard_endpoint_id": {},
	"http_status": {}, "latency_ms": {}, "token_applied": {}, "retryable": {},
	"event_id": {}, "requested_count": {}, "deleted_events": {}, "deleted_jobs": {},
	"matched_count": {}, "snapshot_max_id": {}, "filter_hash": {}, "confirm": {},
	"workspace_id": {}, "project_id": {}, "run_id": {}, "run_request_id": {},
	"admission": {}, "outcome": {}, "settlement_state": {}, "raw_events_requested": {},
	"text_preview_requested": {}, "text_preview_served": {},
	"reason": {},
}

// SetAuditExtra adds allowlisted, scalar details to the current audit entry.
// It is safe to call more than once; later values replace earlier ones.
func SetAuditExtra(c *gin.Context, fields map[string]any) {
	if c == nil || len(fields) == 0 {
		return
	}
	current := map[string]any{}
	if value, ok := c.Get(auditCtxKeyExtra); ok {
		if existing, ok := value.(map[string]any); ok {
			for key, item := range existing {
				current[key] = item
			}
		}
	}
	for key, value := range fields {
		if _, ok := auditExtraAllowedKeys[key]; !ok || !isAuditExtraScalar(value) {
			continue
		}
		if text, ok := value.(string); ok {
			value = truncateAuditExtraString(text, 128)
		}
		current[key] = value
	}
	c.Set(auditCtxKeyExtra, current)
}

func isAuditExtraScalar(value any) bool {
	switch value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

func truncateAuditExtraString(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// auditSensitiveReads 需要审计的敏感 GET 读取（method+FullPath → 动作名）。
var auditSensitiveReads = map[string]string{
	"GET /api/v1/admin/config/effective":   "admin.config.effective.read",
	"GET /api/v2/admin/catalog/models":     "admin.catalog.models.list",
	"GET /api/v2/admin/catalog/models/:id": "admin.catalog.models.read",

	"GET /api/v2/workspaces/:workspace_id/projects/:project_id/billing/adjustments/:id/payment-evidence": "billing.payment_evidence.read",
	"GET /api/v2/admin/extensions":                       "admin.extensions.list",
	"GET /api/v1/admin/accounts/data":                    "admin.accounts.export",
	"GET /api/v1/workspaces/:workspace_id/accounts/data": "workspace.accounts.export",
	"GET /api/v1/admin/proxies/data":                     "admin.proxies.export",
	"GET /api/v1/admin/redeem-codes/export":              "admin.redeem_codes.export",
	"GET /api/v1/admin/backups/:id/download-url":         "admin.backups.download",
	"GET /api/v1/admin/settings/admin-api-key":           "admin.admin_api_key.read",
	"GET /api/v1/admin/users/:id/api-keys":               "admin.users.api_keys.read",
	"GET /api/v1/admin/groups/:id/api-keys":              "admin.groups.api_keys.read",
	"GET /api/v1/admin/backups/s3-config":                "admin.backups.s3_config.read",
}

// auditActionOverrides 变更类请求的动作名精确映射（未命中时自动推导）。
var auditActionOverrides = map[string]string{
	"PUT /api/v2/workspaces/:workspace_id/projects/:project_id/billing/adjustments/:id/payment-evidence/:event_id": "billing.payment_evidence.link",
	"POST /api/v2/workspaces/:workspace_id/projects/:project_id/tasks":                                             "tasks.create",
	"POST /api/v2/workspaces/:workspace_id/projects/:project_id/tasks/:id/retry":                                   "tasks.retry",

	"POST /api/v1/admin/proxy-groups":                         "admin.proxy_groups.create",
	"PUT /api/v1/admin/proxy-groups/:id":                      "admin.proxy_groups.update",
	"DELETE /api/v1/admin/proxy-groups/:id":                   "admin.proxy_groups.delete",
	"PUT /api/v1/admin/proxy-groups/:id/members":              "admin.proxy_groups.members.replace",
	"DELETE /api/v1/admin/proxy-groups/:id/bindings":          "admin.proxy_groups.bindings.reset",
	"POST /api/v1/admin/proxies/:id/probe-now":                "admin.proxies.probe_now",
	"POST /api/v1/auth/login":                                 service.AuditActionLogin,
	"POST /api/v1/auth/login/2fa":                             service.AuditActionLogin2FA,
	"POST /api/v1/auth/passkey/login/finish":                  service.AuditActionLogin,
	"POST /api/v1/auth/register":                              service.AuditActionRegister,
	"POST /api/v1/auth/refresh":                               service.AuditActionTokenRefresh,
	"POST /api/v1/user/totp/step-up":                          service.AuditActionStepUpVerify,
	"POST /api/v1/admin/audit-logs/clear":                     service.AuditActionAuditLogClear,
	"POST /api/v1/admin/accounts/data":                        "admin.accounts.import",
	"POST /api/v1/workspaces/:workspace_id/accounts/data":     "workspace.accounts.import",
	"POST /api/v1/admin/backups":                              "admin.backups.create",
	"POST /api/v1/admin/backups/:id/restore":                  "admin.backups.restore",
	"DELETE /api/v1/admin/backups/:id":                        "admin.backups.delete",
	"PUT /api/v1/admin/backups/s3-config":                     "admin.backups.s3_config.update",
	"POST /api/v1/admin/settings/admin-api-key/regenerate":    "admin.admin_api_key.regenerate",
	"DELETE /api/v1/admin/settings/admin-api-key":             "admin.admin_api_key.delete",
	"PUT /api/v1/admin/prompt-audit/config":                   "admin.prompt_audit.config.update",
	"POST /api/v1/admin/prompt-audit/endpoints/probe":         "admin.prompt_audit.endpoint.probe",
	"DELETE /api/v1/admin/prompt-audit/events/:id":            "admin.prompt_audit.event.delete",
	"POST /api/v1/admin/prompt-audit/events/batch-delete":     "admin.prompt_audit.events.batch_delete",
	"POST /api/v1/admin/prompt-audit/events/delete-preview":   "admin.prompt_audit.events.delete_preview",
	"POST /api/v1/admin/prompt-audit/events/delete-by-filter": "admin.prompt_audit.events.filter_delete",
}

const playgroundAuditPrefix = "/api/v2/workspaces/:workspace_id/projects/:project_id"

// Playground audits accept server-derived metadata only.
var playgroundAuditActions = map[string]string{
	"POST " + playgroundAuditPrefix + "/playground/runs":                   "playground.run",
	"POST " + playgroundAuditPrefix + "/requests/:id/replay":               "requests.replay",
	"POST " + playgroundAuditPrefix + "/playground/runs/:id/cancel":        "playground.cancel",
	"GET " + playgroundAuditPrefix + "/playground/runs/:id":                "playground.read",
	"GET " + playgroundAuditPrefix + "/playground/runs/by-idempotency-key": "playground.read_by_idempotency_key",
}

// auditBodyOmittedRoutes contains routes with secrets or private content that
// cannot be safely redacted by JSON field name.
var auditBodyOmittedRoutes = map[string]struct{}{
	"POST /api/v1/admin/config/reset":                                                                              {},
	"POST /api/v2/workspaces/:workspace_id/access-requests":                                                        {},
	"POST /api/v2/workspaces/:workspace_id/access-requests/:id/decision":                                           {},
	"PUT /api/v2/workspaces/:workspace_id/projects/:project_id/billing/adjustments/:id/payment-evidence/:event_id": {},
	"POST " + playgroundAuditPrefix + "/playground/runs":                                                           {},
	"POST " + playgroundAuditPrefix + "/requests/:id/replay":                                                       {},
	"POST " + playgroundAuditPrefix + "/playground/runs/:id/cancel":                                                {},
	"POST /api/v2/workspaces/:workspace_id/projects/:project_id/tasks":                                             {},
	"POST /api/v2/workspaces/:workspace_id/byok":                                                                   {},
	"POST /api/v2/workspaces/:workspace_id/byok/:id/rotate":                                                        {},
	"POST /api/v2/workspaces/:workspace_id/projects/:project_id/byok/credentials":                                  {},
	"POST /api/v2/workspaces/:workspace_id/projects/:project_id/byok/credentials/:id/rotate":                       {},

	"POST /api/v2/workspaces/:workspace_id/projects/:project_id/tasks/:id/retry": {},

	// Omit every import body, including invalid and legacy requests. The route
	// decision also skips SubmittedConfigurationDiff and leaves Body unread for
	// the handler; a client-controlled stage_inactive field grants no audit bypass.
	"POST /api/v1/admin/accounts/data":                          {},
	"POST /api/v1/workspaces/:workspace_id/accounts/data":       {},
	"POST /api/v1/auth/passkey/login/finish":                    {},
	"POST /api/v1/user/passkeys/register/finish":                {},
	"POST /api/v1/admin/accounts/import/codex-session":          {},
	"PUT /api/v1/admin/accounts/:id/ollama-cloud-usage/session": {},
	"PUT /api/v1/admin/prompt-audit/config":                     {},
	"POST /api/v1/admin/prompt-audit/endpoints/probe":           {},
	"DELETE /api/v1/admin/prompt-audit/events/:id":              {},
	"POST /api/v1/admin/prompt-audit/events/batch-delete":       {},
	"POST /api/v1/admin/prompt-audit/events/delete-preview":     {},
	"POST /api/v1/admin/prompt-audit/events/delete-by-filter":   {},

	"PUT /api/v2/workspaces/:workspace_id/connections/:id/credential":    {},
	"POST /api/v2/workspaces/:workspace_id/connections/:id/rotations":    {},
	"PUT /api/v1/workspaces/:workspace_id/connections/:id/authorization": {},
}

// NewAuditLogMiddleware 创建审计中间件。
// 记录范围：变更类请求（POST/PUT/PATCH/DELETE）+ 白名单内的敏感 GET 读取。
// 挂载位置：admin / user / admin-payment 组挂在各自认证中间件之后（只审计已认证请求，
// 未过认证的 401/403 不入库）；auth 组（登录/注册/刷新）无前置认证，天然记录失败尝试。
func NewAuditLogMiddleware(auditService *service.AuditLogService) AuditLogMiddleware {
	return AuditLogMiddleware(func(c *gin.Context) {
		routeKey := c.Request.Method + " " + c.FullPath()

		action, playground := playgroundAuditActions[routeKey]
		record := playground
		if !playground {
			switch c.Request.Method {
			case "POST", "PUT", "PATCH", "DELETE":
				record = true
				action = auditActionOverrides[routeKey]
			case "GET":
				action, record = auditSensitiveReads[routeKey]
			}
		}
		if !record {
			c.Next()
			return
		}

		// 捕获请求体（读出后回填，避免影响后续 ShouldBindJSON）。
		// 只读取脱敏解析上限内的字节，超出部分与已读部分拼接回填，
		// 避免大体积导入请求被完整复制进内存两次。
		var bodyRedacted string
		var submittedDiff []service.ConfigurationFieldDiff
		if _, omit := auditBodyOmittedRoutes[routeKey]; omit {
			bodyRedacted = "<credential-bearing body omitted>"
			if playground {
				bodyRedacted = "<private body omitted>"
			}
			submittedDiff = submittedFieldDiff(routeKey)
		} else if c.Request.Body != nil && c.Request.Method != "GET" {
			orig := c.Request.Body
			raw, err := io.ReadAll(io.LimitReader(orig, service.AuditRequestBodyCaptureLimit+1))
			if err == nil {
				submittedDiff = service.SubmittedConfigurationDiff(raw)
				c.Request.Body = &restoredBody{
					Reader: io.MultiReader(bytes.NewReader(raw), orig),
					closer: orig,
				}
				bodyRedacted = service.RedactAuditBody(raw, c.GetHeader("Content-Type"))
			}
		}

		actor := service.ConfigurationAuditActor{Role: c.GetString("user_role")}
		if subject, ok := GetAuthSubjectFromContext(c); ok {
			actor.UserID = subject.UserID
		}
		if role, ok := GetUserRoleFromContext(c); ok {
			actor.Role = role
		}
		actor.RequestID, _ = c.Request.Context().Value(ctxkey.RequestID).(string)
		c.Request = c.Request.WithContext(service.WithConfigurationAuditActor(c.Request.Context(), actor))
		start := time.Now()
		c.Next()

		if c.GetBool(auditCtxKeySkip) {
			return
		}

		status := c.Writer.Status()
		// token 刷新成功属于高频常规操作，只记录失败（潜在攻击信号）。
		if routeKey == "POST /api/v1/auth/refresh" && status < 400 {
			return
		}

		entry := &service.AuditLog{
			CreatedAt:   time.Now().UTC(),
			Action:      action,
			Method:      c.Request.Method,
			Path:        c.FullPath(),
			ClientIP:    SecurityClientIP(c),
			UserAgent:   c.Request.UserAgent(),
			RequestBody: bodyRedacted,
			StatusCode:  status,
			LatencyMs:   time.Since(start).Milliseconds(),
		}
		if entry.Path == "" {
			entry.Path = c.Request.URL.Path
		}
		if entry.Action == "" {
			entry.Action = deriveAuditAction(c.Request.Method, entry.Path)
		}
		if v, ok := c.Get(auditCtxKeyAction); ok {
			if s, ok := v.(string); ok && s != "" {
				entry.Action = s
			}
		}
		if requestID, ok := c.Request.Context().Value(ctxkey.RequestID).(string); ok {
			entry.RequestID = requestID
		}

		// 操作者身份：优先取认证中间件写入的上下文，其次取 handler 覆写（登录等场景）。
		if subject, ok := GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
			uid := subject.UserID
			entry.ActorUserID = &uid
		}
		if role, ok := GetUserRoleFromContext(c); ok {
			entry.ActorRole = role
		}
		entry.ActorEmail = c.GetString(ContextKeyAuthEmail)
		entry.AuthMethod = c.GetString("auth_method")
		if entry.AuthMethod == "" && entry.ActorUserID != nil {
			entry.AuthMethod = service.AuditAuthMethodJWT
		}
		if v, ok := c.Get(auditCtxKeyActorID); ok {
			if id, ok := v.(int64); ok && id > 0 {
				entry.ActorUserID = &id
			}
		}
		if v, ok := c.Get(auditCtxKeyActorEmail); ok {
			if s, ok := v.(string); ok && s != "" {
				entry.ActorEmail = s
			}
		}

		// 请求头凭证掩码（仅保留首尾）。
		entry.CredentialMasked = MaskedRequestCredential(c)

		extra := map[string]any{}
		if len(submittedDiff) > 0 {
			extra["submitted_diff"] = submittedDiff
		}
		if workspaceID, scoped := domain.WorkspaceScope(c.Request.Context()); scoped {
			extra["workspace_id"] = workspaceID
		}
		if value, ok := c.Get(auditCtxKeyExtra); ok {
			if details, ok := value.(map[string]any); ok {
				for key, item := range details {
					extra[key] = item
				}
			}
		}
		if playground {
			entry.UserAgent = ""
			entry.CredentialMasked = ""
		} else if len(c.Params) > 0 {
			params := make(map[string]string, len(c.Params))
			for _, p := range c.Params {
				params[p.Key] = p.Value
			}
			extra["params"] = params
		}
		if !playground {
			if q := service.RedactAuditQuery(c.Request.URL.RawQuery); q != "" {
				extra["query"] = q
			}
		}
		if len(extra) > 0 {
			entry.Extra = extra
		}
		if params, ok := extra["params"].(map[string]string); ok {
			entry.ResourceID = params["id"]
			if entry.ResourceID == "" {
				entry.ResourceID = params["credential_id"]
			}
			if id, err := strconv.ParseInt(params["project_id"], 10, 64); err == nil && id > 0 {
				entry.ProjectID = &id
			}
		}

		auditService.Record(entry)
	})
}

func submittedFieldDiff(routeKey string) []service.ConfigurationFieldDiff {
	if strings.Contains(routeKey, "/credential") || strings.Contains(routeKey, "/rotations") {
		return []service.ConfigurationFieldDiff{{Field: "credential", Operation: "submitted", After: "changed"}}
	}
	return nil
}

// restoredBody 把审计中间件按上限读出的前缀与未读完的原始 body 拼接回填，
// 保证 handler 读到完整请求体；Close 委托给原始 body。
type restoredBody struct {
	io.Reader
	closer io.Closer
}

func (b *restoredBody) Close() error { return b.closer.Close() }

// MaskedRequestCredential 提取请求头中的凭证并做首尾掩码。
func MaskedRequestCredential(c *gin.Context) string {
	if apiKey := strings.TrimSpace(c.GetHeader("x-api-key")); apiKey != "" {
		return "x-api-key " + service.MaskAuditCredential(apiKey)
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 {
		return parts[0] + " " + service.MaskAuditCredential(strings.TrimSpace(parts[1]))
	}
	return service.MaskAuditCredential(authHeader)
}

// deriveAuditAction 由 method + 路由模板自动推导动作名，
// 例：PUT /api/v1/admin/accounts/:id → admin.accounts.update
func deriveAuditAction(method, fullPath string) string {
	path := strings.TrimPrefix(fullPath, "/api/v1/")
	path = strings.Trim(path, "/")
	segs := strings.Split(path, "/")
	parts := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg == "" || strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
			continue
		}
		parts = append(parts, strings.ReplaceAll(seg, "-", "_"))
	}
	verb := ""
	switch method {
	case "POST":
		verb = "create"
	case "PUT", "PATCH":
		verb = "update"
	case "DELETE":
		verb = "delete"
	case "GET":
		verb = "read"
	default:
		verb = strings.ToLower(method)
	}
	if len(parts) == 0 {
		return verb
	}
	return strings.Join(parts, ".") + "." + verb
}
