package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const (
	oauthPendingBrowserCookiePath = "/api/v1/auth/oauth"
	oauthPendingBrowserCookieName = "oauth_pending_browser_session"
	oauthPendingSessionCookiePath = "/api/v1/auth/oauth"
	oauthPendingSessionCookieName = "oauth_pending_session"
	oauthPromoCodeCookieName      = "oauth_promo_code"
	oauthPendingCookieMaxAgeSec   = 10 * 60
	oauthPendingChoiceStep        = "choose_account_action_required"

	oauthCompletionResponseKey = "completion_response"
	oauthPromoCodeStateKey     = "promo_code"
)

var pendingOAuthCreateAccountPreCommitHook func(context.Context, *dbent.PendingAuthSession) error

type oauthPendingSessionPayload struct {
	Intent                 string
	Identity               service.PendingAuthIdentityKey
	TargetUserID           *int64
	ResolvedEmail          string
	RedirectTo             string
	BrowserSessionKey      string
	UpstreamIdentityClaims map[string]any
	CompletionResponse     map[string]any
}

type oauthAdoptionDecisionRequest struct {
	CompletionCode   string `json:"code,omitempty"`
	AdoptDisplayName *bool  `json:"adopt_display_name,omitempty"`
	AdoptAvatar      *bool  `json:"adopt_avatar,omitempty"`
}

type bindPendingOAuthLoginRequest struct {
	Email            string `json:"email" binding:"required,email"`
	Password         string `json:"password" binding:"required"`
	AdoptDisplayName *bool  `json:"adopt_display_name,omitempty"`
	AdoptAvatar      *bool  `json:"adopt_avatar,omitempty"`
}

type createPendingOAuthAccountRequest struct {
	Email                 string `json:"email" binding:"required,email"`
	VerifyCode            string `json:"verify_code,omitempty"`
	Password              string `json:"password" binding:"required,min=6"`
	TurnstileToken        string `json:"turnstile_token,omitempty"`
	TencentCaptchaTicket  string `json:"tencent_captcha_ticket,omitempty"`
	TencentCaptchaRandstr string `json:"tencent_captcha_randstr,omitempty"`
	InvitationCode        string `json:"invitation_code,omitempty"`
	AffCode               string `json:"aff_code,omitempty"`
	AdoptDisplayName      *bool  `json:"adopt_display_name,omitempty"`
	AdoptAvatar           *bool  `json:"adopt_avatar,omitempty"`
}

type sendPendingOAuthVerifyCodeRequest struct {
	Email                 string `json:"email" binding:"required,email"`
	TurnstileToken        string `json:"turnstile_token,omitempty"`
	TencentCaptchaTicket  string `json:"tencent_captcha_ticket,omitempty"`
	TencentCaptchaRandstr string `json:"tencent_captcha_randstr,omitempty"`
	PendingAuthToken      string `json:"pending_auth_token,omitempty"`
	PendingOAuthToken     string `json:"pending_oauth_token,omitempty"`
}

func (r bindPendingOAuthLoginRequest) adoptionDecision() oauthAdoptionDecisionRequest {
	return oauthAdoptionDecisionRequest{
		AdoptDisplayName: r.AdoptDisplayName,
		AdoptAvatar:      r.AdoptAvatar,
	}
}

func (r createPendingOAuthAccountRequest) adoptionDecision() oauthAdoptionDecisionRequest {
	return oauthAdoptionDecisionRequest{
		AdoptDisplayName: r.AdoptDisplayName,
		AdoptAvatar:      r.AdoptAvatar,
	}
}

func (h *AuthHandler) pendingIdentityService() (*service.AuthPendingIdentityService, error) {
	if h == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}
	return h.authService.PendingIdentityService()
}

func generateOAuthPendingBrowserSession() (string, error) {
	return oauth.GenerateState()
}

