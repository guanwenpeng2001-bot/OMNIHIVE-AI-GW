package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/runtimeenv"
	"github.com/stretchr/testify/require"
)

func TestEffectiveConfigurationSourcesAndLegacyDefaults(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "existing"}[installed], func(t *testing.T) {
			resetViperWithJWTSecret(t)
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv(runtimeenv.DataDirKey, dir)
			t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "")
			t.Setenv("LOG_FORMAT", "")
			if installed {
				require.NoError(t, os.WriteFile(filepath.Join(dir, runtimeenv.InstallLockFile), nil, 0600))
			}
			cfg, err := Load()
			require.NoError(t, err)
			require.Equal(t, installed, cfg.ModelCatalogRefreshEnabled)
			if installed {
				require.Equal(t, "migration_snapshot", cfg.SourceFor("model_catalog_refresh_enabled"))
				require.Equal(t, "console", cfg.Log.Format)
			} else {
				require.Equal(t, "registry_default", cfg.SourceFor("model_catalog_refresh_enabled"))
				require.Equal(t, "json", cfg.Log.Format)
			}
			t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "false")
			cfg, err = Load()
			require.NoError(t, err)
			require.False(t, cfg.ModelCatalogRefreshEnabled)
			require.Equal(t, "environment", cfg.SourceFor("model_catalog_refresh_enabled"))
			t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "true")
			require.Equal(t, "environment", cfg.SourceFor("model_catalog_refresh_enabled"))
			require.False(t, cfg.ModelCatalogRefreshEnabled, "a running config is not changed by diagnostics")
		})
	}
}

func TestIntegrationSecretKeyEnvironmentAndValidation(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Chdir(t.TempDir())
	t.Setenv("SECRETS_ENCRYPTION_KEY", strings.Repeat("a1", 32))
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("a1", 32), cfg.Secrets.EncryptionKey)
	require.Equal(t, "environment", cfg.SourceFor("secrets.encryption_key"))
	t.Setenv("SECRETS_ENCRYPTION_KEY", "invalid-secret-must-not-be-echoed")
	_, err = Load()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "invalid-secret-must-not-be-echoed")
}

func TestReleaseCompatibilityUsesSemanticFloor(t *testing.T) {
	require.Equal(t, "0.3.0", CurrentReleaseCompatibility().SchemaRollbackFloor)
	require.Equal(t, "0.3.5", CurrentReleaseCompatibility().SemanticRollbackFloor)
	require.Equal(t, "v0.3.5", MinimumRollbackVersion())
}
