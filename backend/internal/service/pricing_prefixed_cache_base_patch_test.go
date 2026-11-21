package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func prefixedCacheBaseTargets() []string {
	return []string{
		"vertex_ai/gemini-3-pro-preview",
		"vertex_ai/gemini-3.1-pro-preview",
		"vertex_ai/gemini-3.1-pro-preview-customtools",
		"openrouter/google/gemini-3-pro-preview",
	}
}

func TestPrefixedCacheBasePatch_OnlyEvidenceBackedExactIDs(t *testing.T) {
	var manifest struct {
		Reviews []struct {
			ModelID string            `json:"exact_model_id"`
			Field   string            `json:"field"`
			Status  string            `json:"status"`
			Value   *float64          `json:"value"`
			Sources []json.RawMessage `json:"sources"`
		} `json:"prefixed_source_review"`
	}
	body, err := os.ReadFile(filepath.Join("testdata", "cache_base_patch_manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &manifest))
	_, _, patch := cacheBasePatchFixture(t)
	require.Len(t, manifest.Reviews, len(prefixedCacheBaseTargets()))
	seen := map[string]bool{}
	for _, review := range manifest.Reviews {
		require.Contains(t, prefixedCacheBaseTargets(), review.ModelID)
		require.False(t, seen[review.ModelID])
		seen[review.ModelID] = true
		require.Equal(t, "cache_creation_input_token_cost", review.Field)
		require.Equal(t, "unproven", review.Status)
		require.Nil(t, review.Value, "no source is not a zero-price approval")
		require.NotEmpty(t, review.Sources)
		require.NotContains(t, patch, review.ModelID)
	}
	// Freeze the reverse boundary too: no unreviewed prefix may enter the patch.
	require.ElementsMatch(t, []string{"gemini-2.5-pro", "gemini-3-pro-preview", "gpt-5.4-pro", "gpt-5.4-pro-2026-03-05", "gpt-5.5-pro", "gpt-5.5-pro-2026-04-23"}, mapKeysForCachePatch(patch))
}

func mapKeysForCachePatch(patch map[string]map[string]float64) []string {
	keys := make([]string, 0, len(patch))
	for modelID := range patch {
		keys = append(keys, modelID)
	}
	return keys
}

func TestPrefixedCacheBasePatch_DoesNotInheritNativePrice(t *testing.T) {
	svc, catalogBody, _ := cacheBasePatchFixture(t)
	var catalog map[string]map[string]any
	require.NoError(t, json.Unmarshal(catalogBody, &catalog))
	// The control is an existing exact OpenRouter quote, not a price for 3 Pro.
	catalog["openrouter/google/gemini-2.5-pro"] = map[string]any{
		"input_cost_per_token": 1.25e-6, "output_cost_per_token": 1e-5,
		"cache_creation_input_token_cost": 3.75e-7, "litellm_provider": "openrouter",
	}
	body, err := json.Marshal(catalog)
	require.NoError(t, err)
	loadPricingCatalogSnapshot(t, svc, string(body))
	require.Equal(t, 1.25e-6, svc.pricingData["gemini-2.5-pro"].CacheCreationInputTokenCost)
	require.Equal(t, 3.75e-7, svc.pricingData["openrouter/google/gemini-2.5-pro"].CacheCreationInputTokenCost)
	for _, modelID := range prefixedCacheBaseTargets() {
		require.Nil(t, svc.pricingData[modelID].sourceQuotes["cache_creation_input_token_cost"].Value)
	}
	// A native quote change must not mutate either provider-prefixed identity.
	svc.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "operator.json")
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(`{"gemini-2.5-pro":{"cache_creation_input_token_cost":9e-6},"gemini-3-pro-preview":{"cache_creation_input_token_cost":8e-6}}`), 0600))
	loadPricingCatalogSnapshot(t, svc, string(body))
	require.Equal(t, 9e-6, svc.pricingData["gemini-2.5-pro"].CacheCreationInputTokenCost)
	require.Equal(t, 3.75e-7, svc.pricingData["openrouter/google/gemini-2.5-pro"].CacheCreationInputTokenCost)
	for _, modelID := range prefixedCacheBaseTargets() {
		require.Nil(t, svc.pricingData[modelID].sourceQuotes["cache_creation_input_token_cost"].Value)
	}
}

