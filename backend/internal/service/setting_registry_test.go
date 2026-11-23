//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type registrySettingRepoStub struct {
	values     map[string]string
	readErr    error
	deleteKeys []string
	deleteErr  error
}

func (r *registrySettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}
func (r *registrySettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	item, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return item.Value, nil
}
func (r *registrySettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func (r *registrySettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := map[string]string{}
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, r.readErr
}
func (r *registrySettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}
func (r *registrySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	result := map[string]string{}
	for key, value := range r.values {
		result[key] = value
	}
	return result, r.readErr
}
func (r *registrySettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}
func (r *registrySettingRepoStub) DeleteMultiple(_ context.Context, keys []string) error {
	r.deleteKeys = append([]string{}, keys...)
	if r.deleteErr != nil {
		return r.deleteErr
	}
	for _, key := range keys {
		delete(r.values, key)
	}
	return nil
}

func registryService(repo *registrySettingRepoStub) *SettingService {
	return NewSettingService(repo, &config.Config{Server: config.ServerConfig{Mode: "release"}})
}

func effectiveItem(t *testing.T, svc *SettingService, key string) EffectiveSetting {
	t.Helper()
	page, err := svc.EffectiveSettings(context.Background(), EffectiveSettingsFilter{Query: key, Limit: 200})
	require.NoError(t, err)
	for _, item := range page.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("setting %q not returned", key)
	return EffectiveSetting{}
}

func TestEffectiveSettingsDatabaseOnlyPreservesExplicitZeroAndSource(t *testing.T) {
	t.Setenv("DEFAULT_USER_BALANCE", "99")
	repo := &registrySettingRepoStub{values: map[string]string{
		SettingKeyDefaultBalance: "0", SettingKeyDefaultConcurrency: "5",
		SettingKeyDefaultBalance + "_install_seed_source": "environment",
	}}
	svc := registryService(repo)
	balance := effectiveItem(t, svc, SettingKeyDefaultBalance)
	require.Equal(t, float64(0), balance.Value)
	require.Equal(t, "database", balance.Source)
	require.Equal(t, "environment", balance.InstallSeedSource)
	require.Equal(t, defaultRegistryBalance, svc.GetDefaultBalance(context.Background()))
	require.Equal(t, defaultRegistryConcurrency, svc.GetDefaultConcurrency(context.Background()))
	zeroConcurrency := registryService(&registrySettingRepoStub{values: map[string]string{SettingKeyDefaultConcurrency: "0"}})
	require.Equal(t, 0, zeroConcurrency.GetDefaultConcurrency(context.Background()))
}

func TestEffectiveSettingsDeploymentSourcesAndSecrets(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("log:\n  format: console\nlinuxdo_connect:\n  client_secret: yaml-secret\n"), 0o600))
	t.Setenv("CONFIG_FILE", configPath)
	t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "false")
	repo := &registrySettingRepoStub{values: map[string]string{}}
	svc := registryService(repo)

	refresh := effectiveItem(t, svc, "model_catalog_refresh_enabled")
	require.Equal(t, false, refresh.Value)
	require.Equal(t, "environment", refresh.Source)
	logFormat := effectiveItem(t, svc, "log.format")
	require.Equal(t, "console", logFormat.Value)
	require.Equal(t, "yaml", logFormat.Source)
	secret := effectiveItem(t, svc, SettingKeyLinuxDoConnectClientSecret)
	require.Nil(t, secret.Value)
	require.NotNil(t, secret.Configured)
	require.True(t, *secret.Configured)
	require.Equal(t, "yaml", secret.Source)

	raw, err := json.Marshal(secret)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "yaml-secret")
}

func TestEffectiveSettingsDistinguishesInvalidAndUnavailableSecurity(t *testing.T) {
	svc := registryService(&registrySettingRepoStub{values: map[string]string{SettingKeyRegistrationEnabled: "not-a-bool"}})
	invalid := effectiveItem(t, svc, SettingKeyRegistrationEnabled)
	require.Equal(t, EffectiveSettingStateInvalid, invalid.State)
	require.Equal(t, false, invalid.Value)

	svc = registryService(&registrySettingRepoStub{values: map[string]string{}, readErr: errors.New("database unavailable")})
	unavailable := effectiveItem(t, svc, SettingKeyRegistrationEnabled)
	require.Equal(t, EffectiveSettingStateUnavailable, unavailable.State)
	require.Equal(t, false, unavailable.Value)
}

func TestEffectiveSettingsHardGatesFailClosed(t *testing.T) {
	svc := NewSettingService(&registrySettingRepoStub{values: map[string]string{
		SettingKeyPasskeyEnabled: "true", SettingKeyOpsMonitoringEnabled: "true",
		SettingKeyTurnstileEnabled: "true",
	}}, &config.Config{Server: config.ServerConfig{Mode: "release"}, Turnstile: config.TurnstileConfig{Required: true}, Ops: config.OpsConfig{Enabled: false}})
	passkey := effectiveItem(t, svc, SettingKeyPasskeyEnabled)
	require.Equal(t, EffectiveSettingStateBlockedByHardGate, passkey.State)
	require.Equal(t, "webauthn.enabled", passkey.BlockedBy)
	require.Equal(t, false, passkey.Value)
	ops := effectiveItem(t, svc, SettingKeyOpsMonitoringEnabled)
	require.Equal(t, "ops.enabled", ops.BlockedBy)
	turnstile := effectiveItem(t, svc, SettingKeyTurnstileEnabled)
	require.Equal(t, false, turnstile.Value)
	require.Equal(t, "turnstile.required", turnstile.BlockedBy)
}

