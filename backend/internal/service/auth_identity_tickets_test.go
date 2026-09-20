//go:build unit

package service

import (
	"context"
	"errors"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdentityTicketConcurrentConsumptionHasOneWinner(testingT *testing.T) {
	pending, _ := newAuthPendingIdentityServiceTestClient(testingT)
	ctx := context.Background()
	token, err := pending.IssueIdentityTicket(ctx, "oauth-completion", "browser", nil, time.Minute)
	require.NoError(testingT, err)
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
	require.Equal(testingT, int32(1), winners.Load())
}

func TestIdentityTicketsAreBoundScopedExpiringAndSingleUse(testingT *testing.T) {
	for _, purpose := range []string{"oauth-bind-start", "oauth-state:oidc", "oauth-completion", "admin-ws:" + AdminWebSocketTicketPath} {
		testingT.Run(purpose, func(testingT *testing.T) {
			pending, client := newAuthPendingIdentityServiceTestClient(testingT)
			ctx := context.Background()
			token, err := pending.IssueIdentityTicket(ctx, purpose, "browser-session", map[string]any{"result": "confirmed"}, time.Minute)
			require.NoError(testingT, err)
			stored, err := client.PendingAuthSession.Query().Where(pendingauthsession.SessionTokenEQ(hashPendingAuthCode(token))).Only(ctx)
			require.NoError(testingT, err)
			require.NotEqual(testingT, token, stored.SessionToken)
			_, err = pending.GetBrowserSession(ctx, stored.SessionToken, "browser-session")
			require.ErrorIs(testingT, err, ErrPendingAuthSessionNotFound)
			_, err = pending.ConsumeIdentityTicket(ctx, token, purpose, "other-browser")
			require.ErrorIs(testingT, err, ErrPendingAuthBrowserMismatch)
			_, err = pending.ConsumeIdentityTicket(ctx, token, purpose+"-wrong", "browser-session")
			require.ErrorIs(testingT, err, ErrPendingAuthCodeInvalid)
			result, err := pending.ConsumeIdentityTicket(ctx, token, purpose, "browser-session")
			require.NoError(testingT, err)
			require.Equal(testingT, "confirmed", result.LocalFlowState["result"])
			_, err = pending.ConsumeIdentityTicket(ctx, token, purpose, "browser-session")
			require.ErrorIs(testingT, err, ErrPendingAuthCodeConsumed)
			expiredToken, err := pending.IssueIdentityTicket(ctx, purpose, "browser-session", nil, time.Minute)
			require.NoError(testingT, err)
			_, err = client.PendingAuthSession.Update().Where(pendingauthsession.SessionTokenEQ(hashPendingAuthCode(expiredToken))).SetExpiresAt(time.Now().Add(-time.Second)).Save(ctx)
			require.NoError(testingT, err)
			_, err = pending.ConsumeIdentityTicket(ctx, expiredToken, purpose, "browser-session")
			require.ErrorIs(testingT, err, ErrPendingAuthCodeExpired)
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

func TestAdminWebSocketTicketConsumesOnceAndChecksScope(testingT *testing.T) {
	_, client := newAuthPendingIdentityServiceTestClient(testingT)
	admin := &User{ID: 1, Email: "admin@example.com", Role: RoleAdmin, Status: StatusActive}
	auth := &AuthService{entClient: client, userRepo: &identityTicketUserRepository{user: admin}, cfg: &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}}
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.1", UserAgent: "browser"})
	token, err := auth.GenerateToken(ctx, admin)
	require.NoError(testingT, err)
	ticket, err := auth.IssueAdminWebSocketTicket(ctx, token)
	require.NoError(testingT, err)
	require.NotEqual(testingT, token, ticket)
	_, err = auth.ConsumeAdminWebSocketTicket(ctx, ticket, "/wrong")
	require.ErrorIs(testingT, err, ErrInvalidToken)
	otherBrowser := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.2", UserAgent: "browser"})
	_, err = auth.ConsumeAdminWebSocketTicket(otherBrowser, ticket, AdminWebSocketTicketPath)
	require.ErrorIs(testingT, err, ErrPendingAuthBrowserMismatch)
	claims, err := auth.ConsumeAdminWebSocketTicket(ctx, ticket, AdminWebSocketTicketPath)
	require.NoError(testingT, err)
	require.Equal(testingT, admin.ID, claims.UserID)
	_, err = auth.ConsumeAdminWebSocketTicket(ctx, ticket, AdminWebSocketTicketPath)
	require.ErrorIs(testingT, err, ErrPendingAuthCodeConsumed)
}

func TestLegacyUnboundTokenCannotRenewWithoutBinding(testingT *testing.T) {
	settings := &identityBindingSettings{}
	user := &User{ID: 1, Email: "user@example.com", Role: RoleUser, Status: StatusActive}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}
	auth := &AuthService{userRepo: &identityTicketUserRepository{user: user}, cfg: cfg, settingService: NewSettingService(settings, cfg)}
	legacy, err := auth.GenerateToken(context.Background(), user)
	require.NoError(testingT, err)
	settings.enabled = true
	_, err = auth.GenerateToken(context.Background(), user)
	require.ErrorIs(testingT, err, ErrSessionBindingMismatch)
	_, err = auth.GenerateTokenPair(context.Background(), user, "")
	require.ErrorIs(testingT, err, ErrSessionBindingMismatch)
	_, err = auth.RefreshToken(context.Background(), legacy)
	require.ErrorIs(testingT, err, ErrSessionBindingMismatch)
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.1", UserAgent: "browser"})
	renewed, err := auth.RefreshToken(ctx, legacy)
	require.NoError(testingT, err)
	claims, err := auth.ValidateToken(renewed)
	require.NoError(testingT, err)
	require.Equal(testingT, SessionBindingFromContext(ctx).Hash(), claims.BindingHash)
	settings.enabled = false
	cfg.JWT.ExpireHour = -1
	expiredLegacy, err := auth.GenerateToken(context.Background(), user)
	require.NoError(testingT, err)
	settings.enabled = true
	_, err = auth.RefreshToken(ctx, expiredLegacy)
	require.ErrorIs(testingT, err, ErrTokenExpired)
}

func TestPendingOAuthCompletionRejectsInvalidConsumptionWithoutBinding(testingT *testing.T) {
	for _, completion := range []string{"bind_login", "adoption"} {
		for _, failure := range []string{"consumed", "expired", "browser_mismatch", "code_expired", "write_failure"} {
			testingT.Run(completion+"/"+failure, func(testingT *testing.T) {
				pending, client := newAuthPendingIdentityServiceTestClient(testingT)
				requestContext := context.Background()
				user, err := client.User.Create().SetEmail("stale@example.test").SetUsername("Original").SetPasswordHash("hash").SetStatus(StatusActive).Save(requestContext)
				require.NoError(testingT, err)
				session, err := pending.CreatePendingSession(requestContext, CreatePendingAuthSessionInput{
					Intent: "bind_current_user", TargetUserID: &user.ID,
					Identity: PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: "fixture", ProviderSubject: "stale-subject"}, BrowserSessionKey: "browser",
					UpstreamIdentityClaims: map[string]any{"suggested_display_name": "Adopted"},
				})
				require.NoError(testingT, err)
				decision, err := client.IdentityAdoptionDecision.Create().SetPendingAuthSessionID(session.ID).SetAdoptDisplayName(true).SetAdoptAvatar(false).Save(requestContext)
				require.NoError(testingT, err)
				settings := NewSettingService(&pendingCompletionGrantSettings{}, &config.Config{})
				defaults, enabled, err := settings.ResolveAuthSourceGrantSettings(requestContext, "oidc", true)
				require.NoError(testingT, err)
				require.True(testingT, enabled)
				require.Equal(testingT, float64(9), defaults.Balance)
				authService := &AuthService{entClient: client, settingService: settings}
				expectedError := ErrPendingAuthSessionConsumed
				switch failure {
				case "consumed":
					_, err = pending.ConsumeBrowserSession(requestContext, session.SessionToken, session.BrowserSessionKey)
				case "expired":
					err = client.PendingAuthSession.UpdateOneID(session.ID).SetExpiresAt(time.Now().Add(-time.Minute)).Exec(requestContext)
					expectedError = ErrPendingAuthSessionExpired
				case "code_expired":
					err = client.PendingAuthSession.UpdateOneID(session.ID).SetCompletionCodeExpiresAt(time.Now().Add(-time.Minute)).Exec(requestContext)
					expectedError = ErrPendingAuthSessionExpired
				case "browser_mismatch":
					session.BrowserSessionKey = "wrong-browser"
					expectedError = ErrPendingAuthBrowserMismatch
				case "write_failure":
					injectedError := errors.New("consume write failed")
					client.PendingAuthSession.Use(func(next dbent.Mutator) dbent.Mutator {
						return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) { return nil, injectedError })
					})
					// Assert the injected storage error separately from public session errors.
					if completion == "bind_login" {
						err = authService.CompletePendingOAuthBindLogin(requestContext, nil, session, decision, user.ID)
					} else {
						err = authService.ApplyPendingOAuthAdoptionAndConsumeSession(requestContext, nil, session, decision, user.ID)
					}
					require.ErrorIs(testingT, err, injectedError)
				}
				if failure != "write_failure" {
					require.NoError(testingT, err)
					if completion == "bind_login" {
						err = authService.CompletePendingOAuthBindLogin(requestContext, nil, session, decision, user.ID)
					} else {
						err = authService.ApplyPendingOAuthAdoptionAndConsumeSession(requestContext, nil, session, decision, user.ID)
					}
					require.ErrorIs(testingT, err, expectedError)
				}
				identities, err := client.AuthIdentity.Query().All(requestContext)
				require.NoError(testingT, err)
				require.Empty(testingT, identities)
				unchangedUser, err := client.User.Get(requestContext, user.ID)
				require.NoError(testingT, err)
				require.Equal(testingT, user.Username, unchangedUser.Username)
				require.Equal(testingT, user.Balance, unchangedUser.Balance)
				require.Equal(testingT, user.Concurrency, unchangedUser.Concurrency)
				storedDecision, err := client.IdentityAdoptionDecision.Get(requestContext, decision.ID)
				require.NoError(testingT, err)
				require.Nil(testingT, storedDecision.IdentityID)
			})
		}
	}
}

