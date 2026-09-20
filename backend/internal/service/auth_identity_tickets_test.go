//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdentityTicketConcurrentConsumptionHasOneWinner(t *testing.T) {
	pending, _ := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()
	token, err := pending.IssueIdentityTicket(ctx, "oauth-completion", "browser", nil, time.Minute)
	require.NoError(t, err)
	var winners atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if _, err := pending.ConsumeIdentityTicket(ctx, token, "oauth-completion", "browser"); err == nil {
				winners.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	require.Equal(t, int32(1), winners.Load())
}

func TestIdentityTicketsAreBoundScopedExpiringAndSingleUse(t *testing.T) {
	for _, purpose := range []string{"oauth-bind-start", "oauth-state:oidc", "oauth-completion", "admin-ws:" + AdminWebSocketTicketPath} {
		t.Run(purpose, func(t *testing.T) {
			pending, client := newAuthPendingIdentityServiceTestClient(t)
			ctx := context.Background()
			token, err := pending.IssueIdentityTicket(ctx, purpose, "browser-session", map[string]any{"result": "confirmed"}, time.Minute)
			require.NoError(t, err)
			stored, err := client.PendingAuthSession.Query().Where(pendingauthsession.SessionTokenEQ(hashPendingAuthCode(token))).Only(ctx)
			require.NoError(t, err)
			require.NotEqual(t, token, stored.SessionToken)
			_, err = pending.GetBrowserSession(ctx, stored.SessionToken, "browser-session")
			require.ErrorIs(t, err, ErrPendingAuthSessionNotFound)
			_, err = pending.ConsumeIdentityTicket(ctx, token, purpose, "other-browser")
			require.ErrorIs(t, err, ErrPendingAuthBrowserMismatch)
			_, err = pending.ConsumeIdentityTicket(ctx, token, purpose+"-wrong", "browser-session")
			require.ErrorIs(t, err, ErrPendingAuthCodeInvalid)
			result, err := pending.ConsumeIdentityTicket(ctx, token, purpose, "browser-session")
			require.NoError(t, err)
			require.Equal(t, "confirmed", result.LocalFlowState["result"])
			_, err = pending.ConsumeIdentityTicket(ctx, token, purpose, "browser-session")
			require.ErrorIs(t, err, ErrPendingAuthCodeConsumed)
			expiredToken, err := pending.IssueIdentityTicket(ctx, purpose, "browser-session", nil, time.Minute)
			require.NoError(t, err)
			_, err = client.PendingAuthSession.Update().Where(pendingauthsession.SessionTokenEQ(hashPendingAuthCode(expiredToken))).SetExpiresAt(time.Now().Add(-time.Second)).Save(ctx)
			require.NoError(t, err)
			_, err = pending.ConsumeIdentityTicket(ctx, expiredToken, purpose, "browser-session")
			require.ErrorIs(t, err, ErrPendingAuthCodeExpired)
		})
	}
}

type identityTicketUserRepository struct {
	UserRepository
	user *User
}

func (repository *identityTicketUserRepository) GetByID(context.Context, int64) (*User, error) {
	return repository.user, nil
}

type identityBindingSettings struct {
	SettingRepository
	enabled bool
}

func (repository *identityBindingSettings) GetValue(context.Context, string) (string, error) {
	if repository.enabled {
		return "true", nil
	}
	return "false", nil
}

func TestAdminWebSocketTicketConsumesOnceAndChecksScope(t *testing.T) {
	_, client := newAuthPendingIdentityServiceTestClient(t)
	admin := &User{ID: 1, Email: "admin@example.com", Role: RoleAdmin, Status: StatusActive}
	auth := &AuthService{entClient: client, userRepo: &identityTicketUserRepository{user: admin}, cfg: &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}}
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.1", UserAgent: "browser"})
	token, err := auth.GenerateToken(ctx, admin)
	require.NoError(t, err)
	ticket, err := auth.IssueAdminWebSocketTicket(ctx, token)
	require.NoError(t, err)
	require.NotEqual(t, token, ticket)
	_, err = auth.ConsumeAdminWebSocketTicket(ctx, ticket, "/wrong")
	require.ErrorIs(t, err, ErrInvalidToken)
	otherBrowser := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.2", UserAgent: "browser"})
	_, err = auth.ConsumeAdminWebSocketTicket(otherBrowser, ticket, AdminWebSocketTicketPath)
	require.ErrorIs(t, err, ErrPendingAuthBrowserMismatch)
	claims, err := auth.ConsumeAdminWebSocketTicket(ctx, ticket, AdminWebSocketTicketPath)
	require.NoError(t, err)
	require.Equal(t, admin.ID, claims.UserID)
	_, err = auth.ConsumeAdminWebSocketTicket(ctx, ticket, AdminWebSocketTicketPath)
	require.ErrorIs(t, err, ErrPendingAuthCodeConsumed)
}

func TestLegacyUnboundTokenCannotRenewWithoutBinding(t *testing.T) {
	settings := &identityBindingSettings{}
	user := &User{ID: 1, Email: "user@example.com", Role: RoleUser, Status: StatusActive}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}
	auth := &AuthService{userRepo: &identityTicketUserRepository{user: user}, cfg: cfg, settingService: NewSettingService(settings, cfg)}
	legacy, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	settings.enabled = true
	_, err = auth.GenerateToken(context.Background(), user)
	require.ErrorIs(t, err, ErrSessionBindingMismatch)
	_, err = auth.GenerateTokenPair(context.Background(), user, "")
	require.ErrorIs(t, err, ErrSessionBindingMismatch)
	_, err = auth.RefreshToken(context.Background(), legacy)
	require.ErrorIs(t, err, ErrSessionBindingMismatch)
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.1", UserAgent: "browser"})
	renewed, err := auth.RefreshToken(ctx, legacy)
	require.NoError(t, err)
	claims, err := auth.ValidateToken(renewed)
	require.NoError(t, err)
	require.Equal(t, SessionBindingFromContext(ctx).Hash(), claims.BindingHash)
	settings.enabled = false
	cfg.JWT.ExpireHour = -1
	expiredLegacy, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	settings.enabled = true
	_, err = auth.RefreshToken(ctx, expiredLegacy)
	require.ErrorIs(t, err, ErrTokenExpired)
}
