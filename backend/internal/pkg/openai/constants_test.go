package openai_test

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, service.NewModelRegistry().Snapshot().CandidateIDs(service.PlatformOpenAI, ""), "gpt-5.6")
}

func TestDefaultModelsIncludeGPT6Astra(t *testing.T) {
	require.Contains(t, service.NewModelRegistry().Snapshot().CandidateIDs(service.PlatformOpenAI, ""), "gpt-6-astra")
	require.NotContains(t, service.NewModelRegistry().Snapshot().CandidateIDs(service.PlatformOpenAI, ""), "gpt-6", "approved review did not establish the bare alias")
	var displayName string
	for _, model := range service.OpenAIPresetModels() {
		if model.ID == "gpt-6-astra" {
			displayName = model.DisplayName
			break
		}
	}
	require.Equal(t, "GPT-6 Astra", displayName)
}

func TestRegistryPresetsIncludeConcreteGPT56SolForAccountTests(t *testing.T) {
	require.NotEmpty(t, service.OpenAIPresetModels())
	require.Contains(t, service.NewModelRegistry().Snapshot().CandidateIDs(service.PlatformOpenAI, ""), "gpt-5.6-sol")
}

func TestDefaultModelsIncludeGPTImage25(t *testing.T) {
	require.Contains(t, service.NewModelRegistry().Snapshot().CandidateIDs(service.PlatformOpenAI, ""), "gpt-image-2.5-flare")
	require.Contains(t, service.NewModelRegistry().Snapshot().CandidateIDs(service.PlatformOpenAI, ""), "gpt-image-2.5-sunburst")
}
