package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Adapted from the final audit: run PricingService in a native release directory,
// with the actual default path rather than an unrelated custom absolute path.
func TestAuditBinaryDefaultCacheRepair(t *testing.T) {
	patch, err := os.ReadFile("../../resources/model-pricing/cache-base-overrides.json")
	require.NoError(t, err)
	for _, packaged := range []bool{false, true} {
		t.Run(map[bool]string{false: "release_archive_layout", true: "resource_present_control"}[packaged], func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			override := "./resources/model-pricing/cache-base-overrides.json"
			if packaged {
				require.NoError(t, os.MkdirAll(filepath.Dir(override), 0700))
				require.NoError(t, os.WriteFile(override, patch, 0600))
			}
			pricing := NewPricingService(&config.Config{Pricing: config.PricingConfig{DataDir: directory, OverrideFile: override}}, nil)
			require.NoError(t, os.WriteFile(pricing.getPricingFilePath(), []byte(`{"gpt-5.4-pro":{"input_cost_per_token":3e-5,"output_cost_per_token":1.8e-4,"cache_read_input_token_cost_above_272k_tokens":6e-6}}`), 0600))
			require.NoError(t, pricing.loadPricingData(pricing.getPricingFilePath()))
			actual := pricing.pricingData["gpt-5.4-pro"].CacheReadInputTokenCost
			t.Logf("override_present=%t effective_cache_read=%g expected=3e-6", packaged, actual)
			require.Equal(t, 3e-6, actual)
		})
	}
}

func TestPricingReleaseOverridePrecedence(t *testing.T) {
	for _, scenario := range []struct {
		name string
		path string
		body string
		want float64
	}{
		{name: "custom_replaces_default", path: "custom.json", body: `{"gpt-5.4-pro":{"cache_read_input_token_cost":9e-6}}`, want: 9e-6},
		{name: "custom_does_not_merge_default", path: "custom.json", body: `{}`},
		{name: "missing_custom_does_not_use_default", path: "missing.json"},
		{name: "explicit_empty_disables_default"},
		{name: "default_file_precedes_embed", path: "./resources/model-pricing/cache-base-overrides.json", body: `{"gpt-5.4-pro":{"cache_read_input_token_cost":8e-6}}`, want: 8e-6},
		{name: "invalid_default_does_not_use_embed", path: "./resources/model-pricing/cache-base-overrides.json", body: `invalid`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if scenario.body != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(scenario.path), 0700))
				require.NoError(t, os.WriteFile(scenario.path, []byte(scenario.body), 0600))
			}
			pricing := NewPricingService(&config.Config{Pricing: config.PricingConfig{OverrideFile: scenario.path}}, nil)
			data, err := pricing.parsePricingData([]byte(`{"gpt-5.4-pro":{"input_cost_per_token":3e-5,"output_cost_per_token":1.8e-4,"cache_read_input_token_cost_above_272k_tokens":6e-6}}`))
			require.NoError(t, err)
			require.Equal(t, scenario.want, data["gpt-5.4-pro"].CacheReadInputTokenCost)
		})
	}
}
