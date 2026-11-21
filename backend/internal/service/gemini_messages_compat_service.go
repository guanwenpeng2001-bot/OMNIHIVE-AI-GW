package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	geminiprovider "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/providers/gemini"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/responseheaders"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const geminiStickySessionTTL = time.Hour

const (
	geminiMaxRetries     = 5
	geminiRetryBaseDelay = 1 * time.Second
	geminiRetryMaxDelay  = 16 * time.Second
)

const (
	// google.rpc.RetryInfo 的标准 @type（Vertex AI 429 响应携带，retryDelay 形如 "39s"）
	geminiRetryInfoTypeURL = "type.googleapis.com/google.rpc.RetryInfo"
	// RetryInfo.retryDelay 的上限，防止异常值导致账号长时间不可用
	geminiRetryInfoMaxDelay = 15 * time.Minute
	// Vertex（service_account，按量付费）429 无法解析重置时间时的兜底冷却：
	// 按量付费限速是短窗口 RPM/TPM，没有每日配额，不适用 PST 午夜重置
	geminiVertexFallbackCooldown = time.Minute
)

// Gemini tool calling now requires `thoughtSignature` in parts that include `functionCall`.
// Many clients don't send it; we inject a known dummy signature to satisfy the validator.
// Ref: https://ai.google.dev/gemini-api/docs/thought-signatures
const geminiDummyThoughtSignature = apicompat.GeminiDummyThoughtSignature

type GeminiMessagesCompatService struct {
	accountRepo               AccountRepository
	groupRepo                 GroupRepository
	cache                     GatewayCache
	schedulerSnapshot         *SchedulerSnapshotService
	tokenProvider             *GeminiTokenProvider
	rateLimitService          *RateLimitService
	httpUpstream              HTTPUpstream
	antigravityGatewayService *AntigravityGatewayService
	cfg                       *config.Config
	responseHeaderFilter      *responseheaders.CompiledHeaderFilter
}

func (s *GeminiMessagesCompatService) readUpstreamErrorBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	limit := gatewayUpstreamErrorBodyReadLimit
	if s != nil && s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody && s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes > int(limit) {
		limit = int64(s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	return body
}

func NewGeminiMessagesCompatService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	cache GatewayCache,
	schedulerSnapshot *SchedulerSnapshotService,
	tokenProvider *GeminiTokenProvider,
	rateLimitService *RateLimitService,
	httpUpstream HTTPUpstream,
	antigravityGatewayService *AntigravityGatewayService,
	cfg *config.Config,
) *GeminiMessagesCompatService {
	return &GeminiMessagesCompatService{
		accountRepo:               accountRepo,
		groupRepo:                 groupRepo,
		cache:                     cache,
		schedulerSnapshot:         schedulerSnapshot,
		tokenProvider:             tokenProvider,
		rateLimitService:          rateLimitService,
		httpUpstream:              httpUpstream,
		antigravityGatewayService: antigravityGatewayService,
		cfg:                       cfg,
		responseHeaderFilter:      compileResponseHeaderFilter(cfg),
	}
}

// GetTokenProvider returns the token provider for OAuth accounts
func (s *GeminiMessagesCompatService) GetTokenProvider() *GeminiTokenProvider {
	return s.tokenProvider
}

func (s *GeminiMessagesCompatService) SelectAccountForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*Account, error) {
	return s.SelectAccountForModelWithExclusions(ctx, groupID, sessionHash, requestedModel, nil)
}

func (s *GeminiMessagesCompatService) SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	// 1. 确定目标平台和调度模式
	// Determine target platform and scheduling mode
	platform, useMixedScheduling, hasForcePlatform, err := s.resolvePlatformAndSchedulingMode(ctx, groupID)
	if err != nil {
		return nil, err
	}

	cacheKey := "gemini:" + sessionHash

	// 2. 尝试粘性会话命中
	// Try sticky session hit
	if account := s.tryStickySessionHit(ctx, groupID, sessionHash, cacheKey, requestedModel, excludedIDs, platform, useMixedScheduling); account != nil {
		return account, nil
	}

	// 3. 查询可调度账户（强制平台模式：优先按分组查找，找不到再查全部）
	// Query schedulable accounts (force platform mode: try group first, fallback to all)
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, platform, hasForcePlatform)
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}
	// 强制平台模式下，分组中找不到账户时回退查询全部
	if len(accounts) == 0 && groupID != nil && hasForcePlatform {
		accounts, err = s.listSchedulableAccountsOnce(ctx, nil, platform, hasForcePlatform)
		if err != nil {
			return nil, fmt.Errorf("query accounts failed: %w", err)
		}
	}

	// 4. 按优先级 + LRU 选择最佳账号
	// Select best account by priority + LRU
	selected := s.selectBestGeminiAccount(ctx, accounts, requestedModel, excludedIDs, platform, useMixedScheduling)

	if selected == nil {
		if requestedModel != "" {
			return nil, fmt.Errorf("no available Gemini accounts supporting model: %s", requestedModel)
		}
		return nil, errors.New("no available Gemini accounts")
	}

	// 5. 设置粘性会话绑定
	// Set sticky session binding
	if sessionHash != "" {
		_ = s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, selected.ID, geminiStickySessionTTL)
	}

	return s.hydrateSelectedAccount(ctx, selected)
}

// resolvePlatformAndSchedulingMode 解析目标平台和调度模式。
// 返回：平台名称、是否使用混合调度、是否强制平台、错误。
//
// resolvePlatformAndSchedulingMode resolves target platform and scheduling mode.
// Returns: platform name, whether to use mixed scheduling, whether force platform, error.
func (s *GeminiMessagesCompatService) resolvePlatformAndSchedulingMode(ctx context.Context, groupID *int64) (platform string, useMixedScheduling bool, hasForcePlatform bool, err error) {
	// 优先检查 context 中的强制平台（/antigravity 路由）
	forcePlatform, hasForcePlatform := ctx.Value(ctxkey.ForcePlatform).(string)
	if hasForcePlatform && forcePlatform != "" {
		return forcePlatform, false, true, nil
	}

	if groupID != nil {
		// 根据分组 platform 决定查询哪种账号
		var group *Group
		if ctxGroup, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(ctxGroup) && ctxGroup.ID == *groupID {
			group = ctxGroup
		} else {
			group, err = s.groupRepo.GetByIDLite(ctx, *groupID)
			if err != nil {
				return "", false, false, fmt.Errorf("get group failed: %w", err)
			}
		}
		// gemini 分组支持混合调度（包含启用了 mixed_scheduling 的 antigravity 账户）
		return group.Platform, group.Platform == PlatformGemini, false, nil
	}

	// 无分组时只使用原生 gemini 平台
	return PlatformGemini, true, false, nil
}

// tryStickySessionHit 尝试从粘性会话获取账号。
// 如果命中且账号可用则返回账号；如果账号不可用则清理会话并返回 nil。
//
// tryStickySessionHit attempts to get account from sticky session.
// Returns account if hit and usable; clears session and returns nil if account unavailable.
func (s *GeminiMessagesCompatService) tryStickySessionHit(
	ctx context.Context,
	groupID *int64,
	sessionHash, cacheKey, requestedModel string,
	excludedIDs map[int64]struct{},
	platform string,
	useMixedScheduling bool,
) *Account {
	if sessionHash == "" {
		return nil
	}

	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
	if err != nil || accountID <= 0 {
		return nil
	}

	if _, excluded := excludedIDs[accountID]; excluded {
		return nil
	}

	account, err := s.getSchedulableAccount(ctx, accountID)
	if err != nil {
		return nil
	}

	// 检查账号是否需要清理粘性会话
	// Check if sticky session should be cleared
	if shouldClearStickySession(account, requestedModel) {
		_ = s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
		return nil
	}

	// 验证账号是否可用于当前请求
	// Verify account is usable for current request
	if !s.isAccountUsableForRequest(ctx, account, requestedModel, platform, useMixedScheduling) {
		return nil
	}

	// 刷新会话 TTL 并返回账号
	// Refresh session TTL and return account
	_ = s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), cacheKey, geminiStickySessionTTL)
	return account
}

// isAccountUsableForRequest 检查账号是否可用于当前请求。
// 验证：模型调度、模型支持、平台匹配、速率限制预检。
//
// isAccountUsableForRequest checks if account is usable for current request.
// Validates: model scheduling, model support, platform matching, rate limit precheck.
func (s *GeminiMessagesCompatService) isAccountUsableForRequest(
	ctx context.Context,
	account *Account,
	requestedModel, platform string,
	useMixedScheduling bool,
) bool {
	return s.isAccountUsableForRequestWithPrecheck(ctx, account, requestedModel, platform, useMixedScheduling, nil)
}

func (s *GeminiMessagesCompatService) isAccountUsableForRequestWithPrecheck(
	ctx context.Context,
	account *Account,
	requestedModel, platform string,
	useMixedScheduling bool,
	precheckResult map[int64]bool,
) bool {
	// 检查模型调度能力
	// Check model scheduling capability
	if !account.IsSchedulableForModelWithContext(ctx, requestedModel) {
		return false
	}

	// 检查模型支持
	// Check model support
	if requestedModel != "" && !s.isModelSupportedByAccount(account, requestedModel) {
		return false
	}

	// 检查平台匹配
	// Check platform matching
	if !s.isAccountValidForPlatform(account, platform, useMixedScheduling) {
		return false
	}

	// 速率限制预检
	// Rate limit precheck
	if !s.passesRateLimitPreCheckWithCache(ctx, account, requestedModel, precheckResult) {
		return false
	}

	return true
}

// isAccountValidForPlatform 检查账号是否匹配目标平台。
// 原生平台直接匹配；混合调度模式下 antigravity 需要启用 mixed_scheduling。
//
// isAccountValidForPlatform checks if account matches target platform.
// Native platform matches directly; mixed scheduling mode requires antigravity to enable mixed_scheduling.
func (s *GeminiMessagesCompatService) isAccountValidForPlatform(account *Account, platform string, useMixedScheduling bool) bool {
	if account.Platform == platform {
		return true
	}
	if useMixedScheduling && account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled() {
		return true
	}
	return false
}

