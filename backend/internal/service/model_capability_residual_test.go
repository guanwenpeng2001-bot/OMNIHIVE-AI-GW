package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCapabilityResidualTaskLimitsMergeAndEvidence(t *testing.T) {
	native := UpstreamModelMetadata{TaskLimits: map[string]json.RawMessage{"output_formats": json.RawMessage(`[]`), "enabled": json.RawMessage(`false`)}}
	fallback := UpstreamModelMetadata{TaskLimits: map[string]json.RawMessage{"output_formats": json.RawMessage(`["png"]`), "enabled": json.RawMessage(`true`), "resolutions": json.RawMessage(`["1024x1024"]`)}}
	stampCapabilityEvidence(&native, "upstream", "native-v1", "sample", "media", "exact", "verified")
	stampCapabilityEvidence(&fallback, "registry", "registry-v1", "sample", "media", "exact", "verified")
	m, changed := mergeUpstreamModelMetadata(native, fallback)
	require.True(t, changed)
	require.JSONEq(t, `[]`, string(m.TaskLimits["output_formats"]))
	require.JSONEq(t, `false`, string(m.TaskLimits["enabled"]))
	require.JSONEq(t, `["1024x1024"]`, string(m.TaskLimits["resolutions"]))
	require.Equal(t, "upstream", m.FieldEvidence["task_limits"].Source)
	require.Equal(t, "registry", m.FieldEvidence["task_limits.resolutions"].Source)
	require.Len(t, m.RejectedCandidates, 2)
	require.Equal(t, "registry", m.RejectedCandidates[0].Evidence.Source)
	again, changed := mergeUpstreamModelMetadata(m, fallback)
	require.False(t, changed)
	require.Equal(t, m, again)
	again.TaskLimits["output_formats"][0] = 'X'
	again.TaskLimits["resolutions"][0] = 'X'
	again.RejectedCandidates[0].Value[0] = 'X'
	require.Equal(t, byte('['), native.TaskLimits["output_formats"][0])
	require.Equal(t, byte('['), fallback.TaskLimits["resolutions"][0])
	require.NotEqual(t, byte('X'), m.RejectedCandidates[0].Value[0])
	body, err := json.Marshal(m)
	require.NoError(t, err)
	var roundtrip UpstreamModelMetadata
	require.NoError(t, json.Unmarshal(body, &roundtrip))
	require.Equal(t, m.RejectedCandidates, roundtrip.RejectedCandidates)
}

func TestModelCapabilityResidualReasoningGroups(t *testing.T) {
	yes := true
	for _, kind := range []string{"fixed", "budget"} {
		t.Run(kind, func(t *testing.T) {
			native := UpstreamModelMetadata{Reasoning: &yes, ReasoningControl: &ReasoningControl{Kind: kind}}
			fallback := UpstreamModelMetadata{SupportedReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: "high", ReasoningControl: &ReasoningControl{Kind: "effort", EffortValues: []string{"low", "high"}}}
			m, changed := mergeUpstreamModelMetadata(native, fallback)
			require.False(t, changed)
			require.Nil(t, m.SupportedReasoningLevels)
			require.Empty(t, m.DefaultReasoningLevel)
			require.Equal(t, kind, m.ReasoningControl.Kind)
			require.Len(t, m.RejectedCandidates, 3)
			require.Empty(t, evaluateModelMetadataCompleteness(m).Conflicts)
			m.SupportedReasoningLevels = []string{"high"}
			require.Contains(t, evaluateModelMetadataCompleteness(m).Conflicts, "reasoning_control.effort")
		})
	}
	// A native legacy effort declaration also prevents an incompatible control.
	native := UpstreamModelMetadata{Reasoning: &yes, SupportedReasoningLevels: []string{"low"}}
	m, _ := mergeUpstreamModelMetadata(native, UpstreamModelMetadata{ReasoningControl: &ReasoningControl{Kind: "fixed"}})
	require.Nil(t, m.ReasoningControl)
	require.Len(t, m.RejectedCandidates, 1)
}

