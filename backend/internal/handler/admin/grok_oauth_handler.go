package admin

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/xai"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

const grokSSOImportConcurrency = 3

type GrokOAuthHandler struct {
	credentialDraftHandler
	grokOAuthService *service.GrokOAuthService
	adminService     service.AdminService
	quotaService     *service.GrokQuotaService
	importProber     grokImportProber
	reconciler       service.GrokOAuthReconciler
}

func NewGrokOAuthHandler(
	grokOAuthService *service.GrokOAuthService,
	adminService service.AdminService,
	quotaService *service.GrokQuotaService,
	reconciler service.GrokOAuthReconciler,
) *GrokOAuthHandler {
	return &GrokOAuthHandler{
		grokOAuthService: grokOAuthService,
		adminService:     adminService,
		quotaService:     quotaService,
		importProber:     quotaService,
		reconciler:       reconciler,
	}
}

type GrokGenerateAuthURLRequest struct {
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
	RedirectURI  string `json:"redirect_uri"`
}

func (h *GrokOAuthHandler) GetCapabilities(c *gin.Context) {
	response.Success(c, h.grokOAuthService.GetCapabilities())
}

func (h *GrokOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req GrokGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = GrokGenerateAuthURLRequest{}
	}
	result, err := h.grokOAuthService.GenerateAuthURL(c.Request.Context(), req.ProxyID, req.RedirectURI, req.ProxyGroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	reference, err := beginCredentialAuthorization(c, h.credentialDrafts, "grok:oauth", result.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result.SessionID = reference
	response.Success(c, result)
}

type GrokExchangeCodeRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	Code         string `json:"code" binding:"required"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

func (h *GrokOAuthHandler) ExchangeCode(c *gin.Context) {
	var req GrokExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	sessionID, err := consumeCredentialAuthorization(c, h.credentialDrafts, "grok:oauth", req.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	req.SessionID = sessionID

	tokenInfo, err := h.grokOAuthService.ExchangeCode(c.Request.Context(), &service.GrokExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID, ProxyGroupID: req.ProxyGroupID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	respondCredentialDraft(c, h.credentialDrafts, service.PlatformGrok, service.AccountTypeOAuth, h.grokOAuthService.BuildAccountCredentials(tokenInfo), tokenInfo.Egress)
}

type GrokRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
	RT           string `json:"rt"`
	ClientID     string `json:"client_id"`
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

type GrokSSOTokenRequest struct {
	SSOToken     string `json:"sso_token"`
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

type GrokPasswordAuthorizeRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	ProxyID      *int64 `json:"proxy_id"`
	ProxyGroupID *int64 `json:"proxy_group_id"`
}

func (h *GrokOAuthHandler) RefreshToken(c *gin.Context) {
	var req GrokRefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RT)
	}
	if refreshToken == "" {
		response.BadRequest(c, "refresh_token is required")
		return
	}

	selection, egressErr := h.credentialDrafts.ResolveAuthorizationEgress(c.Request.Context(), req.ProxyID, req.ProxyGroupID)
	if egressErr != nil {
		response.ErrorFrom(c, egressErr)
		return
	}
	var proxyURL string
	if selection != nil {
		proxyURL = selection.URL
	}
	if req.ProxyID != nil && *req.ProxyID > 0 {
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if proxy == nil {
			response.BadRequest(c, "GROK_OAUTH_PROXY_NOT_FOUND: proxy not found")
			return
		}
		proxyURL = proxy.URL()
	}
	tokenInfo, err := h.grokOAuthService.RefreshToken(c.Request.Context(), refreshToken, proxyURL, req.ClientID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	tokenInfo.Egress = selection
	respondCredentialDraft(c, h.credentialDrafts, service.PlatformGrok, service.AccountTypeOAuth, h.grokOAuthService.BuildAccountCredentials(tokenInfo), tokenInfo.Egress)
}

// ValidateSSOToken converts a Web SSO cookie into Build OAuth tokens.
// Response contains OAuth token info only — never echoes sso_token.
func (h *GrokOAuthHandler) ValidateSSOToken(c *gin.Context) {
	var req GrokSSOTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tokenInfo, err := h.grokOAuthService.ValidateSSOToken(c.Request.Context(), req.SSOToken, req.ProxyID, req.ProxyGroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	respondCredentialDraft(c, h.credentialDrafts, service.PlatformGrok, service.AccountTypeOAuth, h.grokOAuthService.BuildAccountCredentials(tokenInfo), tokenInfo.Egress)
}

// AuthorizePassword exchanges email/password for Build OAuth tokens via SSO conversion.
// Response never includes password or raw sso_token.
func (h *GrokOAuthHandler) AuthorizePassword(c *gin.Context) {
	var req GrokPasswordAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tokenInfo, err := h.grokOAuthService.AuthorizePassword(c.Request.Context(), req.Email, req.Password, req.ProxyID, req.ProxyGroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	respondCredentialDraft(c, h.credentialDrafts, service.PlatformGrok, service.AccountTypeOAuth, h.grokOAuthService.BuildAccountCredentials(tokenInfo), tokenInfo.Egress)
}

func (h *GrokOAuthHandler) RefreshAccountToken(c *gin.Context) {
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
	if account.Platform != service.PlatformGrok {
		response.BadRequest(c, "Account platform does not match Grok OAuth endpoint")
		return
	}
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}
	tokenInfo, err := h.grokOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	newCredentials := h.grokOAuthService.BuildAccountCredentials(tokenInfo)
	newCredentials = service.MergeCredentials(account.Credentials, newCredentials)
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		newCredentials["base_url"] = baseURL
	}
	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(updatedAccount))
}

type GrokOAuthReconcileRequest struct {
	DryRun               *bool `json:"dry_run"`
	Apply                bool  `json:"apply"`
	AfterID              int64 `json:"after_id"`
	Limit                int   `json:"limit"`
	RefreshWindowSeconds int64 `json:"refresh_window_seconds"`
}

func (h *GrokOAuthHandler) ReconcileOAuthAccounts(c *gin.Context) {
	var req GrokOAuthReconcileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	if req.Apply == dryRun {
		response.ErrorFrom(c, service.ErrGrokOAuthReconcileMode)
		return
	}
	if req.RefreshWindowSeconds < 0 || req.RefreshWindowSeconds > int64((24*time.Hour)/time.Second) {
		response.ErrorFrom(c, service.ErrGrokOAuthReconcileWindow)
		return
	}
	if h.reconciler == nil {
		response.InternalError(c, "Grok OAuth reconciliation service is unavailable")
		return
	}
	result, err := h.reconciler.ReconcileGrokOAuth(c.Request.Context(), service.GrokOAuthReconcileInput{
		DryRun:        dryRun,
		Apply:         req.Apply,
		AfterID:       req.AfterID,
		Limit:         req.Limit,
		RefreshWindow: time.Duration(req.RefreshWindowSeconds) * time.Second,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	var req struct {
		SessionID    string  `json:"session_id" binding:"required"`
		Code         string  `json:"code" binding:"required"`
		State        string  `json:"state"`
		RedirectURI  string  `json:"redirect_uri"`
		ProxyID      *int64  `json:"proxy_id"`
		ProxyGroupID *int64  `json:"proxy_group_id"`
		Name         string  `json:"name"`
		Concurrency  int     `json:"concurrency"`
		Priority     int     `json:"priority"`
		GroupIDs     []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	sessionID, err := consumeCredentialAuthorization(c, h.credentialDrafts, "grok:oauth", req.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	req.SessionID = sessionID
	tokenInfo, err := h.grokOAuthService.ExchangeCode(c.Request.Context(), &service.GrokExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID, ProxyGroupID: req.ProxyGroupID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	respondCredentialDraft(c, h.credentialDrafts, service.PlatformGrok, service.AccountTypeOAuth, h.grokOAuthService.BuildAccountCredentials(tokenInfo), tokenInfo.Egress)
}

type GrokSSOToOAuthRequest struct {
	TPMLimit           service.OptionalTPMLimit `json:"tpm_limit,omitzero"`
	SSOTokens          []string                 `json:"sso_tokens"`
	SSOToken           string                   `json:"sso_token"`
	Name               string                   `json:"name"`
	Notes              *string                  `json:"notes"`
	ProxyID            *int64                   `json:"proxy_id"`
	ProxyGroupID       *int64                   `json:"proxy_group_id"`
	GroupIDs           []int64                  `json:"group_ids"`
	Credentials        map[string]any           `json:"credentials"`
	Extra              map[string]any           `json:"extra"`
	Concurrency        int                      `json:"concurrency"`
	LoadFactor         *int                     `json:"load_factor"`
	Priority           int                      `json:"priority"`
	RateMultiplier     *float64                 `json:"rate_multiplier"`
	ExpiresAt          *int64                   `json:"expires_at"`
	AutoPauseOnExpired *bool                    `json:"auto_pause_on_expired"`
}

type GrokSSOToOAuthItemResult struct {
	Draft   *service.AccountCredentialDraft `json:"draft,omitempty"`
	Index   int                             `json:"index"`
	Name    string                          `json:"name,omitempty"`
	Email   string                          `json:"email,omitempty"`
	Account *dto.Account                    `json:"account,omitempty"`
	Error   string                          `json:"error,omitempty"`
}

type GrokSSOToOAuthResponse struct {
	Ready  []GrokSSOToOAuthItemResult `json:"ready"`
	Failed []GrokSSOToOAuthItemResult `json:"failed"`
}

type grokSSOImportJob struct {
	index int
	token string
}

type grokSSOImportWorkerResult struct {
	created bool
	item    GrokSSOToOAuthItemResult
}

func (h *GrokOAuthHandler) CreateAccountsFromSSO(c *gin.Context) {
	var req GrokSSOToOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tokens := normalizeSSOImportTokens(req.SSOTokens, req.SSOToken)
	if len(tokens) == 0 {
		response.BadRequest(c, "sso_tokens is required")
		return
	}
	binding, err := credentialDraftBinding(c, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := requireCredentialDraftStore(h.credentialDrafts); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	ctx := c.Request.Context()
	workerCount := grokSSOImportConcurrency
	if len(tokens) < workerCount {
		workerCount = len(tokens)
	}
	jobs := make(chan grokSSOImportJob)
	items := make([]grokSSOImportWorkerResult, len(tokens))
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				items[job.index] = h.safeCreateAccountFromSSOToken(ctx, req, job.token, job.index+1, len(tokens), binding)
			}
		}()
	}
	for i, token := range tokens {
		jobs <- grokSSOImportJob{index: i, token: token}
	}
	close(jobs)
	wg.Wait()

	result := GrokSSOToOAuthResponse{
		Ready:  make([]GrokSSOToOAuthItemResult, 0, len(tokens)),
		Failed: make([]GrokSSOToOAuthItemResult, 0),
	}
	for _, item := range items {
		if item.created {
			result.Ready = append(result.Ready, item.item)
		} else {
			result.Failed = append(result.Failed, item.item)
		}
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) safeCreateAccountFromSSOToken(ctx context.Context, req GrokSSOToOAuthRequest, token string, index, total int, binding string) (result grokSSOImportWorkerResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("grok_sso_import_worker_panic", "index", index, "recover", recovered)
			result = grokSSOImportWorkerResult{
				item: GrokSSOToOAuthItemResult{
					Index: index,
					Error: fmt.Sprintf("internal worker panic: %v", recovered),
				},
			}
		}
	}()
	return h.createAccountFromSSOToken(ctx, req, token, index, total, binding)
}

func (h *GrokOAuthHandler) createAccountFromSSOToken(ctx context.Context, req GrokSSOToOAuthRequest, token string, index, total int, binding string) grokSSOImportWorkerResult {
	tokenInfo, err := h.grokOAuthService.ConvertFromSSO(ctx, token, req.ProxyID, req.ProxyGroupID)
	if err != nil {
		return grokSSOImportWorkerResult{item: GrokSSOToOAuthItemResult{Index: index, Error: grokSSOImportErrorMessage(err)}}
	}

	credentials := grokSSOImportCredentials(h.grokOAuthService.BuildAccountCredentials(tokenInfo), req.Credentials)
	name := grokSSOImportAccountName(req.Name, tokenInfo, index, total)
	draft, err := h.credentialDrafts.IssueAccountCredentialDraft(ctx, binding, service.PlatformGrok, service.AccountTypeOAuth, credentials, tokenInfo.Egress)
	if err != nil {
		return grokSSOImportWorkerResult{item: GrokSSOToOAuthItemResult{Index: index, Name: name, Email: tokenInfo.Email, Error: grokSSOImportErrorMessage(err)}}
	}
	return grokSSOImportWorkerResult{
		created: true,
		item: GrokSSOToOAuthItemResult{
			Index: index,
			Name:  name,
			Email: tokenInfo.Email,
			Draft: draft,
		},
	}
}

// grokSSOImportCredentials 合并 SSO 兑换出的凭据与导入请求携带的运营侧配置。
// token 字段以 BuildAccountCredentials 为准（请求不可覆盖）；但 base_url 是运营侧
// 配置且 Build 恒写官方地址，会吞掉导入时指定的自定义转发地址——与
// RefreshAccountToken 的保留逻辑对齐，请求显式提供时以请求为准。
func grokSSOImportCredentials(built map[string]any, reqCredentials map[string]any) map[string]any {
	// Only merge operator config from the request — never free-form secrets
	// (password / sso_token / cookie / etc.) into stored credentials.
	allowedReqKeys := map[string]struct{}{
		"base_url": {}, "model_mapping": {},
		"header_override": {}, "header_overrides": {}, "header_override_enabled": {},
		"custom_headers": {},
	}
	ops := map[string]any{}
	for k, v := range reqCredentials {
		if _, ok := allowedReqKeys[k]; !ok {
			continue
		}
		if service.IsSensitiveCredentialKey(k) {
			continue
		}
		ops[k] = v
	}
	credentials := service.MergeCredentials(ops, built)
	// Strip any sensitive keys that might have slipped in via older callers.
	for k := range credentials {
		if service.IsSensitiveCredentialKey(k) {
			// Keep only keys produced by BuildAccountCredentials (tokens).
			if k == "access_token" || k == "refresh_token" || k == "id_token" {
				continue
			}
			delete(credentials, k)
		}
	}
	if reqBaseURL, ok := reqCredentials["base_url"].(string); ok && strings.TrimSpace(reqBaseURL) != "" {
		credentials["base_url"] = strings.TrimSpace(reqBaseURL)
	}
	return service.SanitizeStoredCredentials(service.PlatformGrok, credentials)
}

//nolint:unused // referenced only from //go:build unit code, so the default lint pass cannot see its callers.
func grokSSOImportExpiry(requestExpiresAt *int64, requestAutoPause *bool, tokenInfo *service.GrokTokenInfo) (*int64, *bool) {
	if tokenInfo == nil || strings.TrimSpace(tokenInfo.RefreshToken) != "" || tokenInfo.ExpiresAt <= 0 {
		return requestExpiresAt, requestAutoPause
	}

	expiresAt := tokenInfo.ExpiresAt
	if requestExpiresAt != nil && *requestExpiresAt > 0 && *requestExpiresAt < expiresAt {
		expiresAt = *requestExpiresAt
	}
	autoPause := true
	return &expiresAt, &autoPause
}

func normalizeSSOImportTokens(tokens []string, single string) []string {
	items := make([]string, 0, len(tokens)+1)
	if strings.TrimSpace(single) != "" {
		items = append(items, single)
	}
	items = append(items, tokens...)
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		parts := strings.Split(strings.NewReplacer(",", "\n", "\r", "\n").Replace(item), "\n")
		for _, token := range parts {
			if token = xai.NormalizeSSOToken(token); token == "" {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			result = append(result, token)
		}
	}
	return result
}

func grokSSOImportAccountName(base string, tokenInfo *service.GrokTokenInfo, index, total int) string {
	base = strings.TrimSpace(base)
	if base == "" && tokenInfo != nil {
		base = strings.TrimSpace(tokenInfo.Email)
	}
	if base == "" {
		base = "Grok OAuth Account"
	}
	if total > 1 {
		return base + " #" + strconv.Itoa(index)
	}
	return base
}

func grokSSOImportErrorMessage(err error) string {
	status := infraerrors.FromError(err)
	if status == nil {
		return ""
	}
	if status.Reason != "" {
		return status.Reason + ": " + status.Message
	}
	return status.Message
}

func (h *GrokOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "grok quota service is not enabled")
		return
	}
	result, err := h.quotaService.QueryQuota(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) ResetQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "grok quota service is not enabled")
		return
	}
	// ResetQuota 恒返回 GROK_QUOTA_RESET_UNSUPPORTED（xAI 无 OAuth 配额重置接口），err != nil 恒真为预期。
	//nolint:staticcheck // SA4023
	result, err := h.quotaService.ResetQuota(c.Request.Context(), accountID)
	if err != nil { //nolint:staticcheck // SA4023
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) RuntimeSanity(c *gin.Context) {
	response.Success(c, xai.RuntimeSanity())
}