func TestPrefixedCacheBasePatch_UnprovenIDsKeepWarnings(t *testing.T) {
	svc, _, _ := cacheBasePatchFixture(t)
	logSink, restore := captureStructuredLog(t)
	defer restore()
	require.NoError(t, svc.Initialize())
	defer svc.Stop()
	for _, modelID := range prefixedCacheBaseTargets() {
		require.Nil(t, svc.pricingData[modelID].sourceQuotes["cache_creation_input_token_cost"].Value)
		require.True(t, logSink.ContainsMessage(modelID+"(cache_creation_input_token_cost_above_200k_tokens)"))
	}
}

func TestPrefixedCacheBasePatch_ReloadKeepsOtherFields(t *testing.T) {
	svc, catalogBody, _ := cacheBasePatchFixture(t)
	patchBody, err := os.ReadFile(svc.cfg.Pricing.OverrideFile)
	require.NoError(t, err)
	svc.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "operator.json")
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, patchBody, 0600))
	require.NoError(t, svc.Initialize())
	defer svc.Stop()
	before := make(map[string]LiteLLMModelPricing)
	for _, modelID := range prefixedCacheBaseTargets() {
		before[modelID] = *svc.pricingData[modelID]
	}
	// An operator edit elsewhere must not alter any field of the four targets.
	var patch map[string]map[string]float64
	require.NoError(t, json.Unmarshal(patchBody, &patch))
	patch["gpt-5.4-pro"]["output_cost_per_token"] = 7e-5
	customBody, err := json.Marshal(patch)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, customBody, 0600))
	svc.reloadIfCustomFilesChanged()
	cold := &PricingService{cfg: svc.cfg}
	require.NoError(t, cold.loadPricingData(svc.getPricingFilePath()))
	for _, modelID := range prefixedCacheBaseTargets() {
		require.Equal(t, before[modelID], *svc.pricingData[modelID])
		require.Equal(t, before[modelID], *cold.pricingData[modelID])
	}
	for _, loaded := range []*PricingService{svc, cold} {
		require.Equal(t, 7e-5, loaded.pricingData["gpt-5.4-pro"].OutputCostPerToken)
	}
	cachedBody, err := os.ReadFile(svc.getPricingFilePath())
	require.NoError(t, err)
	require.Equal(t, catalogBody, cachedBody, "raw tiers, priority, flex and cache-read fields must survive byte-for-byte")
	operatorBody, err := os.ReadFile(svc.cfg.Pricing.OverrideFile)
	require.NoError(t, err)
	require.Equal(t, customBody, operatorBody)
}

func TestPrefixedCacheBasePatch_BillingUsesProvenBase(t *testing.T) {
	svc, _, patch := cacheBasePatchFixture(t)
	require.NoError(t, svc.loadPricingData(svc.getPricingFilePath()))
	billing := NewBillingService(&config.Config{}, svc)
	for _, modelID := range []string{"gemini-2.5-pro", "gemini-3-pro-preview"} {
		cost, err := billing.CalculateCost(modelID, UsageTokens{InputTokens: 900, CacheCreationTokens: 100}, 1)
		require.NoError(t, err)
		require.InDelta(t, 100*patch[modelID]["cache_creation_input_token_cost"], cost.CacheCreationCost, 1e-12)
	}
	// No new prefix was proven. Check missing provenance, rather than asserting
	// that the legacy numeric zero in billing means these models are free.
	for _, modelID := range prefixedCacheBaseTargets() {
		require.Nil(t, svc.pricingData[modelID].sourceQuotes["cache_creation_input_token_cost"].Value,
			"the above-200k field must not be promoted to a base quote")
	}
}
