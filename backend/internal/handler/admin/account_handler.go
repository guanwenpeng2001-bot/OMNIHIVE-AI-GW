// Package admin provides HTTP handlers for administrative operations.
package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"golang.org/x/sync/errgroup"
)

// Register once, but resolve each identity against the current domain registry.
func init() {
	bindingValidator, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		panic("admin validation requires validator.Validate")
	}
	if err := bindingValidator.RegisterValidation("concrete_platform", func(field validator.FieldLevel) bool {
		return domain.IsConcretePlatform(field.Field().String())
	}); err != nil {
		panic(err)
	}
	if err := bindingValidator.RegisterValidation("group_platform", func(field validator.FieldLevel) bool {
		return domain.IsGroupPlatform(field.Field().String())
	}); err != nil {
		panic(err)
	}
	bindingValidator.RegisterStructValidation(func(validation validator.StructLevel) {
		req, ok := validation.Current().Interface().(CreateAccountRequest)
		if !ok {
			panic("unexpected admin validation request type")
		}
		if err := validateCompatibleAccountIdentity(req.Platform, req.Type, req.Credentials); err != nil {
			validation.ReportError(req.Platform, "Platform", "platform", "compatible_identity", "")
		}
	}, CreateAccountRequest{})
}

// Existing platforms retain their existing credential contracts. This check
// never normalizes identities or mutates credentials, URLs, or model mappings.
func validateCompatibleAccountIdentity(platform, accountType string, credentials map[string]any) error {
	provider, ok := domain.LookupCompatibleProvider(platform)
	if !ok {
		return nil
	}
	if accountType != domain.AccountTypeAPIKey {
		return fmt.Errorf("compatible provider requires apikey account type")
	}
	read := func(key string) (string, error) {
		value, present := credentials[key]
		if !present {
			return "", nil
		}
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", key)
		}
		return text, nil
	}
	if err := provider.ValidateBillingPlan(credentials); err != nil {
		return err
	}
	modeName, err := read("account_mode")
	if err != nil {
		return err
	}
	mode, ok := provider.ResolveMode(modeName)
	if !ok {
		return fmt.Errorf("unsupported compatible provider account_mode")
	}
	protocol, err := read("api_protocol")
	if err != nil {
		return err
	}
	if protocol != "" && protocol != domain.APIProtocolAdaptive {
		if _, ok := mode.LookupEndpoint(protocol); !ok {
			return fmt.Errorf("unsupported compatible provider api_protocol for account_mode")
		}
	}
	if provider.BaseSource == "account" {
		base, err := read("base_url")
		if err != nil {
			return err
		}
		if strings.TrimSpace(base) == "" {
			return fmt.Errorf("compatible provider requires account resource base_url")
		}
	}
	return nil
}

func prepareCompatibleAccountUpdate(existing *service.Account, req *UpdateAccountRequest) error {
	if _, ok := domain.LookupCompatibleProvider(existing.Platform); !ok {
		return nil
	}
	accountType := existing.Type
	if req.Type != "" {
		accountType = req.Type
	}
	credentials := existing.Credentials
	if len(req.Credentials) > 0 {
		credentials = make(map[string]any, len(existing.Credentials)+len(req.Credentials))
		for key, value := range existing.Credentials {
			credentials[key] = value
		}
		for key, value := range req.Credentials {
			credentials[key] = value
		}
		// Per-protocol URL edits must not erase another protocol's custom host.
		if incoming, ok := req.Credentials["api_base_urls"].(map[string]any); ok && len(incoming) > 0 {
			bases := make(map[string]any)
			switch stored := existing.Credentials["api_base_urls"].(type) {
			case map[string]any:
				for protocol, base := range stored {
					bases[protocol] = base
				}
			case map[string]string:
				for protocol, base := range stored {
					bases[protocol] = base
				}
			}
			for protocol, base := range incoming {
				bases[protocol] = base
			}
			credentials["api_base_urls"] = bases
		}
	}
	if err := validateCompatibleAccountIdentity(existing.Platform, accountType, credentials); err != nil {
		return err
	}
	// The legacy service replaces non-secret keys on PUT. Forward the validated
	// resulting map for compatible providers; leave absent credential edits absent.
	if len(req.Credentials) > 0 {
		domain.ClearEmptyBillingPlan(credentials)
		req.Credentials = credentials
	}
	return nil
}

// OAuthHandler handles OAuth-related operations for accounts
type OAuthHandler struct {
	credentialDraftHandler
	oauthService *service.OAuthService
}

// NewOAuthHandler creates a new OAuth handler
func NewOAuthHandler(oauthService *service.OAuthService) *OAuthHandler {
	return &OAuthHandler{
		oauthService: oauthService,
	}
}

// AccountHandler handles admin account management
type AccountHandler struct {
	credentialDraftHandler
	cursorAuthService       *service.CursorAuthService
	cursorSessions          *service.CursorSessionProvider
	adminService            service.AdminService
	oauthService            *service.OAuthService
	openaiOAuthService      *service.OpenAIOAuthService
	geminiOAuthService      *service.GeminiOAuthService
	antigravityOAuthService *service.AntigravityOAuthService
	grokOAuthService        service.GrokOAuthTokenService
	rateLimitService        *service.RateLimitService
	accountUsageService     *service.AccountUsageService
	accountTestService      *service.AccountTestService
	concurrencyService      *service.ConcurrencyService
	crsSyncService          *service.CRSSyncService
	sessionLimitCache       service.SessionLimitCache
	rpmCache                service.RPMCache
	tokenCacheInvalidator   service.TokenCacheInvalidator
	grokImportProber        grokImportProber
	upstreamBillingProbe    *service.UpstreamBillingProbeService
	ollamaCloudUsage        *service.OllamaCloudUsageService
	cfg                     *config.Config
}

// SetUpstreamBillingProbeService attaches the optional remote billing probe service.
func (h *AccountHandler) SetUpstreamBillingProbeService(probe *service.UpstreamBillingProbeService) {
	h.upstreamBillingProbe = probe
}

func (h *AccountHandler) SetOllamaCloudUsageService(usage *service.OllamaCloudUsageService) {
	h.ollamaCloudUsage = usage
}

// NewAccountHandler creates a new admin account handler
func NewAccountHandler(
	adminService service.AdminService,
	oauthService *service.OAuthService,
	openaiOAuthService *service.OpenAIOAuthService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
	grokOAuthService service.GrokOAuthTokenService,
	rateLimitService *service.RateLimitService,
	accountUsageService *service.AccountUsageService,
	accountTestService *service.AccountTestService,
	concurrencyService *service.ConcurrencyService,
	crsSyncService *service.CRSSyncService,
	sessionLimitCache service.SessionLimitCache,
	rpmCache service.RPMCache,
	tokenCacheInvalidator service.TokenCacheInvalidator,
) *AccountHandler {
	return &AccountHandler{
		cursorAuthService:       service.NewCursorAuthService(),
		adminService:            adminService,
		oauthService:            oauthService,
		openaiOAuthService:      openaiOAuthService,
		geminiOAuthService:      geminiOAuthService,
		antigravityOAuthService: antigravityOAuthService,
		grokOAuthService:        grokOAuthService,
		rateLimitService:        rateLimitService,
		accountUsageService:     accountUsageService,
		accountTestService:      accountTestService,
		concurrencyService:      concurrencyService,
		crsSyncService:          crsSyncService,
		sessionLimitCache:       sessionLimitCache,
		rpmCache:                rpmCache,
		tokenCacheInvalidator:   tokenCacheInvalidator,
	}
}

