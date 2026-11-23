package service

import (
	"context"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// EnsureAutomaticAccountPrivacy is only for automatic post-write effects. Manual
// refresh keeps using the Ensure helpers directly, including on inactive accounts.
func EnsureAutomaticAccountPrivacy(ctx context.Context, admin AdminService, account *Account) {
	if account == nil || account.Status != StatusActive || account.Type != AccountTypeOAuth ||
		(account.Platform != PlatformOpenAI && account.Platform != PlatformAntigravity) {
		return
	}
	workspaceID := account.WorkspaceID
	if workspaceID <= 0 {
		workspaceID = domain.ResourceWorkspaceID(ctx)
		if workspaceID <= 0 {
			return
		}
	}
	ctx = domain.WithWorkspaceScope(ctx, workspaceID)
	fresh, err := admin.GetAccount(ctx, account.ID)
	if err != nil || fresh == nil || fresh.Status != StatusActive || fresh.ID != account.ID ||
		(fresh.WorkspaceID > 0 && fresh.WorkspaceID != workspaceID) || fresh.Type != AccountTypeOAuth {
		return
	}
	switch fresh.Platform {
	case PlatformOpenAI:
		admin.EnsureOpenAIPrivacy(ctx, fresh)
	case PlatformAntigravity:
		mode := admin.EnsureAntigravityPrivacy(ctx, fresh)
		// Preserve the create response's privacy result while probing fresh credentials.
		applyAntigravityPrivacyMode(account, mode)
	}
}
