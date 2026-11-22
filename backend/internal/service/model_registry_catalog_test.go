package service

import (
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/stretchr/testify/require"
)

func TestModelRegistryCatalogFactsAllTasks(t *testing.T) {
	snapshot := &ModelRegistrySnapshot{presets: []PlatformModelPreset{{Platform: PlatformOpenAI, Models: []PlatformPresetModel{}}}}
	for _, task := range []string{"image_generation", "video_generation", "audio_generation", "speech", "transcription", "embedding", "realtime", "three_dimensional_generation", "text_generation", "code_generation", "unknown"} {
		snapshot.presets[0].Models = append(snapshot.presets[0].Models, PlatformPresetModel{ID: task, DisplayName: "Display " + task, TaskType: task, ContextWindow: 1234, ContextLimitKind: "total"})
	}
	now := time.Now()
	facts := snapshot.CatalogFacts(now)
	require.Len(t, facts, len(snapshot.presets[0].Models))
	for _, entry := range facts {
		detail := catalog.Project(catalog.Perspective{}, catalog.Input{Registry: []catalog.RegistryModel{entry}, Models: []catalog.Candidate{{Provider: entry.Provider, Model: entry.Model}}}, catalog.Window{}, now).Details[0]
		require.Equal(t, "Display "+entry.Model, detail.Model.DisplayName)
		require.EqualValues(t, 1234, *detail.Model.Context)
		require.Equal(t, "requires_scope", detail.Model.AvailabilityState)
		require.Equal(t, "requires_scope", detail.Model.RouteState)
		require.Equal(t, "unpriced", detail.Model.PriceState)
		if entry.Model == "unknown" {
			require.Empty(t, detail.Model.Modalities)
			require.Empty(t, detail.Model.Actions)
		} else {
			require.NotEmpty(t, detail.Model.Modalities)
			require.NotEmpty(t, detail.Model.Actions)
		}
		for _, fact := range detail.Facts {
			require.Equal(t, catalog.SourcePreset, fact.Source)
		}
	}
}

func TestModelRegistryCatalogFactsRetainPerFieldOrigins(t *testing.T) {
	yes := true
	snapshot := &ModelRegistrySnapshot{
		presets: []PlatformModelPreset{{Platform: PlatformOpenAI, ProviderProfile: "openai", Models: []PlatformPresetModel{{ID: "mixed", ContextWindow: 1234}}}},
		capabilities: &ModelCapabilitySnapshot{records: map[string]CapabilityRecord{
			capabilityKey("openai", "mixed"): {Provider: PlatformOpenAI, Profile: "openai", ModelID: "mixed", Source: "models.dev", LiteLLMCapabilityFields: LiteLLMCapabilityFields{SupportsFunctionCalling: &yes, SupportsVision: &yes, SupportsReasoning: &yes, SupportsPromptCaching: &yes}},
		}},
	}
	rows := snapshot.CatalogFacts(time.Now())
	require.Len(t, rows, 1)
	facts := rows[0].Facts
	require.Equal(t, catalog.SourcePreset, facts.Source)
	require.Equal(t, catalog.SourcePreset, facts.Provenance["context_tokens"].SourceKind)
	for _, field := range []string{"tools", "vision", "reasoning", "prompt_caching"} {
		require.Equal(t, catalog.SourceBuiltin, facts.Provenance[field].SourceKind, field)
		require.Equal(t, "approved", facts.Provenance[field].Approval)
	}
	require.NotContains(t, facts.Provenance, "pdf", "missing capability remains unknown")
	require.True(t, catalog.PublicEvidence(facts.Evidence))
}

func TestModelRegistryCatalogFactsCapabilityOnlyIsolatedAndImmutable(t *testing.T) {
	limit := int64(4096)
	key := capabilityKey("openai", "capability-only")
	snapshot := &ModelRegistrySnapshot{capabilities: &ModelCapabilitySnapshot{records: map[string]CapabilityRecord{
		key: {Provider: PlatformOpenAI, Profile: "openai", ModelID: "capability-only", Mode: "embedding", LiteLLMCapabilityFields: LiteLLMCapabilityFields{MaxOutputTokens: &limit}},
	}}}
	facts := snapshot.CatalogFacts(time.Now())
	require.Len(t, facts, 1)
	require.Equal(t, PlatformOpenAI, facts[0].Provider)
	require.Equal(t, []string{"text.embeddings"}, facts[0].Actions)
	*facts[0].Facts.Output = 1
	require.EqualValues(t, 4096, *snapshot.capabilities.records[key].MaxOutputTokens)
}

func TestModelRegistryCatalogFactsCompiledMultimodalCoverage(t *testing.T) {
	now := time.Now()
	facts := NewModelRegistry().Snapshot().CatalogFacts(now)
	input := catalog.Input{Registry: facts}
	seen := map[string]bool{}
	for _, fact := range facts {
		id := catalog.StableID(fact.Provider, fact.Model)
		if !seen[id] {
			input.Models = append(input.Models, catalog.Candidate{Provider: fact.Provider, Model: fact.Model})
			seen[id] = true
		}
	}
	byPlatform := map[string]map[string]bool{}
	for _, detail := range catalog.Project(catalog.Perspective{}, input, catalog.Window{}, now).Details {
		if byPlatform[detail.Model.Provider] == nil {
			byPlatform[detail.Model.Provider] = map[string]bool{}
		}
		for _, action := range detail.Model.Actions {
			byPlatform[detail.Model.Provider][action] = true
		}
	}
	for platform, actions := range map[string][]string{
		PlatformOpenAI: {"text.chat", "image.generate", "audio.speech", "audio.transcription", "text.embeddings", "realtime"},
		PlatformGrok:   {"text.chat", "image.generate", "video.generate"},
		PlatformGemini: {"text.chat", "image.generate", "video.generate", "audio.speech", "audio.generate", "text.embeddings", "realtime"},
	} {
		for _, action := range actions {
			require.True(t, byPlatform[platform][action], "%s: %s", platform, action)
		}
	}
}

func TestModelRegistryCatalogFactsSharedProfileDoesNotRelabelProviders(t *testing.T) {
	snapshot := &ModelRegistrySnapshot{
		presets: []PlatformModelPreset{{Platform: PlatformGemini, ProviderProfile: "native"}, {Platform: PlatformGrok, ProviderProfile: "native"}},
		capabilities: &ModelCapabilitySnapshot{records: map[string]CapabilityRecord{
			"native-model": {Provider: PlatformOpenAI, Profile: "native", ModelID: "native-model", Mode: "chat"},
		}},
	}
	facts := snapshot.CatalogFacts(time.Now())
	require.Len(t, facts, 1)
	require.Equal(t, PlatformOpenAI, facts[0].Provider)
}