func (s *GeminiMessagesCompatService) passesRateLimitPreCheckWithCache(ctx context.Context, account *Account, requestedModel string, precheckResult map[int64]bool) bool {
	if s.rateLimitService == nil || requestedModel == "" {
		return true
	}

	if precheckResult != nil {
		if ok, exists := precheckResult[account.ID]; exists {
			return ok
		}
	}

	ok, err := s.rateLimitService.PreCheckUsage(ctx, account, requestedModel)
	if err != nil {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini PreCheck] Account %d precheck error: %v", account.ID, err)
	}
	return ok
}

// selectBestGeminiAccount 从候选账号中选择最佳账号（优先级 + LRU + OAuth 优先）。
// 返回 nil 表示无可用账号。
//
// selectBestGeminiAccount selects best account from candidates (priority + LRU + OAuth preferred).
// Returns nil if no available account.
func (s *GeminiMessagesCompatService) selectBestGeminiAccount(
	ctx context.Context,
	accounts []Account,
	requestedModel string,
	excludedIDs map[int64]struct{},
	platform string,
	useMixedScheduling bool,
) *Account {
	var selected *Account
	precheckResult := s.buildPreCheckUsageResultMap(ctx, accounts, requestedModel)

	for i := range accounts {
		acc := &accounts[i]

		// 跳过被排除的账号
		if _, excluded := excludedIDs[acc.ID]; excluded {
			continue
		}

		// 检查账号是否可用于当前请求
		if !s.isAccountUsableForRequestWithPrecheck(ctx, acc, requestedModel, platform, useMixedScheduling, precheckResult) {
			continue
		}

		// 选择最佳账号
		if selected == nil {
			selected = acc
			continue
		}

		if s.isBetterGeminiAccount(acc, selected) {
			selected = acc
		}
	}

	return selected
}

func (s *GeminiMessagesCompatService) buildPreCheckUsageResultMap(ctx context.Context, accounts []Account, requestedModel string) map[int64]bool {
	if s.rateLimitService == nil || requestedModel == "" || len(accounts) == 0 {
		return nil
	}

	candidates := make([]*Account, 0, len(accounts))
	for i := range accounts {
		candidates = append(candidates, &accounts[i])
	}

	result, err := s.rateLimitService.PreCheckUsageBatch(ctx, candidates, requestedModel)
	if err != nil {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini PreCheckBatch] failed: %v", err)
	}
	return result
}

// isBetterGeminiAccount 判断 candidate 是否比 current 更优。
// 规则：优先级更高（数值更小）优先；同优先级时，未使用过的优先（OAuth > 非 OAuth），其次是最久未使用的。
//
// isBetterGeminiAccount checks if candidate is better than current.
// Rules: higher priority (lower value) wins; same priority: never used (OAuth > non-OAuth) > least recently used.
func (s *GeminiMessagesCompatService) isBetterGeminiAccount(candidate, current *Account) bool {
	// 优先级更高（数值更小）
	if candidate.Priority < current.Priority {
		return true
	}
	if candidate.Priority > current.Priority {
		return false
	}

	// 同优先级，比较最后使用时间
	switch {
	case candidate.LastUsedAt == nil && current.LastUsedAt != nil:
		// candidate 从未使用，优先
		return true
	case candidate.LastUsedAt != nil && current.LastUsedAt == nil:
		// current 从未使用，保持
		return false
	case candidate.LastUsedAt == nil && current.LastUsedAt == nil:
		// 都未使用，优先选择 OAuth 账号（更兼容 Code Assist 流程）
		return candidate.Type == AccountTypeOAuth && current.Type != AccountTypeOAuth
	default:
		// 都使用过，选择最久未使用的
		return candidate.LastUsedAt.Before(*current.LastUsedAt)
	}
}

// isModelSupportedByAccount 根据账户平台检查模型支持
func (s *GeminiMessagesCompatService) isModelSupportedByAccount(account *Account, requestedModel string) bool {
	if account.Platform == PlatformAntigravity {
		if strings.TrimSpace(requestedModel) == "" {
			return true
		}
		return mapAntigravityModel(account, requestedModel) != ""
	}
	return account.IsModelSupported(requestedModel)
}

// GetAntigravityGatewayService 返回 AntigravityGatewayService
func (s *GeminiMessagesCompatService) GetAntigravityGatewayService() *AntigravityGatewayService {
	return s.antigravityGatewayService
}

func (s *GeminiMessagesCompatService) getSchedulableAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s.schedulerSnapshot != nil {
		return s.schedulerSnapshot.GetAccount(ctx, accountID)
	}
	return s.accountRepo.GetByID(ctx, accountID)
}

func (s *GeminiMessagesCompatService) hydrateSelectedAccount(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || s.schedulerSnapshot == nil {
		return account, nil
	}
	hydrated, err := s.schedulerSnapshot.GetAccount(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if hydrated == nil {
		return nil, fmt.Errorf("selected gemini account %d not found during hydration", account.ID)
	}
	return hydrated, nil
}

func (s *GeminiMessagesCompatService) listSchedulableAccountsOnce(ctx context.Context, groupID *int64, platform string, hasForcePlatform bool) ([]Account, error) {
	if s.schedulerSnapshot != nil {
		accounts, _, err := s.schedulerSnapshot.ListSchedulableAccounts(ctx, groupID, platform, hasForcePlatform)
		return accounts, err
	}

	useMixedScheduling := platform == PlatformGemini && !hasForcePlatform
	queryPlatforms := []string{platform}
	if useMixedScheduling {
		queryPlatforms = []string{platform, PlatformAntigravity}
	}

	if groupID != nil {
		return s.accountRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, queryPlatforms)
	}
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return s.accountRepo.ListSchedulableByPlatforms(ctx, queryPlatforms)
	}
	return s.accountRepo.ListSchedulableUngroupedByPlatforms(ctx, queryPlatforms)
}

func (s *GeminiMessagesCompatService) validateUpstreamBaseURL(raw string) (string, error) {
	normalized, err := urlvalidator.ValidateHTTPURL(raw, s.cfg != nil && s.cfg.Security.URLAllowlist.AllowInsecureHTTP, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: s.cfg != nil && s.cfg.Security.URLAllowlist.Enabled,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return normalized, nil
}

// AntigravityGeminiModelIDs returns the effective Gemini mappings of schedulable
// Antigravity accounts. Mixed routes must additionally respect the account opt-in.
func (s *GeminiMessagesCompatService) AntigravityGeminiModelIDs(ctx context.Context, groupID *int64, requireMixed bool) ([]string, error) {
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, PlatformAntigravity, false)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, account := range accounts {
		if account.Platform != PlatformAntigravity || (requireMixed && !account.IsMixedSchedulingEnabled()) {
			continue
		}
		for model := range account.GetModelMapping() {
			if isAntigravityGeminiModel(model) {
				seen[model] = struct{}{}
			}
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models, nil
}

// HasAntigravityAccounts 检查是否有可用的 antigravity 账户
func (s *GeminiMessagesCompatService) HasAntigravityAccounts(ctx context.Context, groupID *int64) (bool, error) {
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, PlatformAntigravity, false)
	if err != nil {
		return false, err
	}
	return len(accounts) > 0, nil
}

// SelectAccountForAIStudioEndpoints selects an account that is likely to succeed against
// generativelanguage.googleapis.com (e.g. GET /v1beta/models).
//
// Preference order:
// 1) API key accounts (AI Studio)
// 2) OAuth accounts without project_id (AI Studio OAuth)
// 3) OAuth accounts explicitly marked as ai_studio
// 4) Any remaining Gemini accounts (fallback)
func (s *GeminiMessagesCompatService) SelectAccountForAIStudioEndpoints(ctx context.Context, groupID *int64) (*Account, error) {
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, PlatformGemini, true)
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}
	if len(accounts) == 0 {
		return nil, errors.New("no available Gemini accounts")
	}

	rank := func(a *Account) int {
		if a == nil {
			return 999
		}
		switch a.Type {
		case AccountTypeAPIKey:
			if strings.TrimSpace(a.GetCredential("api_key")) != "" {
				return 0
			}
			return 9
		case AccountTypeOAuth:
			if strings.TrimSpace(a.GetCredential("project_id")) == "" {
				return 1
			}
			if strings.TrimSpace(a.GetCredential("oauth_type")) == "ai_studio" {
				return 2
			}
			// Code Assist OAuth tokens often lack AI Studio scopes for models listing.
			return 3
		case AccountTypeServiceAccount:
			// Vertex service accounts use aiplatform.googleapis.com, not the AI Studio
			// endpoint (generativelanguage.googleapis.com), so they cannot serve these requests.
			return 999
		default:
			return 10
		}
	}

	var selected *Account
	for i := range accounts {
		acc := &accounts[i]
		if selected == nil {
			selected = acc
			continue
		}

		r1, r2 := rank(acc), rank(selected)
		if r1 < r2 {
			selected = acc
			continue
		}
		if r1 > r2 {
			continue
		}

		if acc.Priority < selected.Priority {
			selected = acc
		} else if acc.Priority == selected.Priority {
			switch {
			case acc.LastUsedAt == nil && selected.LastUsedAt != nil:
				selected = acc
			case acc.LastUsedAt != nil && selected.LastUsedAt == nil:
				// keep selected
			case acc.LastUsedAt == nil && selected.LastUsedAt == nil:
				if acc.Type == AccountTypeOAuth && selected.Type != AccountTypeOAuth {
					selected = acc
				}
			default:
				if acc.LastUsedAt.Before(*selected.LastUsedAt) {
					selected = acc
				}
			}
		}
	}

	if selected == nil {
		return nil, errors.New("no available Gemini accounts")
	}
	return s.hydrateSelectedAccount(ctx, selected)
}

