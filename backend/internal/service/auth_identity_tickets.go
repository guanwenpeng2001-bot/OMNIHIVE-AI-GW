package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

// Identity tickets use the existing expiring pending-session store and its
// compare-and-set consumption. They cannot be used as post-login sessions.
const identityTicketProviderKey = "urn:sub2api:identity-ticket"

func (authService *AuthService) PendingIdentityService() (*AuthPendingIdentityService, error) {
	if authService == nil || authService.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}
	return NewAuthPendingIdentityService(authService.entClient), nil
}

func (pendingService *AuthPendingIdentityService) IssueIdentityTicket(ctx context.Context, purpose, binding string, data map[string]any, ttl time.Duration) (string, error) {
	if strings.TrimSpace(purpose) == "" || strings.TrimSpace(binding) == "" || ttl <= 0 || ttl > 10*time.Minute {
		return "", ErrPendingAuthCodeInvalid
	}
	rawToken, err := randomOpaqueToken(32)
	if err != nil {
		return "", err
	}
	_, err = pendingService.CreatePendingSession(ctx, CreatePendingAuthSessionInput{
		SessionToken:      hashPendingAuthCode(rawToken),
		Intent:            "login",
		Identity:          PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: identityTicketProviderKey, ProviderSubject: purpose},
		BrowserSessionKey: binding,
		LocalFlowState:    data,
		ExpiresAt:         time.Now().UTC().Add(ttl),
	})
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

func (pendingService *AuthPendingIdentityService) ConsumeIdentityTicket(ctx context.Context, token, purpose, binding string) (*dbent.PendingAuthSession, error) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(binding) == "" {
		return nil, ErrPendingAuthCodeInvalid
	}
	session, err := pendingService.getBrowserSession(ctx, hashPendingAuthCode(token))
	if err != nil {
		return nil, err
	}
	if session.ProviderKey != identityTicketProviderKey || session.ProviderSubject != purpose {
		return nil, ErrPendingAuthCodeInvalid
	}
	return pendingService.consumeSession(ctx, session, binding, ErrPendingAuthCodeExpired, ErrPendingAuthCodeConsumed)
}

const AdminWebSocketTicketPath = "/api/v1/admin/ops/ws/qps"

func (authService *AuthService) IssueAdminWebSocketTicket(ctx context.Context, token string) (string, error) {
	claims, err := authService.ValidateTokenContext(ctx, token)
	if err != nil {
		return "", err
	}
	user, err := authService.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		return "", err
	}
	if !user.IsActive() || !user.IsAdmin() || claims.TokenVersion != resolvedTokenVersion(user) {
		return "", ErrInvalidToken
	}
	binding := sessionBindingHashFromContext(ctx)
	if binding == "" || (claims.BindingHash != "" && binding != claims.BindingHash) {
		return "", ErrSessionBindingMismatch
	}
	pending, err := authService.PendingIdentityService()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return pending.IssueIdentityTicket(ctx, "admin-ws:"+AdminWebSocketTicketPath, binding, map[string]any{"claims": string(encoded)}, time.Minute)
}

func (authService *AuthService) ConsumeAdminWebSocketTicket(ctx context.Context, token, path string) (*JWTClaims, error) {
	if path != AdminWebSocketTicketPath {
		return nil, ErrInvalidToken
	}
	pending, err := authService.PendingIdentityService()
	if err != nil {
		return nil, err
	}
	ticket, err := pending.ConsumeIdentityTicket(ctx, token, "admin-ws:"+path, sessionBindingHashFromContext(ctx))
	if err != nil {
		return nil, err
	}
	encoded, ok := ticket.LocalFlowState["claims"].(string)
	if !ok {
		return nil, ErrInvalidToken
	}
	var claims JWTClaims
	if err := json.Unmarshal([]byte(encoded), &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if claims.ExpiresAt == nil || !time.Now().Before(claims.ExpiresAt.Time) {
		return nil, ErrTokenExpired
	}
	return &claims, nil
}