func TestResetConfigOverridesValidatesBeforeDeletingAndResetsProviderGroup(t *testing.T) {
	repo := &registrySettingRepoStub{values: map[string]string{
		SettingKeyLinuxDoConnectEnabled: "true", SettingKeyLinuxDoConnectClientID: "db-client",
	}}
	svc := registryService(repo)
	_, err := svc.ResetConfigOverrides(context.Background(), []string{SettingKeyLinuxDoConnectEnabled, SettingKeyDefaultBalance}, SettingResetHooks{})
	require.Equal(t, "CONFIG_KEY_NOT_RESETTABLE", infraerrors.Reason(err))
	require.Empty(t, repo.deleteKeys)
	require.Equal(t, "db-client", repo.values[SettingKeyLinuxDoConnectClientID])

	keys, err := svc.ResetConfigOverrides(context.Background(), []string{SettingKeyLinuxDoConnectEnabled}, SettingResetHooks{})
	require.NoError(t, err)
	require.Contains(t, keys, SettingKeyLinuxDoConnectClientSecret)
	require.NotContains(t, repo.values, SettingKeyLinuxDoConnectEnabled)
	require.NotContains(t, repo.values, SettingKeyLinuxDoConnectClientID)
}

func TestSeedInitialSettingsOnlyFillsMissingValues(t *testing.T) {
	repo := &registrySettingRepoStub{values: map[string]string{
		SettingKeyRegistrationEnabled: "false", SettingKeyDefaultBalance: "0",
		SettingKeyDefaultConcurrency: "0", SettingKeyModelPlazaDescription: "",
	}}
	svc := registryService(repo)
	require.NoError(t, svc.SeedInitialSettings(context.Background(), true))
	require.Equal(t, "false", repo.values[SettingKeyRegistrationEnabled])
	require.Equal(t, "0", repo.values[SettingKeyDefaultBalance])
	require.Equal(t, "0", repo.values[SettingKeyDefaultConcurrency])
	require.Equal(t, "", repo.values[SettingKeyModelPlazaDescription])
	require.Equal(t, "true", repo.values[SettingKeyModelPlazaEnabled])
	require.Equal(t, "false", repo.values[SettingKeyTotpEnabled])
}

func TestEffectiveMonitoringStateFailsClosed(t *testing.T) {
	svc := &OpsService{settingRepo: &registrySettingRepoStub{values: map[string]string{}, readErr: errors.New("unavailable")}}
	enabled, healthy, available := svc.EffectiveMonitoringState(context.Background())
	require.False(t, enabled)
	require.False(t, healthy)
	require.False(t, available)

	svc = &OpsService{settingRepo: &registrySettingRepoStub{values: map[string]string{SettingKeyOpsMonitoringEnabled: "bad"}}}
	enabled, healthy, available = svc.EffectiveMonitoringState(context.Background())
	require.False(t, enabled)
	require.False(t, healthy)
	require.True(t, available)
}

func TestTurnstileRequiredFailsClosedWithoutCompleteProvider(t *testing.T) {
	for _, stored := range []map[string]string{
		{},
		{SettingKeyTurnstileEnabled: "true"},
		{SettingKeyTurnstileEnabled: "invalid", SettingKeyTurnstileSecretKey: "stored"},
	} {
		svc := NewSettingService(&registrySettingRepoStub{values: stored}, &config.Config{Server: config.ServerConfig{Mode: "release"}, Turnstile: config.TurnstileConfig{Required: true}})
		item := effectiveItem(t, svc, SettingKeyTurnstileEnabled)
		require.Equal(t, false, item.Value)
		require.Equal(t, "turnstile.required", item.BlockedBy)
	}
}

func TestRegistryCoversExistingDefaultAndWriteKeys(t *testing.T) {
	svc := registryService(&registrySettingRepoStub{values: map[string]string{}})
	registry := map[string]bool{}
	for _, registration := range svc.SettingRegistry() {
		require.False(t, registry[registration.Key], "duplicate registry key")
		registry[registration.Key] = true
	}
	for key := range svc.installSettingDefaults(false) {
		require.True(t, registry[key], "install default has no registration: %s", key)
	}
	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{CodexCLIOnlyEngineFingerprintSignals: "[]"})
	require.NoError(t, err)
	for key := range updates {
		require.True(t, registry[key], "write key has no registration: %s", key)
	}
	for key := range svc.databaseSettingDefaults() {
		require.NotContains(t, strings.ToLower(key), "password", "passwords must be explicitly redacted")
	}
}

func TestRegistryDeclaresRequiredDefaultsAndResetAuthority(t *testing.T) {
	registry := NewSettingService(nil, nil).SettingRegistry()
	for _, registration := range registry {
		require.NotEmpty(t, registration.Key)
		require.NotEmpty(t, registration.Group)
		require.NotEmpty(t, registration.ValueType)
		require.NotEmpty(t, registration.Authority)
		require.NotEmpty(t, registration.ErrorPolicy)
		require.NotEmpty(t, registration.Validation)
		if registration.Resettable {
			require.Equal(t, SettingAuthorityDatabaseOverride, registration.Authority)
		}
	}
}
