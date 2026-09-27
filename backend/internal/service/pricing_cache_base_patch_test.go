package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// This prepares the existing override pipeline for the proven subset of the
// restoration. It is not a production patch or evidence that all twelve prices
// have been recovered: the provider-prefixed entries still need exact sources.
func TestCacheBasePatch_ProvenSourceFieldsUseExistingPipeline(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json")
	sourceBody, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	var sourceEntries map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sourceBody, &sourceEntries))

	for _, reviewed := range []struct {
		modelID string
		field   string
		oldBase float64
	}{
		{"gemini-2.5-pro", "cache_creation_input_token_cost", 1.25e-6},
		{"gemini-3-pro-preview", "cache_creation_input_token_cost", 2e-6},
		{"gemini-3.1-pro-preview", "cache_creation_input_token_cost", 2e-6},
		{"gemini-3.1-pro-preview-customtools", "cache_creation_input_token_cost", 2e-6},
		{"gpt-5.4-pro", "cache_read_input_token_cost", 3e-6},
		{"gpt-5.4-pro-2026-03-05", "cache_read_input_token_cost", 3e-6},
		{"gpt-5.5-pro", "cache_read_input_token_cost", 3e-6},
		{"gpt-5.5-pro-2026-04-23", "cache_read_input_token_cost", 3e-6},
	} {
		t.Run(reviewed.modelID, func(t *testing.T) {
			modelID, field := reviewed.modelID, reviewed.field
			entry := make(map[string]json.RawMessage)
			for field, value := range sourceEntries[modelID] {
				entry[field] = value
			}
			var approvedBase float64
			require.NoError(t, json.Unmarshal(entry[field], &approvedBase))
			require.Equal(t, reviewed.oldBase, approvedBase, "restore only the exact reviewed source field")
			patchBody, err := json.Marshal(map[string]map[string]json.RawMessage{
				modelID: {field: entry[field]},
			})
			require.NoError(t, err)
			delete(entry, field)
			// Preserve the observed upstream tier discrepancy; restoring a base
			// does not authorize replacing the tier with the bundled source value.
			if modelID == "gemini-2.5-pro" {
				entry["cache_creation_input_token_cost_above_200k_tokens"] = json.RawMessage(`2.5e-07`)
			}
			catalogBody, err := json.Marshal(map[string]map[string]json.RawMessage{modelID: entry})
			require.NoError(t, err)
			directory := t.TempDir()
			svc := &PricingService{cfg: &config.Config{Pricing: config.PricingConfig{
				DataDir: directory, FallbackFile: sourcePath,
				OverrideFile: filepath.Join(directory, "overrides.json"),
			}}}
			require.NoError(t, os.WriteFile(svc.getPricingFilePath(), catalogBody, 0600))
			require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(`{}`), 0600))
			require.NoError(t, svc.loadPricingData(svc.getPricingFilePath()))
			before := *svc.pricingData[modelID]
			if field == "cache_creation_input_token_cost" {
				require.Zero(t, before.CacheCreationInputTokenCost, "fallback must not fill an existing model's missing field")
			} else {
				require.Zero(t, before.CacheReadInputTokenCost)
			}
			unrelated := *svc.pricingData["claude-sonnet-4-5"]
			anchor := svc.localHash
			require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, patchBody, 0600))
			svc.reloadIfCustomFilesChanged()
			if field == "cache_creation_input_token_cost" {
				before.CacheCreationInputTokenCost = approvedBase
			} else {
				before.CacheReadInputTokenCost = approvedBase
			}
			quote := before.sourceQuotes[field]
			quote.Value = &approvedBase
			before.sourceQuotes[field] = quote
			require.Equal(t, before, *svc.pricingData[modelID], "only the proven base field may change")
			require.Equal(t, unrelated, *svc.pricingData["claude-sonnet-4-5"])
			require.Equal(t, anchor, svc.localHash)
			cachedBody, err := os.ReadFile(svc.getPricingFilePath())
			require.NoError(t, err)
			require.Equal(t, catalogBody, cachedBody, "override must not rewrite the downloaded body or its tiers")

			// The same patch survives remote refresh and a subsequent cold load.
			svc.cfg.Pricing.RemoteURL = "https://example.com/pricing.json"
			svc.remoteClient = stubPricingRemoteClient{body: string(catalogBody)}
			require.NoError(t, svc.downloadPricingData())
			require.Equal(t, before, *svc.pricingData[modelID])
			downloadHash := sha256.Sum256(catalogBody)
			require.Equal(t, hex.EncodeToString(downloadHash[:]), svc.localHash)
			coldService := &PricingService{cfg: svc.cfg}
			require.NoError(t, coldService.loadPricingData(svc.getPricingFilePath()))
			require.Equal(t, before, *coldService.pricingData[modelID])

			// A missing repair file must be detectable by effective-value checks.
			require.NoError(t, os.Remove(svc.cfg.Pricing.OverrideFile))
			svc.reloadIfCustomFilesChanged()
			if field == "cache_creation_input_token_cost" {
				require.Zero(t, svc.pricingData[modelID].CacheCreationInputTokenCost)
			} else {
				require.Zero(t, svc.pricingData[modelID].CacheReadInputTokenCost)
			}
		})
	}
}