func (s *GeminiMessagesCompatService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	beginGeminiImageOutputObservation(c)
	startTime := time.Now()

	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("missing model")
	}

	originalModel := req.Model
	mappedModel := req.Model
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(req.Model)
	}

	plan := apicompat.MessagesToGeminiPlan(body)
	toolIDProfile := geminiToolIDProfileForAccount(account)
	geminiReq, err := convertClaudeMessagesToGeminiGenerateContent(body, toolIDProfile)
	if err != nil {
		plan.Mode = protocol.Rejected
		plan.Diagnostics = append(plan.Diagnostics, protocol.Diagnostic{Field: "", Reason: "conversion_rejected", Severity: "error"})
		bindGeminiConversionPlan(ctx, c, plan)
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	ctx = bindGeminiConversionPlan(ctx, c, plan)
	geminiReq = ensureGeminiFunctionCallThoughtSignatures(geminiReq)
	originalClaudeBody := body

	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}

	useUpstreamStream := req.Stream
	if account.Type == AccountTypeOAuth && !req.Stream && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		// Code Assist's non-streaming generateContent may return no content; use streaming upstream and aggregate.
		useUpstreamStream = true
	}
	if !isGeminiTransportAccount(account) {
		return nil, fmt.Errorf("unsupported account type: %s", account.Type)
	}
	upstreamAction := "generateContent"
	if useUpstreamStream {
		upstreamAction = "streamGenerateContent"
	}
	selectedAction := geminiprovider.Action{Model: mappedModel, Name: upstreamAction, Stream: useUpstreamStream}
	requestIDHeader := geminiprovider.RequestIDHeader

	var resp *http.Response
	signatureRetryStage := 0
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, err := s.buildGeminiGenerationRequest(ctx, account, selectedAction, geminiReq, false, true)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Local build error: don't retry.
			if strings.Contains(err.Error(), "missing project_id") {
				return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			}
			return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", err.Error())
		}

		resp, err = doCatalogCheckedGeneration(s.httpUpstream, s.accountRepo, upstreamReq, proxyURL, account)
		if err != nil {
			if IsAttemptAdmissionError(err) || isClientCanceledTransportError(ctx, err) {
				return nil, err
			}
			return nil, s.handleUpstreamTransportError(ctx, c, account, err)
		}

		// Special-case: signature/thought_signature validation errors are not transient, but may be fixed by
		// downgrading Claude thinking/tool history to plain text (conservative two-stage retry).
		if resp.StatusCode == http.StatusBadRequest && signatureRetryStage < 2 {
			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()

			if isGeminiSignatureRelatedError(respBody) {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "signature_error",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				var strippedClaudeBody []byte
				stageName := ""
				// 路径说明：本处上游是 Gemini，但被剥离的 body 是 Anthropic 格式。传 originalModel
				// （客户端原 Anthropic model）而非 mappedModel（上游 Gemini model），让剥离逻辑按
				// 客户端请求的 Anthropic 子协议族判定（详见 ResolveThinkingProtocol 文档）。
				switch signatureRetryStage {
				case 0:
					// Stage 1: disable thinking + thinking->text
					strippedClaudeBody = FilterThinkingBlocksForRetry(originalClaudeBody, originalModel)
					stageName = "thinking-only"
					signatureRetryStage = 1
				default:
					// Stage 2: additionally downgrade tool_use/tool_result blocks to text
					strippedClaudeBody = FilterSignatureSensitiveBlocksForRetry(originalClaudeBody, originalModel)
					stageName = "thinking+tools"
					signatureRetryStage = 2
				}
				retryGeminiReq, txErr := convertClaudeMessagesToGeminiGenerateContent(strippedClaudeBody, toolIDProfile)
				if txErr == nil {
					logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: detected signature-related 400, retrying with downgraded Claude blocks (%s)", account.ID, stageName)
					geminiReq = retryGeminiReq
					// Consume one retry budget attempt and continue with the updated request payload.
					sleepGeminiBackoff(1)
					continue
				}
			}

			// Restore body for downstream error handling.
			resp = &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     resp.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}
			break
		}

		// 错误策略优先：匹配则跳过重试直接处理。
		if matched, rebuilt := s.checkErrorPolicyInLoop(ctx, account, resp, mappedModel); matched {
			resp = rebuilt
			break
		} else {
			resp = rebuilt
		}

		if resp.StatusCode >= 400 && s.shouldRetryGeminiUpstreamError(account, resp.StatusCode) {
			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()
			// Don't treat insufficient-scope as transient.
			if resp.StatusCode == 403 && isGeminiInsufficientScope(resp.Header, respBody) {
				resp = &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(respBody)),
				}
				break
			}
			if resp.StatusCode == 429 {
				// Mark as rate-limited early so concurrent requests avoid this account.
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			if attempt < geminiMaxRetries {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "retry",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, resp.StatusCode, attempt, geminiMaxRetries)
				sleepGeminiBackoff(attempt)
				continue
			}
			// Final attempt: surface the upstream error body (mapped below) instead of a generic retry error.
			resp = &http.Response{
				StatusCode: resp.StatusCode,
				Header:     resp.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}
			break
		}

		break
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody := s.readUpstreamErrorBody(resp)
		// 统一错误策略：自定义错误码 + 临时不可调度
		if s.rateLimitService != nil {
			policy := s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, respBody, mappedModel)
			switch policy {
			case ErrorPolicySkipped:
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				if failoverErr := s.skippedErrorPolicyFailoverError(c, account, resp.StatusCode, respBody, upstreamReqID); failoverErr != nil {
					return nil, failoverErr
				}
				if account.IsCustomErrorCodesEnabled() {
					return nil, s.writeGeminiCustomCodeSkippedError(c, account, resp.StatusCode, upstreamReqID, respBody, func() {
						_ = s.writeClaudeError(c, http.StatusInternalServerError, "api_error", geminiCustomCodeSkippedClientMessage)
					})
				}
				// 池模式：客户端写出与 ErrorPolicyNone 相同（按上游真实状态码映射），仅跳过账号状态标记。
				return nil, s.writeGeminiMappedError(c, account, resp.StatusCode, upstreamReqID, respBody)
			case ErrorPolicyMatched, ErrorPolicyTempUnscheduled:
				if policy == ErrorPolicyMatched {
					s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
				}
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
			}
		}

		// ErrorPolicyNone → 原有逻辑
		s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		// 精确匹配服务端配置类 400 错误，触发 failover + 临时封禁
		if resp.StatusCode == http.StatusBadRequest {
			msg400 := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			if isGoogleProjectConfigError(msg400) {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				if !ctxkey.PrivateGatewayContent(ctx) {
					log.Printf("[Gemini] status=400 google_config_error failover=true upstream_message=%q account=%d", upstreamMsg, account.ID)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody, RetryableOnSameAccount: true}
			}
		}
		if s.shouldFailoverGeminiUpstreamError(resp.StatusCode) {
			upstreamReqID := resp.Header.Get(requestIDHeader)
			if upstreamReqID == "" {
				upstreamReqID = resp.Header.Get("x-goog-request-id")
			}
			upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
			upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(respBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  upstreamReqID,
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
		}
		upstreamReqID := resp.Header.Get(requestIDHeader)
		if upstreamReqID == "" {
			upstreamReqID = resp.Header.Get("x-goog-request-id")
		}
		return nil, s.writeGeminiMappedError(c, account, resp.StatusCode, upstreamReqID, respBody)
	}

	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = resp.Header.Get("x-goog-request-id")
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	if req.Stream {
		streamRes, err := s.handleStreamingResponse(c, resp, startTime, originalModel)
		if err != nil {
			if streamRes != nil && streamRes.usage != nil {
				return &ForwardResult{RequestID: requestID, UpstreamHeaders: resp.Header, Usage: *streamRes.usage, Model: originalModel, UpstreamModel: mappedModel, Stream: true, Duration: time.Since(startTime), FirstTokenMs: streamRes.firstTokenMs}, err
			}
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
	} else {
		if useUpstreamStream {
			collected, usageObj, err := collectGeminiSSE(s.cfg, resp.Body, true)
			if err != nil {
				return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream stream")
			}
			collectedBytes, _ := json.Marshal(collected)
			upstreamResponseModelObserverFromContext(c).ObserveGemini(collectedBytes)
			observeGeminiImageOutputs(c, collectedBytes)
			if err := validateGeminiResponseToolIDs(collected); err != nil {
				return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", err.Error())
			}
			claudeResp, usageObj2 := convertGeminiToClaudeMessage(collected, originalModel, collectedBytes, false)
			c.JSON(http.StatusOK, claudeResp)
			usage = usageObj2
			if usageObj != nil && (usageObj.InputTokens > 0 || usageObj.OutputTokens > 0) {
				usage = usageObj
			}
		} else {
			usage, err = s.handleNonStreamingResponse(c, resp, originalModel)
			if err != nil {
				return nil, err
			}
		}
	}

	// 图片生成计费
	imageInputSize := s.extractImageInputSize(body)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)
	imageCount := resolveGeminiImageCount(c, originalModel, mappedModel)

	return &ForwardResult{
		RequestID:                     requestID,
		UpstreamHeaders:               resp.Header,
		Usage:                         *usage,
		Model:                         originalModel,
		UpstreamModel:                 mappedModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		Stream:                        req.Stream,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
		ImageCount:                    imageCount,
		ImageSize:                     imageSize,
		ImageInputSize:                imageInputSize,
	}, nil
}

func bindGeminiConversionPlan(ctx context.Context, c *gin.Context, plan protocol.ConversionPlan) context.Context {
	if c.Request != nil {
		c.Request = c.Request.WithContext(protocol.WithConversionPlan(c.Request.Context(), plan))
	}
	return protocol.WithConversionPlan(ctx, plan)
}

func isGeminiTransportAccount(account *Account) bool {
	switch account.Type {
	case AccountTypeAPIKey, AccountTypeOAuth, AccountTypeServiceAccount:
		return true
	default:
		return false
	}
}

func (s *GeminiMessagesCompatService) buildGeminiGenerationRequest(ctx context.Context, account *Account, action geminiprovider.Action, body []byte, forceAIStudio, bridge bool) (*http.Request, error) {
	var apiKey, token string
	if account.Type == AccountTypeAPIKey {
		apiKey = account.GetCredential("api_key")
		if err := geminiprovider.ValidateAPIKey(apiKey); err != nil {
			return nil, err
		}
	} else {
		if s.tokenProvider == nil {
			return nil, errors.New("gemini token provider not configured")
		}
		var err error
		token, err = s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return nil, err
		}
	}
	if account.Type == AccountTypeOAuth && !forceAIStudio {
		if project := strings.TrimSpace(account.GetCredential("project_id")); project != "" {
			baseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
			if err != nil {
				return nil, err
			}
			return action.CodeAssist(ctx, baseURL, project, token, body)
		}
	}

	var fullURL string
	var err error
	if account.Type == AccountTypeServiceAccount {
		fullURL, err = buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(action.Model), action.Model, action.Name, action.Stream)
	} else {
		baseURL, validationErr := s.validateUpstreamBaseURL(account.GetGeminiBaseURL(geminicli.AIStudioBaseURL))
		if validationErr != nil {
			return nil, validationErr
		}
		fullURL, err = buildGeminiAIStudioModelActionURL(baseURL, action.Model, action.Name, action.Stream)
	}
	if err != nil {
		return nil, err
	}
	if bridge {
		body = apicompat.NormalizeGeminiRequestForAIStudio(body)
	}
	return geminiprovider.Generate(ctx, fullURL, apiKey, token, body)
}

