package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPricingCapabilityFieldsPreservePresence(t *testing.T) {
	var fields LiteLLMCapabilityFields
	require.NoError(t, json.Unmarshal([]byte(`{"supports_reasoning":false,"max_input_tokens":0,"input_modalities":[]}`), &fields))
	require.NotNil(t, fields.SupportsReasoning)
	require.False(t, *fields.SupportsReasoning)
	require.Nil(t, fields.SupportsVision)
	require.NotNil(t, fields.MaxInputTokens)
	require.Zero(t, *fields.MaxInputTokens)
	require.NotNil(t, fields.InputModalities)
	require.Empty(t, fields.InputModalities)
	require.Nil(t, fields.OutputModalities)
}

func TestPricingCapabilityMetadataDoesNotChangePrices(t *testing.T) {
	svc := NewPricingService(nil, nil)
	baseline := []byte(`{"model":{"input_cost_per_token":0.001,"output_cost_per_token":0.002,"supports_prompt_caching":true}}`)
	expected, err := svc.parsePricingData(baseline)
	require.NoError(t, err)
	for _, metadata := range []string{
		`"max_input_tokens":128000,"supports_reasoning":false,"input_modalities":[]`,
		`"max_input_tokens":"invalid","max_output_tokens":{},"max_tokens":[],"supports_reasoning":{},"supports_vision":[],"supports_function_calling":1,"input_modalities":false,"output_modalities":1`,
	} {
		actual, err := svc.parsePricingData([]byte(`{"model":{"input_cost_per_token":0.001,"output_cost_per_token":0.002,"supports_prompt_caching":true,` + metadata + `}}`))
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func TestPricingCapabilitySnapshotRetainsOriginalCatalog(t *testing.T) {
	directory := t.TempDir()
	fallback := filepath.Join(directory, "fallback.json")
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{FallbackFile: fallback}}, nil)
	baseline := svc.CapabilitySnapshot()
	require.NotNil(t, baseline)
	require.NotEmpty(t, baseline.Version())
	require.Positive(t, baseline.Len())
	body := []byte(`{"model":{"litellm_provider":"test","max_input_tokens":123,"input_modalities":[],"supports_reasoning":false,"input_cost_per_token":0.001,"output_cost_per_token":0.002}}`)
	hash := capabilityHash(body)
	require.Nil(t, svc.buildCapabilityCandidate(body, "unverified-hash"))
	require.Same(t, baseline, svc.CapabilitySnapshot())
	snapshot := svc.buildCapabilityCandidate(body, hash)
	require.NotNil(t, snapshot)
	body[0] = ' '
	require.Same(t, baseline, svc.CapabilitySnapshot())
	record, ok := snapshot.LookupExact("test", "model")
	require.True(t, ok)
	require.EqualValues(t, 123, *record.MaxInputTokens)
	require.False(t, *record.SupportsReasoning)
	require.NotNil(t, record.InputModalities)
	require.Empty(t, record.InputModalities)
	*record.MaxInputTokens = 999
	require.Error(t, svc.loadPricingData(fallback))
	require.Same(t, baseline, svc.CapabilitySnapshot())

	path := filepath.Join(directory, "catalog.json")
	catalog := []byte(`{"model":{"litellm_provider":"test","max_input_tokens":456,"input_modalities":[],"input_cost_per_token":0.003,"output_cost_per_token":0.004,"supports_reasoning":false}}`)
	require.NoError(t, os.WriteFile(path, catalog, 0600))
	require.NoError(t, svc.loadPricingData(path))
	require.Same(t, baseline, svc.CapabilitySnapshot())
	_, ok = svc.CapabilitySnapshot().LookupExact("test", "model")
	require.False(t, ok)
	require.Equal(t, 0.003, svc.pricingData["model"].InputCostPerToken)
	previous, ok := snapshot.LookupExact("test", "model")
	require.True(t, ok)
	require.EqualValues(t, 123, *previous.MaxInputTokens, "previously captured snapshot stays immutable")
	require.Equal(t, hash, snapshot.ContentHash())
}

func TestPricingCapabilityPromptCachingPreservesPriceSemantics(t *testing.T) {
	for _, entry := range []struct {
		name  string
		field string
		want  bool
	}{
		{name: "absent"},
		{name: "null", field: `,"supports_prompt_caching":null`},
		{name: "false", field: `,"supports_prompt_caching":false`},
		{name: "true", field: `,"supports_prompt_caching":true`, want: true},
	} {
		t.Run(entry.name, func(t *testing.T) {
			prices, err := (&PricingService{}).parsePricingData([]byte(`{"model":{"input_cost_per_token":0.001` + entry.field + `}}`))
			require.NoError(t, err)
			require.Contains(t, prices, "model")
			require.Equal(t, entry.want, prices["model"].SupportsPromptCaching)
		})
	}
}