// CreateAccountRequest represents create account request
type CreateAccountRequest struct {
	TPMLimit                service.OptionalTPMLimit `json:"tpm_limit,omitzero"`
	Status                  string                   `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
	ExistingAccountID       int64                    `json:"existing_account_id,omitempty"`
	CredentialDraft         string                   `json:"credential_draft,omitempty"`
	CredentialDraftRevision string                   `json:"credential_draft_revision,omitempty"`
	Name                    string                   `json:"name" binding:"required"`
	Notes                   *string                  `json:"notes"`
	Platform                string                   `json:"platform" binding:"required,concrete_platform"`
	Type                    string                   `json:"type" binding:"required,oneof=oauth setup-token apikey upstream bedrock service_account"`
	Credentials             map[string]any           `json:"credentials" binding:"required"`
	Extra                   map[string]any           `json:"extra"`
	ProxyID                 *int64                   `json:"proxy_id"`
	ProxyGroupID            *int64                   `json:"proxy_group_id"`
	Concurrency             int                      `json:"concurrency"`
	Priority                int                      `json:"priority"`
	RateMultiplier          *float64                 `json:"rate_multiplier"`
	LoadFactor              *int                     `json:"load_factor"`
	GroupIDs                []int64                  `json:"group_ids"`
	ExpiresAt               *int64                   `json:"expires_at"`
	AutoPauseOnExpired      *bool                    `json:"auto_pause_on_expired"`
	ProbeEnabled            *bool                    `json:"upstream_billing_probe_enabled"`
	ConfirmMixedChannelRisk *bool                    `json:"confirm_mixed_channel_risk"` // 用户确认混合渠道风险
}

// UpdateAccountRequest represents update account request
// 使用指针类型来区分"未提供"和"设置为0"
type UpdateAccountRequest struct {
	TPMLimit                service.OptionalTPMLimit `json:"tpm_limit,omitzero"`
	Name                    string                   `json:"name"`
	Notes                   *string                  `json:"notes"`
	Type                    string                   `json:"type" binding:"omitempty,oneof=oauth setup-token apikey upstream bedrock service_account"`
	Credentials             map[string]any           `json:"credentials"`
	Extra                   map[string]any           `json:"extra"`
	ProxyID                 *int64                   `json:"proxy_id"`
	ProxyGroupID            *int64                   `json:"proxy_group_id"`
	Concurrency             *int                     `json:"concurrency"`
	Priority                *int                     `json:"priority"`
	RateMultiplier          *float64                 `json:"rate_multiplier"`
	LoadFactor              *int                     `json:"load_factor"`
	Status                  string                   `json:"status" binding:"omitempty,oneof=active inactive error"`
	GroupIDs                *[]int64                 `json:"group_ids"`
	ExpiresAt               *int64                   `json:"expires_at"`
	AutoPauseOnExpired      *bool                    `json:"auto_pause_on_expired"`
	ProbeEnabled            *bool                    `json:"upstream_billing_probe_enabled"`
	RateSyncEnabled         *bool                    `json:"upstream_billing_rate_sync_enabled"`
	ConfirmMixedChannelRisk *bool                    `json:"confirm_mixed_channel_risk"` // 用户确认混合渠道风险
}

// BulkUpdateAccountsRequest represents the payload for bulk editing accounts
type BulkUpdateAccountsRequest struct {
	TPMLimit                service.OptionalTPMLimit  `json:"tpm_limit,omitzero"`
	AccountIDs              []int64                   `json:"account_ids"`
	Filters                 *BulkUpdateAccountFilters `json:"filters"`
	Name                    string                    `json:"name"`
	ProxyID                 *int64                    `json:"proxy_id"`
	ProxyGroupID            *int64                    `json:"proxy_group_id"`
	Concurrency             *int                      `json:"concurrency"`
	Priority                *int                      `json:"priority"`
	RateMultiplier          *float64                  `json:"rate_multiplier"`
	LoadFactor              *int                      `json:"load_factor"`
	Status                  string                    `json:"status" binding:"omitempty,oneof=active inactive error"`
	Schedulable             *bool                     `json:"schedulable"`
	GroupIDs                *[]int64                  `json:"group_ids"`
	Credentials             map[string]any            `json:"credentials"`
	Extra                   map[string]any            `json:"extra"`
	ProbeEnabled            *bool                     `json:"upstream_billing_probe_enabled"`
	ConfirmMixedChannelRisk *bool                     `json:"confirm_mixed_channel_risk"` // 用户确认混合渠道风险
}

type BulkUpdateAccountFilters struct {
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Group       string `json:"group"`
	Search      string `json:"search"`
	PrivacyMode string `json:"privacy_mode"`
}

// CheckMixedChannelRequest represents check mixed channel risk request
type CheckMixedChannelRequest struct {
	Platform  string  `json:"platform" binding:"required"`
	GroupIDs  []int64 `json:"group_ids"`
	AccountID *int64  `json:"account_id"`
}

// AccountWithConcurrency extends Account with real-time concurrency info
type AccountWithConcurrency struct {
	*dto.Account
	simpleMode         bool                         `json:"-"`
	CurrentConcurrency int                          `json:"current_concurrency"`
	SchedulerScore     *AccountSchedulerScore       `json:"scheduler_score,omitempty"`
	SchedulerScores    []AccountSchedulerGroupScore `json:"scheduler_scores,omitempty"`
	// 以下字段仅对 Anthropic OAuth/SetupToken 账号有效，且仅在启用相应功能时返回
	CurrentWindowCost *float64 `json:"current_window_cost,omitempty"` // 当前窗口费用
	ActiveSessions    *int     `json:"active_sessions,omitempty"`     // 当前活跃会话数
	CurrentRPM        *int     `json:"current_rpm,omitempty"`         // 当前分钟 RPM 计数
}

// AccountListItemWithConcurrency is the compact account-list envelope used
// for lite=1. It embeds dto.AccountListItem instead of the full dto.Account,
// so groups/account_groups never appear in the list payload.
type AccountListItemWithConcurrency struct {
	*dto.AccountListItem
	CurrentConcurrency int                          `json:"current_concurrency"`
	SchedulerScore     *AccountSchedulerScore       `json:"scheduler_score,omitempty"`
	SchedulerScores    []AccountSchedulerGroupScore `json:"scheduler_scores,omitempty"`
	CurrentWindowCost  *float64                     `json:"current_window_cost,omitempty"`
	ActiveSessions     *int                         `json:"active_sessions,omitempty"`
	CurrentRPM         *int                         `json:"current_rpm,omitempty"`
}

type simpleModeGroupReference struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
}

type simpleModeAccountGroupReference struct {
	AccountID int64                     `json:"account_id"`
	GroupID   int64                     `json:"group_id"`
	Priority  int                       `json:"priority"`
	CreatedAt time.Time                 `json:"created_at"`
	Group     *simpleModeGroupReference `json:"group,omitempty"`
}

func simpleModeGroupReferenceFromDTO(group *dto.Group) *simpleModeGroupReference {
	if group == nil {
		return nil
	}
	return &simpleModeGroupReference{ID: group.ID, Name: group.Name, Platform: group.Platform, Status: group.Status}
}

func simpleModeCompositeGroupIDs(account *dto.Account) map[int64]struct{} {
	hidden := make(map[int64]struct{})
	if account == nil {
		return hidden
	}
	for _, group := range account.Groups {
		if group != nil && group.Platform == service.PlatformComposite {
			hidden[group.ID] = struct{}{}
		}
	}
	for _, accountGroup := range account.AccountGroups {
		if accountGroup.Group != nil && accountGroup.Group.Platform == service.PlatformComposite {
			hidden[accountGroup.GroupID] = struct{}{}
		}
	}
	return hidden
}

func filterSimpleModeGroupIDs(groupIDs []int64, hidden map[int64]struct{}) []int64 {
	visible := make([]int64, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		if _, ok := hidden[groupID]; !ok {
			visible = append(visible, groupID)
		}
	}
	return visible
}

func simpleModeCompositeServiceGroupIDs(account *service.Account) map[int64]struct{} {
	hidden := make(map[int64]struct{})
	if account == nil {
		return hidden
	}
	for _, group := range account.Groups {
		if group != nil && group.Platform == service.PlatformComposite {
			hidden[group.ID] = struct{}{}
		}
	}
	for _, accountGroup := range account.AccountGroups {
		if accountGroup.Group != nil && accountGroup.Group.Platform == service.PlatformComposite {
			hidden[accountGroup.GroupID] = struct{}{}
		}
	}
	return hidden
}

func (a AccountWithConcurrency) MarshalJSON() ([]byte, error) {
	type alias AccountWithConcurrency
	if !a.simpleMode || a.Account == nil {
		return json.Marshal(alias(a))
	}
	groups := make([]simpleModeGroupReference, 0, len(a.Groups))
	compositeIDs := simpleModeCompositeGroupIDs(a.Account)
	for _, group := range a.Groups {
		if group != nil && group.Platform == service.PlatformComposite {
			continue
		}
		if ref := simpleModeGroupReferenceFromDTO(group); ref != nil {
			groups = append(groups, *ref)
		}
	}
	accountGroups := make([]simpleModeAccountGroupReference, 0, len(a.AccountGroups))
	for _, accountGroup := range a.AccountGroups {
		if accountGroup.Group != nil && accountGroup.Group.Platform == service.PlatformComposite {
			continue
		}
		if _, hidden := compositeIDs[accountGroup.GroupID]; hidden {
			continue
		}
		accountGroups = append(accountGroups, simpleModeAccountGroupReference{
			AccountID: accountGroup.AccountID, GroupID: accountGroup.GroupID, Priority: accountGroup.Priority,
			CreatedAt: accountGroup.CreatedAt, Group: simpleModeGroupReferenceFromDTO(accountGroup.Group),
		})
	}
	return json.Marshal(struct {
		alias
		GroupIDs      []int64                           `json:"group_ids,omitempty"`
		Groups        []simpleModeGroupReference        `json:"groups"`
		AccountGroups []simpleModeAccountGroupReference `json:"account_groups"`
	}{alias: alias(a), GroupIDs: filterSimpleModeGroupIDs(a.GroupIDs, compositeIDs), Groups: groups, AccountGroups: accountGroups})
}

type AccountSchedulerScore struct {
	BaseScore             float64 `json:"base_score"`
	StickyScore           float64 `json:"sticky_score"`
	StickyScoreInfinity   bool    `json:"sticky_score_infinity"`
	StickyWeightedEnabled bool    `json:"sticky_weighted_enabled"`
}

type AccountSchedulerGroupScore struct {
	GroupID       *int64 `json:"group_id"`
	GroupName     string `json:"group_name,omitempty"`
	GroupPriority *int   `json:"group_priority,omitempty"`
	AccountSchedulerScore
}

const accountListGroupUngroupedQueryValue = "ungrouped"

func (h *AccountHandler) accountResponseFromService(account *service.Account) *dto.Account {
	out := dto.AccountFromService(account)
	if h != nil && h.ollamaCloudUsage != nil && out != nil {
		h.ollamaCloudUsage.EnrichState(out.OllamaCloudUsage)
	}
	return out
}

func (h *AccountHandler) accountListResponseFromService(account *service.Account) *dto.Account {
	out := dto.AccountFromServiceShallow(account)
	if out != nil && account != nil {
		out.Proxy = dto.ProxyFromService(account.Proxy)
	}
	if h != nil && h.ollamaCloudUsage != nil && out != nil {
		h.ollamaCloudUsage.EnrichState(out.OllamaCloudUsage)
	}
	return out
}

func (h *AccountHandler) isSimpleMode() bool {
	return h != nil && h.cfg != nil && h.cfg.RunMode == config.RunModeSimple
}

func (h *AccountHandler) buildAccountResponseWithRuntime(ctx context.Context, account *service.Account) AccountWithConcurrency {
	item := AccountWithConcurrency{
		Account:            h.accountResponseFromService(account),
		simpleMode:         h.isSimpleMode(),
		CurrentConcurrency: 0,
	}
	if account == nil {
		return item
	}

	if h.concurrencyService != nil {
		if counts, err := h.concurrencyService.GetAccountConcurrencyBatch(ctx, []int64{account.ID}); err == nil {
			item.CurrentConcurrency = counts[account.ID]
		}
	}

	if account.IsAnthropicOAuthOrSetupToken() {
		if h.accountUsageService != nil && account.GetWindowCostLimit() > 0 {
			startTime := account.GetCurrentWindowStartTime()
			if stats, err := h.accountUsageService.GetAccountWindowStats(ctx, account.ID, startTime); err == nil && stats != nil {
				cost := stats.StandardCost
				item.CurrentWindowCost = &cost
			}
		}

		if h.sessionLimitCache != nil && account.GetMaxSessions() > 0 {
			idleTimeout := time.Duration(account.GetSessionIdleTimeoutMinutes()) * time.Minute
			idleTimeouts := map[int64]time.Duration{account.ID: idleTimeout}
			if sessions, err := h.sessionLimitCache.GetActiveSessionCountBatch(ctx, []int64{account.ID}, idleTimeouts); err == nil {
				if count, ok := sessions[account.ID]; ok {
					item.ActiveSessions = &count
				}
			}
		}

		if h.rpmCache != nil && account.GetBaseRPM() > 0 {
			if rpm, err := h.rpmCache.GetRPM(ctx, account.ID); err == nil {
				item.CurrentRPM = &rpm
			}
		}
	}

	h.enrichShadowParents(ctx, []AccountWithConcurrency{item})

	return item
}

// scoreOpenAIAccountSchedulerPool 对池内 OpenAI 账号计算调度分数快照。
// loadMap 为共享的账号负载数据（含池内全部账号即可，多余条目无害）；传 nil 时自行批查。
func (h *AccountHandler) scoreOpenAIAccountSchedulerPool(ctx context.Context, accounts []service.Account, loadMap map[int64]*service.AccountLoadInfo) map[int64]AccountSchedulerScore {
	if len(accounts) == 0 {
		return nil
	}

	openAIAccounts := make([]*service.Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != service.PlatformOpenAI {
			continue
		}
		openAIAccounts = append(openAIAccounts, account)
	}
	if len(openAIAccounts) == 0 {
		return nil
	}

	if loadMap == nil {
		loadMap = h.fetchOpenAIAccountLoadMap(ctx, openAIAccounts)
	}

	var scores map[int64]service.OpenAIAccountSchedulerScoreSnapshot
	if h.rateLimitService != nil {
		scores = h.rateLimitService.BuildOpenAIAccountSchedulerScoreSnapshot(ctx, openAIAccounts, loadMap)
	} else {
		scores = service.BuildOpenAIAccountSchedulerScoreSnapshot(openAIAccounts, loadMap)
	}
	result := make(map[int64]AccountSchedulerScore, len(scores))
	for accountID, score := range scores {
		result[accountID] = AccountSchedulerScore{
			BaseScore:             score.BaseScore,
			StickyScore:           score.StickyScore,
			StickyScoreInfinity:   score.StickyScoreInfinity,
			StickyWeightedEnabled: score.StickyWeightedEnabled,
		}
	}
	return result
}

// fetchOpenAIAccountLoadMap 一次性批查给定 OpenAI 账号的负载数据；
// 失败时记录日志并返回空表（分数按零负载计算，属可接受降级）。
func (h *AccountHandler) fetchOpenAIAccountLoadMap(ctx context.Context, openAIAccounts []*service.Account) map[int64]*service.AccountLoadInfo {
	loadMap := map[int64]*service.AccountLoadInfo{}
	if h.concurrencyService == nil || len(openAIAccounts) == 0 {
		return loadMap
	}
	seen := make(map[int64]struct{}, len(openAIAccounts))
	loadReq := make([]service.AccountWithConcurrency, 0, len(openAIAccounts))
	for _, account := range openAIAccounts {
		if account == nil {
			continue
		}
		if _, ok := seen[account.ID]; ok {
			continue
		}
		seen[account.ID] = struct{}{}
		loadReq = append(loadReq, service.AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: account.EffectiveLoadFactor(),
		})
	}
	if batchLoad, err := h.concurrencyService.GetAccountsLoadBatch(ctx, loadReq); err != nil {
		slog.Warn("openai_scheduler_score_load_batch_failed", "error", err)
	} else if batchLoad != nil {
		loadMap = batchLoad
	}
	return loadMap
}

func (h *AccountHandler) buildOpenAIAccountSchedulerScores(
	ctx context.Context,
	accounts []service.Account,
	filterPool []service.Account,
) (map[int64]*AccountSchedulerScore, map[int64][]AccountSchedulerGroupScore) {
	if len(accounts) == 0 {
		return nil, nil
	}
	if len(filterPool) == 0 {
		filterPool = accounts
	}

	pageOpenAIAccountIDs := make(map[int64]struct{})
	groupIDs := make(map[int64]struct{})
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != service.PlatformOpenAI {
			continue
		}
		pageOpenAIAccountIDs[account.ID] = struct{}{}
		if len(account.AccountGroups) == 0 && len(account.GroupIDs) == 0 {
			continue
		}
		for _, accountGroup := range account.AccountGroups {
			if accountGroup.GroupID > 0 {
				groupIDs[accountGroup.GroupID] = struct{}{}
			}
		}
		for _, groupID := range account.GroupIDs {
			if groupID > 0 {
				groupIDs[groupID] = struct{}{}
			}
		}
	}
	if len(pageOpenAIAccountIDs) == 0 {
		return nil, nil
	}

	// 先取各分组池，再对"过滤池 ∪ 分组池"的账号并集做一次负载批查，
	// 避免每个池各查一次 Redis 的 N+1。
	groupIDList := make([]int64, 0, len(groupIDs))
	for groupID := range groupIDs {
		groupIDList = append(groupIDList, groupID)
	}
	sort.Slice(groupIDList, func(i, j int) bool { return groupIDList[i] < groupIDList[j] })

	groupPools := make(map[int64][]service.Account, len(groupIDList))
	if h.adminService != nil {
		for _, groupID := range groupIDList {
			gid := groupID
			pool, err := h.adminService.ListOpenAISchedulableAccountsForSchedulerScore(ctx, &gid)
			if err != nil {
				slog.Warn("openai_scheduler_group_score_pool_failed", "group_id", gid, "error", err)
				continue
			}
			groupPools[gid] = pool
		}
	}

	loadUnion := make([]*service.Account, 0, len(filterPool))
	collectOpenAIAccounts := func(pool []service.Account) {
		for i := range pool {
			if pool[i].Platform == service.PlatformOpenAI {
				loadUnion = append(loadUnion, &pool[i])
			}
		}
	}
	collectOpenAIAccounts(filterPool)
	for _, pool := range groupPools {
		collectOpenAIAccounts(pool)
	}
	loadMap := h.fetchOpenAIAccountLoadMap(ctx, loadUnion)

	baseScores := make(map[int64]*AccountSchedulerScore)
	for accountID, score := range h.scoreOpenAIAccountSchedulerPool(ctx, filterPool, loadMap) {
		copiedScore := score
		baseScores[accountID] = &copiedScore
	}

	groupScoresByAccount := make(map[int64][]AccountSchedulerGroupScore)
	scoreGroupPool := func(groupID *int64, groupNameByID map[int64]string, groupPriorityByAccount map[int64]int, pool []service.Account) {
		if len(pool) == 0 {
			return
		}
		scores := h.scoreOpenAIAccountSchedulerPool(ctx, pool, loadMap)
		for accountID, schedulerScore := range scores {
			if _, ok := pageOpenAIAccountIDs[accountID]; !ok {
				continue
			}
			groupScore := AccountSchedulerGroupScore{
				GroupID:               groupID,
				AccountSchedulerScore: schedulerScore,
			}
			if groupID != nil {
				groupScore.GroupName = groupNameByID[*groupID]
				if priority, ok := groupPriorityByAccount[accountID]; ok {
					groupScore.GroupPriority = &priority
				}
			}
			groupScoresByAccount[accountID] = append(groupScoresByAccount[accountID], groupScore)
		}
	}

	for _, groupID := range groupIDList {
		gid := groupID
		pool, ok := groupPools[gid]
		if !ok {
			continue
		}
		groupNameByID := make(map[int64]string)
		groupPriorityByAccount := make(map[int64]int)
		for i := range pool {
			account := &pool[i]
			for _, accountGroup := range account.AccountGroups {
				if accountGroup.GroupID != gid {
					continue
				}
				groupPriorityByAccount[account.ID] = accountGroup.Priority
				if accountGroup.Group != nil {
					groupNameByID[gid] = accountGroup.Group.Name
				}
			}
		}
		scoreGroupPool(&gid, groupNameByID, groupPriorityByAccount, pool)
	}

	for accountID := range groupScoresByAccount {
		sort.SliceStable(groupScoresByAccount[accountID], func(i, j int) bool {
			left := groupScoresByAccount[accountID][i]
			right := groupScoresByAccount[accountID][j]
			return *left.GroupID < *right.GroupID
		})
	}
	return baseScores, groupScoresByAccount
}

func (h *AccountHandler) listAccountSchedulerScoreFilterPool(
	ctx context.Context,
	platform, accountType, status, search string,
	groupID int64,
	privacyMode string,
) []service.Account {
	if h.adminService == nil || (platform != "" && platform != service.PlatformOpenAI) {
		return nil
	}
	// 池只用于 OpenAI 分数计算（非 OpenAI 账号会在打分时被丢弃），
	// 无论列表页平台过滤为何，查询一律限定 openai，避免无过滤时全表扫描。
	accounts, err := h.adminService.ListAccountsForSchedulerScoreFilter(ctx, service.PlatformOpenAI, accountType, status, search, groupID, privacyMode)
	if err != nil {
		slog.Warn("openai_scheduler_filter_score_pool_failed", "error", err)
		return nil
	}
	return accounts
}

// List handles listing all accounts with pagination
// GET /api/v1/admin/accounts
func (h *AccountHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	platform := c.Query("platform")
	accountType := c.Query("type")
	status := c.Query("status")
	search := c.Query("search")
	privacyMode := strings.TrimSpace(c.Query("privacy_mode"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	// 标准化和验证 search 参数
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}
	lite := parseBoolQueryWithDefault(c.Query("lite"), false)
	// 调度分需要跨候选池批量打分并读取负载，默认列表不计算；只有前端列可见时才显式开启。
	includeSchedulerScore := parseBoolQueryWithDefault(c.Query("include_scheduler_score"), false)

	var groupID int64
	if groupIDStr := c.Query("group"); groupIDStr != "" {
		if groupIDStr == accountListGroupUngroupedQueryValue {
			groupID = service.AccountListGroupUngrouped
		} else {
			parsedGroupID, parseErr := strconv.ParseInt(groupIDStr, 10, 64)
			if parseErr != nil {
				response.ErrorFrom(c, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter"))
				return
			}
			if parsedGroupID < 0 {
				response.ErrorFrom(c, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter"))
				return
			}
			groupID = parsedGroupID
		}
	}

	accounts, total, err := h.adminService.ListAccounts(c.Request.Context(), page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.ollamaCloudUsage != nil && len(accounts) > 0 {
		accountPointers := make([]*service.Account, len(accounts))
		for index := range accounts {
			accountPointers[index] = &accounts[index]
		}
		if err := h.ollamaCloudUsage.ResolveAccounts(c.Request.Context(), accountPointers); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	// Get current concurrency counts for all accounts
	accountIDs := make([]int64, len(accounts))
	for i, acc := range accounts {
		accountIDs[i] = acc.ID
	}

	concurrencyCounts := make(map[int64]int)
	var windowCosts map[int64]float64
	var activeSessions map[int64]int
	var rpmCounts map[int64]int
	// 双重门控：用户要看该列，且当前页确实有 OpenAI 账号，才进入昂贵的候选池打分路径。
	var schedulerScores map[int64]*AccountSchedulerScore
	var schedulerGroupScores map[int64][]AccountSchedulerGroupScore
	pageHasOpenAIAccounts := false
	for i := range accounts {
		if accounts[i].Platform == service.PlatformOpenAI {
			pageHasOpenAIAccounts = true
			break
		}
	}
	if includeSchedulerScore && pageHasOpenAIAccounts {
		schedulerFilterPool := h.listAccountSchedulerScoreFilterPool(c.Request.Context(), platform, accountType, status, search, groupID, privacyMode)
		schedulerScores, schedulerGroupScores = h.buildOpenAIAccountSchedulerScores(c.Request.Context(), accounts, schedulerFilterPool)
	}

	// 始终获取并发数（Redis ZCARD，极低开销）
	if h.concurrencyService != nil {
		if cc, ccErr := h.concurrencyService.GetAccountConcurrencyBatch(c.Request.Context(), accountIDs); ccErr == nil && cc != nil {
			concurrencyCounts = cc
		}
	}

	// 识别需要查询窗口费用、会话数和 RPM 的账号（Anthropic OAuth/SetupToken 且启用了相应功能）
	windowCostAccountIDs := make([]int64, 0)
	sessionLimitAccountIDs := make([]int64, 0)
	rpmAccountIDs := make([]int64, 0)
	sessionIdleTimeouts := make(map[int64]time.Duration) // 各账号的会话空闲超时配置
	for i := range accounts {
		acc := &accounts[i]
		if acc.IsAnthropicOAuthOrSetupToken() {
			if acc.GetWindowCostLimit() > 0 {
				windowCostAccountIDs = append(windowCostAccountIDs, acc.ID)
			}
			if acc.GetMaxSessions() > 0 {
				sessionLimitAccountIDs = append(sessionLimitAccountIDs, acc.ID)
				sessionIdleTimeouts[acc.ID] = time.Duration(acc.GetSessionIdleTimeoutMinutes()) * time.Minute
			}
			if acc.GetBaseRPM() > 0 {
				rpmAccountIDs = append(rpmAccountIDs, acc.ID)
			}
		}
	}

	// 始终获取 RPM 计数（Redis GET，极低开销）
	if len(rpmAccountIDs) > 0 && h.rpmCache != nil {
		rpmCounts, _ = h.rpmCache.GetRPMBatch(c.Request.Context(), rpmAccountIDs)
		if rpmCounts == nil {
			rpmCounts = make(map[int64]int)
		}
	}

	// 始终获取活跃会话数（Redis ZCARD，低开销）
	if len(sessionLimitAccountIDs) > 0 && h.sessionLimitCache != nil {
		activeSessions, _ = h.sessionLimitCache.GetActiveSessionCountBatch(c.Request.Context(), sessionLimitAccountIDs, sessionIdleTimeouts)
		if activeSessions == nil {
			activeSessions = make(map[int64]int)
		}
	}

	// 始终获取窗口费用（PostgreSQL 聚合查询）
	if len(windowCostAccountIDs) > 0 {
		windowCosts = make(map[int64]float64)
		var mu sync.Mutex
		g, gctx := errgroup.WithContext(c.Request.Context())
		g.SetLimit(10) // 限制并发数

		for i := range accounts {
			acc := &accounts[i]
			if !acc.IsAnthropicOAuthOrSetupToken() || acc.GetWindowCostLimit() <= 0 {
				continue
			}
			accCopy := acc // 闭包捕获
			g.Go(func() error {
				// 使用统一的窗口开始时间计算逻辑（考虑窗口过期情况）
				startTime := accCopy.GetCurrentWindowStartTime()
				stats, err := h.accountUsageService.GetAccountWindowStats(gctx, accCopy.ID, startTime)
				if err == nil && stats != nil {
					mu.Lock()
					windowCosts[accCopy.ID] = stats.StandardCost // 使用标准费用
					mu.Unlock()
				}
				return nil // 不返回错误，允许部分失败
			})
		}
		_ = g.Wait()
	}

	// Build response with concurrency info
	result := make([]AccountWithConcurrency, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		accountResponse := h.accountResponseFromService(acc)
		if lite {
			accountResponse = h.accountListResponseFromService(acc)
			if h.isSimpleMode() {
				accountResponse.GroupIDs = filterSimpleModeGroupIDs(accountResponse.GroupIDs, simpleModeCompositeServiceGroupIDs(acc))
			}
		}
		item := AccountWithConcurrency{
			Account:            accountResponse,
			simpleMode:         h.isSimpleMode(),
			CurrentConcurrency: concurrencyCounts[acc.ID],
			SchedulerScore:     schedulerScores[acc.ID],
			SchedulerScores:    schedulerGroupScores[acc.ID],
		}

		// 添加窗口费用（仅当启用时）
		if windowCosts != nil {
			if cost, ok := windowCosts[acc.ID]; ok {
				item.CurrentWindowCost = &cost
			}
		}

		// 添加活跃会话数（仅当启用时）
		if activeSessions != nil {
			if count, ok := activeSessions[acc.ID]; ok {
				item.ActiveSessions = &count
			}
		}

		// 添加 RPM 计数（仅当启用时）
		if rpmCounts != nil {
			if rpm, ok := rpmCounts[acc.ID]; ok {
				item.CurrentRPM = &rpm
			}
		}

		result[i] = item
	}

	h.enrichShadowParents(c.Request.Context(), result)

	if lite {
		compact := make([]AccountListItemWithConcurrency, len(result))
		for i := range result {
			item := result[i]
			compact[i] = AccountListItemWithConcurrency{
				AccountListItem:    dto.AccountListItemFromAccount(item.Account),
				CurrentConcurrency: item.CurrentConcurrency,
				SchedulerScore:     item.SchedulerScore,
				SchedulerScores:    item.SchedulerScores,
				CurrentWindowCost:  item.CurrentWindowCost,
				ActiveSessions:     item.ActiveSessions,
				CurrentRPM:         item.CurrentRPM,
			}
		}
		etag := buildAccountsListETag(compact, total, page, pageSize, platform, accountType, status, search, true)
		if etag != "" {
			c.Header("ETag", etag)
			c.Header("Vary", "If-None-Match")
			if ifNoneMatchMatched(c.GetHeader("If-None-Match"), etag) {
				c.Status(http.StatusNotModified)
				return
			}
		}
		response.Paginated(c, compact, total, page, pageSize)
		return
	}

	etag := buildAccountsListETag(result, total, page, pageSize, platform, accountType, status, search, false)
	if etag != "" {
		c.Header("ETag", etag)
		c.Header("Vary", "If-None-Match")
		if ifNoneMatchMatched(c.GetHeader("If-None-Match"), etag) {
			c.Status(http.StatusNotModified)
			return
		}
	}

	response.Paginated(c, result, total, page, pageSize)
}

func buildAccountsListETag[T any](
	items []T,
	total int64,
	page, pageSize int,
	platform, accountType, status, search string,
	lite bool,
) string {
	payload := struct {
		Total       int64  `json:"total"`
		Page        int    `json:"page"`
		PageSize    int    `json:"page_size"`
		Platform    string `json:"platform"`
		AccountType string `json:"type"`
		Status      string `json:"status"`
		Search      string `json:"search"`
		Lite        bool   `json:"lite"`
		Items       []T    `json:"items"`
	}{
		Total:       total,
		Page:        page,
		PageSize:    pageSize,
		Platform:    platform,
		AccountType: accountType,
		Status:      status,
		Search:      search,
		Lite:        lite,
		Items:       items,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "\"" + hex.EncodeToString(sum[:]) + "\""
}

func ifNoneMatchMatched(ifNoneMatch, etag string) bool {
	if etag == "" || ifNoneMatch == "" {
		return false
	}
	for _, token := range strings.Split(ifNoneMatch, ",") {
		candidate := strings.TrimSpace(token)
		if candidate == "*" {
			return true
		}
		if candidate == etag {
			return true
		}
		if strings.HasPrefix(candidate, "W/") && strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// GetByID handles getting an account by ID
// GET /api/v1/admin/accounts/:id
func (h *AccountHandler) GetByID(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.ollamaCloudUsage != nil {
		if err := h.ollamaCloudUsage.ResolveAccounts(c.Request.Context(), []*service.Account{account}); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// CheckMixedChannel handles checking mixed channel risk for account-group binding.
// POST /api/v1/admin/accounts/check-mixed-channel
func (h *AccountHandler) CheckMixedChannel(c *gin.Context) {
	var req CheckMixedChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if len(req.GroupIDs) == 0 {
		response.Success(c, gin.H{"has_risk": false})
		return
	}

	accountID := int64(0)
	if req.AccountID != nil {
		accountID = *req.AccountID
	}

	err := h.adminService.CheckMixedChannelRisk(c.Request.Context(), accountID, req.Platform, req.GroupIDs)
	if err != nil {
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			response.Success(c, gin.H{
				"has_risk": true,
				"error":    "mixed_channel_warning",
				"message":  mixedErr.Error(),
				"details": gin.H{
					"group_id":         mixedErr.GroupID,
					"group_name":       mixedErr.GroupName,
					"current_platform": mixedErr.CurrentPlatform,
					"other_platform":   mixedErr.OtherPlatform,
				},
			})
			return
		}

		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"has_risk": false})
}

// Create handles creating a new account
// POST /api/v1/admin/accounts
func (h *AccountHandler) Create(c *gin.Context) {
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.ValidateOpenAILongContextBillingExtra(req.Platform, req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	// base_rpm 输入校验：负值归零，超过 10000 截断
	sanitizeExtraBaseRPM(req.Extra)
	if err := validateModelCapabilityDefaultsExtra(req.Extra); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := service.ValidateUpstreamRequestIDHeaderExtra(req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 确定是否跳过混合渠道检查
	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	// 捕获闭包内创建的账号引用，用于创建成功后触发异步探测。
	// 幂等重放时闭包不会执行 → createdAccount 为 nil → 不重复调度。
	var createdAccount *service.Account

	result, err := executeAdminIdempotent(c, "admin.accounts.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		account, execErr := h.createAccountFromRequest(ctx, c, req, skipCheck, true)
		if execErr != nil {
			return nil, execErr
		}
		createdAccount = account
		if req.CredentialDraft == "" {
			h.adminService.EnsureAntigravityPrivacy(ctx, account)
			h.adminService.EnsureOpenAIPrivacy(ctx, account)
		}
		return h.buildAccountResponseWithRuntime(ctx, account), nil
	})
	if err != nil {
		// 检查是否为混合渠道错误
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			// Return confirmation context with the conflict envelope.
			response.ErrorWithDetails(c, http.StatusConflict, mixedErr.Error(), "mixed_channel_warning", map[string]any{
				"group_id":         mixedErr.GroupID,
				"group_name":       mixedErr.GroupName,
				"current_platform": mixedErr.CurrentPlatform,
				"other_platform":   mixedErr.OtherPlatform,
			})
			return
		}

		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}

	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	// 保存后异步补充目录；OpenAI APIKey 同任务探测 /v1/responses。
	// 探测或发现失败不影响账号创建响应。
	h.scheduleModelCatalogApply(createdAccount, true)
	h.scheduleGrokImportProbe(createdAccount)
	response.Success(c, result.Data)
}

// Duplicate handles creating an independent account from an existing account's configuration.
// POST /api/v1/admin/accounts/:id/duplicate
func (h *AccountHandler) Duplicate(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	actorScope := adminActorScope(c)

	result, err := executeAdminIdempotent(
		c,
		"admin.accounts.duplicate",
		struct {
			AccountID int64 `json:"account_id"`
		}{AccountID: accountID},
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			account, execErr := h.adminService.DuplicateAccount(ctx, accountID, actorScope, c.GetHeader("Idempotency-Key"))
			if execErr != nil {
				return nil, execErr
			}
			return h.buildAccountResponseWithRuntime(ctx, account), nil
		},
	)
	if err != nil {
		reason := infraerrors.Reason(err)
		if reason == infraerrors.Reason(service.ErrIdempotencyInProgress) || reason == infraerrors.Reason(service.ErrIdempotencyStoreUnavail) {
			recovered, recoverErr := h.adminService.RecoverDuplicateAccount(c.Request.Context(), accountID, actorScope, c.GetHeader("Idempotency-Key"))
			if recoverErr != nil {
				slog.Warn("account_duplicate_recovery_failed", "account_id", accountID, "actor_scope", actorScope, "reason", reason, "error", recoverErr)
			} else if recovered != nil {
				c.Header("X-Idempotency-Recovered", "true")
				response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), recovered))
				return
			}
		}
		response.ErrorFrom(c, err)
		return
	}

	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result.Data)
}

// Update handles updating an account
// PUT /api/v1/admin/accounts/:id
func (h *AccountHandler) Update(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	// base_rpm 输入校验：负值归零，超过 10000 截断
	sanitizeExtraBaseRPM(req.Extra)
	if err := validateModelCapabilityDefaultsExtra(req.Extra); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := service.ValidateUpstreamRequestIDHeaderExtra(req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// An empty table must preserve the existing save path, including its
	// asynchronous catalog refresh. Once providers are admitted, resolve the
	// stored identity before validating the resulting compatible account.
	if len(domain.CompatibleProviders()) > 0 {
		existing, lookupErr := h.adminService.GetAccount(c.Request.Context(), accountID)
		if lookupErr != nil {
			response.ErrorFrom(c, lookupErr)
			return
		}
		if err := prepareCompatibleAccountUpdate(existing, &req); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	if req.Extra[modelCapabilityDefaultsKey] != nil {
		existing, lookupErr := h.adminService.GetAccount(c.Request.Context(), accountID)
		if lookupErr != nil {
			response.NotFound(c, "Account not found")
			return
		}
		if err := prepareUpdatedModelCapabilityDefaults(existing, req.Type, req.Credentials, req.Extra, adminActorScope(c)); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	// 确定是否跳过混合渠道检查
	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	account, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		TPMLimit:    req.TPMLimit.Pointer(),
		Name:        req.Name,
		Notes:       req.Notes,
		Type:        req.Type,
		Credentials: req.Credentials,
		Extra:       req.Extra,
		ProxyID:     req.ProxyID, ProxyGroupID: req.ProxyGroupID,
		Concurrency:           req.Concurrency, // 指针类型，nil 表示未提供
		Priority:              req.Priority,    // 指针类型，nil 表示未提供
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		Status:                req.Status,
		GroupIDs:              req.GroupIDs,
		ExpiresAt:             req.ExpiresAt,
		AutoPauseOnExpired:    req.AutoPauseOnExpired,
		ProbeEnabled:          req.ProbeEnabled,
		RateSyncEnabled:       req.RateSyncEnabled,
		SkipMixedChannelCheck: skipCheck,
	})
	if err != nil {
		// 检查是否为混合渠道错误
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			// 更新接口仅返回最小必要字段，详细信息由专门检查接口提供
			response.ErrorWithDetails(c, http.StatusConflict, mixedErr.Error(), "mixed_channel_warning", map[string]any{
				"group_id":         mixedErr.GroupID,
				"group_name":       mixedErr.GroupName,
				"current_platform": mixedErr.CurrentPlatform,
				"other_platform":   mixedErr.OtherPlatform,
			})
			return
		}

		response.ErrorFrom(c, err)
		return
	}

	// 保存后异步补充目录；credentials 变更时同时重探测 Responses 能力。
	// 发现失败不影响账号更新响应或已保存的模型选择。
	h.scheduleModelCatalogApply(account, len(req.Credentials) > 0)

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// scheduleOpenAIResponsesProbe 异步触发 OpenAI APIKey 账号的 Responses API 能力探测。
//
// 仅对 platform=openai && type=apikey 账号生效；其他账号无操作。
// 探测本身在 goroutine 中执行（会发一次 HTTP 请求到上游），不会阻塞
// 当前请求。探测错误仅记录日志，不向上下文传播：探测失败时标记保持缺失，
// 网关会按"现状即证据"默认走 Responses。
func (h *AccountHandler) scheduleOpenAIResponsesProbe(account *service.Account) {
	if account == nil || account.Type != service.AccountTypeAPIKey ||
		(account.Platform != service.PlatformOpenAI && !service.IsCNProvider(account.Platform)) {
		return
	}
	if h.accountTestService == nil {
		return
	}
	accountID := account.ID
	defaultOpenAIImportProbeScheduler.scheduleProbe(accountID, func(ctx context.Context) {
		h.accountTestService.ProbeOpenAIAPIKeyResponsesSupport(ctx, accountID)
	})
}

// Queue discovery with the optional Responses probe as one bounded task: the
// shared scheduler deduplicates by account ID, so separate tasks would collide.
// Capture only immutable ownership identifiers, never the request/gin context.
func (h *AccountHandler) scheduleModelCatalogApply(account *service.Account, probeResponses bool) {
	if account == nil || h.accountTestService == nil || h.adminService == nil {
		return
	}
	accountID, workspaceID := account.ID, account.WorkspaceID
	if workspaceID <= 0 {
		workspaceID = domain.DefaultWorkspaceID
	}
	defaultOpenAIImportProbeScheduler.scheduleProbe(accountID, func(ctx context.Context) {
		ctx = domain.WithWorkspaceScope(ctx, workspaceID)
		fresh, err := h.adminService.GetAccount(ctx, accountID)
		if err != nil || fresh == nil || fresh.ID != accountID || (fresh.WorkspaceID > 0 && fresh.WorkspaceID != workspaceID) {
			return
		}
		if probeResponses && fresh.Type == service.AccountTypeAPIKey &&
			(fresh.Platform == service.PlatformOpenAI || service.IsCNProvider(fresh.Platform)) {
			h.accountTestService.ProbeOpenAIAPIKeyResponsesSupport(ctx, accountID)
			fresh, err = h.adminService.GetAccount(ctx, accountID)
			if err != nil || fresh == nil || fresh.ID != accountID || (fresh.WorkspaceID > 0 && fresh.WorkspaceID != workspaceID) {
				return
			}
		}
		// Apply owns conflict/budget handling and records discovery failures without
		// replacing model selection. Its result is independent of the save response.
		_, _ = h.accountTestService.ApplyAccountModelCatalog(ctx, fresh, nil)
	})
}

// Delete handles deleting an account
// DELETE /api/v1/admin/accounts/:id
func (h *AccountHandler) Delete(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	err = h.adminService.DeleteAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Account deleted successfully"})
}

// TestAccountRequest represents the request body for testing an account
type TestAccountRequest struct {
	ModelID string `json:"model_id"`
	Prompt  string `json:"prompt"`
	Mode    string `json:"mode"`
	// Optional media for Grok (and future) real generation tests.
	// ImageDataURL / AudioDataURL are data:<mime>;base64,... payloads.
	ImageDataURL string `json:"image_data_url"`
	AudioDataURL string `json:"audio_data_url"`
}

type SyncFromCRSRequest struct {
	BaseURL            string   `json:"base_url" binding:"required"`
	Username           string   `json:"username" binding:"required"`
	Password           string   `json:"password" binding:"required"`
	SyncProxies        *bool    `json:"sync_proxies"`
	SelectedAccountIDs []string `json:"selected_account_ids"`
}

type PreviewFromCRSRequest struct {
	BaseURL  string `json:"base_url" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Test handles testing account connectivity with SSE streaming
// POST /api/v1/admin/accounts/:id/test
func (h *AccountHandler) Test(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req TestAccountRequest
	// Allow empty body, model_id is optional
	_ = c.ShouldBindJSON(&req)

	opts := service.AccountTestOptions{
		ImageDataURL: req.ImageDataURL,
		AudioDataURL: req.AudioDataURL,
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	result := h.accountTestService.ProbeAccount(c.Request.Context(), service.AccountProbeInput{AccountID: accountID, ModelID: req.ModelID, Prompt: req.Prompt, Mode: req.Mode, Options: opts}, func(event service.TestEvent) {
		body, err := json.Marshal(event)
		if err != nil {
			_ = c.Error(err)
			return
		}
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", body); err != nil {
			_ = c.Error(err)
			return
		}
		c.Writer.Flush()
	})
	if !result.Success {
		return
	}

	if h.rateLimitService != nil {
		if _, err := h.rateLimitService.RecoverAccountAfterSuccessfulTest(c.Request.Context(), accountID); err != nil {
			_ = c.Error(err)
		}
	}
}

// RecoverState handles unified recovery of recoverable account runtime state.
// POST /api/v1/admin/accounts/:id/recover-state
func (h *AccountHandler) RecoverState(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if h.rateLimitService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Rate limit service unavailable")
		return
	}

	if _, err := h.rateLimitService.RecoverAccountState(c.Request.Context(), accountID, service.AccountRecoveryOptions{
		InvalidateToken: true,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// SyncFromCRS handles syncing accounts from claude-relay-service (CRS)
// POST /api/v1/admin/accounts/sync/crs
func (h *AccountHandler) SyncFromCRS(c *gin.Context) {
	var req SyncFromCRSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Default to syncing proxies (can be disabled by explicitly setting false)
	syncProxies := true
	if req.SyncProxies != nil {
		syncProxies = *req.SyncProxies
	}

	result, err := h.crsSyncService.SyncFromCRS(c.Request.Context(), service.SyncFromCRSInput{
		BaseURL:            req.BaseURL,
		Username:           req.Username,
		Password:           req.Password,
		SyncProxies:        syncProxies,
		SelectedAccountIDs: req.SelectedAccountIDs,
	})
	if err != nil {
		// Provide detailed error message for CRS sync failures
		response.InternalError(c, "CRS sync failed: "+err.Error())
		return
	}

	response.Success(c, result)
}

// PreviewFromCRS handles previewing accounts from CRS before sync
// POST /api/v1/admin/accounts/sync/crs/preview
func (h *AccountHandler) PreviewFromCRS(c *gin.Context) {
	var req PreviewFromCRSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.crsSyncService.PreviewFromCRS(c.Request.Context(), service.SyncFromCRSInput{
		BaseURL:  req.BaseURL,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		response.InternalError(c, "CRS preview failed: "+err.Error())
		return
	}

	response.Success(c, result)
}

// refreshSingleAccount refreshes credentials for a single OAuth account.
// Returns (updatedAccount, warning, error) where warning is used for Antigravity ProjectIDMissing scenario.
func (h *AccountHandler) refreshSingleAccount(ctx context.Context, account *service.Account) (*service.Account, string, error) {
	if !account.IsOAuth() {
		return nil, "", infraerrors.BadRequest("NOT_OAUTH", "cannot refresh non-OAuth account")
	}
	switch account.Platform {
	case service.PlatformAnthropic, service.PlatformOpenAI, service.PlatformGemini, service.PlatformAntigravity, service.PlatformGrok, service.PlatformCursor:
	default:
		return nil, "", infraerrors.BadRequest("REFRESH_UNSUPPORTED", "this account type does not support credential refresh")
	}
	if account.IsCursor() {
		if h.cursorSessions == nil {
			//nolint:staticcheck // ST1005: the message is returned to the admin API caller as-is, so its capitalised wording is part of the client-visible contract.
			return nil, "", errors.New("Cursor session service unavailable")
		}
		if _, err := h.cursorSessions.GetSessionToken(ctx, account, account.GetCredential("access_token")); err != nil {
			return nil, "", err
		}
		updated, err := h.adminService.GetAccount(ctx, account.ID)
		return updated, "", err
	}
	// spark 影子凭据由母账号管理、自身恒空,刷新无意义且会先打上游;在调用上游前早拒
	// (覆盖单账号与批量两入口;批量侧将其计为 failed 并附说明)(外审第6轮)。
	if account.IsCredentialShadow() {
		return nil, "", infraerrors.BadRequest("SPARK_SHADOW_NO_REFRESH",
			"cannot refresh spark shadow account; its credentials are managed by the parent account")
	}

	var newCredentials map[string]any

	if account.IsOpenAI() {
		tokenInfo, err := h.openaiOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			// 刷新失败但 access_token 可能仍有效，尝试设置隐私
			h.adminService.EnsureOpenAIPrivacy(ctx, account)
			return nil, "", err
		}

		newCredentials = h.openaiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
		newCredentials = service.NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)
	} else if account.Platform == service.PlatformGemini {
		tokenInfo, err := h.geminiOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", fmt.Errorf("failed to refresh credentials: %w", err)
		}

		newCredentials = h.geminiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
	} else if account.Platform == service.PlatformAntigravity {
		tokenInfo, err := h.antigravityOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", err
		}

		newCredentials = h.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}

		// 特殊处理 project_id：如果新值为空但旧值非空，保留旧值
		// 这确保了即使 LoadCodeAssist 失败，project_id 也不会丢失
		if newProjectID, _ := newCredentials["project_id"].(string); newProjectID == "" {
			if oldProjectID := strings.TrimSpace(account.GetCredential("project_id")); oldProjectID != "" {
				newCredentials["project_id"] = oldProjectID
			}
		}

		// 如果 project_id 获取失败，更新凭证但不标记为 error
		if tokenInfo.ProjectIDMissing {
			updatedAccount, updateErr := h.persistRefreshedCredentials(ctx, account, newCredentials)
			if updateErr != nil {
				return nil, "", fmt.Errorf("failed to update credentials: %w", updateErr)
			}
			h.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)
			return updatedAccount, "missing_project_id_temporary", nil
		}

		// 成功获取到 project_id，如果之前是 missing_project_id 错误则清除
		if account.Status == service.StatusError && strings.Contains(account.ErrorMessage, "missing_project_id:") {
			if _, clearErr := h.adminService.ClearAccountError(ctx, account.ID); clearErr != nil {
				return nil, "", fmt.Errorf("failed to clear account error: %w", clearErr)
			}
		}
	} else if account.Platform == service.PlatformGrok {
		if h.grokOAuthService == nil {
			return nil, "", fmt.Errorf("grok oauth service is not configured")
		}
		tokenInfo, err := h.grokOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", fmt.Errorf("failed to refresh Grok credentials: %w", err)
		}

		newCredentials = service.MergeCredentials(account.Credentials, h.grokOAuthService.BuildAccountCredentials(tokenInfo))
		if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
			newCredentials["base_url"] = baseURL
		}
	} else if account.Platform == service.PlatformAnthropic {
		// Use Anthropic/Claude OAuth service to refresh token
		tokenInfo, err := h.oauthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", err
		}

		// Copy existing credentials to preserve non-token settings (e.g., intercept_warmup_requests)
		newCredentials = make(map[string]any)
		for k, v := range account.Credentials {
			newCredentials[k] = v
		}

		// Update token-related fields
		newCredentials["access_token"] = tokenInfo.AccessToken
		newCredentials["token_type"] = tokenInfo.TokenType
		newCredentials["expires_in"] = strconv.FormatInt(tokenInfo.ExpiresIn, 10)
		newCredentials["expires_at"] = strconv.FormatInt(tokenInfo.ExpiresAt, 10)
		if strings.TrimSpace(tokenInfo.RefreshToken) != "" {
			newCredentials["refresh_token"] = tokenInfo.RefreshToken
		}
		if strings.TrimSpace(tokenInfo.Scope) != "" {
			newCredentials["scope"] = tokenInfo.Scope
		}
	}

	updatedAccount, err := h.persistRefreshedCredentials(ctx, account, newCredentials)
	if err != nil {
		return nil, "", err
	}

	// 刷新成功后，清除 token 缓存，确保下次请求使用新 token
	if h.tokenCacheInvalidator != nil {
		if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(ctx, updatedAccount); invalidateErr != nil {
			log.Printf("[WARN] Failed to invalidate token cache for account %d: %v", updatedAccount.ID, invalidateErr)
		}
	}

	// OpenAI OAuth: 刷新成功后检查并设置 privacy_mode
	h.adminService.EnsureOpenAIPrivacy(ctx, updatedAccount)
	// Antigravity OAuth: 刷新成功后检查并设置 privacy_mode
	h.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)

	return updatedAccount, "", nil
}

