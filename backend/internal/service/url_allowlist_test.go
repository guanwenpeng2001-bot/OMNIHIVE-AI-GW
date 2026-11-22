package service

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestURLAllowlistServiceCallers(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist = config.URLAllowlistConfig{UpstreamHosts: []string{"listed.example"}, PricingHosts: []string{"listed.example"}}
	validators := map[string]func(string) (string, error){
		"account": (&AccountTestService{cfg: cfg}).validateUpstreamBaseURL,
		"gateway": (&GatewayService{cfg: cfg}).validateUpstreamBaseURL,
		"openai":  (&OpenAIGatewayService{cfg: cfg}).validateOutboundURL,
		"gemini":  (&GeminiMessagesCompatService{cfg: cfg}).validateUpstreamBaseURL,
		"cn":      func(raw string) (string, error) { return cnValidateProbeURL(cfg, raw) },
		"grok":    grokOperatorPolicyValidator(cfg),
		"pricing": (&PricingService{cfg: cfg}).validatePricingURL,
	}
	for name, validate := range validators {
		t.Run(name, func(t *testing.T) {
			cfg.Security.URLAllowlist.Enabled = false
			_, err := validate("https://red-river-ztn9ajl8.sfo.unikraft.app")
			require.NoError(t, err)
			for _, target := range []string{"http://example.com", "https://127.0.0.1", "https://169.254.169.254", "https://10.0.0.1"} {
				_, err = validate(target)
				require.Error(t, err, target)
			}
			cfg.Security.URLAllowlist.Enabled = true
			_, err = validate("https://unlisted.example")
			require.Error(t, err)
			_, err = validate("https://listed.example")
			require.NoError(t, err)
		})
	}
}
