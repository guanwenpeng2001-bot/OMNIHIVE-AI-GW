package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	modelcapabilities "github.com/Wei-Shaw/sub2api/resources/model-capabilities"
	modelpricing "github.com/Wei-Shaw/sub2api/resources/model-pricing"
	"github.com/stretchr/testify/require"
)

func TestModelCapabilityEmbeddedBaseline(t *testing.T) {
	snapshot, err := buildModelCapabilitySnapshot(nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, snapshot.Len(), 189)
	require.Equal(t, capabilityHash(modelpricing.JSON()), snapshot.ContentHash())
	gpt, ok := snapshot.LookupExact("openai", "gpt-4o")
	require.True(t, ok)
	require.EqualValues(t, 128000, *gpt.MaxInputTokens)
	require.EqualValues(t, 16384, *gpt.MaxOutputTokens)
	require.True(t, *gpt.SupportsVision)
	require.True(t, *gpt.SupportsFunctionCalling)
	require.Nil(t, gpt.SupportsReasoning)
	require.Nil(t, gpt.InputModalities, "vision is not a complete modality declaration")
	for _, id := range []string{"deepseek-chat", "deepseek-reasoner"} {
		r, ok := snapshot.LookupExact("deepseek", id)
		require.True(t, ok, id)
		require.EqualValues(t, 131072, *r.MaxInputTokens)
		require.Nil(t, r.SupportsVision)
		require.Equal(t, "unknown", r.ReasoningControl)
		if id == "deepseek-reasoner" {
			require.True(t, *r.SupportsReasoning)
			require.False(t, *r.SupportsFunctionCalling)
		} else {
			require.Nil(t, r.SupportsReasoning)
		}
	}
	_, ok = snapshot.LookupExact("cursor", "gpt-4o")
	require.False(t, ok)
	_, ok = snapshot.LookupExact("openai", "GPT-4O")
	require.False(t, ok)
	_, ok = snapshot.LookupExact("openai", "gpt-4o-made-up")
	require.False(t, ok)
	again, err := buildModelCapabilitySnapshot(nil)
	require.NoError(t, err)
	require.Equal(t, snapshot.Version(), again.Version())
	var supplement map[string]any
	require.NoError(t, json.Unmarshal(modelcapabilities.JSON(), &supplement))
	require.NotContains(t, string(modelcapabilities.JSON()), "cost_per")
}

func TestModelCapabilityPriceIsolationAndMissingOnlyMerge(t *testing.T) {
	body := []byte(`{"gpt-4o":{"litellm_provider":"openai","max_input_tokens":32000,"supports_vision":false,"supports_prompt_caching":false,"input_modalities":[]},"spec-only":{"litellm_provider":"test","max_output_tokens":123,"supports_reasoning":false},"price-only":{"input_cost_per_token":0.1}}`)
	snapshot, err := buildModelCapabilitySnapshot(body)
	require.NoError(t, err)
	r, ok := snapshot.LookupExact("openai", "gpt-4o")
	require.True(t, ok)
	require.EqualValues(t, 128000, *r.MaxInputTokens)
	require.EqualValues(t, 16384, *r.MaxOutputTokens)
	require.True(t, *r.SupportsVision)
	require.True(t, *r.SupportsPromptCaching)
	require.NotNil(t, r.InputModalities)
	require.Empty(t, r.InputModalities)
	require.NotEqual(t, "litellm:runtime", r.FieldSources["supports_vision"].Source)
	require.NotEqual(t, "litellm:runtime", r.FieldSources["max_output_tokens"].Source)
	spec, ok := snapshot.LookupExact("test", "spec-only")
	require.True(t, ok)
	require.False(t, *spec.SupportsReasoning)
	_, ok = snapshot.LookupExact("", "price-only")
	require.False(t, ok)
	prices, err := (&PricingService{}).parsePricingData(body)
	require.NoError(t, err)
	require.NotContains(t, prices, "spec-only")
	require.Contains(t, prices, "price-only")
	// Malformed capability fields cannot remove an otherwise valid price.
	prices, err = (&PricingService{}).parsePricingData([]byte(`{"priced":{"input_cost_per_token":0.1,"supports_reasoning":"bad","max_input_tokens":"bad"}}`))
	require.NoError(t, err)
	require.Contains(t, prices, "priced")
	// Reviewed specifications remain applicable across runtime price versions.
	_, ok = snapshot.LookupExact("deepseek", "deepseek-chat")
	require.True(t, ok)
}

func TestModelCapabilitySnapshotDefensiveCopies(t *testing.T) {
	snapshot, err := buildModelCapabilitySnapshot([]byte(`{"mutable":{"litellm_provider":"test","max_input_tokens":123,"input_modalities":["audio"],"supports_reasoning":false}}`))
	require.NoError(t, err)
	r, ok := snapshot.LookupExact("test", "mutable")
	require.True(t, ok)
	*r.MaxInputTokens = 0
	*r.SupportsReasoning = true
	r.InputModalities[0] = "image"
	r.FieldSources["max_input_tokens"] = CapabilityFieldSource{}
	r, _ = snapshot.LookupExact("test", "mutable")
	require.EqualValues(t, 123, *r.MaxInputTokens)
	require.False(t, *r.SupportsReasoning)
	require.Equal(t, []string{"audio"}, r.InputModalities)
	require.NotEmpty(t, r.FieldSources["max_input_tokens"].ContentHash)
}