func isGeminiSignatureRelatedError(respBody []byte) bool {
	msg := strings.ToLower(strings.TrimSpace(extractAntigravityErrorMessage(respBody)))
	if msg == "" {
		msg = strings.ToLower(string(respBody))
	}
	return strings.Contains(msg, "thought_signature") || strings.Contains(msg, "signature")
}

func (s *GeminiMessagesCompatService) ForwardNative(ctx context.Context, c *gin.Context, account *Account, originalModel string, action string, stream bool, body []byte) (*ForwardResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	beginUpstreamResponseModelObservation(c)
	beginGeminiImageOutputObservation(c)
	startTime := time.Now()

	if strings.TrimSpace(originalModel) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing model in URL")
	}
	if strings.TrimSpace(action) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing action in URL")
	}
	if len(body) == 0 {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Request body is empty")
	}

	// 过滤掉 parts 为空的消息（Gemini API 不接受空 parts）
	if filteredBody, err := filterEmptyPartsFromGeminiRequest(body); err == nil {
		body = filteredBody
	}

	switch action {
	case "generateContent", "streamGenerateContent", "countTokens", "embedContent", "batchEmbedContents":
		// ok
	default:
		return nil, s.writeGoogleError(c, http.StatusNotFound, "Unsupported action: "+action)
	}

	// Some Gemini upstreams validate tool call parts strictly; ensure any `functionCall` part includes a
	// `thoughtSignature` to avoid frequent INVALID_ARGUMENT 400s.
	body = ensureGeminiFunctionCallThoughtSignatures(body)

	mappedModel := originalModel
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(originalModel)
	}
	if action == "embedContent" || action == "batchEmbedContents" {
		if stream || account.Type != AccountTypeAPIKey {
			return nil, s.writeGoogleError(c, http.StatusBadRequest, "Native embeddings require a non-streaming Gemini API key request")
		}
		var err error
		body, err = prepareGeminiEmbeddingBody(body, action, originalModel, mappedModel)
		if err != nil {
			return nil, s.writeGoogleError(c, http.StatusBadRequest, err.Error())
		}
	}

	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}

	useUpstreamStream := stream && c.Query("alt") == "sse"
	if stream && account.Type == AccountTypeOAuth && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		useUpstreamStream = true
	}
	upstreamAction := action
	if account.Type == AccountTypeOAuth && !stream && action == "generateContent" && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		// Code Assist's non-streaming generateContent may return no content; use streaming upstream and aggregate.
		useUpstreamStream = true
		upstreamAction = "streamGenerateContent"
	}
	forceAIStudio := action == "countTokens"

	ctx = bindGeminiConversionPlan(ctx, c, protocol.GeminiNativePlan(action))

	if !isGeminiTransportAccount(account) {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Unsupported account type: "+account.Type)
	}
	selectedAction := geminiprovider.Action{Model: mappedModel, Name: upstreamAction, Stream: useUpstreamStream}
	requestIDHeader := geminiprovider.RequestIDHeader

	var resp *http.Response
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, err := s.buildGeminiGenerationRequest(ctx, account, selectedAction, body, forceAIStudio, false)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Local build error: don't retry.
			if strings.Contains(err.Error(), "missing project_id") {
				return nil, s.writeGoogleError(c, http.StatusBadRequest, err.Error())
			}
			return nil, s.writeGoogleError(c, http.StatusBadGateway, err.Error())
		}

		resp, err = doCatalogCheckedGeneration(s.httpUpstream, s.accountRepo, upstreamReq, proxyURL, account)
		if err != nil {
			if IsAttemptAdmissionError(err) || isClientCanceledTransportError(ctx, err) {
				return nil, err
			}
			transportErr := s.handleUpstreamTransportError(ctx, c, account, err)
			// countTokens 不因上游链路故障而失败：本地估算兜底，不换号。
			var failoverErr *UpstreamFailoverError
			if action == "countTokens" && errors.As(transportErr, &failoverErr) {
				estimated := estimateGeminiCountTokens(body)
				c.Header("X-Token-Count-Estimated", "true")
				c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
				return &ForwardResult{
					RequestID:     "",
					Usage:         ClaudeUsage{},
					Model:         originalModel,
					UpstreamModel: mappedModel,
					Stream:        false,
					Duration:      time.Since(startTime),
					FirstTokenMs:  nil,
				}, nil
			}
			return nil, transportErr
		}

		// 错误策略优先：匹配则跳过重试直接处理。
		if matched, rebuilt := s.checkErrorPolicyInLoop(ctx, account, resp, mappedModel); matched {
			resp = rebuilt
			break
		} else {
			resp = rebuilt
		}

		if resp.StatusCode >= 400 && s.shouldRetryGeminiUpstreamError(account, resp.StatusCode) {
			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()
			// Don't treat insufficient-scope as transient.
			if resp.StatusCode == 403 && isGeminiInsufficientScope(resp.Header, respBody) {
				resp = &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(respBody)),
				}
				break
			}
			if resp.StatusCode == 429 {
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			if attempt < geminiMaxRetries {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "retry",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, resp.StatusCode, attempt, geminiMaxRetries)
				sleepGeminiBackoff(attempt)
				continue
			}
			if action == "countTokens" {
				estimated := estimateGeminiCountTokens(body)
				c.Header("X-Token-Count-Estimated", "true")
				c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
				return &ForwardResult{
					RequestID:     "",
					Usage:         ClaudeUsage{},
					Model:         originalModel,
					UpstreamModel: mappedModel,
					Stream:        false,
					Duration:      time.Since(startTime),
					FirstTokenMs:  nil,
				}, nil
			}
			// Final attempt: surface the upstream error body (passed through below) instead of a generic retry error.
			resp = &http.Response{
				StatusCode: resp.StatusCode,
				Header:     resp.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}
			break
		}

		break
	}
	defer func() { _ = resp.Body.Close() }()

	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = resp.Header.Get("x-goog-request-id")
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	isOAuth := account.Type == AccountTypeOAuth

	if resp.StatusCode >= 400 {
		respBody := s.readUpstreamErrorBody(resp)
		// Best-effort fallback for OAuth tokens missing AI Studio scopes when calling countTokens.
		// This avoids Gemini SDKs failing hard during preflight token counting.
		// Checked before error policy so it always works regardless of custom error codes.
		if action == "countTokens" && isOAuth && isGeminiInsufficientScope(resp.Header, respBody) {
			estimated := estimateGeminiCountTokens(body)
			c.Header("X-Token-Count-Estimated", "true")
			c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
			return &ForwardResult{
				RequestID:       requestID,
				UpstreamHeaders: resp.Header,
				Usage:           ClaudeUsage{},
				Model:           originalModel,
				UpstreamModel:   mappedModel,
				Stream:          false,
				Duration:        time.Since(startTime),
				FirstTokenMs:    nil,
			}, nil
		}

		// 统一错误策略：自定义错误码 + 临时不可调度
		if s.rateLimitService != nil {
			policy := s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, respBody, mappedModel)
			switch policy {
			case ErrorPolicySkipped:
				if failoverErr := s.skippedErrorPolicyFailoverError(c, account, resp.StatusCode, respBody, requestID); failoverErr != nil {
					return nil, failoverErr
				}
				if account.IsCustomErrorCodesEnabled() {
					return nil, s.writeGeminiCustomCodeSkippedError(c, account, resp.StatusCode, requestID, respBody, func() {
						_ = s.writeGoogleError(c, http.StatusInternalServerError, geminiCustomCodeSkippedClientMessage)
					})
				}
				// 池模式：客户端写出与 ErrorPolicyNone 相同（状态码/响应体保真），仅跳过账号状态标记。
				return nil, s.writeGeminiNativeUpstreamError(c, account, resp, respBody, requestID, isOAuth)
			case ErrorPolicyMatched, ErrorPolicyTempUnscheduled:
				if policy == ErrorPolicyMatched {
					s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
				}
				evBody := unwrapIfNeeded(isOAuth, respBody)
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(evBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(evBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  requestID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
			}
		}

		// ErrorPolicyNone → 原有逻辑
		s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		// 精确匹配服务端配置类 400 错误，触发 failover + 临时封禁
		if resp.StatusCode == http.StatusBadRequest {
			msg400 := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			if isGoogleProjectConfigError(msg400) {
				evBody := unwrapIfNeeded(isOAuth, respBody)
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(evBody), maxBytes)
				}
				if !ctxkey.PrivateGatewayContent(ctx) {
					log.Printf("[Gemini] status=400 google_config_error failover=true upstream_message=%q account=%d", upstreamMsg, account.ID)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  requestID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: evBody, RetryableOnSameAccount: true}
			}
		}
		if s.shouldFailoverGeminiUpstreamError(resp.StatusCode) {
			evBody := unwrapIfNeeded(isOAuth, respBody)
			upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(evBody))
			upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(evBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  requestID,
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: evBody}
		}

		return nil, s.writeGeminiNativeUpstreamError(c, account, resp, respBody, requestID, isOAuth)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int

	if stream {
		var streamRes *geminiNativeStreamResult
		var err error
		if c.Query("alt") == "sse" && strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			streamRes, err = s.handleNativeJSONAsSSE(c, resp, startTime, isOAuth, account, requestID)
		} else if c.Query("alt") == "sse" {
			streamRes, err = s.handleNativeStreamingResponse(c, resp, startTime, isOAuth, account, requestID)
		} else {
			streamRes, err = s.handleNativeJSONStreamingResponse(c, resp, startTime, isOAuth, strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream"), account, requestID)
		}
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
	} else {
		if useUpstreamStream {
			var best geminiResponseSignal
			collected, usageObj, stats, err := collectGeminiSSEObserved(s.cfg, resp.Body, isOAuth, func(rawBytes []byte) {
				if sig, ok := detectGeminiResponseSignal(rawBytes); ok && sig.Kind > best.Kind {
					best = sig
				}
			})
			if err != nil {
				return nil, s.writeGoogleError(c, http.StatusBadGateway, "Failed to read upstream stream")
			}
			s.finalizeGeminiSSESignal(c, account, false, requestID, best, stats.dataEvents > 0, stats.fallback)
			b, _ := json.Marshal(collected)
			upstreamResponseModelObserverFromContext(c).ObserveGemini(b)
			observeGeminiImageOutputs(c, b)
			c.Data(http.StatusOK, "application/json", b)
			usage = usageObj
		} else {
			usageResp, err := s.handleNativeNonStreamingResponse(c, resp, isOAuth, account, requestID)
			if err != nil {
				return nil, err
			}
			usage = usageResp
		}
	}

	if usage == nil {
		usage = &ClaudeUsage{}
	}

	// 图片生成计费
	imageInputSize := s.extractImageInputSize(body)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)
	imageCount := resolveGeminiImageCount(c, originalModel, mappedModel)

	return &ForwardResult{
		RequestID:                     requestID,
		UpstreamHeaders:               resp.Header,
		Usage:                         *usage,
		Model:                         originalModel,
		UpstreamModel:                 mappedModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		Stream:                        stream,
		ReasoningEffort:               extractGeminiReasoningEffortFromBody(body),
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
		ImageCount:                    imageCount,
		ImageSize:                     imageSize,
		ImageInputSize:                imageInputSize,
	}, nil
}

