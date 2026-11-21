package admin

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type WorkBuddyOAuthHandler struct {
	oauth *service.WorkBuddyOAuthService
}

func NewWorkBuddyOAuthHandler(oauth *service.WorkBuddyOAuthService) *WorkBuddyOAuthHandler {
	return &WorkBuddyOAuthHandler{oauth}
}
func (h *WorkBuddyOAuthHandler) Start(c *gin.Context) {
	var req struct {
		Site    string `json:"site"`
		ProxyID *int64 `json:"proxy_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid WorkBuddy login request")
		return
	}
	binding, err := credentialDraftBinding(c, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	state, err := h.oauth.Start(c.Request.Context(), req.Site, req.ProxyID, binding)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, state)
}
func (h *WorkBuddyOAuthHandler) Poll(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.SessionID) == "" {
		response.BadRequest(c, "WorkBuddy session_id is required")
		return
	}
	binding, err := credentialDraftBinding(c, false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	state, err := h.oauth.Poll(c.Request.Context(), req.SessionID, binding)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	// This explicit authorization exchange is the only response carrying tokens;
	// persisted account responses use the common credential redactor.
	c.Header("Cache-Control", "no-store")
	response.Success(c, state)
}
