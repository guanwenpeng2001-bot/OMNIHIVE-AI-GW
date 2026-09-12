package admin

import (
	"context"
	"errors"
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
	if request.AccountID == 0 {
		if request.Account == nil || strings.TrimSpace(request.Account.Name) == "" || request.Account.Platform != service.PlatformCursor || request.Account.Type != service.AccountTypeOAuth {
			response.BadRequest(ctx, "Cursor browser authorization requires an auto account name")
			return
		}
	} else {
		account, err := handler.adminService.GetAccount(ctx.Request.Context(), request.AccountID)
		if err != nil || !account.IsCursor() || account.Type != service.AccountTypeOAuth {
			response.BadRequest(ctx, "Cursor browser authorization requires a Cursor auto account")
			return
		}
	}
	state, err := handler.cursorAuthService.Start(func(persistCtx context.Context, credentials map[string]any) (int64, error) {
		credentials["client_version"] = strings.TrimSpace(request.ClientVersion)
		if request.AccountID != 0 {
			// Recheck at completion: the account may have been deleted or changed
			// while its administrator was in the browser.
			existing, err := handler.adminService.GetAccount(persistCtx, request.AccountID)
			if err != nil || !existing.IsCursor() || existing.Type != service.AccountTypeOAuth {
				return 0, errors.New("Cursor account changed during authorization")
			}
			credentials = service.MergeCredentials(existing.Credentials, credentials)
			account, err := handler.adminService.UpdateAccount(persistCtx, request.AccountID, &service.UpdateAccountInput{Credentials: credentials})
			if err != nil {
				return 0, err
			}
			if _, err = handler.adminService.ClearAccountError(persistCtx, account.ID); err != nil {
				return 0, err
			}
			return account.ID, nil
		}
		settings := request.Account
		if mapping, ok := settings.Credentials["model_mapping"]; ok {
			credentials["model_mapping"] = mapping
		}
		account, err := handler.adminService.CreateAccount(persistCtx, &service.CreateAccountInput{
			Name: settings.Name, Notes: settings.Notes, Platform: service.PlatformCursor, Type: service.AccountTypeOAuth,
			Credentials: credentials, Extra: settings.Extra, ProxyID: settings.ProxyID, Concurrency: 1,
			Priority: settings.Priority, RateMultiplier: settings.RateMultiplier, LoadFactor: settings.LoadFactor,
			GroupIDs: settings.GroupIDs, ExpiresAt: settings.ExpiresAt, AutoPauseOnExpired: settings.AutoPauseOnExpired,
		})
		if err != nil {
			return 0, err
		}
		return account.ID, nil
	})
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
	state, err := handler.cursorAuthService.Poll(ctx.Request.Context(), ctx.Param("session"), cancel)
	if err != nil {
		response.BadRequest(ctx, "Cursor authorization expired or server restarted; start again")
		return
	}
	response.Success(ctx, state)
}
