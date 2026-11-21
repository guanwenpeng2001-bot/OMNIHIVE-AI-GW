package config

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
	"github.com/stretchr/testify/require"
)

func TestURLAllowlistValidationOptionsDefaults(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	c := cfg.Security.URLAllowlist
	require.False(t, c.Enabled)
	require.NotEmpty(t, c.UpstreamHosts)
	for name, opts := range map[string]urlvalidator.ValidationOptions{
		"upstream": c.UpstreamValidationOptions(), "pricing": c.PricingValidationOptions(), "crs": c.CRSValidationOptions(),
	} {
		t.Run(name, func(t *testing.T) {
			require.Empty(t, opts.AllowedHosts)
			for _, target := range []string{"https://red-river-ztn9ajl8.sfo.unikraft.app", "https://api.openai.com"} {
				_, err := urlvalidator.ValidateHTTPURL(target, c.AllowInsecureHTTP, opts)
				require.NoError(t, err)
			}
			for _, target := range []string{"http://example.com", "https://127.0.0.1", "https://169.254.169.254", "https://10.0.0.1", "https://localhost", "https://[::1]"} {
				_, err := urlvalidator.ValidateHTTPURL(target, c.AllowInsecureHTTP, opts)
				require.Error(t, err, target)
			}
		})
	}
}

func TestURLAllowlistValidationOptionsEnabled(t *testing.T) {
	c := URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"upstream.example"}, PricingHosts: []string{"pricing.example"}, CRSHosts: []string{"crs.example"}}
	for host, opts := range map[string]urlvalidator.ValidationOptions{
		"upstream.example": c.UpstreamValidationOptions(), "pricing.example": c.PricingValidationOptions(), "crs.example": c.CRSValidationOptions(),
	} {
		_, err := urlvalidator.ValidateHTTPURL("https://"+host, false, opts)
		require.NoError(t, err)
		_, err = urlvalidator.ValidateHTTPURL("https://unlisted.example", false, opts)
		require.Error(t, err)
	}
	c.UpstreamHosts = nil
	_, err := urlvalidator.ValidateHTTPURL("https://upstream.example", false, c.UpstreamValidationOptions())
	require.Error(t, err)
	c.Enabled = false
	c.AllowPrivateHosts = true
	opts := c.UpstreamValidationOptions()
	require.True(t, opts.AllowPrivate)
	require.False(t, opts.RequireAllowlist)
}
