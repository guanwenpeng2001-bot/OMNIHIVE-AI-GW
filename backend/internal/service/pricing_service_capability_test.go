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

func TestPricingCapabilityInputRetainsOriginalCatalog(t *testing.T) {
	directory := t.TempDir()
	fallback := filepath.Join(directory, "fallback.json")
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{FallbackFile: fallback}}, nil)
	require.NotNil(t, svc.capabilityData)
	require.Empty(t, svc.capabilityData.body)
	body := []byte(`{"model":{"input_cost_per_token":0.001,"output_cost_per_token":0.002}}`)
	input := svc.buildPricingCapabilityInput(body, "unverified-hash", "")
	svc.publishPricingCapabilityInput(input)
	body[0] = ' '
	require.Equal(t, byte('{'), svc.capabilityData.body[0])
	require.Equal(t, "unverified-hash", svc.capabilityData.declaredHash)
	svc.publishPricingCapabilityInput(svc.buildPricingCapabilityInput(body, "", fallback))
	require.Same(t, input, svc.capabilityData)

	path := filepath.Join(directory, "catalog.json")
	catalog := []byte(`{"model":{"input_cost_per_token":0.003,"output_cost_per_token":0.004,"supports_reasoning":false}}`)
	require.NoError(t, os.WriteFile(path, catalog, 0600))
	require.NoError(t, svc.loadPricingData(path))
	require.Equal(t, catalog, svc.capabilityData.body)
	require.Equal(t, path, svc.capabilityData.sourcePath)
	require.Equal(t, 0.003, svc.pricingData["model"].InputCostPerToken)
	require.Equal(t, byte('{'), input.body[0], "previously captured input stays immutable")
}
