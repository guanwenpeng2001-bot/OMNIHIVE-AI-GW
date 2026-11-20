package middleware

import (
	"context"
	"errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	identitycompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/identity"
	executioncompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/execution"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"net/http"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const maxAPIKeyAuthorizationHeaderBytes = service.MaxAPIKeyCredentialBytes + 128

// NewAPIKeyAuthMiddleware 创建 API Key 认证中间件
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, cfg))
}

// apiKeyAuthWithSubscription API Key认证中间件（支持订阅验证）
//
// 中间件职责分为两层：
//   - 鉴权（Authentication）：验证 Key 有效性、用户状态、IP 限制 —— 始终执行
//   - 计费执行（Billing Enforcement）：过期/配额/订阅/余额检查 —— skipBilling 时整块跳过
//
// /v1/usage、/v1/sub2api/billing 端点与异步生图任务查询只需鉴权，不需要计费执行。
// usage 允许过期/配额耗尽的 Key 查询自身用量，billing 用于读取当前 Key 的倍率配置，
// 异步生图查询允许已耗尽额度的 Key 拉取自身任务结果。
func apiKeyAuthWithSubscription(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("gateway_protocol_errors", true)
		// ── 1. 提取 API Key ──────────────────────────────────────────
		if rejectInvalidAuthAbuse(c, apiKeyService) {
			AbortWithError(c, http.StatusTooManyRequests, "INVALID_AUTH_RATE_LIMITED", "Too many invalid authentication attempts; retry later")
			return
		}

		if apiKeyHeadersTooLarge(c) {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			return
		}

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectQueryAPIKeyDeprecated)
			AbortWithError(c, 400, "api_key_in_query_deprecated", "Query API keys are not supported on this endpoint. Use Authorization header.")
			return
		}

		// 尝试从Authorization header中提取API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string

		if authHeader != "" {
			// 验证Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
			}
		}

		// 如果Authorization header中没有，尝试从x-api-key header中提取
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
		}
		if len(apiKeyString) > service.MaxAPIKeyCredentialBytes {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			return
		}

		// 如果x-api-key header中没有，尝试从x-goog-api-key header中提取（Gemini CLI兼容）
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
		}

		// 如果所有header都没有API key
		if apiKeyString == "" {
			recordInvalidAuthFailure(c, apiKeyService)
			if hasAPIKeyCredentialInput(c) {
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			} else {
				MarkIngressRejected(c, IngressRejectAPIKeyRequired)
			}
			AbortWithError(c, 401, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}

		authenticateKey(c, apiKeyString, apiKeyService, subscriptionService, cfg, false)
	}
}

type projectScopesContextKey struct{}

type projectAuthResolver struct {
	identity.GatewayScopes
	slots chan struct{}
}

func (r projectAuthResolver) ProjectCredential(ctx context.Context, digest string) (*identity.KeyPolicy, error) {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
		return r.GatewayScopes.ProjectCredential(ctx, digest)
	default:
		return nil, service.ErrAPIKeyAuthOverloaded
	}
}

// InstallProjectScopes installs the resolver before both protocol-specific auth handlers.
func InstallProjectScopes(scopes identity.GatewayScopes, cfg *config.Config) gin.HandlerFunc {
	limit := cfg.APIKeyAuth.LookupConcurrency
	if limit <= 0 {
		limit = 64
	}
	resolver := projectAuthResolver{GatewayScopes: scopes, slots: make(chan struct{}, limit)}
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), projectScopesContextKey{}, resolver))
		c.Next()
	}
}