func TestModelCapabilityCandidateValidation(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `null`, `{"sample_spec":{}}`, `{"x":{"input_cost_per_token":1}}`, `{"x":{"max_input_tokens":0}}`, `{"x":{"max_output_tokens":-1}}`, `{"x":{"supports_vision":"true"}}`} {
		t.Run(body, func(t *testing.T) { _, err := buildModelCapabilitySnapshot([]byte(body)); require.Error(t, err) })
	}
	// max_tokens has no inferred context semantics; input is not total context.
	snap, err := buildModelCapabilitySnapshot([]byte(`{"x":{"max_tokens":99},"y":{"max_input_tokens":10,"max_output_tokens":100}}`))
	require.NoError(t, err)
	x, ok := snap.LookupExact("", "x")
	require.True(t, ok)
	require.Nil(t, x.MaxInputTokens)
	require.Nil(t, x.MaxOutputTokens)
}

type capabilityRemote struct {
	body []byte
	hash string
}

func (r *capabilityRemote) FetchPricingJSON(context.Context, string) ([]byte, error) {
	return r.body, nil
}
func (r *capabilityRemote) FetchHashText(context.Context, string) (string, error) { return r.hash, nil }

func newCapabilityPricing(t *testing.T, remote PricingRemoteClient) *PricingService {
	t.Helper()
	return NewPricingService(&config.Config{Pricing: config.PricingConfig{DataDir: t.TempDir(), RemoteURL: "https://example.com/pricing.json", HashURL: "https://example.com/pricing.sha256"}}, remote)
}

func TestModelCapabilityDownloadHashMismatchAndCacheReplay(t *testing.T) {
	body := []byte(`{"runtime":{"litellm_provider":"test","max_input_tokens":256,"supports_reasoning":false,"input_cost_per_token":0.01}}`)
	remote := &capabilityRemote{body: body, hash: capabilityHash(body)}
	svc := newCapabilityPricing(t, remote)
	require.NoError(t, svc.downloadPricingData())
	valid := svc.CapabilitySnapshot()
	require.Equal(t, capabilityHash(body), valid.ContentHash())
	remote.body = []byte(`{"runtime":{"litellm_provider":"test","max_input_tokens":512,"supports_reasoning":true,"input_cost_per_token":0.02}}`)
	remote.hash = "mismatched-remote-sync-anchor"
	require.NoError(t, svc.downloadPricingData())
	require.Same(t, valid, svc.CapabilitySnapshot())
	require.Equal(t, remote.hash, svc.localHash)
	require.Equal(t, 0.02, svc.pricingData["runtime"].InputCostPerToken)
	// A local reload cannot bypass the rejected download, even after restart.
	require.NoError(t, svc.loadPricingData(svc.getPricingFilePath()))
	require.Same(t, valid, svc.CapabilitySnapshot())
	restarted := NewPricingService(svc.cfg, nil)
	embed := restarted.CapabilitySnapshot()
	require.NoError(t, restarted.loadPricingData(svc.getPricingFilePath()))
	require.Same(t, embed, restarted.CapabilitySnapshot())
	remote.hash = capabilityHash(remote.body)
	require.NoError(t, svc.downloadPricingData())
	require.NotEqual(t, valid.Version(), svc.CapabilitySnapshot().Version())
	old, _ := valid.LookupExact("test", "runtime")
	require.EqualValues(t, 256, *old.MaxInputTokens)
}

func TestModelCapabilityReloadAndOverrideIsolation(t *testing.T) {
	svc := newCapabilityPricing(t, nil)
	file := svc.getPricingFilePath()
	body := []byte(`{"runtime-only":{"litellm_provider":"test","max_input_tokens":32000,"supports_vision":false,"supports_prompt_caching":false,"input_cost_per_token":0.01}}`)
	require.NoError(t, os.WriteFile(file, body, 0644))
	require.NoError(t, svc.loadPricingData(file))
	original := svc.CapabilitySnapshot()
	svc.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "override.json")
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(`{"runtime-only":{"max_input_tokens":999999,"supports_vision":true,"input_cost_per_token":0.02}}`), 0644))
	require.NoError(t, svc.reloadCustomPricingLayers())
	r, found := svc.CapabilitySnapshot().LookupExact("test", "runtime-only")
	require.True(t, found)
	require.NotNil(t, r.MaxInputTokens)
	require.EqualValues(t, 32000, *r.MaxInputTokens)
	require.False(t, *r.SupportsVision)
	require.Equal(t, original.Version(), svc.CapabilitySnapshot().Version())
	require.Equal(t, 0.02, svc.pricingData["runtime-only"].InputCostPerToken)
	stable := svc.CapabilitySnapshot()
	require.NoError(t, os.WriteFile(file, []byte(`{"runtime-only":{"litellm_provider":"test","max_input_tokens":64000,"input_cost_per_token":0.03}}`), 0644))
	require.NoError(t, svc.reloadCustomPricingLayers())
	require.Same(t, stable, svc.CapabilitySnapshot(), "runtime field removal must not silently restore embed true")
	require.NoError(t, os.WriteFile(file, []byte(`{"runtime-only":{"max_input_tokens":"bad","input_cost_per_token":0.03}}`), 0644))
	require.NoError(t, svc.loadPricingData(file))
	require.Same(t, stable, svc.CapabilitySnapshot())
	require.NoError(t, os.WriteFile(file, []byte(`{"runtime-only":`), 0644))
	require.Error(t, svc.loadPricingData(file))
	require.Same(t, stable, svc.CapabilitySnapshot())
}