func TestModelCapabilityResidualNativeThinkingAndPDF(t *testing.T) {
	// Reduced Models API schema fixture; no model specification is inferred.
	// https://platform.claude.com/docs/en/api/models (2026-09-17)
	for _, tc := range []struct{ name, types, kind string }{
		{"adaptive", `{"adaptive":{"supported":true},"enabled":{"supported":false}}`, "adaptive"},
		{"budget", `{"adaptive":{"supported":false},"enabled":{"supported":true}}`, "budget"},
		{"unknown", `{"adaptive":{"supported":false},"enabled":{"supported":false}}`, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"data":[{"id":"native","max_input_tokens":200000,"max_tokens":64000,"capabilities":{"pdf_input":{"supported":true},"image_input":{"supported":false},"thinking":{"supported":true,"types":` + tc.types + `}}}]}`
			_, entries, err := extractUpstreamModelCatalog([]byte(body), false, PlatformAnthropic)
			require.NoError(t, err)
			m := entries["native"]
			require.True(t, *m.SupportsPDF)
			require.False(t, *m.SupportsVision)
			require.Equal(t, []string{"text", "pdf"}, m.InputModalities)
			require.Equal(t, "partial", m.InputModalitiesStatus)
			require.Equal(t, tc.kind, m.ReasoningControl.Kind)
			require.Nil(t, m.SupportedReasoningLevels)
			require.Nil(t, m.ReasoningControl.BudgetMin)
			require.Nil(t, m.ReasoningControl.BudgetMax)
			m.TaskType = "text_generation"
			if tc.kind != "adaptive" {
				require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "reasoning_control")
			}
		})
	}
	_, entries, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"disabled","capabilities":{"pdf_input":{"supported":false},"thinking":{"supported":false,"types":{"adaptive":{"supported":true}}}}},{"id":"audio","input_modalities":["audio"],"output_modalities":["text"]}]}`), false, PlatformAnthropic)
	require.NoError(t, err)
	m := entries["disabled"]
	require.False(t, *m.SupportsPDF)
	require.Equal(t, "none", m.ReasoningControl.Kind)
	m, _ = mergeUpstreamModelMetadata(m, UpstreamModelMetadata{InputModalities: []string{"text", "pdf"}, InputModalitiesStatus: "complete"})
	require.NotContains(t, m.InputModalities, "pdf")
	require.NotEmpty(t, m.RejectedCandidates)
	require.Empty(t, entries["audio"].TaskType)
}

func TestModelCapabilityResidualKimiProfileReleaseScope(t *testing.T) {
	snapshot, err := buildModelCapabilitySnapshot(nil)
	require.NoError(t, err)
	// The global deployment retains historical moonshot records; CN has a
	// separately reviewed identity (covered by the regional isolation fixture).
	for _, host := range []string{"api.moonshot.ai"} {
		a := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://" + host + "/v1"}}
		require.Equal(t, "moonshot", resolveCapabilityProfile(a))
		require.Equal(t, "matched", lookupCapabilityMetadata(snapshot, resolveCapabilityProfile(a), "kimi-k2.5").Status)
	}
	coding := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": DefaultKimiCodingBaseURL}}
	require.Equal(t, "kimi-for-coding", resolveCapabilityProfile(coding))
	// Identity is known even before the generation chain publishes Coding facts.
	// The PAYG release must never fill this separate deployment implicitly.
	require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, resolveCapabilityProfile(coding), "kimi-k2.5").Status)
	for _, url := range []string{"https://private.example/v1", "https://api.moonshot.cn.example/v1"} {
		a := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": url}}
		require.NotEqual(t, "moonshot", resolveCapabilityProfile(a))
		require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, resolveCapabilityProfile(a), "kimi-k2.5").Status)
	}
}

func TestModelCapabilityResidualEmbeddingMissingFactsRemainExplicit(t *testing.T) {
	m := inferModelCapabilityHints("private-text-embedding")
	m.ProviderProfile = "custom:https://private.example"
	result := evaluateModelMetadataCompleteness(m)
	require.Equal(t, "incomplete", result.Status)
	for _, field := range []string{"supported_methods", "context_window", "embedding_dimensions"} {
		require.Contains(t, result.MissingFields, field)
	}
	require.Nil(t, m.SupportedMethods)
	m.MetadataStatus, m.MissingFields = result.Status, result.MissingFields
}
