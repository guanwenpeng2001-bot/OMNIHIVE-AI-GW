package admin

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (handler *AccountHandler) StartCursorAuthorization(ctx *gin.Context) {
	var request struct {
		AccountID     int64                 `json:"account_id"`
		Account       *CreateAccountRequest `json:"account"`
		ClientVersion string                `json:"client_version"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		response.BadRequest(ctx, "Invalid Cursor authorization account settings")
		return
	}
	var selectedAccount *service.Account
	var proxyID, proxyGroupID *int64
	if request.AccountID == 0 {
		if request.Account == nil || strings.TrimSpace(request.Account.Name) == "" || request.Account.Platform != service.PlatformCursor || request.Account.Type != service.AccountTypeOAuth {
			response.BadRequest(ctx, "Cursor browser authorization requires an auto account name")
			return
		}
		proxyID, proxyGroupID = request.Account.ProxyID, request.Account.ProxyGroupID
	} else {
		account, err := handler.adminService.GetAccount(ctx.Request.Context(), request.AccountID)
		if err != nil || !account.IsCursor() || account.Type != service.AccountTypeOAuth {
			response.BadRequest(ctx, "Cursor browser authorization requires a Cursor auto account")
			return
		}
		selectedAccount = account
	}
	if err := requireCredentialDraftStore(handler.credentialDrafts); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	binding, err := credentialDraftBinding(ctx, true)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}

	var selection *service.OAuthEgressSelection
	var proxyURL string
	if selectedAccount != nil {
		selection, err = selectedAccount.OAuthEgressSelection(ctx.Request.Context())
		if err == nil {
			proxyURL, err = selectedAccount.EgressProxyURL(ctx.Request.Context())
		}
	} else {
		selection, err = handler.credentialDrafts.ResolveAuthorizationEgress(ctx.Request.Context(), proxyID, proxyGroupID)
		if err == nil && selection != nil {
			proxyURL = selection.URL
		}
		if err == nil && proxyID != nil && *proxyID > 0 {
			var proxy *service.Proxy
			proxy, err = handler.adminService.GetProxy(ctx.Request.Context(), *proxyID)
			if err == nil && proxy != nil {
				proxyURL = proxy.URL()
			}
		}
	}
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	state, err := handler.cursorAuthService.StartDraft(binding, func(persistCtx context.Context, credentials map[string]any) (*service.AccountCredentialDraft, error) {
		credentials["client_version"] = strings.TrimSpace(request.ClientVersion)
		return handler.credentialDrafts.IssueAccountCredentialDraft(persistCtx, binding, service.PlatformCursor, service.AccountTypeOAuth, credentials, selection)
	}, proxyURL)
	if err != nil {
		response.BadRequest(ctx, "Cursor authorization could not start; retry later")
		return
	}
	response.Success(ctx, state)
}

func (handler *AccountHandler) PollCursorAuthorization(ctx *gin.Context) {
	handler.cursorAuthorizationStatus(ctx, false)
}
func (handler *AccountHandler) CancelCursorAuthorization(ctx *gin.Context) {
	handler.cursorAuthorizationStatus(ctx, true)
}
func (handler *AccountHandler) cursorAuthorizationStatus(ctx *gin.Context, cancel bool) {
	binding, err := credentialDraftBinding(ctx, false)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	state, err := handler.cursorAuthService.PollBound(ctx.Request.Context(), ctx.Param("session"), binding, cancel)
	if err != nil {
		response.BadRequest(ctx, "Cursor authorization expired or server restarted; start again")
		return
	}
	if (cancel || state.Status == "cancelled") && state.Draft != nil {
		if err := handler.credentialDrafts.CancelAccountCredentialDraft(ctx.Request.Context(), state.Draft.Reference, binding); err != nil {
			response.ErrorFrom(ctx, err)
			return
		}
		state.Status, state.Draft = "cancelled", nil
	}
	response.Success(ctx, state)
}