func setOAuthPendingBrowserCookie(c *gin.Context, sessionKey string, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthPendingBrowserCookieName,
		Value:    encodeCookieValue(sessionKey),
		Path:     oauthPendingBrowserCookiePath,
		MaxAge:   oauthPendingCookieMaxAgeSec,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearOAuthPendingBrowserCookie(c *gin.Context, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthPendingBrowserCookieName,
		Value:    "",
		Path:     oauthPendingBrowserCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readOAuthPendingBrowserCookie(c *gin.Context) (string, error) {
	return readCookieDecoded(c, oauthPendingBrowserCookieName)
}

func setOAuthPendingSessionCookie(c *gin.Context, sessionToken string, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthPendingSessionCookieName,
		Value:    encodeCookieValue(sessionToken),
		Path:     oauthPendingSessionCookiePath,
		MaxAge:   oauthPendingCookieMaxAgeSec,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearOAuthPendingSessionCookie(c *gin.Context, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthPendingSessionCookieName,
		Value:    "",
		Path:     oauthPendingSessionCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearOAuthCompletionCodeCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: "oauth_completion_code", Path: oauthPendingBrowserCookiePath, MaxAge: -1, HttpOnly: true, Secure: isRequestHTTPS(c), SameSite: http.SameSiteLaxMode})
}

func readOAuthPendingSessionCookie(c *gin.Context) (string, error) {
	return readCookieDecoded(c, oauthPendingSessionCookieName)
}

func captureOAuthPromoCode(c *gin.Context, secure bool) {
	promoCode := strings.TrimSpace(c.Query("promo_code"))
	if promoCode == "" {
		clearOAuthPromoCodeCookie(c, secure)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthPromoCodeCookieName,
		Value:    encodeCookieValue(promoCode),
		Path:     oauthPendingBrowserCookiePath,
		MaxAge:   oauthPendingCookieMaxAgeSec,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearOAuthPromoCodeCookie(c *gin.Context, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthPromoCodeCookieName,
		Value:    "",
		Path:     oauthPendingBrowserCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readOAuthPromoCode(c *gin.Context) string {
	if c == nil {
		return ""
	}
	promoCode, err := readCookieDecoded(c, oauthPromoCodeCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(promoCode)
}

func pendingOAuthPromoCode(session *dbent.PendingAuthSession) string {
	if session == nil {
		return ""
	}
	return pendingSessionStringValue(session.LocalFlowState, oauthPromoCodeStateKey)
}

func redirectToFrontendCallback(c *gin.Context, frontendCallback string) {
	u, err := url.Parse(frontendCallback)
	if err != nil {
		c.Redirect(http.StatusFound, linuxDoOAuthDefaultRedirectTo)
		return
	}
	if u.Scheme != "" && !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		c.Redirect(http.StatusFound, linuxDoOAuthDefaultRedirectTo)
		return
	}
	u.Fragment = ""
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Redirect(http.StatusFound, u.String())
}

func (h *AuthHandler) createOAuthPendingSession(c *gin.Context, payload oauthPendingSessionPayload) error {
	svc, err := h.pendingIdentityService()
	if err != nil {
		return err
	}

	localFlowState := map[string]any{
		oauthCompletionResponseKey: payload.CompletionResponse,
	}
	if promoCode := readOAuthPromoCode(c); promoCode != "" {
		localFlowState[oauthPromoCodeStateKey] = promoCode
	}

	session, err := svc.CreatePendingSession(c.Request.Context(), service.CreatePendingAuthSessionInput{
		Intent:                 strings.TrimSpace(payload.Intent),
		Identity:               payload.Identity,
		TargetUserID:           payload.TargetUserID,
		ResolvedEmail:          strings.TrimSpace(payload.ResolvedEmail),
		RedirectTo:             strings.TrimSpace(payload.RedirectTo),
		BrowserSessionKey:      strings.TrimSpace(payload.BrowserSessionKey),
		UpstreamIdentityClaims: payload.UpstreamIdentityClaims,
		LocalFlowState:         localFlowState,
	})
	if err != nil {
		slog.Error("pending auth session create failed",
			"intent", strings.TrimSpace(payload.Intent),
			"provider_type", strings.TrimSpace(payload.Identity.ProviderType),
			"provider_key", strings.TrimSpace(payload.Identity.ProviderKey),
			"provider_subject_len", len(strings.TrimSpace(payload.Identity.ProviderSubject)),
			"resolved_email_len", len(strings.TrimSpace(payload.ResolvedEmail)),
			"has_target_user", payload.TargetUserID != nil,
			"error", err.Error())
		return infraerrors.InternalServer("PENDING_AUTH_SESSION_CREATE_FAILED", "failed to create pending auth session").WithCause(err)
	}

	setOAuthPendingSessionCookie(c, session.SessionToken, isRequestHTTPS(c))
	clearOAuthCompletionCodeCookie(c)
	return nil
}

func readCompletionResponse(session map[string]any) (map[string]any, bool) {
	if len(session) == 0 {
		return nil, false
	}
	value, ok := session[oauthCompletionResponseKey]
	if !ok {
		return nil, false
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return result, true
}

func clonePendingMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func mergePendingCompletionResponse(session *dbent.PendingAuthSession, overrides map[string]any) map[string]any {
	payload, _ := readCompletionResponse(session.LocalFlowState)
	merged := clonePendingMap(payload)
	if strings.TrimSpace(session.RedirectTo) != "" {
		if _, exists := merged["redirect"]; !exists {
			merged["redirect"] = session.RedirectTo
		}
	}
	for key, value := range overrides {
		if value == nil {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	applySuggestedProfileToCompletionResponse(merged, session.UpstreamIdentityClaims)
	return merged
}

func pendingSessionStringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func pendingSessionWantsInvitation(payload map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(pendingSessionStringValue(payload, "error")), "invitation_required")
}

// pendingSessionRequiresEmailCompletion 判断 callback 写入的 completion payload 是否处于"补邮箱"状态。
// 钉钉跨组织/staff 邮箱缺失时进入此状态：前端跳到补邮箱页，exchange 不应走 adoption apply。
func pendingSessionRequiresEmailCompletion(payload map[string]any) bool {
	if v, ok := payload["requires_email_completion"].(bool); ok && v {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(pendingSessionStringValue(payload, "step")), "email_completion")
}

// pendingSessionRequiresBindLogin 判断 callback 写入的 completion payload 是否处于"必须绑定已有账户"状态。
// 钉钉 signupBlocked=true（注册关 + 钉钉企业豁免关）时进入此状态：前端渲染 bind_login 表单，
// exchange 不应消费 session，否则后续 /pending/bind-login 找不到 session。
func pendingSessionRequiresBindLogin(payload map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(pendingSessionStringValue(payload, "step")), "bind_login_required")
}

func pendingOAuthCompletionCanIssueTokenPair(session *dbent.PendingAuthSession, payload map[string]any) bool {
	if session == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(session.Intent), oauthIntentLogin) {
		return false
	}
	if session.TargetUserID == nil || *session.TargetUserID <= 0 {
		return false
	}
	if pendingSessionWantsInvitation(payload) {
		return false
	}
	return strings.TrimSpace(pendingSessionStringValue(payload, "step")) == ""
}

func ensurePendingOAuthCompleteRegistrationSession(session *dbent.PendingAuthSession) error {
	if session == nil {
		return infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}
	if strings.TrimSpace(session.Intent) != oauthIntentLogin {
		return infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}
	if session.TargetUserID != nil && *session.TargetUserID > 0 {
		return infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}
	payload, _ := readCompletionResponse(session.LocalFlowState)
	if strings.EqualFold(strings.TrimSpace(pendingSessionStringValue(payload, "step")), "bind_login_required") {
		return infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}
	return nil
}

func buildLegacyCompleteRegistrationPendingResponse(
	session *dbent.PendingAuthSession,
	forceEmailOnSignup bool,
	emailVerificationRequired bool,
) map[string]any {
	completionResponse := normalizePendingOAuthCompletionResponse(mergePendingCompletionResponse(session, map[string]any{
		"step":                   oauthPendingChoiceStep,
		"adoption_required":      true,
		"create_account_allowed": true,
		"force_email_on_signup":  forceEmailOnSignup,
	}))

	if email := strings.TrimSpace(session.ResolvedEmail); email != "" {
		if _, exists := completionResponse["email"]; !exists {
			completionResponse["email"] = email
		}
		if _, exists := completionResponse["resolved_email"]; !exists {
			completionResponse["resolved_email"] = email
		}
	}
	if _, exists := completionResponse["choice_reason"]; !exists {
		switch {
		case forceEmailOnSignup:
			completionResponse["choice_reason"] = "force_email_on_signup"
		case emailVerificationRequired:
			completionResponse["choice_reason"] = "email_verification_required"
		default:
			completionResponse["choice_reason"] = "third_party_signup"
		}
	}
	return completionResponse
}

func (h *AuthHandler) legacyCompleteRegistrationSessionStatus(
	c *gin.Context,
	session *dbent.PendingAuthSession,
) (*dbent.PendingAuthSession, bool, error) {
	if session == nil {
		return nil, false, infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}

	payload := normalizePendingOAuthCompletionResponse(mergePendingCompletionResponse(session, nil))
	if step := pendingSessionStringValue(payload, "step"); step != "" {
		return session, true, nil
	}

	emailVerificationRequired := h != nil && h.authService != nil && h.authService.IsEmailVerifyEnabled(c.Request.Context())
	forceEmailOnSignup := h.isForceEmailOnThirdPartySignup(c.Request.Context())
	if !emailVerificationRequired && !forceEmailOnSignup {
		return session, false, nil
	}

	if _, err := h.pendingIdentityService(); err != nil {
		return nil, false, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	updatedSession, err := h.authService.UpdatePendingOAuthSessionProgress(c.Request.Context(), session, strings.TrimSpace(session.Intent), strings.TrimSpace(session.ResolvedEmail), nil, buildLegacyCompleteRegistrationPendingResponse(session, forceEmailOnSignup, emailVerificationRequired))
	if err != nil {
		return nil, false, infraerrors.InternalServer("PENDING_AUTH_SESSION_UPDATE_FAILED", "failed to update pending oauth session").WithCause(err)
	}
	return updatedSession, true, nil
}

func (r oauthAdoptionDecisionRequest) hasDecision() bool {
	return r.AdoptDisplayName != nil || r.AdoptAvatar != nil
}

func bindOptionalOAuthAdoptionDecision(c *gin.Context) (oauthAdoptionDecisionRequest, error) {
	var req oauthAdoptionDecisionRequest
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return req, nil
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, nil
		}
		return req, err
	}
	return req, nil
}

func (h *AuthHandler) isForceEmailOnThirdPartySignup(ctx context.Context) bool {
	if h == nil || h.settingSvc == nil {
		return false
	}
	defaults, err := h.settingSvc.GetAuthSourceDefaultSettings(ctx)
	if err != nil || defaults == nil {
		return false
	}
	return defaults.ForceEmailOnThirdPartySignup
}

func (h *AuthHandler) findOAuthIdentityUser(ctx context.Context, identity service.PendingAuthIdentityKey) (*dbent.User, error) {
	return h.authService.FindOAuthIdentityUser(ctx, identity)
}

func (h *AuthHandler) BindLinuxDoOAuthLogin(c *gin.Context) { h.bindPendingOAuthLogin(c, "linuxdo") }
func (h *AuthHandler) BindOIDCOAuthLogin(c *gin.Context)    { h.bindPendingOAuthLogin(c, "oidc") }
func (h *AuthHandler) BindWeChatOAuthLogin(c *gin.Context)  { h.bindPendingOAuthLogin(c, "wechat") }
func (h *AuthHandler) BindPendingOAuthLogin(c *gin.Context) { h.bindPendingOAuthLogin(c, "") }

func (h *AuthHandler) CreateLinuxDoOAuthAccount(c *gin.Context) {
	h.createPendingOAuthAccount(c, "linuxdo")
}

func (h *AuthHandler) CreateOIDCOAuthAccount(c *gin.Context) { h.createPendingOAuthAccount(c, "oidc") }

func (h *AuthHandler) CreateWeChatOAuthAccount(c *gin.Context) {
	h.createPendingOAuthAccount(c, "wechat")
}

func (h *AuthHandler) CreatePendingOAuthAccount(c *gin.Context) {
	h.createPendingOAuthAccount(c, "")
}

// SendPendingOAuthVerifyCode sends a verification code for a browser-bound
// pending OAuth account-creation flow.
// POST /api/v1/auth/oauth/pending/send-verify-code
func (h *AuthHandler) SendPendingOAuthVerifyCode(c *gin.Context) {
	var req sendPendingOAuthVerifyCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	proof := captchaProof(req.TurnstileToken, req.TencentCaptchaTicket, req.TencentCaptchaRandstr)
	if err := h.authService.VerifyCaptcha(c.Request.Context(), proof, ip.GetClientIP(c)); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	_, session, _, err := readPendingOAuthBrowserSession(c, h)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := ensurePendingOAuthCompleteRegistrationSession(session); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if _, err := h.pendingIdentityService(); err != nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready"))
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if existingUser, err := h.authService.FindUserByNormalizedEmail(c.Request.Context(), email); err == nil && existingUser != nil {
		session, err = h.transitionPendingOAuthAccountToChoiceState(c, session, existingUser, email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		c.JSON(http.StatusOK, buildPendingOAuthSessionStatusPayload(session))
		return
	} else if err != nil && !errors.Is(err, service.ErrUserNotFound) {
		response.ErrorFrom(c, err)
		return
	}

	result, err := h.authService.SendPendingOAuthVerifyCode(c.Request.Context(), req.Email, c.GetHeader("Accept-Language"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, SendVerifyCodeResponse{
		Message:   "Verification code sent successfully",
		Countdown: result.Countdown,
	})
}

func (h *AuthHandler) upsertPendingOAuthAdoptionDecision(
	c *gin.Context,
	sessionID int64,
	req oauthAdoptionDecisionRequest,
) (*dbent.IdentityAdoptionDecision, error) {
	existing, err := h.authService.LoadPendingAdoptionDecision(c.Request.Context(), sessionID)
	if err != nil && !dbent.IsNotFound(err) {
		return nil, infraerrors.InternalServer("PENDING_AUTH_ADOPTION_LOAD_FAILED", "failed to load oauth profile adoption decision").WithCause(err)
	}
	if existing != nil && !req.hasDecision() {
		return existing, nil
	}
	if existing == nil && !req.hasDecision() {
		return nil, nil
	}

	input := service.PendingIdentityAdoptionDecisionInput{
		PendingAuthSessionID: sessionID,
	}
	if existing != nil {
		input.AdoptDisplayName = existing.AdoptDisplayName
		input.AdoptAvatar = existing.AdoptAvatar
		input.IdentityID = existing.IdentityID
	}
	if req.AdoptDisplayName != nil {
		input.AdoptDisplayName = *req.AdoptDisplayName
	}
	if req.AdoptAvatar != nil {
		input.AdoptAvatar = *req.AdoptAvatar
	}

	svc, err := h.pendingIdentityService()
	if err != nil {
		return nil, err
	}
	decision, err := svc.UpsertAdoptionDecision(c.Request.Context(), input)
	if err != nil {
		return nil, infraerrors.InternalServer("PENDING_AUTH_ADOPTION_SAVE_FAILED", "failed to save oauth profile adoption decision").WithCause(err)
	}
	return decision, nil
}

func (h *AuthHandler) ensurePendingOAuthAdoptionDecision(
	c *gin.Context,
	sessionID int64,
	req oauthAdoptionDecisionRequest,
) (*dbent.IdentityAdoptionDecision, error) {
	decision, err := h.upsertPendingOAuthAdoptionDecision(c, sessionID, req)
	if err != nil {
		return nil, err
	}
	if decision != nil {
		return decision, nil
	}

	svc, err := h.pendingIdentityService()
	if err != nil {
		return nil, err
	}
	decision, err = svc.UpsertAdoptionDecision(c.Request.Context(), service.PendingIdentityAdoptionDecisionInput{
		PendingAuthSessionID: sessionID,
	})
	if err != nil {
		return nil, infraerrors.InternalServer("PENDING_AUTH_ADOPTION_SAVE_FAILED", "failed to save oauth profile adoption decision").WithCause(err)
	}
	return decision, nil
}

func applySuggestedProfileToCompletionResponse(payload map[string]any, upstream map[string]any) {
	if len(payload) == 0 || len(upstream) == 0 {
		return
	}

	displayName := pendingSessionStringValue(upstream, "suggested_display_name")
	avatarURL := pendingSessionStringValue(upstream, "suggested_avatar_url")

	if displayName != "" {
		if _, exists := payload["suggested_display_name"]; !exists {
			payload["suggested_display_name"] = displayName
		}
	}
	if avatarURL != "" {
		if _, exists := payload["suggested_avatar_url"]; !exists {
			payload["suggested_avatar_url"] = avatarURL
		}
	}
	if displayName != "" || avatarURL != "" {
		payload["adoption_required"] = true
	}
}

func (h *AuthHandler) shouldSkipPendingOAuthAdoptionPrompt(
	ctx context.Context,
	session *dbent.PendingAuthSession,
	payload map[string]any,
) (bool, error) {
	if session == nil || len(payload) == 0 {
		return false, nil
	}
	if !pendingOAuthCompletionCanIssueTokenPair(session, payload) {
		return false, nil
	}
	if pendingSessionStringValue(session.UpstreamIdentityClaims, "suggested_display_name") == "" &&
		pendingSessionStringValue(session.UpstreamIdentityClaims, "suggested_avatar_url") == "" {
		return false, nil
	}

	return h.authService.PendingOAuthIdentityExistsForUser(ctx, session, *session.TargetUserID)
}

func readPendingOAuthBrowserSession(c *gin.Context, h *AuthHandler) (*service.AuthPendingIdentityService, *dbent.PendingAuthSession, func(), error) {
	secureCookie := isRequestHTTPS(c)
	clearCookies := func() {
		clearOAuthPendingSessionCookie(c, secureCookie)
		clearOAuthPendingBrowserCookie(c, secureCookie)
	}

	sessionToken, err := readOAuthPendingSessionCookie(c)
	if err != nil || strings.TrimSpace(sessionToken) == "" {
		clearCookies()
		return nil, nil, clearCookies, service.ErrPendingAuthSessionNotFound
	}
	browserSessionKey, err := readOAuthPendingBrowserCookie(c)
	if err != nil || strings.TrimSpace(browserSessionKey) == "" {
		clearCookies()
		return nil, nil, clearCookies, service.ErrPendingAuthBrowserMismatch
	}

	svc, err := h.pendingIdentityService()
	if err != nil {
		clearCookies()
		return nil, nil, clearCookies, err
	}

	session, err := svc.GetBrowserSession(c.Request.Context(), sessionToken, browserSessionKey)
	if err != nil {
		clearCookies()
		return nil, nil, clearCookies, err
	}

	return svc, session, clearCookies, nil
}

func (h *AuthHandler) consumePendingOAuthSessionOnLogout(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}

	sessionToken, err := readOAuthPendingSessionCookie(c)
	if err != nil || strings.TrimSpace(sessionToken) == "" {
		return
	}
	browserSessionKey, err := readOAuthPendingBrowserCookie(c)
	if err != nil || strings.TrimSpace(browserSessionKey) == "" {
		return
	}

	svc, err := h.pendingIdentityService()
	if err != nil {
		return
	}
	_, _ = svc.ConsumeBrowserSession(c.Request.Context(), sessionToken, browserSessionKey)
}

func clearOAuthLogoutCookies(c *gin.Context) {
	secureCookie := isRequestHTTPS(c)

	clearOAuthPendingSessionCookie(c, secureCookie)
	clearOAuthPendingBrowserCookie(c, secureCookie)
	clearOAuthBindAccessTokenCookie(c, secureCookie)
	http.SetCookie(c.Writer, &http.Cookie{Name: "oauth_completion_code", Path: oauthPendingBrowserCookiePath, MaxAge: -1, HttpOnly: true, Secure: secureCookie, SameSite: http.SameSiteLaxMode})

	clearCookie(c, linuxDoOAuthStateCookieName, secureCookie)
	clearCookie(c, linuxDoOAuthVerifierCookie, secureCookie)
	clearCookie(c, linuxDoOAuthRedirectCookie, secureCookie)
	clearCookie(c, linuxDoOAuthIntentCookieName, secureCookie)
	clearCookie(c, linuxDoOAuthBindUserCookieName, secureCookie)

	oidcClearCookie(c, oidcOAuthStateCookieName, secureCookie)
	oidcClearCookie(c, oidcOAuthVerifierCookie, secureCookie)
	oidcClearCookie(c, oidcOAuthRedirectCookie, secureCookie)
	oidcClearCookie(c, oidcOAuthNonceCookie, secureCookie)
	oidcClearCookie(c, oidcOAuthIntentCookieName, secureCookie)
	oidcClearCookie(c, oidcOAuthBindUserCookieName, secureCookie)

	wechatClearCookie(c, wechatOAuthStateCookieName, secureCookie)
	wechatClearCookie(c, wechatOAuthRedirectCookieName, secureCookie)
	wechatClearCookie(c, wechatOAuthIntentCookieName, secureCookie)
	wechatClearCookie(c, wechatOAuthModeCookieName, secureCookie)
	wechatClearCookie(c, wechatOAuthBindUserCookieName, secureCookie)

	wechatPaymentClearCookie(c, wechatPaymentOAuthStateName, secureCookie)
	wechatPaymentClearCookie(c, wechatPaymentOAuthRedirect, secureCookie)
	wechatPaymentClearCookie(c, wechatPaymentOAuthContextName, secureCookie)
	wechatPaymentClearCookie(c, wechatPaymentOAuthScope, secureCookie)
}

func buildPendingOAuthSessionStatusPayload(session *dbent.PendingAuthSession) gin.H {
	completionResponse := normalizePendingOAuthCompletionResponse(mergePendingCompletionResponse(session, nil))
	payload := gin.H{
		"auth_result": "pending_session",
		"provider":    strings.TrimSpace(session.ProviderType),
		"intent":      strings.TrimSpace(session.Intent),
	}
	for key, value := range completionResponse {
		payload[key] = value
	}
	if email := strings.TrimSpace(session.ResolvedEmail); email != "" {
		payload["email"] = email
	}
	return payload
}

func normalizePendingOAuthCompletionResponse(payload map[string]any) map[string]any {
	normalized := clonePendingMap(payload)
	for _, key := range []string{"access_token", "refresh_token", "expires_in", "token_type"} {
		delete(normalized, key)
	}
	step := strings.ToLower(strings.TrimSpace(pendingSessionStringValue(normalized, "step")))
	// 把多种 choice 别名归一为 oauthPendingChoiceStep；bind_login_required 是独立终态
	// （前端渲染 needsBindLogin 而非 needsChooser），故不能并入归一化列表。
	switch step {
	case "choice", "choose_account_action", "choose_account", "choose", "email_required":
		normalized["step"] = oauthPendingChoiceStep
	}
	if strings.EqualFold(strings.TrimSpace(pendingSessionStringValue(normalized, "step")), oauthPendingChoiceStep) {
		normalized["adoption_required"] = true
	}
	if _, exists := normalized["adoption_required"]; !exists {
		if _, hasChoiceFields := normalized["email_binding_required"]; hasChoiceFields {
			normalized["adoption_required"] = true
		}
	}
	return normalized
}

func pendingOAuthChoiceCompletionResponse(session *dbent.PendingAuthSession, email string) map[string]any {
	response := mergePendingCompletionResponse(session, map[string]any{
		"step":                      oauthPendingChoiceStep,
		"adoption_required":         true,
		"force_email_on_signup":     true,
		"email_binding_required":    true,
		"existing_account_bindable": true,
	})
	if email = strings.TrimSpace(email); email != "" {
		response["email"] = email
		response["resolved_email"] = email
	}
	return response
}

func (h *AuthHandler) transitionPendingOAuthAccountToChoiceState(
	c *gin.Context,
	session *dbent.PendingAuthSession,
	targetUser *dbent.User,
	email string,
) (*dbent.PendingAuthSession, error) {
	completionResponse := pendingOAuthChoiceCompletionResponse(session, email)
	var targetUserID *int64
	if targetUser != nil && targetUser.ID > 0 {
		targetUserID = &targetUser.ID
	}
	session, err := h.authService.UpdatePendingOAuthSessionProgress(c.Request.Context(), session, strings.TrimSpace(session.Intent), email, targetUserID, completionResponse)
	if err != nil {
		return nil, infraerrors.InternalServer("PENDING_AUTH_SESSION_UPDATE_FAILED", "failed to update pending oauth session").WithCause(err)
	}
	return session, nil
}

func writeOAuthTokenPairResponse(c *gin.Context, tokenPair *service.TokenPair) {
	c.JSON(http.StatusOK, gin.H{
		"access_token":  tokenPair.AccessToken,
		"refresh_token": tokenPair.RefreshToken,
		"expires_in":    tokenPair.ExpiresIn,
		"token_type":    "Bearer",
	})
}

func (h *AuthHandler) bindPendingOAuthLogin(c *gin.Context, provider string) {
	var req bindPendingOAuthLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	_, session, clearCookies, err := readPendingOAuthBrowserSession(c, h)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if strings.TrimSpace(provider) != "" && !strings.EqualFold(strings.TrimSpace(session.ProviderType), provider) {
		response.BadRequest(c, "Pending oauth session provider mismatch")
		return
	}

	user, err := h.authService.ValidatePasswordCredentials(c.Request.Context(), strings.TrimSpace(req.Email), req.Password)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if session.TargetUserID != nil && *session.TargetUserID > 0 && user.ID != *session.TargetUserID {
		response.ErrorFrom(c, infraerrors.Conflict("PENDING_AUTH_TARGET_USER_MISMATCH", "pending oauth session must be completed by the targeted user"))
		return
	}
	if err := h.ensureBackendModeAllowsUser(c.Request.Context(), user); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	decision, err := h.ensurePendingOAuthAdoptionDecision(c, session.ID, req.adoptionDecision())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.totpService != nil && h.settingSvc.IsTotpEnabled(c.Request.Context()) && user.TotpEnabled {
		tempToken, err := h.totpService.CreatePendingOAuthBindLoginSession(
			c.Request.Context(),
			user.ID,
			user.Email,
			session.SessionToken,
			session.BrowserSessionKey,
		)
		if err != nil {
			response.InternalError(c, "Failed to create 2FA session")
			return
		}
		response.Success(c, TotpLoginResponse{
			Requires2FA:     true,
			TempToken:       tempToken,
			UserEmailMasked: service.MaskEmail(user.Email),
		})
		return
	}
	if err := h.authService.CompletePendingOAuthBindLogin(c.Request.Context(), h.userService, session, decision, user.ID); err != nil {
		respondPendingOAuthBindingApplyError(c, err)
		return
	}

	h.authService.RecordSuccessfulLogin(c.Request.Context(), user.ID)
	// bindPendingOAuthLogin = 绑定已有账户登录，不动 users.username（用户已有自己的名字）
	h.maybeSyncDingTalkAfterLogin(c.Request.Context(), session, user.ID)
	tokenPair, err := h.authService.GenerateTokenPair(c.Request.Context(), user, "")
	if err != nil {
		response.InternalError(c, "Failed to generate token pair")
		return
	}

	clearCookies()
	writeOAuthTokenPairResponse(c, tokenPair)
}

func respondPendingOAuthBindingApplyError(c *gin.Context, err error) {
	if code := infraerrors.Code(err); code >= http.StatusBadRequest && code < http.StatusInternalServerError {
		response.ErrorFrom(c, err)
		return
	}
	response.ErrorFrom(c, infraerrors.InternalServer("PENDING_AUTH_BIND_APPLY_FAILED", "failed to bind pending oauth identity").WithCause(err))
}

func (h *AuthHandler) createPendingOAuthAccount(c *gin.Context, provider string) {
	var req createPendingOAuthAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	_, session, clearCookies, err := readPendingOAuthBrowserSession(c, h)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := ensurePendingOAuthCompleteRegistrationSession(session); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if strings.TrimSpace(provider) != "" && !strings.EqualFold(strings.TrimSpace(session.ProviderType), provider) {
		response.BadRequest(c, "Pending oauth session provider mismatch")
		return
	}

	if _, err := h.pendingIdentityService(); err != nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready"))
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	existingUser, err := h.authService.FindUserByNormalizedEmail(c.Request.Context(), email)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserNotFound):
			existingUser = nil
		case infraerrors.Code(err) >= http.StatusBadRequest && infraerrors.Code(err) < http.StatusInternalServerError:
			response.ErrorFrom(c, err)
			return
		default:
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "service temporarily unavailable"))
			return
		}
	}
	if existingUser != nil {
		session, err = h.transitionPendingOAuthAccountToChoiceState(c, session, existingUser, email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		c.JSON(http.StatusOK, buildPendingOAuthSessionStatusPayload(session))
		return
	}
	if err := h.ensureBackendModeAllowsNewUserLogin(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	proof := captchaProof(req.TurnstileToken, req.TencentCaptchaTicket, req.TencentCaptchaRandstr)
	if err := h.authService.VerifyCaptcha(c.Request.Context(), proof, ip.GetClientIP(c)); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	tokenPair, user, err := h.authService.RegisterOAuthEmailAccount(
		c.Request.Context(),
		email,
		req.Password,
		strings.TrimSpace(req.VerifyCode),
		strings.TrimSpace(req.InvitationCode),
		strings.TrimSpace(session.ProviderType),
	)
	if err != nil {
		if errors.Is(err, service.ErrEmailExists) {
			existingUser, lookupErr := h.authService.FindUserByNormalizedEmail(c.Request.Context(), email)
			if lookupErr != nil {
				response.ErrorFrom(c, lookupErr)
				return
			}
			session, err = h.transitionPendingOAuthAccountToChoiceState(c, session, existingUser, email)
			if err != nil {
				response.ErrorFrom(c, err)
				return
			}
			c.JSON(http.StatusOK, buildPendingOAuthSessionStatusPayload(session))
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	rollbackCreatedUser := func(originalErr error) bool {
		if user == nil || user.ID <= 0 {
			return false
		}
		if rollbackErr := h.authService.RollbackOAuthEmailAccountCreation(
			c.Request.Context(),
			user.ID,
			strings.TrimSpace(req.InvitationCode),
		); rollbackErr != nil {
			response.ErrorFrom(c, infraerrors.InternalServer(
				"PENDING_AUTH_ACCOUNT_ROLLBACK_FAILED",
				"failed to rollback pending oauth account creation",
			).WithCause(fmt.Errorf("original error: %w; rollback error: %v", originalErr, rollbackErr)))
			return true
		}
		user = nil
		return false
	}

	decision, err := h.ensurePendingOAuthAdoptionDecision(c, session.ID, req.adoptionDecision())
	if err != nil {
		if rollbackCreatedUser(err) {
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	clearSessionCookies, err := h.authService.FinalizePendingOAuthRegistration(c.Request.Context(), h.userService, session, decision, user, req.InvitationCode, req.AffCode, pendingOAuthCreateAccountPreCommitHook)
	if err != nil {
		if rollbackCreatedUser(err) {
			return
		}
		if clearSessionCookies {
			clearCookies()
		}
		response.ErrorFrom(c, err)
		return
	}

	h.authService.ApplyOAuthSignupPromoCode(c.Request.Context(), user.ID, pendingOAuthPromoCode(session))
	h.authService.RecordSuccessfulLogin(c.Request.Context(), user.ID)
	// createPendingOAuthAccount = 注册新账户，需要把钉钉昵称同步到 users.username 作为初始值
	h.maybeSyncDingTalkAfterRegistration(c.Request.Context(), session, user.ID)
	clearCookies()
	writeOAuthTokenPairResponse(c, tokenPair)
}

// ExchangePendingOAuthCompletion redeems a pending OAuth browser session into a frontend-safe payload.
// POST /api/v1/auth/oauth/pending/exchange
func (h *AuthHandler) ExchangePendingOAuthCompletion(c *gin.Context) {
	secureCookie := isRequestHTTPS(c)
	clearCookies := func() {
		clearOAuthPendingSessionCookie(c, secureCookie)
		clearOAuthPendingBrowserCookie(c, secureCookie)
	}
	adoptionDecision, err := bindOptionalOAuthAdoptionDecision(c)
	if err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	if code, cookieErr := c.Cookie("oauth_completion_code"); cookieErr == nil && code != "" {
		if adoptionDecision.CompletionCode == "" || adoptionDecision.CompletionCode != code {
			response.ErrorFrom(c, service.ErrPendingAuthCodeInvalid)
			return
		}
		h.exchangeOAuthCompletionCode(c, code)
		return
	}
	if adoptionDecision.CompletionCode != "" {
		response.ErrorFrom(c, service.ErrPendingAuthCodeInvalid)
		return
	}

	sessionToken, err := readOAuthPendingSessionCookie(c)
	if err != nil || strings.TrimSpace(sessionToken) == "" {
		clearCookies()
		response.ErrorFrom(c, service.ErrPendingAuthSessionNotFound)
		return
	}
	browserSessionKey, err := readOAuthPendingBrowserCookie(c)
	if err != nil || strings.TrimSpace(browserSessionKey) == "" {
		clearCookies()
		response.ErrorFrom(c, service.ErrPendingAuthBrowserMismatch)
		return
	}

	svc, err := h.pendingIdentityService()
	if err != nil {
		clearCookies()
		response.ErrorFrom(c, err)
		return
	}

	session, err := svc.GetBrowserSession(c.Request.Context(), sessionToken, browserSessionKey)
	if err != nil {
		clearCookies()
		response.ErrorFrom(c, err)
		return
	}

	payload, ok := readCompletionResponse(session.LocalFlowState)
	if !ok {
		clearCookies()
		response.ErrorFrom(c, infraerrors.InternalServer("PENDING_AUTH_COMPLETION_INVALID", "pending auth completion payload is invalid"))
		return
	}
	payload = normalizePendingOAuthCompletionResponse(payload)
	if strings.TrimSpace(session.RedirectTo) != "" {
		if _, exists := payload["redirect"]; !exists {
			payload["redirect"] = session.RedirectTo
		}
	}
	applySuggestedProfileToCompletionResponse(payload, session.UpstreamIdentityClaims)

	canIssueTokenPair := pendingOAuthCompletionCanIssueTokenPair(session, payload)
	var loginUser *service.User
	if canIssueTokenPair {
		loginUser, err = h.userService.GetByID(c.Request.Context(), *session.TargetUserID)
		if err != nil {
			clearCookies()
			response.ErrorFrom(c, err)
			return
		}
		if err := ensureLoginUserActive(loginUser); err != nil {
			clearCookies()
			response.ErrorFrom(c, err)
			return
		}
		if err := h.ensureBackendModeAllowsUser(c.Request.Context(), loginUser); err != nil {
			clearCookies()
			response.ErrorFrom(c, err)
			return
		}
	}
	skipAdoptionPrompt, err := h.shouldSkipPendingOAuthAdoptionPrompt(c.Request.Context(), session, payload)
	if err != nil {
		clearCookies()
		response.ErrorFrom(c, err)
		return
	}
	if skipAdoptionPrompt {
		delete(payload, "adoption_required")
	}

	if pendingSessionWantsInvitation(payload) {
		if adoptionDecision.hasDecision() {
			decision, err := h.upsertPendingOAuthAdoptionDecision(c, session.ID, adoptionDecision)
			if err != nil {
				response.ErrorFrom(c, err)
				return
			}
			_ = decision
		}
		response.Success(c, payload)
		return
	}
	if pendingSessionRequiresEmailCompletion(payload) {
		response.Success(c, payload)
		return
	}
	if pendingSessionRequiresBindLogin(payload) {
		response.Success(c, payload)
		return
	}
	// ─── 安全修复（账号接管 0day）────────────────────────────────────────────
	// 非终态 session（如 choose_account_action_required）的 TargetUserID 可能来自
	// 攻击者提交的他人邮箱：createPendingOAuthAccount / SendPendingOAuthVerifyCode
	// 发现邮箱已存在时会把本 session 指向该邮箱用户，全程无密码、无邮箱验证码、
	// 无账号所有权证明。若此时带着 adoption decision 继续执行，下方的
	// service.ApplyPendingOAuthAdoption 会把本 OAuth identity 直接绑定到 TargetUserID，
	// 攻击者随后再次 OAuth 登录即被系统识别为受害者本人（完整账号接管）。
	// 只有两类 session 允许在此处执行 adoption/binding：
	//   1. canIssueTokenPair == true —— 登录终态，identity 已安全绑定该用户；
	//   2. intent == bind_current_user —— 已登录用户主动发起绑定（绑定目标来自登录态 cookie）。
	// 其余状态一律只返回 payload，不绑定、不消费 session。
	if !canIssueTokenPair && !strings.EqualFold(strings.TrimSpace(session.Intent), oauthIntentBindCurrentUser) {
		response.Success(c, payload)
		return
	}
	if !adoptionDecision.hasDecision() {
		adoptionRequired, _ := payload["adoption_required"].(bool)
		if adoptionRequired {
			response.Success(c, payload)
			return
		}
	}

	decisionReq := adoptionDecision
	if !decisionReq.hasDecision() {
		adoptDisplayName := false
		adoptAvatar := false
		decisionReq = oauthAdoptionDecisionRequest{
			AdoptDisplayName: &adoptDisplayName,
			AdoptAvatar:      &adoptAvatar,
		}
	}

	decision, err := h.ensurePendingOAuthAdoptionDecision(c, session.ID, decisionReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	targetUserID := int64(0)
	if session.TargetUserID != nil {
		targetUserID = *session.TargetUserID
	}
	if err := h.authService.ApplyPendingOAuthAdoptionAndConsumeSession(c.Request.Context(), h.userService, session, decision, targetUserID); err != nil {
		response.ErrorFrom(c, infraerrors.InternalServer("PENDING_AUTH_ADOPTION_APPLY_FAILED", "failed to apply oauth profile adoption").WithCause(err))
		return
	}

	if canIssueTokenPair {
		tokenPair, err := h.authService.GenerateTokenPair(c.Request.Context(), loginUser, "")
		if err != nil {
			clearCookies()
			response.InternalError(c, "Failed to generate token pair")
			return
		}
		h.authService.RecordSuccessfulLogin(c.Request.Context(), loginUser.ID)
		payload["access_token"] = tokenPair.AccessToken
		payload["refresh_token"] = tokenPair.RefreshToken
		payload["expires_in"] = tokenPair.ExpiresIn
		payload["token_type"] = "Bearer"
	}

	clearCookies()
	response.Success(c, payload)
}

func (h *AuthHandler) exchangeOAuthCompletionCode(c *gin.Context, code string) {
	http.SetCookie(c.Writer, &http.Cookie{Name: "oauth_completion_code", Path: oauthPendingBrowserCookiePath, MaxAge: -1, HttpOnly: true, Secure: isRequestHTTPS(c), SameSite: http.SameSiteLaxMode})
	browser, err := readOAuthPendingBrowserCookie(c)
	if err != nil || browser == "" {
		response.ErrorFrom(c, service.ErrPendingAuthBrowserMismatch)
		return
	}
	pending, err := h.pendingIdentityService()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ticket, err := pending.ConsumeIdentityTicket(c.Request.Context(), code, "oauth-completion", browser+":"+oauthRequestBinding(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	clearOAuthPendingBrowserCookie(c, isRequestHTTPS(c))
	userID, err := strconv.ParseInt(pendingSessionStringValue(ticket.LocalFlowState, "user_id"), 10, 64)
	if err != nil {
		response.ErrorFrom(c, service.ErrInvalidToken)
		return
	}
	user, err := h.userService.GetByID(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if user == nil || !user.IsActive() || strconv.FormatInt(user.TokenVersion, 10) != pendingSessionStringValue(ticket.LocalFlowState, "token_version") {
		response.ErrorFrom(c, service.ErrInvalidToken)
		return
	}
	if err := h.ensureBackendModeAllowsUser(c.Request.Context(), user); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	pair, err := h.authService.GenerateTokenPair(c.Request.Context(), user, "")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"access_token": pair.AccessToken, "refresh_token": pair.RefreshToken, "expires_in": pair.ExpiresIn, "token_type": "Bearer", "redirect": pendingSessionStringValue(ticket.LocalFlowState, "redirect")})
}