func authenticateKey(c *gin.Context, secret string, keys *service.APIKeyService, subscriptions *service.SubscriptionService, cfg *config.Config, google bool) {
	abort := func(status int, code, message string) {
		if google {
			if code == "INSUFFICIENT_BALANCE" {
				status = 429
			}
			abortWithGoogleError(c, status, message)
			return
		}
		AbortWithError(c, status, code, message)
	}
	ctx := c.Request.Context()
	scopes, _ := ctx.Value(projectScopesContextKey{}).(identity.GatewayScopes)
	var policy *identity.KeyPolicy
	var key *service.APIKey
	var err error
	if scopes != nil && strings.HasPrefix(secret, identity.ProjectKeyPrefix) {
		policy, err = scopes.ProjectCredential(ctx, service.APIKeyCredentialDigest(secret))
	}
	if err == nil {
		if policy != nil {
			key, err = identitycompat.ProjectKey(ctx, keys, policy)
		} else {
			key, err = keys.GetByKey(ctx, secret)
		}
	}
	if err != nil || key == nil {
		switch {
		case errors.Is(err, fault.NotFound), errors.Is(err, service.ErrAPIKeyNotFound), err == nil:
			recordInvalidAuthFailure(c, keys)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			abort(401, "INVALID_API_KEY", "Invalid API key")
		case errors.Is(err, service.ErrAPIKeyAuthOverloaded):
			MarkIngressRejected(c, IngressRejectAPIKeyAuthOverloaded)
			abort(503, "API_KEY_AUTH_OVERLOADED", "API key authentication is temporarily unavailable")
		default:
			abort(500, "INTERNAL_ERROR", "Failed to validate API key")
		}
		return
	}
	SetOpsFallbackAPIKey(c, key)
	w := key.WorkspaceID
	if w == 0 {
		w = domain.DefaultWorkspaceID
	}
	ctx = domain.WithWorkspaceScope(ctx, w)
	if scopes != nil {
		var a scope.Authorized
		if policy != nil {
			a = policy.Scope
		} else {
			a, err = scopes.LegacyScope(ctx, w, key.ID, key.UserID, service.APIKeyCredentialDigest(secret))
			if errors.Is(err, fault.NotFound) {
				abort(401, "INVALID_API_KEY", "Invalid API key")
				return
			}
			if err != nil {
				abort(503, "SCOPE_UNAVAILABLE", "Access policy unavailable")
				return
			}
		}
		ctx = scope.With(ctx, a)
		if journal.From(ctx) != nil {
			var group int64
			if key.GroupID != nil {
				group = *key.GroupID
			}
			ctx = executioncompat.BindCredential(ctx, service.APIKeyCredentialDigest(secret), projectKeyAction(c), ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL()), policy != nil, group)
		}
	}
	ctx, err = keys.AttachModelAccessPolicy(ctx, key.ID)
	if err != nil {
		if google {
			abortWithGoogleError(c, 503, "Model access policy unavailable")
		} else {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"type": "service_unavailable", "message": "Model access policy unavailable"}})
		}
		return
	}
	c.Request = c.Request.WithContext(ctx)
	ipAllowed := true
	if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
		clientIP := ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL())
		ipAllowed, _ = ip.CheckIPRestrictionWithCompiledRules(clientIP, key.CompiledIPWhitelist, key.CompiledIPBlacklist)
	}
	facts := identitycompat.KeyFacts(key, ipAllowed)
	if f := identity.EvaluateKey(facts); f != nil {
		reasons := map[string]IngressRejectReason{
			"API_KEY_DISABLED":  IngressRejectAPIKeyDisabled,
			"ACCESS_DENIED":     IngressRejectIPRestricted,
			"USER_INACTIVE":     IngressRejectUserInactive,
			"GROUP_DELETED":     IngressRejectGroupDeleted,
			"GROUP_DISABLED":    IngressRejectGroupDisabled,
			"GROUP_NOT_ALLOWED": IngressRejectGroupNotAllowed,
		}
		MarkIngressRejected(c, reasons[f.Reason])
		switch f.Reason {
		case "ACCESS_DENIED":
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonIPRestriction)
		case "GROUP_DELETED", "GROUP_DISABLED", "GROUP_NOT_ALLOWED":
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
		}
		abort(f.Status, f.Reason, f.Message)
		return
	}
	ctx = context.WithValue(ctx, ctxkey.UserID, key.User.ID)
	c.Request = c.Request.WithContext(ctx)
	path := c.Request.URL.Path
	billingInfo := !google && path == "/v1/sub2api/billing"
	diagnostic := !google && (path == "/v1/usage" || billingInfo || isAsyncImageTaskRead(c.Request.Method, path) || isUpstreamJobRead(c.Request.Method, path))
	if policy != nil {
		action := projectKeyAction(c)
		var models []string
		if policy.Mode == "allow_list" && action != "" && action != "usage.read" && action != "tasks.read" {
			if m := groupModelAllowlistModelFromParams(c); m != "" {
				models = []string{m}
			} else {
				var ok bool
				models, ok = groupModelAllowlistModelsFromBody(c)
				if !ok {
					return
				}
			}
		}
		if !policy.Allows(action, models) {
			abort(403, "ACTION_DENIED", "Access denied")
			return
		}
	}
	simple := cfg.RunMode == config.RunModeSimple
	sub, denial := identity.EvaluateFunding(ctx, facts, key.User.Balance > 0, identity.FundingChecks{
		Simple: simple, Diagnostic: diagnostic, BillingInfo: billingInfo, LimitsFirst: (google && !simple) || policy != nil,
	}, identitycompat.Subscriptions(key, subscriptions))
	if denial != nil {
		abort(denial.Status, denial.Reason, denial.Message)
		return
	}
	if sub != nil {
		c.Set(string(ContextKeySubscription), sub)
	}
	c.Set(string(ContextKeyAPIKey), key)
	c.Set(string(ContextKeyUser), AuthSubject{UserID: key.User.ID, Concurrency: key.User.Concurrency})
	c.Set(string(ContextKeyUserRole), key.User.Role)
	setGroupContext(c, key.Group)
	if !billingInfo {
		_ = keys.TouchLastUsed(ctx, key.ID)
	}
	c.Next()
}