// Refresh handles refreshing account credentials
// POST /api/v1/admin/accounts/:id/refresh
func (h *AccountHandler) Refresh(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	// Get account
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	updatedAccount, warning, err := h.refreshSingleAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if warning == "missing_project_id_temporary" {
		response.Success(c, gin.H{
			"account": h.buildAccountResponseWithRuntime(c.Request.Context(), updatedAccount),
			"message": "Token refreshed successfully, but project_id could not be retrieved (will retry automatically)",
			"warning": "missing_project_id_temporary",
		})
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), updatedAccount))
}

// ApplyOAuthCredentialsRequest is the payload for persisting re-authorized OAuth credentials.
type ApplyOAuthCredentialsRequest struct {
	CredentialDraft         string         `json:"credential_draft"`
	CredentialDraftRevision string         `json:"credential_draft_revision"`
	Type                    string         `json:"type" binding:"required,oneof=oauth setup-token"`
	Credentials             map[string]any `json:"credentials"`
	Extra                   map[string]any `json:"extra"`
}

// ApplyOAuthCredentials 将"重新授权"得到的新凭据原子落库。
// POST /api/v1/admin/accounts/:id/apply-oauth-credentials
//
// 与通用 PUT /:id (Update) 接口的关键区别：
//   - 仅接收 type / credentials / extra 三个字段（不接受 concurrency / rpm / quota_* 等可能误传的字段）
//   - Extra 走 UpdateAccountExtra(JSONB key 级合并)，**绝不**全量覆盖；
//     避免 base_rpm / window_cost_limit / max_sessions / quota_* / privacy_mode
//     等持久化配置在重新授权后丢失
//   - 内置 ClearError + InvalidateToken，避免前端额外两次调用，
//     并修复旧路径未失效 token 缓存导致重新授权后立即 401 的隐性 bug
//
// 与 /refresh 的区别：/refresh 用现有 refresh_token 换 access_token（无用户交互），
// 本接口承接前端完成完整 OAuth 流程后的落库步骤。
func (h *AccountHandler) ApplyOAuthCredentials(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req ApplyOAuthCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()

	// 预检查账号存在 + OAuth 类型（与 Refresh handler 语义一致，提供更友好的错误信息）。
	existing, err := h.adminService.GetAccount(ctx, accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if !existing.IsOAuth() {
		response.ErrorFrom(c, infraerrors.BadRequest("NOT_OAUTH", "cannot apply oauth credentials to non-OAuth account"))
		return
	}
	if err := service.ValidateOpenAILongContextBillingExtra(existing.Platform, req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := validateModelCapabilityDefaultsExtra(req.Extra); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := service.ValidateUpstreamRequestIDHeaderExtra(req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	updatedAccount, err := h.applyCredentialDraft(ctx, c, accountID, existing, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Successful re-auth clears the soft spending-limit reauth flag for Grok.
	if existing.Platform == service.PlatformGrok {
		if clearErr := h.adminService.UpdateAccountExtra(ctx, accountID, map[string]any{
			"grok_needs_reauth":        false,
			"grok_needs_reauth_reason": "",
			"grok_needs_reauth_at":     "",
		}); clearErr != nil {
			slog.Warn("apply_oauth_credentials.clear_grok_reauth_failed",
				"account_id", accountID,
				"err", clearErr,
			)
		}
	}

	if cleared, clearErr := h.adminService.ClearAccountError(ctx, accountID); clearErr != nil {
		slog.Warn("apply_oauth_credentials.clear_error_failed",
			"account_id", accountID,
			"err", clearErr,
		)
	} else if cleared != nil {
		updatedAccount = cleared
	}

	if h.tokenCacheInvalidator != nil && updatedAccount.IsOAuth() {
		if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(ctx, updatedAccount); invalidateErr != nil {
			slog.Warn("apply_oauth_credentials.invalidate_token_failed",
				"account_id", accountID,
				"err", invalidateErr,
			)
		}
	}

	response.Success(c, h.buildAccountResponseWithRuntime(ctx, updatedAccount))
}

// GetStats handles getting account statistics
// GET /api/v1/admin/accounts/:id/stats
func (h *AccountHandler) GetStats(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	// Parse days parameter (default 30)
	days := 30
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}

	// Calculate time range
	now := timezone.Now()
	endTime := timezone.StartOfDay(now.AddDate(0, 0, 1))
	startTime := timezone.StartOfDay(now.AddDate(0, 0, -days+1))

	stats, err := h.accountUsageService.GetAccountUsageStats(c.Request.Context(), accountID, startTime, endTime)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, stats)
}

// ClearError handles clearing account error
// POST /api/v1/admin/accounts/:id/clear-error
func (h *AccountHandler) ClearError(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.ClearAccountError(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 清除错误后，同时清除 token 缓存，确保下次请求会获取最新的 token（触发刷新或从 DB 读取）
	// 这解决了管理员重置账号状态后，旧的失效 token 仍在缓存中导致立即再次 401 的问题
	if h.tokenCacheInvalidator != nil && account.IsOAuth() {
		if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(c.Request.Context(), account); invalidateErr != nil {
			log.Printf("[WARN] Failed to invalidate token cache for account %d: %v", accountID, invalidateErr)
		}
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// RevertProxyFallback handles reverting account proxy to original before fallback.
// POST /api/v1/admin/accounts/:id/revert-proxy-fallback
func (h *AccountHandler) RevertProxyFallback(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if err := h.adminService.RevertAccountProxyFallback(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "reverted"})
}

// BatchDelete handles deleting multiple accounts with bounded concurrency.
// POST /api/v1/admin/accounts/batch-delete
func (h *AccountHandler) BatchDelete(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	accountIDs := normalizeInt64IDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}

	accounts, err := h.adminService.GetAccountsByIDs(c.Request.Context(), accountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	type deleteError struct {
		AccountID int64  `json:"account_id"`
		Error     string `json:"error"`
	}

	requestedIDs := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		requestedIDs[accountID] = struct{}{}
	}
	accountsByID := make(map[int64]*service.Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			accountsByID[account.ID] = account
		}
	}

	rootIDs := make([]int64, 0, len(accountIDs))
	dependentIDs := make(map[int64][]int64)
	failedIDs := make([]int64, 0)
	errorsByAccount := make([]deleteError, 0)
	for _, accountID := range accountIDs {
		account := accountsByID[accountID]
		if account == nil {
			failedIDs = append(failedIDs, accountID)
			errorsByAccount = append(errorsByAccount, deleteError{
				AccountID: accountID,
				Error:     "account not found",
			})
			continue
		}

		rootID := accountID
		visited := map[int64]struct{}{accountID: {}}
		for {
			current := accountsByID[rootID]
			if current == nil || current.ParentAccountID == nil {
				break
			}
			parentID := *current.ParentAccountID
			if _, selected := requestedIDs[parentID]; !selected {
				break
			}
			if _, exists := accountsByID[parentID]; !exists {
				break
			}
			if _, cyclic := visited[parentID]; cyclic {
				rootID = accountID
				break
			}
			visited[parentID] = struct{}{}
			rootID = parentID
		}

		if rootID != accountID {
			dependentIDs[rootID] = append(dependentIDs[rootID], accountID)
			continue
		}
		rootIDs = append(rootIDs, accountID)
	}

	const maxConcurrency = 5
	g, gctx := errgroup.WithContext(c.Request.Context())
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	successIDs := make([]int64, 0, len(accountIDs))

	// Every worker returns nil so one account failure does not cancel the remaining deletions.
	for _, id := range rootIDs {
		accountID := id
		g.Go(func() error {
			err := h.adminService.DeleteAccount(gctx, accountID)

			mu.Lock()
			defer mu.Unlock()
			affectedIDs := append([]int64{accountID}, dependentIDs[accountID]...)
			if err != nil {
				for _, affectedID := range affectedIDs {
					failedIDs = append(failedIDs, affectedID)
					errorsByAccount = append(errorsByAccount, deleteError{
						AccountID: affectedID,
						Error:     err.Error(),
					})
				}
				return nil
			}
			successIDs = append(successIDs, affectedIDs...)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	sort.Slice(successIDs, func(i, j int) bool { return successIDs[i] < successIDs[j] })
	sort.Slice(failedIDs, func(i, j int) bool { return failedIDs[i] < failedIDs[j] })
	sort.Slice(errorsByAccount, func(i, j int) bool {
		return errorsByAccount[i].AccountID < errorsByAccount[j].AccountID
	})

	response.Success(c, gin.H{
		"total":       len(accountIDs),
		"success":     len(successIDs),
		"failed":      len(failedIDs),
		"success_ids": successIDs,
		"failed_ids":  failedIDs,
		"errors":      errorsByAccount,
	})
}

// BatchClearError handles batch clearing account errors
// POST /api/v1/admin/accounts/batch-clear-error
func (h *AccountHandler) BatchClearError(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}

	ctx := c.Request.Context()

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	var successCount, failedCount int
	var errors []gin.H

	// 注意：所有 goroutine 必须 return nil，避免 errgroup cancel 其他并发任务
	for _, id := range req.AccountIDs {
		accountID := id // 闭包捕获
		g.Go(func() error {
			account, err := h.adminService.ClearAccountError(gctx, accountID)
			if err != nil {
				mu.Lock()
				failedCount++
				errors = append(errors, gin.H{
					"account_id": accountID,
					"error":      err.Error(),
				})
				mu.Unlock()
				return nil
			}

			// 清除错误后，同时清除 token 缓存
			if h.tokenCacheInvalidator != nil && account.IsOAuth() {
				if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(gctx, account); invalidateErr != nil {
					log.Printf("[WARN] Failed to invalidate token cache for account %d: %v", accountID, invalidateErr)
				}
			}

			mu.Lock()
			successCount++
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"total":   len(req.AccountIDs),
		"success": successCount,
		"failed":  failedCount,
		"errors":  errors,
	})
}

// BatchRefresh handles batch refreshing account credentials
// POST /api/v1/admin/accounts/batch-refresh
func (h *AccountHandler) BatchRefresh(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}

	ctx := c.Request.Context()

	accounts, err := h.adminService.GetAccountsByIDs(ctx, req.AccountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 建立已获取账号的 ID 集合，检测缺失的 ID
	foundIDs := make(map[int64]bool, len(accounts))
	for _, acc := range accounts {
		if acc != nil {
			foundIDs[acc.ID] = true
		}
	}

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	var successCount, failedCount int
	var errors []gin.H
	var warnings []gin.H

	// 将不存在的账号 ID 标记为失败
	for _, id := range req.AccountIDs {
		if !foundIDs[id] {
			failedCount++
			errors = append(errors, gin.H{
				"account_id": id,
				"error":      "account not found",
			})
		}
	}

	// 注意：所有 goroutine 必须 return nil，避免 errgroup cancel 其他并发任务
	for _, account := range accounts {
		acc := account // 闭包捕获
		if acc == nil {
			continue
		}
		g.Go(func() error {
			_, warning, err := h.refreshSingleAccount(gctx, acc)
			mu.Lock()
			if err != nil {
				failedCount++
				errors = append(errors, gin.H{
					"account_id": acc.ID,
					"error":      err.Error(),
				})
			} else {
				successCount++
				if warning != "" {
					warnings = append(warnings, gin.H{
						"account_id": acc.ID,
						"warning":    warning,
					})
				}
			}
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"total":    len(req.AccountIDs),
		"success":  successCount,
		"failed":   failedCount,
		"errors":   errors,
		"warnings": warnings,
	})
}

// BatchCreate handles batch creating accounts
// POST /api/v1/admin/accounts/batch
func (h *AccountHandler) BatchCreate(c *gin.Context) {
	var req struct {
		Accounts []CreateAccountRequest `json:"accounts" binding:"required,min=1,dive"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	for _, item := range req.Accounts {
		if err := service.ValidateOpenAILongContextBillingExtra(item.Platform, item.Extra); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	groupIDs := make([]int64, 0)
	for _, item := range req.Accounts {
		groupIDs = append(groupIDs, item.GroupIDs...)
	}
	if err := h.adminService.ValidateAccountGroupBindings(c.Request.Context(), groupIDs); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	executeAdminIdempotentJSON(c, "admin.accounts.batch_create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := requireCredentialDraftStore(h.credentialDrafts); err != nil {
			return nil, err
		}
		accounts := make([]*service.Account, 0, len(req.Accounts))
		results := make([]gin.H, 0, len(req.Accounts))
		err := h.credentialDrafts.RunAccountWriteTransaction(ctx, func(transactionContext context.Context) error {
			for _, item := range req.Accounts {
				if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
					return infraerrors.BadRequest("INVALID_RATE_MULTIPLIER", "rate_multiplier must be >= 0")
				}
				sanitizeExtraBaseRPM(item.Extra)
				if err := service.ValidateUpstreamRequestIDHeaderExtra(item.Extra); err != nil {
					return err
				}
				skipCheck := item.ConfirmMixedChannelRisk != nil && *item.ConfirmMixedChannelRisk
				account, err := h.createAccountFromRequest(transactionContext, c, item, skipCheck, true)
				if err != nil {
					return accountWriteError(err)
				}
				accounts = append(accounts, account)
				results = append(results, gin.H{"name": item.Name, "id": account.ID, "success": true})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		for _, account := range accounts {
			h.scheduleOpenAIResponsesProbe(account)
			h.scheduleGrokImportProbe(account)
			service.InvalidateAvailableModelsCacheForAccount(account)
		}
		return gin.H{"success": len(accounts), "failed": 0, "results": results}, nil
	})
}

// BatchUpdateCredentialsRequest represents batch credentials update request
type BatchUpdateCredentialsRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required,min=1"`
	Field      string  `json:"field" binding:"required,oneof=account_uuid org_uuid intercept_warmup_requests"`
	Value      any     `json:"value"`
}

// BatchUpdateCredentials handles batch updating credentials fields
// POST /api/v1/admin/accounts/batch-update-credentials
func (h *AccountHandler) BatchUpdateCredentials(c *gin.Context) {
	var req BatchUpdateCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Validate value type based on field
	if req.Field == "intercept_warmup_requests" {
		// Must be boolean
		if _, ok := req.Value.(bool); !ok {
			response.BadRequest(c, "intercept_warmup_requests must be boolean")
			return
		}
	} else {
		// account_uuid and org_uuid can be string or null
		if req.Value != nil {
			if _, ok := req.Value.(string); !ok {
				response.BadRequest(c, req.Field+" must be string or null")
				return
			}
		}
	}

	ctx := c.Request.Context()

	// 阶段一：预验证所有账号存在，收集 credentials
	type accountUpdate struct {
		ID          int64
		Credentials map[string]any
	}
	updates := make([]accountUpdate, 0, len(req.AccountIDs))
	for _, accountID := range req.AccountIDs {
		account, err := h.adminService.GetAccount(ctx, accountID)
		if err != nil {
			response.Error(c, 404, fmt.Sprintf("Account %d not found", accountID))
			return
		}
		if account.Credentials == nil {
			account.Credentials = make(map[string]any)
		}
		account.Credentials[req.Field] = req.Value
		updates = append(updates, accountUpdate{ID: accountID, Credentials: account.Credentials})
	}

	// 阶段二：依次更新，返回每个账号的成功/失败明细，便于调用方重试
	success := 0
	failed := 0
	successIDs := make([]int64, 0, len(updates))
	failedIDs := make([]int64, 0, len(updates))
	results := make([]gin.H, 0, len(updates))
	for _, u := range updates {
		updateInput := &service.UpdateAccountInput{Credentials: u.Credentials}
		if _, err := h.adminService.UpdateAccount(ctx, u.ID, updateInput); err != nil {
			failed++
			failedIDs = append(failedIDs, u.ID)
			results = append(results, gin.H{
				"account_id": u.ID,
				"success":    false,
				"error":      err.Error(),
			})
			continue
		}
		success++
		successIDs = append(successIDs, u.ID)
		results = append(results, gin.H{
			"account_id": u.ID,
			"success":    true,
		})
	}

	response.Success(c, gin.H{
		"success":     success,
		"failed":      failed,
		"success_ids": successIDs,
		"failed_ids":  failedIDs,
		"results":     results,
	})
}

// BulkUpdate handles bulk updating accounts with selected fields/credentials.
// POST /api/v1/admin/accounts/bulk-update
func (h *AccountHandler) BulkUpdate(c *gin.Context) {
	var req BulkUpdateAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	if len(req.AccountIDs) == 0 && req.Filters == nil {
		response.BadRequest(c, "account_ids or filters is required")
		return
	}
	// base_rpm 输入校验：负值归零，超过 10000 截断
	sanitizeExtraBaseRPM(req.Extra)
	if err := validateModelCapabilityDefaultsExtra(req.Extra); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := service.ValidateUpstreamRequestIDHeaderExtra(req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 确定是否跳过混合渠道检查
	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	hasUpdates := req.TPMLimit.Present || req.Name != "" ||
		req.ProxyID != nil || req.ProxyGroupID != nil ||
		req.Concurrency != nil ||
		req.Priority != nil ||
		req.RateMultiplier != nil ||
		req.LoadFactor != nil ||
		req.Status != "" ||
		req.Schedulable != nil ||
		req.GroupIDs != nil ||
		len(req.Credentials) > 0 ||
		len(req.Extra) > 0 ||
		req.ProbeEnabled != nil

	if !hasUpdates {
		response.BadRequest(c, "No updates provided")
		return
	}

	result, err := h.adminService.BulkUpdateAccounts(c.Request.Context(), &service.BulkUpdateAccountsInput{
		TPMLimit:   req.TPMLimit.Pointer(),
		AccountIDs: req.AccountIDs,
		Filters:    toServiceBulkUpdateAccountFilters(req.Filters),
		Name:       req.Name,
		ProxyID:    req.ProxyID, ProxyGroupID: req.ProxyGroupID,
		Concurrency:           req.Concurrency,
		Priority:              req.Priority,
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		Status:                req.Status,
		Schedulable:           req.Schedulable,
		GroupIDs:              req.GroupIDs,
		Credentials:           req.Credentials,
		Extra:                 req.Extra,
		ProbeEnabled:          req.ProbeEnabled,
		SkipMixedChannelCheck: skipCheck,
	})
	if err != nil {
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			response.ErrorWithDetails(c, http.StatusConflict, mixedErr.Error(), "mixed_channel_warning", map[string]any{
				"group_id":         mixedErr.GroupID,
				"group_name":       mixedErr.GroupName,
				"current_platform": mixedErr.CurrentPlatform,
				"other_platform":   mixedErr.OtherPlatform,
			})
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

func toServiceBulkUpdateAccountFilters(filters *BulkUpdateAccountFilters) *service.BulkUpdateAccountFilters {
	if filters == nil {
		return nil
	}
	return &service.BulkUpdateAccountFilters{
		Platform:    filters.Platform,
		Type:        filters.Type,
		Status:      filters.Status,
		Group:       filters.Group,
		Search:      filters.Search,
		PrivacyMode: filters.PrivacyMode,
	}
}

// ========== OAuth Handlers ==========

// GenerateAuthURLRequest represents the request for generating auth URL
type GenerateAuthURLRequest struct {
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

// GenerateAuthURL generates OAuth authorization URL with full scope
// POST /api/v1/admin/accounts/generate-auth-url
func (h *OAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req GenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = GenerateAuthURLRequest{}
	}

	result, err := h.oauthService.GenerateAuthURL(c.Request.Context(), req.ProxyID, req.ProxyGroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	reference, err := beginCredentialAuthorization(c, h.credentialDrafts, "anthropic:oauth", result.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result.SessionID = reference
	response.Success(c, result)
}

// GenerateSetupTokenURL generates OAuth authorization URL for setup token (inference only)
// POST /api/v1/admin/accounts/generate-setup-token-url
func (h *OAuthHandler) GenerateSetupTokenURL(c *gin.Context) {
	var req GenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = GenerateAuthURLRequest{}
	}

	result, err := h.oauthService.GenerateSetupTokenURL(c.Request.Context(), req.ProxyID, req.ProxyGroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	reference, err := beginCredentialAuthorization(c, h.credentialDrafts, "anthropic:setup", result.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result.SessionID = reference
	response.Success(c, result)
}

// ExchangeCodeRequest represents the request for exchanging auth code
type ExchangeCodeRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	Code         string `json:"code" binding:"required"`
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

// ExchangeCode exchanges authorization code for tokens
// POST /api/v1/admin/accounts/exchange-code
func (h *OAuthHandler) ExchangeCode(c *gin.Context) {
	var req ExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	sessionID, err := consumeCredentialAuthorization(c, h.credentialDrafts, "anthropic:oauth", req.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	req.SessionID = sessionID

	tokenInfo, err := h.oauthService.ExchangeCode(c.Request.Context(), &service.ExchangeCodeInput{
		SessionID: req.SessionID,
		Code:      req.Code,
		ProxyID:   req.ProxyID, ProxyGroupID: req.ProxyGroupID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	respondCredentialDraft(c, h.credentialDrafts, service.PlatformAnthropic, service.AccountTypeOAuth, claudeDraftCredentials(tokenInfo), tokenInfo.Egress)
}

// ExchangeSetupTokenCode exchanges authorization code for setup token
// POST /api/v1/admin/accounts/exchange-setup-token-code
func (h *OAuthHandler) ExchangeSetupTokenCode(c *gin.Context) {
	var req ExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	sessionID, err := consumeCredentialAuthorization(c, h.credentialDrafts, "anthropic:setup", req.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	req.SessionID = sessionID

	tokenInfo, err := h.oauthService.ExchangeCode(c.Request.Context(), &service.ExchangeCodeInput{
		SessionID: req.SessionID,
		Code:      req.Code,
		ProxyID:   req.ProxyID, ProxyGroupID: req.ProxyGroupID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	respondCredentialDraft(c, h.credentialDrafts, service.PlatformAnthropic, service.AccountTypeSetupToken, claudeDraftCredentials(tokenInfo), tokenInfo.Egress)
}

// CookieAuthRequest represents the request for cookie-based authentication
type CookieAuthRequest struct {
	SessionKey   string `json:"code" binding:"required"` // Using 'code' field as sessionKey (frontend sends it this way)
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

// CookieAuth performs OAuth using sessionKey (cookie-based auto-auth)
// POST /api/v1/admin/accounts/cookie-auth
func (h *OAuthHandler) CookieAuth(c *gin.Context) {
	var req CookieAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	tokenInfo, err := h.oauthService.CookieAuth(c.Request.Context(), &service.CookieAuthInput{
		SessionKey: req.SessionKey,
		ProxyID:    req.ProxyID, ProxyGroupID: req.ProxyGroupID,
		Scope: "full",
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	respondCredentialDraft(c, h.credentialDrafts, service.PlatformAnthropic, service.AccountTypeOAuth, claudeDraftCredentials(tokenInfo), tokenInfo.Egress)
}

// SetupTokenCookieAuth performs OAuth using sessionKey for setup token (inference only)
// POST /api/v1/admin/accounts/setup-token-cookie-auth
func (h *OAuthHandler) SetupTokenCookieAuth(c *gin.Context) {
	var req CookieAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	tokenInfo, err := h.oauthService.CookieAuth(c.Request.Context(), &service.CookieAuthInput{
		SessionKey: req.SessionKey,
		ProxyID:    req.ProxyID, ProxyGroupID: req.ProxyGroupID,
		Scope: "inference",
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	respondCredentialDraft(c, h.credentialDrafts, service.PlatformAnthropic, service.AccountTypeSetupToken, claudeDraftCredentials(tokenInfo), tokenInfo.Egress)
}

// GetUsage handles getting account usage information
// GET /api/v1/admin/accounts/:id/usage?source=passive|active&force=true
func (h *AccountHandler) GetUsage(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	source := c.DefaultQuery("source", "active")
	force := c.Query("force") == "true"

	var usage *service.UsageInfo
	if source == "passive" {
		usage, err = h.accountUsageService.GetPassiveUsage(c.Request.Context(), accountID)
	} else {
		usage, err = h.accountUsageService.GetUsage(c.Request.Context(), accountID, force)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, usage)
}

// ClearRateLimit handles clearing account rate limit status
// POST /api/v1/admin/accounts/:id/clear-rate-limit
func (h *AccountHandler) ClearRateLimit(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	err = h.rateLimitService.ClearRateLimit(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// ResetQuota handles resetting account quota usage
// POST /api/v1/admin/accounts/:id/reset-quota
func (h *AccountHandler) ResetQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if err := h.adminService.ResetAccountQuota(c.Request.Context(), accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// GetTempUnschedulable handles getting temporary unschedulable status
// GET /api/v1/admin/accounts/:id/temp-unschedulable
func (h *AccountHandler) GetTempUnschedulable(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	state, err := h.rateLimitService.GetTempUnschedStatus(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if state == nil || state.UntilUnix <= time.Now().Unix() {
		response.Success(c, gin.H{"active": false})
		return
	}

	response.Success(c, gin.H{
		"active": true,
		"state":  state,
	})
}

// ClearTempUnschedulable handles clearing temporary unschedulable status
// DELETE /api/v1/admin/accounts/:id/temp-unschedulable
func (h *AccountHandler) ClearTempUnschedulable(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if err := h.rateLimitService.ClearTempUnschedulable(c.Request.Context(), accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Temp unschedulable cleared successfully"})
}

// GetTodayStats handles getting account today statistics
// GET /api/v1/admin/accounts/:id/today-stats
func (h *AccountHandler) GetTodayStats(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	stats, err := h.accountUsageService.GetTodayStats(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, stats)
}

// BatchTodayStatsRequest 批量今日统计请求体。
type BatchTodayStatsRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required"`
}

type BatchUsageRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required"`
	Force      bool    `json:"force"`
}

// GetBatchTodayStats 批量获取多个账号的今日统计。
// POST /api/v1/admin/accounts/today-stats/batch
func (h *AccountHandler) GetBatchTodayStats(c *gin.Context) {
	var req BatchTodayStatsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	accountIDs := normalizeInt64IDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.Success(c, gin.H{"stats": map[string]any{}})
		return
	}

	dayStart := timezone.Today()
	revision, err := h.accountUsageService.DashboardCoverageRevision(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	cacheKey := buildAccountTodayStatsBatchCacheKey(accountIDs, dayStart, revision)
	cached, hit, err := accountTodayStatsBatchCache.GetOrLoad(cacheKey, func() (any, error) {
		stats, err := h.accountUsageService.GetTodayStatsBatchFrom(c.Request.Context(), accountIDs, dayStart)
		if err != nil {
			return nil, err
		}
		return gin.H{"stats": stats}, nil
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if cached.ETag != "" {
		c.Header("ETag", cached.ETag)
		c.Header("Vary", "If-None-Match")
		if hit && ifNoneMatchMatched(c.GetHeader("If-None-Match"), cached.ETag) {
			c.Status(http.StatusNotModified)
			return
		}
	}
	if hit {
		c.Header("X-Snapshot-Cache", "hit")
	} else {
		c.Header("X-Snapshot-Cache", "miss")
	}
	response.Success(c, cached.Payload)
}

// GetBatchUsage 批量获取多个账号的 current usage。
// POST /api/v1/admin/accounts/usage/batch
func (h *AccountHandler) GetBatchUsage(c *gin.Context) {
	var req BatchUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	accountIDs := normalizeInt64IDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.Success(c, gin.H{
			"usage":  map[string]any{},
			"errors": map[string]string{},
		})
		return
	}

	usageByAccount, errorsByAccount, err := h.accountUsageService.GetUsageBatch(c.Request.Context(), accountIDs, req.Force)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"usage":  usageByAccount,
		"errors": errorsByAccount,
	})
}

// SetSchedulableRequest represents the request body for setting schedulable status
type SetSchedulableRequest struct {
	Schedulable bool `json:"schedulable"`
}

// SetSchedulable handles toggling account schedulable status
// POST /api/v1/admin/accounts/:id/schedulable
func (h *AccountHandler) SetSchedulable(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req SetSchedulableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	account, err := h.adminService.SetAccountSchedulable(c.Request.Context(), accountID, req.Schedulable)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// GetAvailableModels handles getting available models for an account
// GET /api/v1/admin/accounts/:id/models
func (h *AccountHandler) GetAvailableModels(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	if c.Query("view") == "supported" {
		models := make([]map[string]string, 0)
		snapshot := account.EffectiveUpstreamModelCatalog()
		if account.IsShadow() {
			credentialAccount, lookupErr := h.adminService.GetAccount(c.Request.Context(), *account.ParentAccountID)
			if lookupErr != nil {
				response.ErrorFrom(c, lookupErr)
				return
			}
			snapshot = account.EffectiveUpstreamModelCatalogForCredentialAccount(credentialAccount)
		}
		if snapshot != nil {
			for _, model := range snapshot.EligibleModels {
				models = append(models, map[string]string{"id": model.ID, "display_name": model.ID, "catalog_source": "applied", "category": service.ModelDisplayCategory(service.UpstreamModelMetadata{Category: model.Category}), "evidence_kind": model.EvidenceKind, "evidence_observed_at": model.EvidenceObservedAt})
			}
		}
		c.Header("X-Model-Catalog-Source", "applied")
		response.Success(c, models)
		return
	}

	catalogSource := "preset"
	if raw, ok := account.Credentials["model_mapping"]; ok {
		switch mapping := raw.(type) {
		case map[string]any:
			if len(mapping) > 0 {
				catalogSource = "configured"
			}
		case map[string]string:
			if len(mapping) > 0 {
				catalogSource = "configured"
			}
		}
	}
	respond := func(models any) {
		body, err := json.Marshal(models)
		if err != nil {
			_ = c.Error(err)
			response.InternalError(c, "Failed to encode model catalog")
			return
		}
		entries := make([]map[string]any, 0)
		if err := json.Unmarshal(body, &entries); err != nil {
			_ = c.Error(err)
			response.InternalError(c, "Failed to encode model catalog")
			return
		}
		if entries == nil {
			entries = make([]map[string]any, 0)
		}
		c.Header("X-Model-Catalog-Source", catalogSource)
		snapshot := account.GetUpstreamModelMetadataSnapshot()
		for _, entry := range entries {
			if _, present := entry["catalog_source"]; !present {
				entry["catalog_source"] = catalogSource
			}
			id, _ := entry["id"].(string)
			entry["probe_supported"] = service.AccountModelProbeSupported(account, id)
			metadata := service.UpstreamModelMetadata{}
			if snapshot != nil {
				metadata = snapshot.Models[account.GetMappedModel(id)]
			}
			entry["category"] = service.ModelDisplayCategory(metadata)
			if snapshot != nil {
				if _, found := snapshot.Models[account.GetMappedModel(id)]; found {
					entry["metadata_source"] = snapshot.Source
				}
			}
		}
		response.Success(c, entries)
	}

	if account.IsCursor() {
		ids := service.DefaultCursorModelIDs()
		if h.accountTestService != nil {
			if live, err := h.accountTestService.FetchUpstreamSupportedModels(c.Request.Context(), account); err == nil && len(live) > 0 {
				ids = live
				catalogSource = "live"
			}
		}
		if mapping := account.GetModelMapping(); len(mapping) > 0 {
			ids = nil
			catalogSource = "configured"
			for model := range mapping {
				ids = append(ids, model)
			}
		}
		models := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			models = append(models, map[string]string{"id": id, "display_name": id})
		}
		respond(models)
		return
	}
	// Handle OpenAI accounts
	if account.IsOpenAI() {
		// Prefer the shared, account-keyed upstream catalog. If discovery fails,
		// retain the legacy local catalog below so the test dialog remains usable.
		if h.accountTestService != nil {
			if models, fetchErr := h.accountTestService.FetchOpenAIAccountModels(c.Request.Context(), account); fetchErr == nil {
				catalogSource = "live"
				respond(models)
				return
			}
		}
		// OpenAI 自动透传会绕过常规模型改写，测试/模型列表也应回落到默认模型集。
		if account.IsOpenAIPassthroughEnabled() {
			catalogSource = "preset"
			respond(service.OpenAIPresetModels())
			return
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			respond(service.OpenAIPresetModels())
			return
		}

		// Return mapped models
		var models []openai.Model
		for requestedModel := range mapping {
			var found bool
			for _, dm := range service.OpenAIPresetModels() {
				if dm.ID == requestedModel {
					models = append(models, dm)
					found = true
					break
				}
			}
			if !found {
				models = append(models, openai.Model{
					ID:          requestedModel,
					Object:      "model",
					Type:        "model",
					DisplayName: requestedModel,
				})
			}
		}
		respond(models)
		return
	}

	// Handle Gemini accounts
	if account.IsGemini() {
		// Consumer Google One OAuth still uses the legacy Gemini CLI / Code
		// Assist channel. Do not advertise newer 3.x or image models that the
		// channel cannot serve.
		if account.IsOAuth() && catalogSource != "configured" {
			if account.IsGeminiGoogleOne() {
				respond(service.GoogleOnePresetModels())
				return
			}
			respond(service.GeminiPresetModels())
			return
		}

		// For API Key accounts: return models based on model_mapping
		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			respond(service.GeminiPresetModels())
			return
		}

		var models []geminicli.Model
		for requestedModel := range mapping {
			var found bool
			for _, dm := range service.GeminiPresetModels() {
				if dm.ID == requestedModel {
					models = append(models, dm)
					found = true
					break
				}
			}
			if !found {
				models = append(models, geminicli.Model{
					ID:          requestedModel,
					Type:        "model",
					DisplayName: requestedModel,
					CreatedAt:   "",
				})
			}
		}
		respond(models)
		return
	}

	// Handle Antigravity accounts: return Claude + Gemini models
	if account.Platform == service.PlatformAntigravity && catalogSource != "configured" {
		// 直接复用 service.AntigravityPresetModels()，与 /v1/models 端点保持同步
		respond(service.AntigravityPresetModels())
		return
	}

	// Handle Grok accounts
	if account.Platform == service.PlatformGrok {
		defaultModels := service.GrokPresetModels()

		hasExplicitMapping := false
		switch rawMapping := account.Credentials["model_mapping"].(type) {
		case map[string]any:
			hasExplicitMapping = len(rawMapping) > 0
		case map[string]string:
			hasExplicitMapping = len(rawMapping) > 0
		}
		if !hasExplicitMapping {
			respond(defaultModels)
			return
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			respond(defaultModels)
			return
		}

		defaultByID := make(map[string]xai.Model, len(defaultModels))
		for _, model := range defaultModels {
			defaultByID[model.ID] = model
		}

		requestedModels := make([]string, 0, len(mapping))
		for requestedModel := range mapping {
			requestedModels = append(requestedModels, requestedModel)
		}
		sort.Strings(requestedModels)

		var models []xai.Model
		for _, requestedModel := range requestedModels {
			if defaultModel, found := defaultByID[requestedModel]; found {
				models = append(models, defaultModel)
				continue
			}
			models = append(models, xai.Model{
				ID:          requestedModel,
				Object:      "model",
				OwnedBy:     "xai",
				DisplayName: requestedModel,
			})
		}
		respond(models)
		return
	}

	defaults := service.NewModelRegistry().Snapshot().CandidateIDs(account.Platform, "")
	source := "preset"
	if mapping := account.GetModelMapping(); len(mapping) > 0 {
		defaults = make([]string, 0, len(mapping))
		for id := range mapping {
			defaults = append(defaults, id)
		}
		sort.Strings(defaults)
		source = "configured"
	}
	models := make([]map[string]string, 0, len(defaults))
	for _, id := range defaults {
		models = append(models, map[string]string{"id": id, "display_name": id, "catalog_source": source})
	}
	respond(models)
}

// SyncUpstreamModels handles syncing live supported models from an account's upstream.
// POST /api/v1/admin/accounts/:id/models/sync-upstream
func (h *AccountHandler) SyncUpstreamModels(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	if h.accountTestService == nil {
		response.InternalError(c, "Account test service is not configured")
		return
	}

	apply := false
	if raw, exists := c.GetQuery("apply"); exists {
		apply, err = strconv.ParseBool(raw)
		if err != nil {
			response.BadRequest(c, "Invalid apply parameter")
			return
		}
	}
	var expected *int64
	if raw, exists := c.GetQuery("expected_rules_revision"); exists {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 0 {
			response.BadRequest(c, "Invalid expected_rules_revision")
			return
		}
		expected = &value
	}
	var req SyncUpstreamModelsRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid model capability declaration request")
			return
		}
	}
	if apply && req.Review == nil {
		response.BadRequest(c, "A complete review is required; preview the account catalog before applying")
		return
	}
	if len(req.ModelCapabilityDefaults) > 0 {
		if apply {
			response.BadRequest(c, "Save model_capability_defaults on the account before applying its catalog")
			return
		}
		declaration, decodeErr := decodeModelCapabilityDefaults(req.ModelCapabilityDefaults)
		if decodeErr != nil {
			response.BadRequest(c, decodeErr.Error())
			return
		}
		// Never mutate the repository-owned account or its shared Extra map in preview.
		previewAccount := *account
		previewAccount.Extra = make(map[string]any, len(account.Extra)+1)
		for key, value := range account.Extra {
			previewAccount.Extra[key] = value
		}
		previewAccount.Extra["model_capability_defaults"] = declaration
		if err := bindModelCapabilityDefaults(&previewAccount, previewAccount.Extra, adminActorScope(c)); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		account = &previewAccount
	} else if err := validateSavedModelCapabilityIdentity(account); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if expected != nil && req.Review != nil && *expected != req.Review.RulesRevision {
		response.BadRequest(c, "expected_rules_revision must match review")
		return
	}
	var catalog *service.UpstreamModelCatalog
	if apply {
		if req.Review != nil {
			catalog, err = h.accountTestService.ApplyReviewedAccountModelCatalog(c.Request.Context(), account, req.Review)
		} else {
			response.BadRequest(c, "A complete review is required; preview the account catalog before applying")
			return
		}
	} else {
		catalog, err = h.accountTestService.PreviewAccountModelCatalog(c.Request.Context(), account)
	}
	if err != nil {
		writeUpstreamModelSyncError(c, err)
		return
	}

	response.Success(c, catalog)
}

// SyncUpstreamModelsPreview handles syncing live supported models using provided credentials (no account ID needed).
// POST /api/v1/admin/accounts/models/sync-upstream-preview
func (h *AccountHandler) SyncUpstreamModelsPreview(c *gin.Context) {
	var req SyncUpstreamModelsPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	declaration, err := decodeModelCapabilityDefaults(req.ModelCapabilityDefaults)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	modelMapping := make(map[string]any, len(req.ModelMapping))
	for sourceModel, upstreamModel := range req.ModelMapping {
		modelMapping[sourceModel] = upstreamModel
	}

	// Only connection fields are accepted: previews must not resolve saved
	// credential shadows or acquire another account's persistence identity.
	credentials := make(map[string]any)
	for _, key := range []string{"api_key", "base_url", "access_token", "expires_at", "machine_id", "mac_machine_id", "client_version", "account_mode", "api_protocol", "api_base_urls", "model_mapping", "header_override_enabled", "header_overrides"} {
		if value, ok := req.Credentials[key]; ok {
			credentials[key] = value
		}
	}
	if req.APIKey != "" {
		credentials["api_key"] = req.APIKey
	}
	if req.BaseURL != "" {
		credentials["base_url"] = req.BaseURL
	}
	if req.ModelMapping != nil {
		credentials["model_mapping"] = modelMapping
	}
	tempAccount := &service.Account{
		ProxyID: req.ProxyID, ProxyGroupID: req.ProxyGroupID,
		Platform:    req.Platform,
		Type:        req.Type,
		Credentials: credentials,
		Extra:       map[string]any{"model_capability_defaults": declaration},
	}
	if req.CredentialDraft != "" {
		if err := h.resolveCredentialDraftPreview(c, req, tempAccount); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	if req.ProxyID != nil && *req.ProxyID > 0 {
		proxy, lookupErr := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if lookupErr != nil {
			response.ErrorFrom(c, lookupErr)
			return
		}
		tempAccount.Proxy = proxy
	}
	if h.accountTestService == nil {
		response.InternalError(c, "Account test service is not configured")
		return
	}

	if err := bindModelCapabilityDefaults(tempAccount, tempAccount.Extra, adminActorScope(c)); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.credentialDrafts.PreparePreviewEgress(c.Request.Context(), tempAccount); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	catalog, err := h.accountTestService.PreviewAccountModelCatalog(c.Request.Context(), tempAccount)
	if err != nil {
		writeUpstreamModelSyncError(c, err)
		return
	}

	response.Success(c, catalog)
}

func writeUpstreamModelSyncError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrAccountModelCatalogConflict) {
		response.ErrorWithDetails(c, http.StatusConflict, "Account configuration or catalog changed; preview again", "upstream_model_sync_conflict", nil)
		return
	}
	status, kind, message := http.StatusBadGateway, service.UpstreamModelSyncErrorUpstream, "Failed to sync upstream models from upstream"
	var syncErr *service.UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		kind, message = syncErr.Kind, syncErr.SafeMessage()
		switch kind {
		case service.UpstreamModelSyncErrorConfiguration, service.UpstreamModelSyncErrorUnsupported, service.UpstreamModelSyncErrorCredentials,
			service.UpstreamModelSyncErrorAccountType, service.UpstreamModelSyncErrorNotAdapted, service.UpstreamModelSyncErrorDiscoveryUnconfirmed,
			service.UpstreamModelSyncErrorEndpointNotFound, service.UpstreamModelSyncErrorMethodNotAllowed:
			status = http.StatusBadRequest
		case service.UpstreamModelSyncErrorInternal:
			status = http.StatusInternalServerError
		}
		if kind == service.UpstreamModelSyncErrorCredentials {
			message = "Upstream rejected the account credentials"
		}
	}
	response.ErrorWithDetails(c, status, message, "upstream_model_sync_"+string(kind), nil)
}

// SetPrivacy handles setting privacy for a single OpenAI/Antigravity OAuth account
// POST /api/v1/admin/accounts/:id/set-privacy
func (h *AccountHandler) SetPrivacy(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "Only OAuth accounts support privacy setting")
		return
	}
	mode, err := h.setCredentialPrivacy(c, account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 从 DB 重新读取以确保返回最新状态
	updated, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		// 隐私已设置成功但读取失败，回退到内存更新
		if account.Extra == nil {
			account.Extra = make(map[string]any)
		}
		account.Extra["privacy_mode"] = mode
		response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), updated))
}

// RefreshTier handles refreshing Google One tier for a single account
// POST /api/v1/admin/accounts/:id/refresh-tier
func (h *AccountHandler) RefreshTier(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	ctx := c.Request.Context()
	account, err := h.adminService.GetAccount(ctx, accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	if account.Platform != service.PlatformGemini || account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "Only Gemini OAuth accounts support tier refresh")
		return
	}

	oauthType, _ := account.Credentials["oauth_type"].(string)
	if oauthType != "google_one" {
		response.BadRequest(c, "Only google_one OAuth accounts support tier refresh")
		return
	}

	tierID, extra, creds, err := h.geminiOAuthService.RefreshAccountGoogleOneTier(ctx, account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	_, updateErr := h.adminService.UpdateAccount(ctx, accountID, &service.UpdateAccountInput{
		Credentials: creds,
		Extra:       extra,
	})
	if updateErr != nil {
		response.ErrorFrom(c, updateErr)
		return
	}

	response.Success(c, gin.H{
		"tier_id":             tierID,
		"storage_info":        extra,
		"drive_storage_limit": extra["drive_storage_limit"],
		"drive_storage_usage": extra["drive_storage_usage"],
		"updated_at":          extra["drive_tier_updated_at"],
	})
}

// BatchRefreshTierRequest represents batch tier refresh request
type BatchRefreshTierRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

// BatchRefreshTier handles batch refreshing Google One tier
// POST /api/v1/admin/accounts/batch-refresh-tier
func (h *AccountHandler) BatchRefreshTier(c *gin.Context) {
	var req BatchRefreshTierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = BatchRefreshTierRequest{}
	}

	ctx := c.Request.Context()
	accounts := make([]*service.Account, 0)

	if len(req.AccountIDs) == 0 {
		allAccounts, _, err := h.adminService.ListAccounts(ctx, 1, 10000, "gemini", "oauth", "", "", 0, "", "name", "asc")
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		for i := range allAccounts {
			acc := &allAccounts[i]
			oauthType, _ := acc.Credentials["oauth_type"].(string)
			if oauthType == "google_one" {
				accounts = append(accounts, acc)
			}
		}
	} else {
		fetched, err := h.adminService.GetAccountsByIDs(ctx, req.AccountIDs)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		for _, acc := range fetched {
			if acc == nil {
				continue
			}
			if acc.Platform != service.PlatformGemini || acc.Type != service.AccountTypeOAuth {
				continue
			}
			oauthType, _ := acc.Credentials["oauth_type"].(string)
			if oauthType != "google_one" {
				continue
			}
			accounts = append(accounts, acc)
		}
	}

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	var successCount, failedCount int
	var errors []gin.H

	for _, account := range accounts {
		acc := account // 闭包捕获
		g.Go(func() error {
			_, extra, creds, err := h.geminiOAuthService.RefreshAccountGoogleOneTier(gctx, acc)
			if err != nil {
				mu.Lock()
				failedCount++
				errors = append(errors, gin.H{
					"account_id": acc.ID,
					"error":      err.Error(),
				})
				mu.Unlock()
				return nil
			}

			_, updateErr := h.adminService.UpdateAccount(gctx, acc.ID, &service.UpdateAccountInput{
				Credentials: creds,
				Extra:       extra,
			})

			mu.Lock()
			if updateErr != nil {
				failedCount++
				errors = append(errors, gin.H{
					"account_id": acc.ID,
					"error":      updateErr.Error(),
				})
			} else {
				successCount++
			}
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	results := gin.H{
		"total":   len(accounts),
		"success": successCount,
		"failed":  failedCount,
		"errors":  errors,
	}

	response.Success(c, results)
}

// GetAntigravityDefaultModelMapping 获取 Antigravity 平台的默认模型映射
// GET /api/v1/admin/accounts/antigravity/default-model-mapping
func (h *AccountHandler) GetAntigravityDefaultModelMapping(c *gin.Context) {
	response.Success(c, domain.DefaultAntigravityModelMapping)
}

// sanitizeExtraBaseRPM 对 extra map 中的 base_rpm 值进行范围校验和归一化。
// 负值归零，超过 10000 截断为 10000。extra 为 nil 或不含 base_rpm 时无操作。
func sanitizeExtraBaseRPM(extra map[string]any) {
	if extra == nil {
		return
	}
	raw, ok := extra["base_rpm"]
	if !ok {
		return
	}
	v := service.ParseExtraInt(raw)
	if v < 0 {
		v = 0
	} else if v > 10000 {
		v = 10000
	}
	extra["base_rpm"] = v
}

// GetModelCatalog reads applied evidence and a separate, non-blocking public capability display projection.
func (h *AccountHandler) GetModelCatalog(c *gin.Context) {
	h.getModelCatalog(c, time.Now())
}

func (h *AccountHandler) getModelCatalog(c *gin.Context, now time.Time) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	snapshot := account.GetUpstreamModelCatalog()
	effective := account.EffectiveUpstreamModelCatalog()
	if snapshot != nil && account.IsShadow() {
		credentialAccount, lookupErr := h.adminService.GetAccount(c.Request.Context(), *account.ParentAccountID)
		if lookupErr != nil {
			response.ErrorFrom(c, lookupErr)
			return
		}
		effective = account.EffectiveUpstreamModelCatalogForCredentialAccount(credentialAccount)
	}
	state := account.ModelCatalogState()
	staleness, lastSuccess := account.ModelCatalogFreshness(now)
	summary := gin.H{
		"snapshot": modelCatalogSnapshotResponse(snapshot), "effective_snapshot": modelCatalogSnapshotResponse(effective),
		"state":    modelCatalogStateResponse{AccountModelCatalogState: state, IdentityRevision: strconv.FormatInt(state.IdentityRevision, 10), RulesRevision: strconv.FormatInt(state.RulesRevision, 10)},
		"baseline": account.ModelCatalogBaseline(), "synced": snapshot != nil,
		"complete": false, "observed_at": nil, "snapshot_revision": nil,
		"discovered_count": 0, "eligible_count": 0, "excluded_count": 0,
		"last_error": nil, "warnings": []string{},
		"staleness": staleness, "last_success_at": nil, "last_attempt_at": nil,
		"invalid_aliases": service.InvalidAccountCatalogAliases(account, effective),
		"alias_endpoint":  service.CompositeRouteEndpointAny,
		"eligible_models": []service.AccountCatalogModel{},
	}
	if display := h.accountTestService.ReadOnlyModelCapabilities(account); len(display) > 0 {
		summary["display_metadata"] = display
	}
	if lastSuccess != "" {
		summary["last_success_at"] = lastSuccess
	}
	if state.AttemptedAt != "" {
		summary["last_attempt_at"] = state.AttemptedAt
	}
	if state.Error != "" {
		summary["last_error"] = state.Error
	}
	if snapshot != nil {
		summary["complete"] = snapshot.Complete
		summary["observed_at"] = snapshot.ObservedAt
		summary["snapshot_revision"] = strconv.FormatInt(snapshot.Revision, 10)
		summary["discovered_count"] = len(snapshot.DiscoveredModels)
		summary["excluded_count"] = len(snapshot.ExcludedModels)
		if effective == nil {
			summary["warnings"] = []string{string(service.CatalogReasonIdentityChanged)}
		}
	}
	if effective != nil {
		summary["eligible_count"] = len(effective.EligibleModels)
		summary["eligible_models"] = effective.EligibleModels
	}
	if staleness == "stale" {
		warnings, _ := summary["warnings"].([]string)
		summary["warnings"] = append(warnings, string(service.CatalogReasonStaleEvidence))
	}
	response.Success(c, summary)
}

// Keep storage revisions numeric internally and expose lossless decimal strings at the HTTP boundary.
type modelCatalogSnapshotDTO struct {
	*service.AccountModelCatalogSnapshot
	Revision                   string `json:"revision"`
	RulesRevision              string `json:"rules_revision"`
	IdentityRevision           string `json:"identity_revision"`
	CredentialIdentityRevision string `json:"credential_identity_revision,omitempty"`
}

func modelCatalogSnapshotResponse(snapshot *service.AccountModelCatalogSnapshot) *modelCatalogSnapshotDTO {
	if snapshot == nil {
		return nil
	}
	result := &modelCatalogSnapshotDTO{AccountModelCatalogSnapshot: snapshot, Revision: strconv.FormatInt(snapshot.Revision, 10), RulesRevision: strconv.FormatInt(snapshot.RulesRevision, 10), IdentityRevision: strconv.FormatInt(snapshot.IdentityRevision, 10)}
	if snapshot.CredentialIdentityRevision != 0 {
		result.CredentialIdentityRevision = strconv.FormatInt(snapshot.CredentialIdentityRevision, 10)
	}
	return result
}

type modelCatalogStateResponse struct {
	service.AccountModelCatalogState
	IdentityRevision string `json:"identity_revision"`
	RulesRevision    string `json:"rules_revision"`
}

func (h *AccountHandler) GetPlatformModelPresets(c *gin.Context) {
	presets, err := service.PlatformModelPresets()
	if err != nil {
		response.InternalError(c, "Platform presets unavailable")
		return
	}
	response.Success(c, presets)
}

func (h *AccountHandler) UpdateModelAllowlist(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	var request struct {
		Enabled               bool               `json:"enabled"`
		Models                []string           `json:"models"`
		Reason                string             `json:"reason"`
		ExpectedRulesRevision *json.Number       `json:"expected_rules_revision"`
		ModelMapping          *map[string]string `json:"model_mapping"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.ExpectedRulesRevision == nil {
		response.BadRequest(c, "Scope and expected_rules_revision are required")
		return
	}
	expectedRevision, parseErr := request.ExpectedRulesRevision.Int64()
	if parseErr != nil || expectedRevision < 0 || expectedRevision == int64(^uint64(0)>>1) {
		response.BadRequest(c, "Invalid expected_rules_revision")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.accountTestService == nil {
		response.InternalError(c, "Account test service unavailable")
		return
	}
	notDiscovered, err := h.accountTestService.ConfigureAccountModelAllowlist(c.Request.Context(), account, request.Enabled, request.Models, request.Reason, subject.UserID, expectedRevision, request.ModelMapping)
	if err != nil {
		writeUpstreamModelSyncError(c, err)
		return
	}
	fresh, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"rules_revision": strconv.FormatInt(fresh.ModelCatalogBaseline().RulesRevision, 10),
		"not_discovered": notDiscovered,
		"scope":          fresh.Extra[service.UpstreamModelAllowlistExtraKey],
	})
}