func TestModelCapabilityOfflineInitializeAndProvider(t *testing.T) {
	cfg := &config.Config{Pricing: config.PricingConfig{DataDir: t.TempDir(), FallbackFile: filepath.Join(t.TempDir(), "missing", "baseline.json")}}
	svc := NewPricingService(cfg, nil)
	// Capability initialization is offline and independent of price loading,
	// remote refresh, and the configured (deliberately missing) fallback file.
	var provider ModelCapabilityProvider = svc
	snapshot := provider.CapabilitySnapshot()
	require.NotNil(t, snapshot)
	require.NotEmpty(t, snapshot.Version())
	require.Equal(t, capabilityHash(modelpricing.JSON()), snapshot.ContentHash())
	require.Empty(t, svc.pricingData)
	require.NoFileExists(t, svc.getPricingFilePath())
	require.NoFileExists(t, cfg.Pricing.FallbackFile)
	r, ok := snapshot.LookupExact("openai", "gpt-4o")
	require.True(t, ok)
	require.EqualValues(t, 128000, *r.MaxInputTokens)
}

func TestModelCapabilityConcurrentPublication(t *testing.T) {
	svc := newCapabilityPricing(t, nil)
	first := svc.CapabilitySnapshot()
	next, err := buildModelCapabilitySnapshot([]byte(`{"gpt-4o":{"litellm_provider":"openai","max_input_tokens":100}}`))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snap := svc.CapabilitySnapshot()
				r, ok := snap.LookupExact("openai", "gpt-4o")
				if !ok {
					t.Error("missing record")
					return
				}
				*r.MaxInputTokens = 0
			}
		}()
	}
	for i := 0; i < 100; i++ {
		svc.mu.Lock()
		if i%2 == 0 {
			svc.capabilityData = next
		} else {
			svc.capabilityData = first
		}
		svc.mu.Unlock()
	}
	wg.Wait()
	r, _ := first.LookupExact("openai", "gpt-4o")
	require.EqualValues(t, 128000, *r.MaxInputTokens)
}

func TestModelCapabilityOnlyLoadDoesNotRequirePriceAdmission(t *testing.T) {
	svc := newCapabilityPricing(t, nil)
	path := svc.getPricingFilePath()
	require.NoError(t, os.WriteFile(path, []byte(`{"spec-only":{"litellm_provider":"test","max_input_tokens":42}}`), 0644))
	require.Error(t, svc.loadPricingData(path), "price loading must still reject absent prices")
	record, ok := svc.CapabilitySnapshot().LookupExact("test", "spec-only")
	require.True(t, ok)
	require.EqualValues(t, 42, *record.MaxInputTokens)
	require.Empty(t, svc.pricingData)
}

func TestModelCapabilityCustomFallbackDoesNotGrantCapabilityAuthority(t *testing.T) {
	svc := newCapabilityPricing(t, nil)
	baseline := svc.CapabilitySnapshot()
	svc.cfg.Pricing.FallbackFile = filepath.Join(t.TempDir(), "fallback.json")
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.FallbackFile, []byte(`{"gpt-4o":{"litellm_provider":"openai","max_input_tokens":999999,"input_cost_per_token":0.01}}`), 0644))
	require.NoError(t, svc.useFallbackPricing())
	require.Same(t, baseline, svc.CapabilitySnapshot())
	require.NoError(t, svc.loadPricingData(svc.getPricingFilePath()))
	require.Same(t, baseline, svc.CapabilitySnapshot())
}

func TestModelCapabilityLiteLLMRawCapabilityPresence(t *testing.T) {
	var raw LiteLLMRawEntry
	require.NoError(t, json.Unmarshal([]byte(`{"max_input_tokens":123,"supports_reasoning":false,"supports_vision":true,"supports_prompt_caching":false}`), &raw))
	require.EqualValues(t, 123, *raw.MaxInputTokens)
	require.False(t, *raw.SupportsReasoning)
	require.True(t, *raw.SupportsVision)
	require.False(t, *raw.SupportsPromptCaching)
	require.Nil(t, raw.SupportsFunctionCalling)
	require.Nil(t, raw.MaxOutputTokens)
}
