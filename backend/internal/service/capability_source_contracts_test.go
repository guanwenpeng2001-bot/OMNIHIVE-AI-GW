package service

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/capabilitysources"
	modelcapabilities "github.com/Wei-Shaw/sub2api/resources/model-capabilities"
	modelpricing "github.com/Wei-Shaw/sub2api/resources/model-pricing"
	"github.com/stretchr/testify/require"
)

func TestCapabilitySourceContracts(t *testing.T) {
	for _, source := range []string{"models.dev", "openrouter", "litellm"} {
		t.Run(source, func(t *testing.T) {
			body, err := os.ReadFile("../pkg/capabilitysources/testdata/" + source + ".json")
			require.NoError(t, err)
			observation, err := capabilitysources.Parse(source, body, "2026-09-19")
			require.NoError(t, err)
			require.Len(t, observation.Records, 1)
			record := observation.Records[0]
			require.Equal(t, []any{}, record["input_modalities"])
			if source != "openrouter" {
				require.Equal(t, false, record["supports_reasoning"])
			}
			for _, field := range []string{"pricing", "cost", "input_cost_per_token", "eligible", "discovered", "model_mapping", "supported_methods"} {
				require.NotContains(t, record, field)
			}
			if source == "openrouter" {
				require.Equal(t, "openrouter", record["profile"])
				require.NotContains(t, record, "supports_function_calling")
			}
		})
	}
}

func TestCapabilityReleaseIndependentOfPricing(t *testing.T) {
	before, err := buildModelCapabilitySnapshot(nil)
	require.NoError(t, err)
	var prices map[string]map[string]any
	require.NoError(t, json.Unmarshal(modelpricing.JSON(), &prices))
	for _, entry := range prices {
		entry["input_cost_per_token"] = 123.456
	}
	body, err := json.Marshal(prices)
	require.NoError(t, err)
	after, err := buildModelCapabilitySnapshot(body)
	require.NoError(t, err)
	var published struct {
		Records []CapabilityRecord `json:"records"`
	}
	require.NoError(t, json.Unmarshal(modelcapabilities.JSON(), &published))
	for _, record := range published.Records {
		previous, exists := before.LookupExact(record.Profile, record.ModelID)
		require.True(t, exists)
		current, exists := after.LookupExact(record.Profile, record.ModelID)
		require.True(t, exists, record.ModelID)
		require.Equal(t, previous.LiteLLMCapabilityFields, current.LiteLLMCapabilityFields, record.ModelID)
		require.Equal(t, previous.SupportedMethods, current.SupportedMethods, record.ModelID)
		require.Equal(t, previous.EmbeddingDimensions, current.EmbeddingDimensions, record.ModelID)
	}
	// Primary false/empty facts survive the unchanged first-party -> reviewed cascade.
	no := false
	merged, _ := mergeUpstreamModelMetadata(UpstreamModelMetadata{Reasoning: &no, InputModalities: []string{}}, UpstreamModelMetadata{Reasoning: boolPtr(true), InputModalities: []string{"image"}})
	require.Equal(t, &no, merged.Reasoning)
	require.Equal(t, []string{}, merged.InputModalities)
}
