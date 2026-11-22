package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// The six exact fields approved for restoration use the shipped patch through
// the existing pipeline; the remaining six audit entries are deliberately deferred.
func TestCacheBasePatch_ProvenSourceFieldsUseExistingPipeline(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json")
	sourceBody, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	var sourceEntries map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sourceBody, &sourceEntries))
	patchBody, err := os.ReadFile(filepath.Join(filepath.Dir(sourcePath), "cache-base-overrides.json"))
	require.NoError(t, err)
	var approvedPatch map[string]map[string]float64
	require.NoError(t, json.Unmarshal(patchBody, &approvedPatch))
	require.Len(t, approvedPatch, 6)

	for _, reviewed := range []struct {
		modelID   string
		field     string
		dimension string
		oldBase   float64
		exactBase string
	}{
		{"gemini-2.5-pro", "cache_creation_input_token_cost", "cache_write", 1.25e-6, "0.00000125"},
		{"gemini-3-pro-preview", "cache_creation_input_token_cost", "cache_write", 2e-6, "0.000002"},
		{"gpt-5.4-pro", "cache_read_input_token_cost", "cache_read", 3e-6, "0.000003"},
		{"gpt-5.4-pro-2026-03-05", "cache_read_input_token_cost", "cache_read", 3e-6, "0.000003"},
		{"gpt-5.5-pro", "cache_read_input_token_cost", "cache_read", 3e-6, "0.000003"},
		{"gpt-5.5-pro-2026-04-23", "cache_read_input_token_cost", "cache_read", 3e-6, "0.000003"},
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
			require.Equal(t, map[string]float64{field: approvedBase}, approvedPatch[modelID])
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
			before.exactTokenRates = maps.Clone(before.exactTokenRates)
			require.NotContains(t, before.exactTokenRates, reviewed.dimension, "missing base rates must not be synthesized from tiers")
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
			// Exact billing retains the decimal form of this same approved field.
			// The full comparison still rejects changes to any other rate or tier.
			before.exactTokenRates[reviewed.dimension] = reviewed.exactBase
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
			billing := NewBillingService(&config.Config{}, coldService)
			pricing, err := billing.GetModelPricing(modelID)
			require.NoError(t, err)
			for _, totalInput := range []int{1000, pricing.LongContextInputThreshold, pricing.LongContextInputThreshold + 1} {
				tokens := UsageTokens{InputTokens: totalInput - 100, OutputTokens: 10}
				if field == "cache_creation_input_token_cost" {
					tokens.CacheCreationTokens = 100
				} else {
					tokens.CacheReadTokens = 100
				}
				cost, err := billing.CalculateCost(modelID, tokens, 1)
				require.NoError(t, err)
				multiplier := 1.0
				if totalInput > pricing.LongContextInputThreshold {
					multiplier = pricing.LongContextInputMultiplier
				}
				require.Equal(t, totalInput > pricing.LongContextInputThreshold, cost.LongContextBillingApplied)
				if field == "cache_creation_input_token_cost" {
					require.InDelta(t, 100*approvedBase*multiplier, cost.CacheCreationCost, 1e-12)
				} else {
					require.InDelta(t, 100*approvedBase*multiplier, cost.CacheReadCost, 1e-12)
				}
			}

			// A missing repair file must be detectable by effective-value checks.
			require.NoError(t, os.Remove(svc.cfg.Pricing.OverrideFile))
			svc.reloadIfCustomFilesChanged()
			if field == "cache_creation_input_token_cost" {
				require.Zero(t, svc.pricingData[modelID].CacheCreationInputTokenCost)
			} else {
				require.Zero(t, svc.pricingData[modelID].CacheReadInputTokenCost)
			}
			require.NotContains(t, svc.pricingData[modelID].exactTokenRates, reviewed.dimension)
		})
	}
}
func cacheBasePatchFixture(t *testing.T) (*PricingService, []byte, map[string]map[string]float64) {
	t.Helper()
	var manifest struct {
		RuntimeEntries map[string]map[string]json.RawMessage `json:"runtime_entries"`
	}
	body, err := os.ReadFile(filepath.Join("testdata", "cache_base_patch_manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &manifest))
	patchPath := filepath.Join("..", "..", "resources", "model-pricing", "cache-base-overrides.json")
	patchBody, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	var patch map[string]map[string]float64
	require.NoError(t, json.Unmarshal(patchBody, &patch))
	catalogBody, err := json.Marshal(manifest.RuntimeEntries)
	require.NoError(t, err)
	directory := t.TempDir()
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{
		DataDir: directory, UpdateIntervalHours: 24,
		FallbackFile: filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"),
		OverrideFile: patchPath,
	}}, nil)
	require.NoError(t, os.WriteFile(svc.getPricingFilePath(), catalogBody, 0600))
	return svc, catalogBody, patch
}

