package service

import (
	"encoding/json"
	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestReviewedCapabilitySuppressionAndProvenance(t *testing.T) {
	body := []byte(`{"schema_version":1,"generated_at":"2026-10-02T00:00:00Z","records":[{"provider":"deepseek","profile":"deepseek","model_id":"deepseek-v4-flash","source":"local-data:old","source_content_hash":"` + strings.Repeat("a", 64) + `","verified_at":"2026-09-17","supports_vision":true,"supports_reasoning":true,"suppressed_fields":["max_output_tokens"],"field_sources":{"supports_vision":{"source":"https://api-docs.deepseek.com/quick_start/pricing","content_hash":"` + strings.Repeat("b", 64) + `","observed_at":"2026-10-02","confidence":"verified"},"supports_reasoning":{"source":"local-data:old","content_hash":"` + strings.Repeat("c", 64) + `","observed_at":"2026-09-17","confidence":"observed"}}}]}`)
	snapshot, err := buildModelCapabilitySnapshotFromCatalog(nil, body)
	require.NoError(t, err)
	record, ok := snapshot.LookupExact("deepseek", "deepseek-v4-flash")
	require.True(t, ok)
	require.Nil(t, record.MaxOutputTokens)
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.NotContains(t, fields, "max_output_tokens")
	require.Equal(t, strings.Repeat("b", 64), record.FieldSources["supports_vision"].ContentHash)
	require.Equal(t, strings.Repeat("c", 64), record.FieldSources["supports_reasoning"].ContentHash)
	metadata := lookupCapabilityMetadata(snapshot, "deepseek", "deepseek-v4-flash").Metadata
	require.Equal(t, "2026-09-17", metadata.FieldEvidence["reasoning"].ObservedAt)
	require.Equal(t, "observed", metadata.FieldEvidence["reasoning"].Confidence)
	require.Equal(t, "2026-10-02", metadata.FieldEvidence["supports_vision"].ObservedAt)
	require.Equal(t, "verified", metadata.FieldEvidence["supports_vision"].Confidence)
	require.NotContains(t, metadata.FieldEvidence, "max_output_tokens")
	public, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(public, &fields))
	require.NotContains(t, fields, "max_output_tokens")
}

func TestReviewedCapabilityCandidateFacts(t *testing.T) {
	body := modelcapabilities.JSON()
	if path := os.Getenv("MODEL_CAPABILITY_TEST_CANDIDATE"); path != "" {
		var err error
		body, err = os.ReadFile(path)
		require.NoError(t, err)
	}
	snapshot, err := buildModelCapabilitySnapshotFromCatalog(nil, body)
	require.NoError(t, err)
	for _, id := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"} {
		record, ok := snapshot.LookupExact("deepseek", id)
		require.True(t, ok)
		require.Nil(t, record.MaxOutputTokens)
		metadata := lookupCapabilityMetadata(snapshot, "deepseek", id).Metadata
		require.NotContains(t, metadata.FieldEvidence, "max_output_tokens")
		require.Equal(t, "2026-09-17", metadata.FieldEvidence["supports_prompt_caching"].ObservedAt)
		require.Equal(t, "observed", metadata.FieldEvidence["supports_prompt_caching"].Confidence)
		require.Equal(t, "2026-10-02", metadata.FieldEvidence["supports_vision"].ObservedAt)
	}
	cn, ok := snapshot.LookupExact("moonshotai-cn", "kimi-k2.6")
	require.True(t, ok)
	require.Nil(t, cn.MaxInputTokens)
	require.Nil(t, cn.MaxOutputTokens)
	require.Zero(t, cn.ContextWindow)
	global, ok := snapshot.LookupExact("moonshot", "kimi-k2.6")
	require.True(t, ok)
	require.EqualValues(t, 1000000, *global.MaxInputTokens)
	require.EqualValues(t, 384000, *global.MaxOutputTokens)
	for _, profile := range []string{"moonshot", "moonshotai-cn"} {
		_, ok := snapshot.LookupExact(profile, "k3")
		require.False(t, ok)
	}
	code, ok := snapshot.LookupExact("kimi-for-coding", "k3")
	require.True(t, ok)
	require.JSONEq(t, `"high"`, string(code.Control.Default))
	_, ok = snapshot.LookupExact("openai-codex", "gpt-6-luna")
	require.False(t, ok)
	_, ok = snapshot.LookupExact("opencode-go", "claude-sonnet-5-5")
	require.False(t, ok, "Zen must not leak into Go")
	command, ok := snapshot.LookupExact("commandcode", "claude-sonnet-5-5")
	require.True(t, ok)
	require.EqualValues(t, 1000000, command.ContextWindow)
	require.Equal(t, []string{"messages"}, command.SupportedMethods)
}

func TestReviewedCapabilityUnknownDateRemainsUnknown(t *testing.T) {
	old := CapabilityRecord{ModelID: "test", Source: "litellm:embed", SourceContentHash: strings.Repeat("a", 64)}
	yes := true
	old.SupportsVision = &yes
	merged := fillCapabilityFields(CapabilityRecord{ModelID: "test", Source: "https://example.com", VerifiedAt: "2026-10-02"}, old)
	m := UpstreamModelMetadata{ID: "test", SupportsVision: merged.SupportsVision}
	stampCapabilityRecordEvidence(&m, merged, "test", "exact")
	require.Empty(t, m.FieldEvidence["supports_vision"].ObservedAt)
	require.Equal(t, "observed", m.FieldEvidence["supports_vision"].Confidence)
}

func TestReviewedCapabilityRejectsUnknownControlSyntax(t *testing.T) {
	for _, body := range []string{
		`{"model_id":"test","suppressed_all":true}`,
		`{"model_id":"test","reasoning_control":{"kind":"effort","wire":"reasoning.effort"}}`,
		`{"model_id":"test","field_sources":{"supports_vision":{"source":"test","suppress":true}}}`,
	} {
		var record CapabilityRecord
		require.Error(t, json.Unmarshal([]byte(body), &record))
	}
}

func TestReviewedCapabilitySuppressionRejectsUnknownScope(t *testing.T) {
	for _, raw := range []string{
		`{"profile":"deepseek","model_id":"deepseek-v4-flash","suppressed_fields":["*"]}`,
		`{"profile":"openai","model_id":"gpt-5-pro","suppressed_fields":["supports_vision"]}`,
		`{"profile":"deepseek","model_id":"deepseek-v4-flash","suppressed_fields":["max_output_tokens"],"max_output_tokens":393216}`,
	} {
		var record CapabilityRecord
		require.NoError(t, json.Unmarshal([]byte(raw), &record))
		require.Error(t, validateCapabilityRecord(record))
	}
}