// checkErrorPolicyInLoop 在重试循环内预检查错误策略。
// 返回 true 表示策略已匹配（调用者应 break），resp 已重建可直接使用。
// 返回 false 表示 ErrorPolicyNone，resp 已重建，调用者继续走重试逻辑。
func (s *GeminiMessagesCompatService) checkErrorPolicyInLoop(
	ctx context.Context, account *Account, resp *http.Response, mappedModel string,
) (matched bool, rebuilt *http.Response) {
	if resp.StatusCode < 400 || s.rateLimitService == nil {
		return false, resp
	}
	body := s.readUpstreamErrorBody(resp)
	_ = resp.Body.Close()
	rebuilt = &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	policy := s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, body, mappedModel)
	return policy != ErrorPolicyNone, rebuilt
}

func (s *GeminiMessagesCompatService) shouldRetryGeminiUpstreamError(account *Account, statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504, 529:
		return true
	case 403:
		// GeminiCli OAuth occasionally returns 403 transiently (activation/quota propagation); allow retry.
		if account == nil || account.Type != AccountTypeOAuth {
			return false
		}
		oauthType := strings.ToLower(strings.TrimSpace(account.GetCredential("oauth_type")))
		if oauthType == "" && strings.TrimSpace(account.GetCredential("project_id")) != "" {
			// Legacy/implicit Code Assist OAuth accounts.
			oauthType = "code_assist"
		}
		return oauthType == "code_assist"
	default:
		return false
	}
}

func (s *GeminiMessagesCompatService) shouldFailoverGeminiUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

// skippedErrorPolicyFailoverError 命中 ErrorPolicySkipped（池模式、或自定义错误码未命中）
// 时构造 failover 错误：可 failover 的状态码返回 UpstreamFailoverError，交给 handler 层换号
// （池模式账号按 pool_mode_retry_count 先同账号重试）；返回 nil 表示状态码不可 failover，
// 由调用方决定客户端写出。Skipped 只豁免账号状态标记，不豁免换号，与 OpenAI 网关路径一致。
func (s *GeminiMessagesCompatService) skippedErrorPolicyFailoverError(c *gin.Context, account *Account, statusCode int, respBody []byte, upstreamRequestID string) *UpstreamFailoverError {
	if !s.shouldFailoverGeminiUpstreamError(statusCode) {
		return nil
	}
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
	upstreamDetail := s.upstreamErrorDetail(respBody)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: statusCode,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "failover",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	return &UpstreamFailoverError{
		StatusCode:             statusCode,
		ResponseBody:           respBody,
		RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(statusCode),
	}
}

// geminiCustomCodeSkippedClientMessage 自定义错误码未命中时对客户端隐藏上游细节的固定文案，
// 与 OpenAI 网关路径同场景的文案一致。
const geminiCustomCodeSkippedClientMessage = "Upstream gateway error"

// upstreamErrorDetail 按配置截断上游错误响应体，用于 ops 错误日志的 Detail 字段；
// 未开启 LogUpstreamErrorBody 时返回空。
func (s *GeminiMessagesCompatService) upstreamErrorDetail(body []byte) string {
	if s.cfg == nil || !s.cfg.Gateway.LogUpstreamErrorBody {
		return ""
	}
	maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2048
	}
	return truncateString(string(body), maxBytes)
}

// writeGeminiCustomCodeSkippedError 处理自定义错误码未命中且不可 failover 的上游错误：
// 客户端统一收到 500 + 固定文案（由 write 按端点格式写出），不透传上游细节；
// 上游真实状态码与错误信息仅记录到 ops 错误日志。
func (s *GeminiMessagesCompatService) writeGeminiCustomCodeSkippedError(c *gin.Context, account *Account, upstreamStatus int, upstreamRequestID string, body []byte, write func()) error {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	upstreamDetail := s.upstreamErrorDetail(body)
	setOpsUpstreamError(c, upstreamStatus, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstreamStatus,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	write()
	if upstreamMsg == "" {
		return fmt.Errorf("gemini upstream error: %d (not in custom error codes)", upstreamStatus)
	}
	return fmt.Errorf("gemini upstream error: %d (not in custom error codes) message=%s", upstreamStatus, upstreamMsg)
}

// writeGeminiNativeUpstreamError 将不可 failover 的上游错误按原始状态码与响应体透传给客户端，
// 并记录 ops 错误事件。状态码保真：下游据此区分请求级错误与可重试的链路故障。
func (s *GeminiMessagesCompatService) writeGeminiNativeUpstreamError(c *gin.Context, account *Account, resp *http.Response, respBody []byte, requestID string, isOAuth bool) error {
	respBody = unwrapIfNeeded(isOAuth, respBody)
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	upstreamDetail := s.upstreamErrorDetail(respBody)
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody && !privateGatewayContent(c) {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini] native upstream error %d: %s", resp.StatusCode, truncateForLog(respBody, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes))
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  requestID,
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	MarkResponseCommitted(c)
	c.Data(resp.StatusCode, contentType, respBody)
	if upstreamMsg == "" {
		return fmt.Errorf("gemini upstream error: %d", resp.StatusCode)
	}
	return fmt.Errorf("gemini upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
}

func sleepGeminiBackoff(attempt int) {
	delay := geminiRetryBaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > geminiRetryMaxDelay {
		delay = geminiRetryMaxDelay
	}

	// +/- 20% jitter
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	jitter := time.Duration(float64(delay) * 0.2 * (r.Float64()*2 - 1))
	sleepFor := delay + jitter
	if sleepFor < 0 {
		sleepFor = 0
	}
	time.Sleep(sleepFor)
}

var (
	sensitiveQueryParamRegex = regexp.MustCompile(`(?i)([?&](?:key|client_secret|access_token|refresh_token)=)[^&"\s]+`)
	retryInRegex             = regexp.MustCompile(`Please retry in ([0-9.]+)s`)
)

func sanitizeUpstreamErrorMessage(msg string) string {
	if msg == "" {
		return msg
	}
	return sensitiveQueryParamRegex.ReplaceAllString(msg, `$1***`)
}

func (s *GeminiMessagesCompatService) writeGeminiMappedError(c *gin.Context, account *Account, upstreamStatus int, upstreamRequestID string, body []byte) error {
	MarkResponseCommitted(c)
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, upstreamStatus, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstreamStatus,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})

	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody && !privateGatewayContent(c) {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini] upstream error %d: %s", upstreamStatus, truncateForLog(body, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes))
	}

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		PlatformGemini,
		upstreamStatus,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		c.JSON(status, gin.H{
			"type":  "error",
			"error": gin.H{"type": errType, "message": errMsg},
		})
		if upstreamMsg == "" {
			upstreamMsg = errMsg
		}
		if upstreamMsg == "" {
			return fmt.Errorf("upstream error: %d (passthrough rule matched)", upstreamStatus)
		}
		return fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", upstreamStatus, upstreamMsg)
	}

	var statusCode int
	var errType, errMsg string

	if mapped := mapGeminiErrorBodyToClaudeError(body); mapped != nil {
		errType = mapped.Type
		if mapped.Message != "" {
			errMsg = mapped.Message
		}
		if mapped.StatusCode > 0 {
			statusCode = mapped.StatusCode
		}
	}

	switch upstreamStatus {
	case 400:
		if statusCode == 0 {
			statusCode = http.StatusBadRequest
		}
		if errType == "" {
			errType = "invalid_request_error"
		}
		// 400 是确定性的请求错误：回传上游 message（已脱敏），客户端据此定位非法字段。
		if errMsg == "" {
			errMsg = upstreamMsg
		}
		if errMsg == "" {
			errMsg = "Invalid request"
		}
	case 401:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			errType = "authentication_error"
		}
		if errMsg == "" {
			errMsg = "Upstream authentication failed, please contact administrator"
		}
	case 403:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			errType = "permission_error"
		}
		if errMsg == "" {
			errMsg = "Upstream access forbidden, please contact administrator"
		}
	case 404:
		if statusCode == 0 {
			statusCode = http.StatusNotFound
		}
		if errType == "" {
			errType = "not_found_error"
		}
		if errMsg == "" {
			errMsg = "Resource not found"
		}
	case 429:
		if statusCode == 0 {
			statusCode = http.StatusTooManyRequests
		}
		if errType == "" {
			errType = "rate_limit_error"
		}
		if errMsg == "" {
			errMsg = "Upstream rate limit exceeded, please retry later"
		}
	case 529:
		if statusCode == 0 {
			statusCode = http.StatusServiceUnavailable
		}
		if errType == "" {
			errType = "overloaded_error"
		}
		if errMsg == "" {
			errMsg = "Upstream service overloaded, please retry later"
		}
	case 500, 502, 503, 504:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			switch upstreamStatus {
			case 504:
				errType = "timeout_error"
			case 503:
				errType = "overloaded_error"
			default:
				errType = "api_error"
			}
		}
		if errMsg == "" {
			errMsg = "Upstream service temporarily unavailable"
		}
	default:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			errType = "upstream_error"
		}
		if errMsg == "" {
			errMsg = "Upstream request failed"
		}
	}

	c.JSON(statusCode, gin.H{
		"type":  "error",
		"error": gin.H{"type": errType, "message": errMsg},
	})
	if upstreamMsg == "" {
		return fmt.Errorf("upstream error: %d", upstreamStatus)
	}
	return fmt.Errorf("upstream error: %d message=%s", upstreamStatus, upstreamMsg)
}

