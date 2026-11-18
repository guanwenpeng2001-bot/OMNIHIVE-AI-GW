package service

import (
	"context"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	dbuser "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/user"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"strings"
)

func (s *AuthService) FindDingTalkCompatEmailUser(ctx context.Context, email string) (*dbent.User, error) {

	client := s.entClient
	if client == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" ||
		strings.HasSuffix(email, DingTalkConnectSyntheticEmailDomain) ||
		strings.HasSuffix(email, LinuxDoConnectSyntheticEmailDomain) ||
		strings.HasSuffix(email, OIDCConnectSyntheticEmailDomain) ||
		strings.HasSuffix(email, WeChatConnectSyntheticEmailDomain) {
		return nil, nil
	}

	userEntities, err := client.User.Query().
		Where(UserNormalizedEmailPredicate(email)).
		Order(dbent.Asc(dbuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, infraerrors.InternalServer("COMPAT_EMAIL_LOOKUP_FAILED", "failed to look up compat email user").WithCause(err)
	}
	switch len(userEntities) {
	case 0:
		return nil, nil
	case 1:
		return userEntities[0], nil
	default:
		return nil, infraerrors.Conflict("USER_EMAIL_CONFLICT", "normalized email matched multiple users")
	}
}

// ─── createDingTalkOAuthChoicePendingSession ───────────────────────────────

// createDingTalkOAuthChoicePendingSession 创建 DingTalk OAuth 三方注册/绑定的 choice pending session。
// signupBlocked=true 时关闭"创建新账户"出口；若同时没有 compat email 匹配的已有账户，
// 直接把 step 切到 bind_login_required，避免前端展示一个没有实际可点选项的 choice 界面。
