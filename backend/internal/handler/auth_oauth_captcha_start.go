package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type oauthStartCaptchaRequest struct {
	// TurnstileToken 承载阿里云验证码的 captchaVerifyParam（复用既有请求字段名）
	TurnstileToken        string `json:"turnstile_token"`
	TencentCaptchaTicket  string `json:"tencent_captcha_ticket"`
	TencentCaptchaRandstr string `json:"tencent_captcha_randstr"`
}

type oauthStartResponse struct {
	AuthorizeURL string `json:"authorize_url"`
}

func (h *AuthHandler) requireActionCaptchaForOAuthLoginStart(c *gin.Context) bool {
	if strings.HasSuffix(strings.TrimRight(c.Request.URL.Path, "/"), "/bind/start") || normalizeOAuthIntent(c.Query("intent")) == oauthIntentBindCurrentUser {
		if _, err := h.resolveOAuthBindTargetUserID(c); err != nil {
			response.ErrorFrom(c, err)
			return false
		}
		return true
	}

	var req oauthStartCaptchaRequest
	if c.Request.Method == http.MethodPost {
		_ = c.ShouldBindJSON(&req)
	}
	if err := h.authService.VerifyActionCaptchaIfEnabled(c.Request.Context(), service.CaptchaProof{
		TurnstileToken: req.TurnstileToken,
		TencentTicket:  req.TencentCaptchaTicket,
		TencentRandstr: req.TencentCaptchaRandstr,
	}, ip.GetClientIP(c)); err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	return true
}

func respondOAuthStart(c *gin.Context, authorizeURL string) {
	if c.Request.Method == http.MethodPost {
		response.Success(c, oauthStartResponse{AuthorizeURL: authorizeURL})
		return
	}
	c.Redirect(http.StatusFound, authorizeURL)
}

func (h *AuthHandler) IssueAdminWebSocketTicket(c *gin.Context) {
	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	ticket, err := h.authService.IssueAdminWebSocketTicket(c.Request.Context(), token)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"ticket": ticket, "expires_in": 60})
}

func oauthRequestBinding(c *gin.Context) string {
	if binding := service.SessionBindingFromContext(c.Request.Context()); binding != nil && binding.Hash() != "" {
		return binding.Hash()
	}
	return (&service.SessionBinding{IP: ip.GetClientIP(c), UserAgent: c.Request.UserAgent()}).Hash()
}

func (h *AuthHandler) issueOAuthState(c *gin.Context, provider string) (string, error) {
	pending, err := h.pendingIdentityService()
	if err != nil {
		return "", err
	}
	state, err := pending.IssueIdentityTicket(c.Request.Context(), "oauth-state:"+provider, oauthRequestBinding(c), nil, 10*time.Minute)
	if err == nil {
		clearOAuthCompletionCodeCookie(c)
	}
	return state, err
}

func (h *AuthHandler) consumeOAuthState(c *gin.Context, provider, state string) error {
	pending, err := h.pendingIdentityService()
	if err != nil {
		return err
	}
	_, err = pending.ConsumeIdentityTicket(c.Request.Context(), state, "oauth-state:"+provider, oauthRequestBinding(c))
	return err
}
