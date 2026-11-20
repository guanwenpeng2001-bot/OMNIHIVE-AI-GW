package service

import (
	"context"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	dbuser "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/user"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"strings"
)

func (s *AuthService) FindLinuxDoCompatEmailUser(ctx context.Context, email string) (*dbent.User, error) {
	client := s.entClient
	if client == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" ||
		strings.HasSuffix(email, LinuxDoConnectSyntheticEmailDomain) ||
		strings.HasSuffix(email, OIDCConnectSyntheticEmailDomain) ||
		strings.HasSuffix(email, WeChatConnectSyntheticEmailDomain) ||
		strings.HasSuffix(email, DingTalkConnectSyntheticEmailDomain) {
		return nil, nil
	}

	userEntity, err := client.User.Query().
		Where(UserNormalizedEmailPredicate(email)).
		Order(dbent.Asc(dbuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, infraerrors.InternalServer("COMPAT_EMAIL_LOOKUP_FAILED", "failed to look up compat email user").WithCause(err)
	}
	switch len(userEntity) {
	case 0:
		return nil, nil
	case 1:
		return userEntity[0], nil
	default:
		return nil, infraerrors.Conflict("USER_EMAIL_CONFLICT", "normalized email matched multiple users")
	}
}