func TestPendingOAuthBindingFailureRollsBackConsumption(testingT *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(testingT)
	requestContext := context.Background()
	user, err := client.User.Create().SetEmail("rollback@example.test").SetPasswordHash("hash").Save(requestContext)
	require.NoError(testingT, err)
	session, err := pending.CreatePendingSession(requestContext, CreatePendingAuthSessionInput{
		Intent: "login", Identity: PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: "fixture", ProviderSubject: "rollback-subject"}, BrowserSessionKey: "browser",
	})
	require.NoError(testingT, err)
	injectedError := errors.New("identity write failed")
	client.AuthIdentity.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) { return nil, injectedError })
	})
	authService := &AuthService{entClient: client}
	require.Error(testingT, authService.CompletePendingOAuthBindLogin(requestContext, nil, session, nil, user.ID))
	storedSession, err := client.PendingAuthSession.Get(requestContext, session.ID)
	require.NoError(testingT, err)
	require.Nil(testingT, storedSession.ConsumedAt)
	identities, err := client.AuthIdentity.Query().All(requestContext)
	require.NoError(testingT, err)
	require.Empty(testingT, identities)
}

func TestPendingOAuthConcurrentBindingHasOneWinner(testingT *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(testingT)
	requestContext := context.Background()
	user, err := client.User.Create().SetEmail("concurrent@example.test").SetPasswordHash("hash").Save(requestContext)
	require.NoError(testingT, err)
	session, err := pending.CreatePendingSession(requestContext, CreatePendingAuthSessionInput{
		Intent: "login", Identity: PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: "fixture", ProviderSubject: "concurrent-subject"}, BrowserSessionKey: "browser",
	})
	require.NoError(testingT, err)
	authService := &AuthService{entClient: client}
	var winners atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if authService.CompletePendingOAuthBindLogin(requestContext, nil, session, nil, user.ID) == nil {
				winners.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	require.Equal(testingT, int32(1), winners.Load())
	identities, err := client.AuthIdentity.Query().All(requestContext)
	require.NoError(testingT, err)
	require.Len(testingT, identities, 1)
}

type pendingCompletionGrantSettings struct{ SettingRepository }

func (repository *pendingCompletionGrantSettings) GetValue(context.Context, string) (string, error) {
	return "", ErrSettingNotFound
}
func (repository *pendingCompletionGrantSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{
		SettingKeyAuthSourceDefaultOIDCBalance:          "9",
		SettingKeyAuthSourceDefaultOIDCConcurrency:      "4",
		SettingKeyAuthSourceDefaultOIDCGrantOnFirstBind: "true",
	}, nil
}