func projectKeyAction(c *gin.Context) string {
	path := c.FullPath()
	switch c.Request.Method {
	case http.MethodGet:
		switch path {
		case "/v1/usage", "/v1/sub2api/billing":
			return "usage.read"
		case "/v1/images/tasks/:task_id", "/images/tasks/:task_id", "/api/v1/tasks/:task_id", "/api/v1/tasks/:request_id":
			return "tasks.read"
		}
	case http.MethodPost:
		switch path {
		case "/v1/messages", "/messages", "/antigravity/v1/messages", "/v1/chat/completions", "/chat/completions", "/responses", "/v1/responses", "/backend-api/codex/responses":
			return "text.chat"
		case "/v1beta/models/*modelAction", "/antigravity/v1beta/models/*modelAction":
			_, operation, _ := strings.Cut(c.Param("modelAction"), ":")
			if operation == "generateContent" || operation == "streamGenerateContent" {
				return "text.chat"
			}
		case "/embeddings", "/v1/embeddings":
			return "text.embeddings"
		case "/images/generations", "/v1/images/generations", "/v1/images/generations/async":
			return "image.generate"
		}
	}
	return ""
}

func apiKeyHeadersTooLarge(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return len(c.GetHeader("Authorization")) > maxAPIKeyAuthorizationHeaderBytes ||
		len(c.GetHeader("x-api-key")) > service.MaxAPIKeyCredentialBytes ||
		len(c.GetHeader("x-goog-api-key")) > service.MaxAPIKeyCredentialBytes
}

func hasAPIKeyCredentialInput(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return c.GetHeader("Authorization") != "" ||
		c.GetHeader("x-api-key") != "" ||
		c.GetHeader("x-goog-api-key") != ""
}

func isAsyncImageTaskRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(path, "/v1/images/tasks/") || strings.HasPrefix(path, "/images/tasks/")
}

func isUpstreamJobRead(method, path string) bool {
	// Resource cleanup must remain possible after inference balance is exhausted.
	// Normal API-key authentication and handler ownership checks still apply.
	if method == http.MethodGet && (path == "/compatible-mode/v1/files" || path == "/compatible-mode/v1/batches") {
		return true
	}
	if method == http.MethodDelete && strings.HasPrefix(path, "/compatible-mode/v1/files/") {
		return true
	}
	if method == http.MethodPost && strings.HasPrefix(path, "/compatible-mode/v1/batches/") && strings.HasSuffix(path, "/cancel") {
		return true
	}
	return method == http.MethodGet && (strings.HasPrefix(path, "/api/v1/tasks/") || strings.HasPrefix(path, "/compatible-mode/v1/files/") || strings.HasPrefix(path, "/compatible-mode/v1/batches/"))
}

// GetAPIKeyFromContext 从上下文中获取API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时回退使用。
// 与 ContextKeyAPIKey 区分：写入它不代表请求已通过鉴权，因此不影响 handler、
// 审计日志等对“已鉴权”的判断。
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey 读取 Ops 错误日志专用的回退 API Key。
func GetOpsFallbackAPIKey(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext 从上下文中获取订阅信息
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	value, exists := c.Get(string(ContextKeySubscription))
	if !exists {
		return nil, false
	}
	subscription, ok := value.(*service.UserSubscription)
	return subscription, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}