func TestCacheBasePatch_InitializeAndReloadKeepDeferredEntriesUnknown(t *testing.T) {
	svc, catalogBody, patch := cacheBasePatchFixture(t)
	require.NoError(t, svc.Initialize())
	defer svc.Stop()
	// The current upstream no longer contains gemini-3-pro-preview; the fallback
	// restores the complete entry before the field patch is applied.
	for modelID, fields := range patch {
		for field, expected := range fields {
			if field == "cache_creation_input_token_cost" {
				require.Equal(t, expected, svc.pricingData[modelID].CacheCreationInputTokenCost)
			} else {
				require.Equal(t, expected, svc.pricingData[modelID].CacheReadInputTokenCost)
			}
		}
	}
	logSink, restore := captureStructuredLog(t)
	defer restore()
	require.NoError(t, svc.loadPricingData(svc.getPricingFilePath()))
	require.False(t, logSink.ContainsMessage("base cache price dropped"))
	for _, modelID := range []string{
		"gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools",
		"vertex_ai/gemini-3-pro-preview", "vertex_ai/gemini-3.1-pro-preview",
		"vertex_ai/gemini-3.1-pro-preview-customtools", "openrouter/google/gemini-3-pro-preview",
	} {
		require.NotContains(t, patch, modelID, "deferred IDs must not be guessed or aliased")
		require.Zero(t, svc.pricingData[modelID].CacheCreationInputTokenCost)
		require.NotContains(t, svc.pricingData[modelID].exactTokenRates, "cache_write")
		require.True(t, logSink.ContainsMessage(modelID+"(cache_creation_input_token_cost_above_200k_tokens)"))
	}
	// The dropped-price sentinel remains active for an unrelated synthetic model.
	var catalog map[string]map[string]any
	require.NoError(t, json.Unmarshal(catalogBody, &catalog))
	catalog["unrelated-cache-sentinel"] = map[string]any{"input_cost_per_token": 1e-6, "cache_read_input_token_cost": 1e-7}
	before, err := json.Marshal(catalog)
	require.NoError(t, err)
	loadPricingCatalogSnapshot(t, svc, string(before))
	delete(catalog["unrelated-cache-sentinel"], "cache_read_input_token_cost")
	after, err := json.Marshal(catalog)
	require.NoError(t, err)
	svc.cfg.Pricing.RemoteURL = "https://example.com/pricing.json"
	svc.remoteClient = stubPricingRemoteClient{body: string(after)}
	require.NoError(t, svc.downloadPricingData())
	require.True(t, logSink.ContainsMessage("unrelated-cache-sentinel(cache_read_input_token_cost)"))
	for modelID, fields := range patch {
		for field, expected := range fields {
			require.False(t, logSink.ContainsMessage(modelID+"("+field+")"), "approved fields must not trigger dropped warnings")
			quote := svc.pricingData[modelID].sourceQuotes[field]
			require.NotNil(t, quote.Value)
			require.Equal(t, expected, *quote.Value)
		}
	}
}

func TestCacheBasePatch_ExplicitOperatorFileKeepsItsOwnFieldsAndConflicts(t *testing.T) {
	svc, _, patch := cacheBasePatchFixture(t)
	svc.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "operator.json")
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(`{"gpt-5.4-pro":{"cache_read_input_token_cost":9e-6,"output_cost_per_token":7e-5}}`), 0600))
	require.NoError(t, svc.loadPricingData(svc.getPricingFilePath()))
	require.Equal(t, 9e-6, svc.pricingData["gpt-5.4-pro"].CacheReadInputTokenCost)
	require.Equal(t, 7e-5, svc.pricingData["gpt-5.4-pro"].OutputCostPerToken)
	require.NotEqual(t, patch["gpt-5.4-pro"]["cache_read_input_token_cost"], svc.pricingData["gpt-5.4-pro"].CacheReadInputTokenCost,
		"conflicting operator values require explicit migration review, not silent replacement")
	require.Zero(t, svc.pricingData["gemini-2.5-pro"].CacheCreationInputTokenCost,
		"a custom override replaces the shipped default; do not claim uniform repair")
}

func TestCacheBasePatch_ResourceAndManifestMatchApprovedScope(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json")
	sourceBody, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	var manifest struct {
		SourceSHA256 string `json:"source_sha256"`
		Entries      []struct {
			ModelID  string   `json:"exact_model_id"`
			Field    string   `json:"field"`
			Action   string   `json:"action"`
			OldValue *float64 `json:"old_value"`
		} `json:"entries"`
	}
	body, err := os.ReadFile(filepath.Join("testdata", "cache_base_patch_manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &manifest))
	normalized := strings.ReplaceAll(string(sourceBody), "\r\n", "\n")
	sourceHash := sha256.Sum256([]byte(normalized))
	require.Equal(t, manifest.SourceSHA256, hex.EncodeToString(sourceHash[:]))
	_, _, patch := cacheBasePatchFixture(t)
	require.Len(t, manifest.Entries, 12)
	seen := map[string]bool{}
	approved := 0
	for _, entry := range manifest.Entries {
		require.False(t, seen[entry.ModelID])
		seen[entry.ModelID] = true
		if entry.Action == "patch" {
			require.NotNil(t, entry.OldValue)
			require.Equal(t, map[string]float64{entry.Field: *entry.OldValue}, patch[entry.ModelID])
			approved++
		} else {
			require.NotContains(t, patch, entry.ModelID)
		}
	}
	require.Equal(t, 6, approved)
}