type claudeErrorMapping struct {
	Type       string
	Message    string
	StatusCode int
}

func mapGeminiErrorBodyToClaudeError(body []byte) *claudeErrorMapping {
	if len(body) == 0 {
		return nil
	}

	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	if strings.TrimSpace(parsed.Error.Status) == "" && parsed.Error.Code == 0 && strings.TrimSpace(parsed.Error.Message) == "" {
		return nil
	}

	mapped := &claudeErrorMapping{
		Type:    mapGeminiStatusToClaudeErrorType(parsed.Error.Status),
		Message: "",
	}
	if mapped.Type == "" {
		mapped.Type = "upstream_error"
	}

	switch strings.ToUpper(strings.TrimSpace(parsed.Error.Status)) {
	case "INVALID_ARGUMENT":
		mapped.StatusCode = http.StatusBadRequest
	case "NOT_FOUND":
		mapped.StatusCode = http.StatusNotFound
	case "RESOURCE_EXHAUSTED":
		mapped.StatusCode = http.StatusTooManyRequests
	default:
		// Keep StatusCode unset and let HTTP status mapping decide.
	}

	// Keep messages generic by default; upstream error message can be long or include sensitive fragments.
	return mapped
}

func mapGeminiStatusToClaudeErrorType(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "INVALID_ARGUMENT":
		return "invalid_request_error"
	case "PERMISSION_DENIED":
		return "permission_error"
	case "NOT_FOUND":
		return "not_found_error"
	case "RESOURCE_EXHAUSTED":
		return "rate_limit_error"
	case "UNAUTHENTICATED":
		return "authentication_error"
	case "UNAVAILABLE":
		return "overloaded_error"
	case "INTERNAL":
		return "api_error"
	case "DEADLINE_EXCEEDED":
		return "timeout_error"
	default:
		return ""
	}
}

type geminiStreamResult struct {
	usage        *ClaudeUsage
	firstTokenMs *int
}

func (s *GeminiMessagesCompatService) handleNonStreamingResponse(c *gin.Context, resp *http.Response, originalModel string) (*ClaudeUsage, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream response")
	}

	unwrappedBody, err := unwrapGeminiResponse(body)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveGemini(unwrappedBody)
	observeGeminiImageOutputs(c, unwrappedBody)

	var geminiResp map[string]any
	if err := json.Unmarshal(unwrappedBody, &geminiResp); err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}

	if err := validateGeminiResponseToolIDs(geminiResp); err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", err.Error())
	}
	if err := validateGeminiTextReply(geminiResp); err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", err.Error())
	}
	claudeResp, usage := convertGeminiToClaudeMessage(geminiResp, originalModel, unwrappedBody, false)
	c.JSON(http.StatusOK, claudeResp)

	return usage, nil
}

func (s *GeminiMessagesCompatService) handleStreamingResponse(c *gin.Context, resp *http.Response, startTime time.Time, originalModel string) (result *geminiStreamResult, streamErr error) {
	var firstTokenMs *int
	var usage ClaudeUsage
	defer func() {
		if streamErr != nil {
			result = &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs}
		}
	}()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}

	stream := apicompat.NewGeminiMessagesStream(
		func(event string, data any) { writeSSE(c.Writer, event, data) },
		flusher.Flush, geminiFunctionCallID, validateGeminiStreamToolCall,
	)
	stream.Start(generateAnthropicMsgID(), originalModel)

	reader := newBoundedSSELineReader(resp.Body, s.cfg)
	for {
		line, err := reader.ReadLineString()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("stream read error: %w", err)
		}

		if !strings.HasPrefix(line, "data:") {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}

		unwrappedBytes, err := unwrapGeminiResponse([]byte(payload))
		if err != nil {
			continue
		}
		observer := upstreamResponseModelObserverFromContext(c)
		if observer == nil {
			observer = beginUpstreamResponseModelObservation(c)
		}
		observer.ObserveGemini(unwrappedBytes)
		observeGeminiImageOutputs(c, unwrappedBytes)

		var geminiResp map[string]any
		if err := json.Unmarshal(unwrappedBytes, &geminiResp); err != nil {
			return nil, fmt.Errorf("invalid Gemini stream JSON: %w", err)
		}
		if err := validateGeminiTextReply(geminiResp); err != nil {
			return nil, err
		}
		if u := extractGeminiUsage(unwrappedBytes); u != nil {
			usage = *u
		}

		if err := stream.Observe(geminiResp, func() {
			if firstTokenMs == nil {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
		}); err != nil {
			return nil, err
		}

		if u := extractGeminiUsage(unwrappedBytes); u != nil {
			usage = *u
		}

		// Process the final unterminated line at EOF as well.
		if errors.Is(err, io.EOF) {
			break
		}
	}

	if err := stream.Finish(usage.InputTokens, usage.OutputTokens, usage.CacheReadInputTokens); err != nil {
		return nil, err
	}

	return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs}, nil
}

func writeSSE(w io.Writer, event string, data any) {
	protocol.WriteSSE(w, event, data)
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// generateAnthropicMsgID 生成 Anthropic 官方格式的 message ID：msg_01 + 22 位 Base62
func generateAnthropicMsgID() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const idLen = 22
	randomBytes := make([]byte, idLen)
	if _, err := rand.Read(randomBytes); err != nil {
		return "msg_01" + randomHex(11)
	}
	b := make([]byte, idLen)
	for i := range b {
		b[i] = charset[int(randomBytes[i])%len(charset)]
	}
	return "msg_01" + string(b)
}

func (s *GeminiMessagesCompatService) writeClaudeError(c *gin.Context, status int, errType, message string) error {
	MarkResponseCommitted(c)
	protocol.WriteMessagesError(c, status, errType, message)
	return fmt.Errorf("%s", message)
}

func (s *GeminiMessagesCompatService) writeGoogleError(c *gin.Context, status int, message string) error {
	MarkResponseCommitted(c)
	message = protocol.WriteGeminiError(c, status, message)
	return fmt.Errorf("%s", message)
}

func unwrapIfNeeded(isOAuth bool, raw []byte) []byte {
	if !isOAuth {
		return raw
	}
	inner, err := unwrapGeminiResponse(raw)
	if err != nil {
		return raw
	}
	return inner
}

func collectGeminiSSE(cfg *config.Config, body io.Reader, isOAuth bool) (map[string]any, *ClaudeUsage, error) {
	collected, usage, _, err := collectGeminiSSEObserved(cfg, body, isOAuth, nil)
	return collected, usage, err
}

// geminiSSECollectStats 记录一次 SSE 聚合读到的 data 事件数，以及非 data 行的兜底内容。
type geminiSSECollectStats struct {
	dataEvents int
	fallback   *geminiSSEFallbackBody
}

// collectGeminiSSEObserved 在聚合的同时把每个解包后的事件原文交给 observe（可为 nil）。
func collectGeminiSSEObserved(cfg *config.Config, body io.Reader, isOAuth bool, observe func(rawBytes []byte)) (map[string]any, *ClaudeUsage, geminiSSECollectStats, error) {
	reader := newBoundedSSELineReader(body, cfg)

	var last map[string]any
	var lastWithParts map[string]any
	var collectedTextParts []string // Collect all text parts for aggregation
	var collectedToolParts []map[string]any
	seenToolIDs := map[string]bool{}
	usage := &ClaudeUsage{}
	stats := geminiSSECollectStats{fallback: &geminiSSEFallbackBody{}}
	finish := func() map[string]any {
		result := pickGeminiCollectResult(last, lastWithParts)
		if len(collectedToolParts) > 0 {
			parts := make([]any, 0)
			for _, part := range extractGeminiParts(result) {
				if _, isCall := part["functionCall"]; !isCall {
					parts = append(parts, part)
				}
			}
			for _, part := range collectedToolParts {
				parts = append(parts, part)
			}
			if candidates, ok := result["candidates"].([]any); ok && len(candidates) > 0 {
				if candidate, ok := candidates[0].(map[string]any); ok {
					if content, ok := candidate["content"].(map[string]any); ok {
						content["parts"] = parts
					}
				}
			}
		}
		return mergeCollectedTextParts(result, collectedTextParts)
	}

	for {
		line, err := reader.ReadLineString()
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if !strings.HasPrefix(trimmed, "data:") {
				if stats.dataEvents == 0 {
					stats.fallback.AddLine(trimmed)
				}
			} else {
				payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				switch payload {
				case "", "[DONE]":
					if payload == "[DONE]" {
						return finish(), usage, stats, nil
					}
				default:
					var parsed map[string]any
					var rawBytes []byte
					if isOAuth {
						innerBytes, err := unwrapGeminiResponse([]byte(payload))
						if err == nil {
							rawBytes = innerBytes
							_ = json.Unmarshal(innerBytes, &parsed)
						}
					} else {
						rawBytes = []byte(payload)
						_ = json.Unmarshal(rawBytes, &parsed)
					}
					if len(rawBytes) > 0 {
						stats.dataEvents++
						if observe != nil {
							observe(rawBytes)
						}
					}
					if parsed != nil {
						last = parsed
						if u := extractGeminiUsage(rawBytes); u != nil {
							usage = u
						}
						if parts := extractGeminiParts(parsed); len(parts) > 0 {
							lastWithParts = parsed
							// Collect text from each part for aggregation
							for _, part := range parts {
								if call, ok := part["functionCall"].(map[string]any); ok {
									// This collector accepts complete tool parts only. A
									// repeated ID cannot silently replace an earlier call.
									if err := validateGeminiStreamToolCall(call, "", "", seenToolIDs); err != nil {
										return nil, usage, stats, err
									}
									collectedToolParts = append(collectedToolParts, part)
								}
								if text, ok := part["text"].(string); ok && text != "" {
									collectedTextParts = append(collectedTextParts, text)
								}
							}
						}
					}
				}
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, stats, err
		}
	}

	return finish(), usage, stats, nil
}

