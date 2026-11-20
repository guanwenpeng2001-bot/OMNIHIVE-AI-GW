package repository

import (
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRegistrationAgreementPersistencePostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	ctx := f.ctx
	documentJSON, err := json.Marshal([]service.LoginAgreementDocument{
		{ID: "terms", Title: "Terms", ContentMD: "Published operator terms"},
		{ID: "privacy-policy", Title: "Privacy", ContentMD: "Published operator privacy policy"},
	})
	require.NoError(t, err)
	documents := string(documentJSON)
	settings := NewSettingRepository(f.client)
	for key, value := range map[string]string{
		service.SettingKeyLoginAgreementEnabled:   "true",
		service.SettingKeyLoginAgreementDocuments: documents,
	} {
		require.NoError(t, settings.Set(ctx, key, value))
	}
	public, err := service.NewSettingService(settings, &config.Config{}).GetPublicSettings(ctx)
	require.NoError(t, err)
	require.True(t, public.LoginAgreementEnabled)
	repo := NewUserRepository(f.client, f.db)
	user := &service.User{
		Email: "agreement@example.com", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive,
		SignupAgreement: &service.SignupAgreement{Accepted: true, Revision: public.LoginAgreementRevision},
	}
	require.NoError(t, repo.CreateWithEmailAliasGuard(ctx, user))
	var revision string
	require.NoError(t, f.db.QueryRowContext(ctx, "SELECT revision FROM registration_agreements WHERE user_id=$1", user.ID).Scan(&revision))
	require.Equal(t, user.SignupAgreement.Revision, revision)

	rejected := &service.User{
		Email: "agreement-rejected@example.com", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive,
		SignupAgreement: &service.SignupAgreement{Accepted: true, Revision: "stale"},
	}
	require.ErrorIs(t, repo.CreateWithEmailAliasGuard(ctx, rejected), service.ErrRegistrationAgreement)
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE email=$1", rejected.Email).Scan(&count))
	require.Zero(t, count)
}
