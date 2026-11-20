package modelpricing_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
	"github.com/stretchr/testify/require"
)

func TestPiPriceSupplementUsesExactDecimalTokenRates(t *testing.T) {
	type reference struct {
		Provider    string      `json:"litellm_provider"`
		Input       json.Number `json:"input_cost_per_token"`
		Output      json.Number `json:"output_cost_per_token"`
		CacheRead   json.Number `json:"cache_read_input_token_cost"`
		CacheWrite  json.Number `json:"cache_creation_input_token_cost"`
		InputLimit  int         `json:"max_input_tokens"`
		OutputLimit int         `json:"max_output_tokens"`
	}
	var prices map[string]reference
	require.NoError(t, json.Unmarshal(modelpricing.PiSupplementJSON(), &prices))
	require.Greater(t, len(prices), 500)
	require.Equal(t, reference{
		Provider: "gemini", Input: "0.000000075", Output: "0.0000003",
		CacheRead: "0.00000001875", CacheWrite: "0", InputLimit: 1000000, OutputLimit: 8192,
	}, prices["gemini-1.5-flash"])
	require.Equal(t, prices["gemini-1.5-flash"], prices["gemini/gemini-1.5-flash"])
	require.Equal(t, json.Number("0.000001"), prices["anthropic/claude-3-5-haiku-20241022"].CacheWrite)
	require.Equal(t, "openrouter", prices["openrouter/openai/gpt-5"].Provider)
	require.Equal(t, "openai", prices["openai/gpt-5"].Provider)
	require.NotContains(t, prices, "openrouter/openrouter/auto")
	require.NotContains(t, prices, "github-copilot/claude-haiku-4.5")
}

func TestPiPriceSupplementIsLowestPriority(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Pricing.UpdateIntervalHours = 24
	cfg.Pricing.FallbackFile = filepath.Join(cfg.Pricing.DataDir, "fallback.json")
	cfg.Pricing.OverrideFile = filepath.Join(cfg.Pricing.DataDir, "override.json")
	require.NoError(t, os.WriteFile(filepath.Join(cfg.Pricing.DataDir, "model_pricing.json"), []byte(`{
		"gemini-1.5-pro": {"input_cost_per_token": 0.000003, "litellm_provider": "gemini"}
	}`), 0600))
	require.NoError(t, os.WriteFile(cfg.Pricing.FallbackFile, []byte(`{
		"groq/deepseek-r1-distill-llama-70b": {"input_cost_per_token": 0.000004, "litellm_provider": "groq"}
	}`), 0600))
	require.NoError(t, os.WriteFile(cfg.Pricing.OverrideFile, []byte(`{
		"anthropic/claude-3-5-haiku-20241022": {"input_cost_per_token": 0.000006, "litellm_provider": "anthropic"}
	}`), 0600))
	svc := service.NewPricingService(cfg, nil)
	require.NoError(t, svc.Initialize())
	t.Cleanup(svc.Stop)

	cases := map[string]float64{
		"gemini-1.5-pro":                      0.000003,
		"groq/deepseek-r1-distill-llama-70b":  0.000004,
		"anthropic/claude-3-5-haiku-20241022": 0.000006,
		"gemini-1.5-flash":                    0.000000075,
		"gemini/gemini-1.5-flash":             0.000000075,
	}
	for model, rate := range cases {
		got := svc.GetModelPricing(model)
		require.NotNil(t, got, model)
		require.Equal(t, rate, got.InputCostPerToken, model)
	}
	require.Nil(t, svc.GetModelPricing("openrouter/gemini-1.5-flash"))
	require.Nil(t, svc.GetIdentifiedModelPricing("gemini-1.5-flash"))
}
