package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCatalogAndAffinityDefaults(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "")
	t.Setenv("GATEWAY_SESSION_AFFINITY_ENABLED", "")
	t.Setenv("GATEWAY_AFFINITY_HMAC_KEY", "")
	t.Setenv("GATEWAY_AFFINITY_HMAC_KEY_VERSION", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.ModelCatalogRefreshEnabled)
	require.True(t, cfg.Gateway.SessionAffinityEnabled)
	require.Empty(t, cfg.Gateway.AffinityHMACKey)
	require.Empty(t, cfg.Gateway.AffinityHMACKeyVersion)
}

func TestLoadCatalogAndAffinityOptIn(t *testing.T) {
	for _, source := range []string{"environment", "yaml"} {
		t.Run(source, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "")
			t.Setenv("GATEWAY_SESSION_AFFINITY_ENABLED", "")
			t.Setenv("GATEWAY_AFFINITY_HMAC_KEY", "")
			t.Setenv("GATEWAY_AFFINITY_HMAC_KEY_VERSION", "")
			if source == "environment" {
				t.Setenv("MODEL_CATALOG_REFRESH_ENABLED", "true")
				t.Setenv("GATEWAY_SESSION_AFFINITY_ENABLED", "true")
				t.Setenv("GATEWAY_AFFINITY_HMAC_KEY", "shared-secret")
				t.Setenv("GATEWAY_AFFINITY_HMAC_KEY_VERSION", "rotation-label")
			} else {
				configPath := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.WriteFile(configPath, []byte("model_catalog_refresh_enabled: true\ngateway:\n  session_affinity_enabled: true\n  affinity_hmac_key: shared-secret\n  affinity_hmac_key_version: rotation-label\n"), 0o600))
				t.Setenv("CONFIG_FILE", configPath)
			}
			cfg, err := Load()
			require.NoError(t, err)
			require.True(t, cfg.ModelCatalogRefreshEnabled)
			require.True(t, cfg.Gateway.SessionAffinityEnabled)
			require.Equal(t, "shared-secret", cfg.Gateway.AffinityHMACKey)
			require.Equal(t, "rotation-label", cfg.Gateway.AffinityHMACKeyVersion)
		})
	}
}

func TestLoadRejectsPartialAffinityHMAC(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, testCase := range []struct {
			name, key, version, missing string
		}{
			{name: "missing version", key: "shared-secret", missing: "gateway.affinity_hmac_key_version is required"},
			{name: "missing key", version: "rotation-label", missing: "gateway.affinity_hmac_key is required"},
		} {
			t.Run("enabled="+enabled+"/"+testCase.name, func(t *testing.T) {
				resetViperWithJWTSecret(t)
				t.Setenv("GATEWAY_SESSION_AFFINITY_ENABLED", enabled)
				t.Setenv("GATEWAY_AFFINITY_HMAC_KEY", testCase.key)
				t.Setenv("GATEWAY_AFFINITY_HMAC_KEY_VERSION", testCase.version)
				_, err := Load()
				require.ErrorContains(t, err, testCase.missing)
				require.NotContains(t, err.Error(), "shared-secret")
			})
		}
	}
}