func pickGeminiCollectResult(last map[string]any, lastWithParts map[string]any) map[string]any {
	if lastWithParts != nil {
		return lastWithParts
	}
	if last != nil {
		return last
	}
	return map[string]any{}
}

// mergeCollectedTextParts merges all collected text chunks into the final response.
// This fixes the issue where non-streaming responses only returned the last chunk
// instead of the complete aggregated text.
func mergeCollectedTextParts(response map[string]any, textParts []string) map[string]any {
	if len(textParts) == 0 {
		return response
	}

	// Join all text parts
	mergedText := strings.Join(textParts, "")

	// Deep copy response
	result := make(map[string]any)
	for k, v := range response {
		result[k] = v
	}

	// Get or create candidates
	candidates, ok := result["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		candidates = []any{map[string]any{}}
	}

	// Get first candidate
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		candidate = make(map[string]any)
		candidates[0] = candidate
	}

	// Get or create content
	content, ok := candidate["content"].(map[string]any)
	if !ok {
		content = map[string]any{"role": "model"}
		candidate["content"] = content
	}

	// Get existing parts
	existingParts, ok := content["parts"].([]any)
	if !ok {
		existingParts = []any{}
	}

	// Find and update first text part, or create new one
	newParts := make([]any, 0, len(existingParts)+1)
	textUpdated := false

	for _, p := range existingParts {
		pm, ok := p.(map[string]any)
		if !ok {
			newParts = append(newParts, p)
			continue
		}
		if _, hasText := pm["text"]; hasText && !textUpdated {
			// Replace with merged text
			newPart := make(map[string]any)
			for k, v := range pm {
				newPart[k] = v
			}
			newPart["text"] = mergedText
			newParts = append(newParts, newPart)
			textUpdated = true
		} else {
			newParts = append(newParts, pm)
		}
	}

	if !textUpdated {
		newParts = append([]any{map[string]any{"text": mergedText}}, newParts...)
	}

	content["parts"] = newParts
	result["candidates"] = candidates

	return result
}

type geminiNativeStreamResult struct {
	usage        *ClaudeUsage
	firstTokenMs *int
}

func isGeminiInsufficientScope(headers http.Header, body []byte) bool {
	if strings.Contains(strings.ToLower(headers.Get("Www-Authenticate")), "insufficient_scope") {
		return true
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "insufficient authentication scopes") || strings.Contains(lower, "access_token_scope_insufficient")
}

func estimateGeminiCountTokens(reqBody []byte) int {
	total := 0

	// systemInstruction.parts[].text
	gjson.GetBytes(reqBody, "systemInstruction.parts").ForEach(func(_, part gjson.Result) bool {
		if t := strings.TrimSpace(part.Get("text").String()); t != "" {
			total += estimateTokensForText(t)
		}
		return true
	})

	// contents[].parts[].text
	gjson.GetBytes(reqBody, "contents").ForEach(func(_, content gjson.Result) bool {
		content.Get("parts").ForEach(func(_, part gjson.Result) bool {
			if t := strings.TrimSpace(part.Get("text").String()); t != "" {
				total += estimateTokensForText(t)
			}
			return true
		})
		return true
	})

	if total < 0 {
		return 0
	}
	return total
}

func estimateTokensForText(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return 0
	}
	ascii := 0
	for _, r := range runes {
		if r <= 0x7f {
			ascii++
		}
	}
	asciiRatio := float64(ascii) / float64(len(runes))
	if asciiRatio >= 0.8 {
		// Roughly 4 chars per token for English-like text.
		return (len(runes) + 3) / 4
	}
	// For CJK-heavy text, approximate 1 rune per token.
	return len(runes)
}

type UpstreamHTTPResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (s *GeminiMessagesCompatService) handleNativeNonStreamingResponse(c *gin.Context, resp *http.Response, isOAuth bool, account *Account, upstreamRequestID string) (*ClaudeUsage, error) {
	if s.cfg != nil && s.cfg.Gateway.GeminiDebugResponseHeaders {
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ========== Response Headers ==========")
		for key, values := range resp.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-ratelimit") {
				logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] %s: %v", key, values)
			}
		}
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ========================================")
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}

	if isOAuth {
		unwrappedBody, uwErr := unwrapGeminiResponse(respBody)
		if uwErr == nil {
			respBody = unwrappedBody
		}
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveGemini(respBody)
	observeGeminiImageOutputs(c, respBody)
	if sig, ok := detectGeminiResponseSignalInBody(respBody); ok {
		s.markGeminiResponseSignal(c, account, sig, false, upstreamRequestID)
	} else if isGeminiEmptyResponseBody(respBody) {
		s.markGeminiEmptyResponse(c, account, false, upstreamRequestID)
	}

	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, respBody)

	if u := extractGeminiUsage(respBody); u != nil {
		return u, nil
	}
	return &ClaudeUsage{}, nil
}

func (s *GeminiMessagesCompatService) handleNativeStreamingResponse(c *gin.Context, resp *http.Response, startTime time.Time, isOAuth bool, account *Account, upstreamRequestID string) (*geminiNativeStreamResult, error) {
	if s.cfg != nil && s.cfg.Gateway.GeminiDebugResponseHeaders {
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ========== Streaming Response Headers ==========")
		for key, values := range resp.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-ratelimit") {
				logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] %s: %v", key, values)
			}
		}
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ====================================================")
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}

	c.Status(resp.StatusCode)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/event-stream; charset=utf-8"
	}
	c.Header("Content-Type", contentType)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}

	reader := newBoundedSSELineReader(resp.Body, s.cfg)
	usage := &ClaudeUsage{}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	var firstTokenMs *int
	var best geminiResponseSignal
	sawDataEvent := false
	fallback := &geminiSSEFallbackBody{}

	for {
		line, err := reader.ReadLineString()
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmed, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				// Keepalive / done markers
				if payload == "" || payload == "[DONE]" {
					_, _ = io.WriteString(c.Writer, line)
					flusher.Flush()
				} else {
					var rawToWrite string
					rawToWrite = payload

					var rawBytes []byte
					if isOAuth {
						innerBytes, err := unwrapGeminiResponse([]byte(payload))
						if err == nil {
							rawToWrite = string(innerBytes)
							rawBytes = innerBytes
						}
					} else {
						rawBytes = []byte(payload)
					}

					sawDataEvent = true
					if sig, ok := detectGeminiResponseSignal(rawBytes); ok && sig.Kind > best.Kind {
						best = sig
					}
					if u := extractGeminiUsage(rawBytes); u != nil {
						usage = u
					}
					observer.ObserveGemini(rawBytes)
					observeGeminiImageOutputs(c, rawBytes)

					if firstTokenMs == nil {
						ms := int(time.Since(startTime).Milliseconds())
						firstTokenMs = &ms
					}

					sanitized, errorPayload := sanitizeGoogleErrorPayload([]byte(rawToWrite))
					if errorPayload {
						rawToWrite = string(sanitized)
					}
					if isOAuth {
						// SSE format requires double newline (\n\n) to separate events
						_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", rawToWrite)
					} else if errorPayload {
						_, _ = fmt.Fprintf(c.Writer, "data: %s\n", rawToWrite)
					} else {
						// Pass-through for AI Studio responses.
						_, _ = io.WriteString(c.Writer, line)
					}
					flusher.Flush()
				}
			} else {
				if !sawDataEvent {
					fallback.AddLine(trimmed)
				}
				_, _ = io.WriteString(c.Writer, line)
				flusher.Flush()
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	s.finalizeGeminiSSESignal(c, account, true, upstreamRequestID, best, sawDataEvent, fallback)

	return &geminiNativeStreamResult{usage: usage, firstTokenMs: firstTokenMs}, nil
}

// ForwardAIStudioGET forwards a GET request to AI Studio (generativelanguage.googleapis.com) for
// endpoints like /v1beta/models and /v1beta/models/{model}.
//
// This is used to support Gemini SDKs that call models listing endpoints before generation.
func (s *GeminiMessagesCompatService) ForwardAIStudioGET(ctx context.Context, account *Account, path string) (*UpstreamHTTPResult, error) {
	if account == nil {
		return nil, errors.New("account is nil")
	}
	// path 会被直接拼到上游 base URL 后面，因此按路径护栏逐片段校验，
	// 见 upstream_path_guard.go。
	resource, err := url.ParseRequestURI(path)
	if err != nil || resource.IsAbs() || resource.Host != "" || resource.Fragment != "" {
		return nil, errors.New("invalid path")
	}
	query, err := url.ParseQuery(resource.RawQuery)
	if err != nil {
		return nil, errors.New("invalid model query")
	}
	for name, values := range query {
		if (name != "pageSize" && name != "pageToken" && name != "alt") || len(values) != 1 {
			return nil, errors.New("invalid model query parameter")
		}
		if name == "alt" && values[0] != "json" {
			return nil, errors.New("model listing only supports alt=json")
		}
	}
	sanitizedPath, ok := sanitizedUpstreamPathSuffix(resource.Path)
	if !ok || sanitizedPath == "" {
		return nil, errors.New("invalid path")
	}
	path = sanitizedPath

	baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	fullURL := strings.TrimRight(normalizedBaseURL, "/") + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	switch account.Type {
	case AccountTypeAPIKey:
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, errors.New("gemini api_key not configured")
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case AccountTypeOAuth:
		if s.tokenProvider == nil {
			return nil, errors.New("gemini token provider not configured")
		}
		accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	default:
		return nil, fmt.Errorf("unsupported account type: %s", account.Type)
	}

	resp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	wwwAuthenticate := resp.Header.Get("Www-Authenticate")
	filteredHeaders := responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter)
	if wwwAuthenticate != "" {
		filteredHeaders.Set("Www-Authenticate", wwwAuthenticate)
	}
	return &UpstreamHTTPResult{
		StatusCode: resp.StatusCode,
		Headers:    filteredHeaders,
		Body:       body,
	}, nil
}

