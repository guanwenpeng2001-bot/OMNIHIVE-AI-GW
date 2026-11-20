//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type grokRefreshOAuthStub struct {
	account *service.Account
	info    *service.GrokTokenInfo
	calls   int
}

func (s *grokRefreshOAuthStub) RefreshAccountToken(_ context.Context, account *service.Account) (*service.GrokTokenInfo, error) {
	s.calls++
	s.account = account
	return s.info, nil
}

func (s *grokRefreshOAuthStub) BuildAccountCredentials(info *service.GrokTokenInfo) map[string]any {
	return map[string]any{
		"access_token":  info.AccessToken,
		"refresh_token": info.RefreshToken,
		"expires_at":    info.ExpiresAt,
		"base_url":      "https://api.x.ai/v1",
	}
}

type grokRefreshAdminService struct {
	*stubAdminService
	updatedCredentials map[string]any
}

func (s *grokRefreshAdminService) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updatedCredentials = input.Credentials
	return &service.Account{
		ID:          id,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Credentials: input.Credentials,
	}, nil
}

func (s *grokRefreshAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	return &service.Account{ID: id, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Credentials: s.updatedCredentials}, nil
}

func TestRefreshSingleAccountRoutesGrokThroughGrokOAuthService(t *testing.T) {
	t.Parallel()

	adminSvc := &grokRefreshAdminService{stubAdminService: newStubAdminService()}
	grokOAuth := &grokRefreshOAuthStub{info: &service.GrokTokenInfo{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresAt:    1_800_000_000,
	}}
	handler := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		grokOAuth,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	account := &service.Account{
		ID:             4227,
		QuotaDimension: "global",
		Platform:       service.PlatformGrok,
		Type:           service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "old-access",
			"refresh_token":      "old-refresh",
			"base_url":           "https://example.invalid/v1",
			"subscription_tier":  "SUPER_GROK",
			"entitlement_status": "ACTIVE",
		},
	}
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	handler.SetCredentialDraftService(service.NewAuthPendingIdentityService(client))
	encoded, err := json.Marshal(account.Credentials)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "accounts" .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "credentials", "extra", "quota_dimension"}).AddRow(account.ID, account.Platform, account.Type, encoded, []byte(`{}`), "global"))
	mock.ExpectCommit()

	updated, warning, err := handler.refreshSingleAccount(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Equal(t, 1, grokOAuth.calls)
	require.Same(t, account, grokOAuth.account)
	require.Equal(t, "new-access", adminSvc.updatedCredentials["access_token"])
	require.Equal(t, "new-refresh", adminSvc.updatedCredentials["refresh_token"])
	require.Equal(t, "https://example.invalid/v1", adminSvc.updatedCredentials["base_url"])
	require.Equal(t, "SUPER_GROK", adminSvc.updatedCredentials["subscription_tier"])
	require.Equal(t, "ACTIVE", adminSvc.updatedCredentials["entitlement_status"])
	require.Equal(t, adminSvc.updatedCredentials, updated.Credentials)
	require.NoError(t, mock.ExpectationsWereMet())
}
