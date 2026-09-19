package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestPricingOfficialDefaultAndCustomSource(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json", cfg.Pricing.RemoteURL)
	require.Empty(t, cfg.Pricing.HashURL)
	viper.Set("pricing.remote_url", "https://pricing.example.test/catalog.json")
	viper.Set("pricing.hash_url", "https://pricing.example.test/catalog.sha256")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, "https://pricing.example.test/catalog.json", cfg.Pricing.RemoteURL)
	require.Equal(t, "https://pricing.example.test/catalog.sha256", cfg.Pricing.HashURL)
}