// unwrapGeminiResponse 解包 Gemini OAuth 响应中的 response 字段
// 使用 gjson 零拷贝提取，避免完整 Unmarshal+Marshal
func unwrapGeminiResponse(raw []byte) ([]byte, error) {
	result := gjson.GetBytes(raw, "response")
	if result.Exists() && result.Type == gjson.JSON {
		return []byte(result.Raw), nil
	}
	return raw, nil
}

func convertGeminiToClaudeMessage(geminiResp map[string]any, originalModel string, rawData []byte, includeInlineData bool) (map[string]any, *ClaudeUsage) {
	usage := extractGeminiUsage(rawData)
	if usage == nil {
		usage = &ClaudeUsage{}
	}
	response := apicompat.GeminiToMessages(geminiResp, originalModel, generateAnthropicMsgID(), includeInlineData, geminiFunctionCallID, map[string]any{
		"input_tokens":            usage.InputTokens,
		"output_tokens":           usage.OutputTokens,
		"cache_read_input_tokens": usage.CacheReadInputTokens,
	})
	return response, usage
}

func isGeminiInlineImageMIMEType(mimeType string) bool {
	return apicompat.IsGeminiInlineImageMIMEType(mimeType)
}

func isValidBase64(data string) bool {
	return apicompat.IsValidGeminiInlineBase64(data)
}

func extractGeminiUsage(data []byte) *ClaudeUsage {
	usage := geminiprovider.ExtractUsage(data)
	if usage == nil {
		return nil
	}
	return &ClaudeUsage{
		InputTokens:          usage.InputTokens,
		OutputTokens:         usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens,
		ImageOutputTokens:    usage.ImageOutputTokens,
	}
}

func (s *GeminiMessagesCompatService) handleGeminiUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, body []byte) {
	// 遵守自定义错误码策略：未命中则跳过所有限流处理
	if !account.ShouldHandleErrorCode(statusCode) {
		return
	}
	if s.rateLimitService != nil && (statusCode == 401 || statusCode == 403 || statusCode == 529) {
		s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, body)
		return
	}
	if statusCode != 429 {
		return
	}
	// 池模式账号不写账号级限流：账号留在池内，由 failover / 同号重试消化 429。
	// 自定义错误码优先级高于池模式，开启后仍按其命中结果标记。
	if account.IsPoolMode() && !account.IsCustomErrorCodesEnabled() {
		return
	}

	oauthType := account.GeminiOAuthType()
	tierID := account.GeminiTierID()
	projectID := strings.TrimSpace(account.GetCredential("project_id"))
	isCodeAssist := account.IsGeminiCodeAssist()

	resetAt := ParseGeminiRateLimitResetTime(body)
	if resetAt == nil {
		// 根据账号类型使用不同的默认重置时间
		var ra time.Time
		if isCodeAssist || oauthType == "google_one" {
			// Gemini CLI / Google One: fallback cooldown by tier
			cooldown := geminiCooldownForTier(tierID)
			if s.rateLimitService != nil {
				cooldown = s.rateLimitService.GeminiCooldown(ctx, account)
			}
			ra = time.Now().Add(cooldown)
			if isCodeAssist {
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (Code Assist, tier=%s, project=%s) rate limited, cooldown=%v", account.ID, tierID, projectID, time.Until(ra).Truncate(time.Second))
			} else {
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (Google One OAuth, tier=%s, project=%s) rate limited, cooldown=%v", account.ID, tierID, projectID, time.Until(ra).Truncate(time.Second))
			}
		} else if account.IsVertexServiceAccount() {
			// Vertex 按量付费：短窗限速，无每日配额，短冷却后即可恢复
			ra = time.Now().Add(geminiVertexFallbackCooldown)
			logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (Vertex service account, project=%s) rate limited, cooldown=%v", account.ID, projectID, time.Until(ra).Truncate(time.Second))
		} else {
			// API Key / AI Studio OAuth: PST 午夜
			if ts := nextGeminiDailyResetUnix(); ts != nil {
				ra = time.Unix(*ts, 0)
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (API Key/AI Studio, type=%s) rate limited, reset at PST midnight (%v)", account.ID, account.Type, ra)
			} else {
				// 兜底：5 分钟
				ra = time.Now().Add(5 * time.Minute)
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d rate limited, fallback to 5min", account.ID)
			}
		}
		_ = s.accountRepo.SetRateLimited(ctx, account.ID, ra)
		return
	}

	// 使用解析到的重置时间
	resetTime := time.Unix(*resetAt, 0)
	_ = s.accountRepo.SetRateLimited(ctx, account.ID, resetTime)
	logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d rate limited until %v (oauth_type=%s, tier=%s)",
		account.ID, resetTime, oauthType, tierID)
}

// ParseGeminiRateLimitResetTime 解析 Gemini 格式的 429 响应，返回重置时间的 Unix 时间戳
func ParseGeminiRateLimitResetTime(body []byte) *int64 {
	// 第一阶段：gjson 结构化提取
	errMsg := gjson.GetBytes(body, "error.message").String()
	if looksLikeGeminiDailyQuota(errMsg) {
		if ts := nextGeminiDailyResetUnix(); ts != nil {
			return ts
		}
	}

	// 遍历 error.details 查找 quotaResetDelay（AI Studio）或 RetryInfo.retryDelay（Vertex）
	var found *int64
	gjson.GetBytes(body, "error.details").ForEach(func(_, detail gjson.Result) bool {
		if quotaResetDelay := detail.Get("metadata.quotaResetDelay").String(); quotaResetDelay != "" {
			if dur, err := time.ParseDuration(quotaResetDelay); err == nil {
				// Use ceil to avoid undercounting fractional seconds (e.g. 10.1s should not become 10s),
				// which can affect scheduling decisions around thresholds (like 10s).
				ts := time.Now().Unix() + int64(math.Ceil(dur.Seconds()))
				found = &ts
				return false
			}
			return true
		}
		// Vertex 标准错误格式：google.rpc.RetryInfo.retryDelay（"39s" / "1.5s"）
		if detail.Get("@type").String() == geminiRetryInfoTypeURL {
			if dur, err := time.ParseDuration(detail.Get("retryDelay").String()); err == nil && dur > 0 {
				if dur > geminiRetryInfoMaxDelay {
					dur = geminiRetryInfoMaxDelay
				}
				ts := time.Now().Unix() + int64(math.Ceil(dur.Seconds()))
				found = &ts
				return false
			}
		}
		return true
	})
	if found != nil {
		return found
	}

	// 第二阶段：regex 回退匹配 "Please retry in Xs"
	matches := retryInRegex.FindStringSubmatch(string(body))
	if len(matches) == 2 {
		if dur, err := time.ParseDuration(matches[1] + "s"); err == nil {
			ts := time.Now().Unix() + int64(math.Ceil(dur.Seconds()))
			return &ts
		}
	}

	return nil
}

func looksLikeGeminiDailyQuota(message string) bool {
	m := strings.ToLower(message)
	if strings.Contains(m, "per day") || strings.Contains(m, "requests per day") || strings.Contains(m, "quota") && strings.Contains(m, "per day") {
		return true
	}
	return false
}

func nextGeminiDailyResetUnix() *int64 {
	reset := geminiDailyResetTime(time.Now())
	ts := reset.Unix()
	return &ts
}

func ensureGeminiFunctionCallThoughtSignatures(body []byte) []byte {
	// Fast path: only run when functionCall is present.
	if !bytes.Contains(body, []byte(`"functionCall"`)) {
		return body
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	contentsAny, ok := payload["contents"].([]any)
	if !ok || len(contentsAny) == 0 {
		return body
	}

	modified := false
	for _, c := range contentsAny {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		partsAny, ok := cm["parts"].([]any)
		if !ok || len(partsAny) == 0 {
			continue
		}
		for _, p := range partsAny {
			pm, ok := p.(map[string]any)
			if !ok || pm == nil {
				continue
			}
			if fc, ok := pm["functionCall"].(map[string]any); !ok || fc == nil {
				continue
			}
			ts, _ := pm["thoughtSignature"].(string)
			if strings.TrimSpace(ts) == "" {
				pm["thoughtSignature"] = geminiDummyThoughtSignature
				modified = true
			}
		}
	}

	if !modified {
		return body
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return b
}

func extractGeminiFinishReason(geminiResp map[string]any) string {
	return apicompat.GeminiFinishReason(geminiResp)
}

func extractGeminiParts(geminiResp map[string]any) []map[string]any {
	return apicompat.GeminiParts(geminiResp)
}

func computeGeminiTextDelta(seen, incoming string) (delta, newSeen string) {
	return apicompat.GeminiTextDelta(seen, incoming)
}

func mapGeminiFinishReasonToClaudeStopReason(finishReason string) string {
	return apicompat.GeminiFinishReasonToMessagesStopReason(finishReason)
}

func convertClaudeMessagesToGeminiGenerateContent(body []byte, profiles ...geminiToolIDProfile) ([]byte, error) {
	profile := geminiToolIDNative
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	return apicompat.MessagesToGemini(body, func(payload map[string]any) error {
		return prepareGeminiToolIDs(payload, profile)
	})
}

func normalizeGeminiRequestForAIStudio(body []byte) []byte {
	return apicompat.NormalizeGeminiRequestForAIStudio(body)
}

func convertClaudeToolsToGeminiTools(tools any) []any {
	return apicompat.ConvertClaudeToolsToGeminiTools(tools)
}

func cleanToolSchema(schema any) any {
	return apicompat.CleanToolSchema(schema)
}

func convertClaudeGenerationConfig(req map[string]any) (map[string]any, error) {
	return apicompat.ConvertClaudeGenerationConfig(req)
}

func (s *GeminiMessagesCompatService) extractImageInputSize(body []byte) string {
	var req struct {
		GenerationConfig *struct {
			ImageConfig *struct {
				ImageSize string `json:"imageSize"`
			} `json:"imageConfig"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}

	if req.GenerationConfig != nil && req.GenerationConfig.ImageConfig != nil {
		return strings.TrimSpace(req.GenerationConfig.ImageConfig.ImageSize)
	}

	return ""
}
