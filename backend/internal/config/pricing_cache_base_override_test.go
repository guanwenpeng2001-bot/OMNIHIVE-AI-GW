package config

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPricingCacheBaseOverrideDefaultAndExplicitConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		content  string
		expected string
	}{
		{"default", "pricing: {}", "./resources/model-pricing/cache-base-overrides.json"},
		{"operator file", "pricing:\n  override_file: ./data/operator.json", "./data/operator.json"},
		{"explicit disable", "pricing:\n  override_file: ''", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("PRICING_OVERRIDE_FILE", "")
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(testCase.content), 0600))
			t.Setenv("CONFIG_FILE", configPath)
			cfg, err := Load()
			require.NoError(t, err)
			require.Equal(t, testCase.expected, cfg.Pricing.OverrideFile)
		})
	}
}
