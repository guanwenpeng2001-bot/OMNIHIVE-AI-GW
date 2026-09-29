package service

import (
	"context"
	"errors"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"strings"
)

func (s *AuthService) FindOIDCCompatEmailUser(ctx context.Context, email string) (*dbent.User, error) {
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

	userEntity, err := s.FindUserByNormalizedEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, nil
		}
		return nil, infraerrors.InternalServer("COMPAT_EMAIL_LOOKUP_FAILED", "failed to look up compat email user").WithCause(err)
	}
	return userEntity, nil
}
