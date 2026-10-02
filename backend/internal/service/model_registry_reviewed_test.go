package service

import (
	"encoding/json"
	modelcapabilities "github.com/Wei-Shaw/sub2api/resources/model-capabilities"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"
)

// Bind in-memory test bytes only. Production still requires the independent
// release pin; this fixture never rewrites the checked-in pin or manifest.
func TestReviewedRegistryRealPresetsAndCatalog(t *testing.T) {
	catalog := modelcapabilities.JSON()
	if path := os.Getenv("MODEL_CAPABILITY_TEST_CANDIDATE"); path != "" {
		var err error
		catalog, err = os.ReadFile(path)
		require.NoError(t, err)
	}
	presets := platformModelPresetsJSON
	if path := os.Getenv("MODEL_CAPABILITY_TEST_PRESETS"); path != "" {
		var err error
		presets, err = os.ReadFile(path)
		require.NoError(t, err)
	}
	presets = []byte(strings.ReplaceAll(string(presets), "\r\n", "\n"))
	var header struct {
		GeneratedAt time.Time         `json:"generated_at"`
		Records     []json.RawMessage `json:"records"`
	}
	require.NoError(t, json.Unmarshal(catalog, &header))
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(modelcapabilities.ManifestJSON(), &manifest))
	manifest["approved_generation"] = header.GeneratedAt.Unix()
	manifest["catalog_sha256"] = capabilityHash(catalog)
	manifest["presets_sha256"] = capabilityHash(presets)
	manifest["record_count"] = len(header.Records)
	body, err := json.Marshal(manifest)
	require.NoError(t, err)
	pin, err := json.Marshal(modelcapabilities.ReleaseIdentity{ApprovedGeneration: header.GeneratedAt.Unix(), ManifestSHA256: capabilityHash(body)})
	require.NoError(t, err)
	registry := &ModelRegistry{trustedPin: pin}
	snapshot, err := registry.build(catalog, body, presets, nil)
	require.NoError(t, err)
	cn, ok := snapshot.capabilities.LookupExact("moonshotai-cn", "kimi-k2.6")
	require.True(t, ok)
	require.Zero(t, cn.ContextWindow)
	require.Empty(t, cn.ContextLimitKind)
	require.Nil(t, cn.MaxInputTokens)
	require.Nil(t, cn.MaxOutputTokens)
	for _, field := range []string{"context_window", "context_limit_kind", "max_output_tokens"} {
		require.NotContains(t, lookupCapabilityMetadata(snapshot.capabilities, "moonshotai-cn", "kimi-k2.6").Metadata.FieldEvidence, field)
	}
	global, ok := snapshot.capabilities.LookupExact("moonshot", "kimi-k2.6")
	require.True(t, ok)
	require.EqualValues(t, 1000000, *global.MaxInputTokens)
	require.EqualValues(t, 384000, *global.MaxOutputTokens)
	official, ok := snapshot.capabilities.LookupExact("zhipuai", "glm-4.6")
	require.True(t, ok)
	require.EqualValues(t, 204800, official.ContextWindow)
	evidence := lookupCapabilityMetadata(snapshot.capabilities, "zhipuai", "glm-4.6").Metadata.FieldEvidence["context_window"]
	require.Equal(t, "https://models.dev/api.json", evidence.Source)
	require.Equal(t, "2026-09-20T14:30:00Z", evidence.ObservedAt)
	require.Equal(t, "observed", evidence.Confidence)
	require.Equal(t, "d1bb46bfc49ddb00acb364b39b1653e00edb65d82a7f595bb43613ef8a33ab76", evidence.ContentHash)
	for _, id := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"} {
		record, ok := snapshot.capabilities.LookupExact("deepseek", id)
		require.True(t, ok)
		require.Nil(t, record.MaxOutputTokens)
	}
	// If the combined preset package includes hosted profiles, all their exact
	// facts must join vertex_ai; public google/gemini facts are never copied.
	definitions, err := parsePlatformModelPresets(presets)
	require.NoError(t, err)
	for _, preset := range definitions {
		if preset.ProviderProfile != "vertex-gemini" && preset.ProviderProfile != "vertex-anthropic" {
			continue
		}
		for _, model := range preset.Models {
			_, ok := snapshot.capabilities.LookupExact("vertex_ai", model.ID)
			require.True(t, ok)
			_, wrong := snapshot.capabilities.LookupExact(preset.ProviderProfile, model.ID)
			require.False(t, wrong)
		}
	}
}
