//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

type effectiveSettingRepo struct {
	*mockSettingRepo
	version int
}

func (r *effectiveSettingRepo) GetAllVersioned(ctx context.Context) (map[string]string, string, error) {
	values, err := r.GetAll(ctx)
	return values, fmt.Sprint(r.version), err
}

func (r *effectiveSettingRepo) ResetOverrides(_ context.Context, keys []string, expected string) error {
	if expected != fmt.Sprint(r.version) {
		return ErrSettingVersionConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range keys {
		delete(r.data, key)
	}
	r.version++
	return nil
}

func effectiveItem(t *testing.T, snapshot *EffectiveSettings, key string) SettingEffectiveValue {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("missing effective setting %s", key)
	return SettingEffectiveValue{}
}

func TestEffectiveSettingsSourceAndSecretRedaction(t *testing.T) {
	repo := &effectiveSettingRepo{mockSettingRepo: newMockSettingRepo(), version: 4}
	repo.data[SettingKeyGitHubOAuthClientID] = "database-client"
	repo.data[SettingKeyGitHubOAuthClientSecret] = "database-secret-not-public"
	repo.data[SettingKeyModelPlazaEnabled] = "true"
	repo.data[settingModelPlazaSourceKey] = "migration_snapshot"
	cfg := &config.Config{GitHubOAuth: config.EmailOAuthProviderConfig{Enabled: true, ClientID: "deployment-client", ClientSecret: "deployment-secret-not-public"}}
	svc := NewSettingService(repo, cfg)
	snapshot, err := svc.GetEffectiveSettings(t.Context())
	require.NoError(t, err)
	client := effectiveItem(t, snapshot, SettingKeyGitHubOAuthClientID)
	require.Equal(t, "database-client", client.EffectiveValue)
	require.Equal(t, "database_override", client.Source)
	secret := effectiveItem(t, snapshot, SettingKeyGitHubOAuthClientSecret)
	require.True(t, secret.Configured)
	require.Nil(t, secret.EffectiveValue)
	require.Nil(t, secret.DefaultValue)
	require.Equal(t, "migration_snapshot", effectiveItem(t, snapshot, SettingKeyModelPlazaEnabled).Source)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "database-secret-not-public")
	require.NotContains(t, string(encoded), "deployment-secret-not-public")
}

func TestOAuthResetRestoresWholeInheritedProviderAndRejectsStaleVersion(t *testing.T) {
	repo := &effectiveSettingRepo{mockSettingRepo: newMockSettingRepo(), version: 2}
	repo.data[SettingKeyGitHubOAuthEnabled] = "false"
	repo.data[SettingKeyGitHubOAuthClientID] = "database-client"
	repo.data[SettingKeyGitHubOAuthClientSecret] = "database-secret"
	repo.data[SettingKeyGoogleOAuthClientID] = "keep-other-provider"
	cfg := &config.Config{GitHubOAuth: config.EmailOAuthProviderConfig{Enabled: true, ClientID: "deployment-client", ClientSecret: "deployment-secret"}}
	svc := NewSettingService(repo, cfg)
	_, err := svc.ResetSettingOverrides(t.Context(), ResetSettingOverridesRequest{ExpectedVersion: "1", Groups: []string{"github_oauth"}})
	require.ErrorIs(t, err, ErrSettingVersionConflict)
	require.Equal(t, "database-client", repo.data[SettingKeyGitHubOAuthClientID])
	_, err = svc.ResetSettingOverrides(t.Context(), ResetSettingOverridesRequest{ExpectedVersion: "2", Keys: []string{SettingKeyGitHubOAuthClientID}})
	require.Error(t, err, "individual OAuth fields cannot split a provider reset")
	snapshot, err := svc.ResetSettingOverrides(t.Context(), ResetSettingOverridesRequest{ExpectedVersion: "2", Groups: []string{"github_oauth"}})
	require.NoError(t, err)
	require.Equal(t, "3", snapshot.SettingVersion)
	require.NotContains(t, repo.data, SettingKeyGitHubOAuthClientID)
	require.NotContains(t, repo.data, SettingKeyGitHubOAuthClientSecret)
	require.NotContains(t, repo.data, SettingKeyGitHubOAuthEnabled)
	require.Equal(t, "keep-other-provider", repo.data[SettingKeyGoogleOAuthClientID])
	resolved := svc.effectiveEmailOAuthConfig(repo.data, "github")
	require.True(t, resolved.Enabled)
	require.Equal(t, "deployment-client", resolved.ClientID)
	require.Equal(t, "deployment-secret", resolved.ClientSecret)
	require.Equal(t, "inherit", effectiveItem(t, snapshot, SettingKeyGitHubOAuthClientID).OverrideState)
}

func TestSettingsResetRejectsDeploymentSecretsAndUnknownKeys(t *testing.T) {
	repo := &effectiveSettingRepo{mockSettingRepo: newMockSettingRepo(), version: 1}
	svc := NewSettingService(repo, &config.Config{})
	for _, key := range []string{"secrets_encryption_key", "totp_encryption_key", "arbitrary_unknown_key"} {
		_, err := svc.ResetSettingOverrides(t.Context(), ResetSettingOverridesRequest{ExpectedVersion: "1", Keys: []string{key}})
		require.Error(t, err)
	}
	require.Equal(t, 1, repo.version)
}

func TestModelPlazaDefaultIsIdenticalForAllProjections(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})
	for _, values := range []map[string]string{{}, {SettingKeyModelPlazaEnabled: "true"}, {SettingKeyModelPlazaEnabled: "false"}} {
		repo.data = values
		canonical := ModelPlazaRuntimeFromValues(values)
		require.Equal(t, canonical, svc.GetModelPlazaRuntime(t.Context()))
		require.Equal(t, canonical.Enabled, svc.parseSettings(values).ModelPlazaEnabled)
	}
	require.False(t, ModelPlazaRuntimeFromValues(nil).Enabled)
}

func TestUnchangedFullSettingsSaveDoesNotFreezeInheritedOAuth(t *testing.T) {
	repo := newMockSettingRepo()
	cfg := &config.Config{GitHubOAuth: config.EmailOAuthProviderConfig{Enabled: true, ClientID: "environment-id", ClientSecret: "environment-secret"}}
	svc := NewSettingService(repo, cfg)
	updates := map[string]string{
		SettingKeyGitHubOAuthEnabled:      "true",
		SettingKeyGitHubOAuthClientID:     "environment-id",
		SettingKeyGitHubOAuthClientSecret: "environment-secret",
		SettingKeyModelPlazaEnabled:       "false",
	}
	require.NoError(t, svc.preserveInheritedSettingValues(t.Context(), updates))
	require.Empty(t, updates)
	updates[SettingKeyGitHubOAuthClientID] = "new-id"
	require.NoError(t, svc.preserveInheritedSettingValues(t.Context(), updates))
	require.Equal(t, "new-id", updates[SettingKeyGitHubOAuthClientID])
	repo.data[SettingKeyGitHubOAuthClientID] = "environment-id"
	updates[SettingKeyGitHubOAuthClientID] = "environment-id"
	require.NoError(t, svc.preserveInheritedSettingValues(t.Context(), updates))
	require.Contains(t, updates, SettingKeyGitHubOAuthClientID, "explicit overrides are never implicitly reset")
}

func TestEffectiveOAuthEndpointsMatchLoginRuntime(t *testing.T) {
	for _, provider := range []string{"github", "google"} {
		for _, customAuthorize := range []string{"", "https://login.example.test/authorize"} {
			t.Run(provider+"/"+customAuthorize, func(t *testing.T) {
				providerConfig := config.EmailOAuthProviderConfig{
					Enabled: true, ClientID: "client", ClientSecret: "secret",
					RedirectURL: "https://console.example.test/callback", AuthorizeURL: customAuthorize,
				}
				cfg := &config.Config{}
				if provider == "github" {
					cfg.GitHubOAuth = providerConfig
				} else {
					cfg.GoogleOAuth = providerConfig
				}
				svc := NewSettingService(newMockSettingRepo(), cfg)
				runtime, err := svc.GetEmailOAuthProviderConfig(t.Context(), provider)
				require.NoError(t, err)
				snapshot, err := svc.GetEffectiveSettings(t.Context())
				require.NoError(t, err)
				for field, expected := range map[string]string{
					"authorize_url": runtime.AuthorizeURL, "token_url": runtime.TokenURL,
					"userinfo_url": runtime.UserInfoURL, "emails_url": runtime.EmailsURL,
					"scopes": runtime.Scopes, "frontend_redirect_url": runtime.FrontendRedirectURL,
				} {
					item := effectiveItem(t, snapshot, provider+"_oauth_"+field)
					require.Equal(t, expected, item.EffectiveValue, field)
				}
				item := effectiveItem(t, snapshot, provider+"_oauth_authorize_url")
				if customAuthorize == "" {
					require.Equal(t, "registry_default", item.Source)
					require.Equal(t, runtime.AuthorizeURL, item.DefaultValue)
				} else {
					require.Equal(t, "configuration", item.Source)
					require.NotEqual(t, customAuthorize, item.DefaultValue)
				}
			})
		}
	}
}
