//go:build unit

package service

import (
	"encoding/json"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/capabilitysources"
	"github.com/stretchr/testify/require"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Frozen pre-optimization implementations provide full-value differential oracles.
func referenceMergeUpstreamModelMetadata(primary, fallback UpstreamModelMetadata) (UpstreamModelMetadata, bool) {
	merged := cloneUpstreamModelMetadata(primary)
	changed := false
	for key, value := range fallback.TaskLimits {
		if _, exists := merged.TaskLimits[key]; exists {
			continue
		}
		if merged.TaskLimits == nil {
			merged.TaskLimits = make(map[string]json.RawMessage)
		}
		merged.TaskLimits[key] = slices.Clone(value)
		if merged.FieldEvidence == nil {
			merged.FieldEvidence = make(map[string]ModelCapabilityEvidence)
		}
		e, ok := fallback.FieldEvidence["task_limits."+key]
		if !ok {
			e = fallback.FieldEvidence["task_limits"]
		}
		merged.FieldEvidence["task_limits."+key] = e
		changed = true
	}
	if merged.Category == "" && fallback.Category != "" {
		merged.Category = fallback.Category
		changed = true
	}
	if merged.TaskType == "" && fallback.TaskType != "" {
		merged.TaskType = fallback.TaskType
		changed = true
	}
	if merged.OutputModalities == nil && fallback.OutputModalities != nil {
		merged.OutputModalities = slices.Clone(fallback.OutputModalities)
		changed = true
	}
	if merged.SupportedParameters == nil && fallback.SupportedParameters != nil {
		merged.SupportedParameters = slices.Clone(fallback.SupportedParameters)
		changed = true
	}
	if merged.SupportedMethods == nil && fallback.SupportedMethods != nil {
		merged.SupportedMethods = slices.Clone(fallback.SupportedMethods)
		changed = true
	}
	for key, value := range fallback.CodexToolCapabilities {
		if _, exists := merged.CodexToolCapabilities[key]; exists {
			continue
		}
		if merged.CodexToolCapabilities == nil {
			merged.CodexToolCapabilities = make(map[string]json.RawMessage)
		}
		merged.CodexToolCapabilities[key] = slices.Clone(value)
		changed = true
	}
	if strings.TrimSpace(merged.ID) == "" && strings.TrimSpace(fallback.ID) != "" {
		merged.ID = strings.TrimSpace(fallback.ID)
		changed = true
	}
	if (strings.TrimSpace(merged.DisplayName) == "" || primary.FieldEvidence["display_name"].Source == "derived") && strings.TrimSpace(fallback.DisplayName) != "" {
		merged.DisplayName = strings.TrimSpace(fallback.DisplayName)
		changed = true
	}
	if strings.TrimSpace(merged.Description) == "" && strings.TrimSpace(fallback.Description) != "" {
		merged.Description = strings.TrimSpace(fallback.Description)
		changed = true
	}
	if merged.Reasoning == nil && fallback.Reasoning != nil {
		reasoning := *fallback.Reasoning
		merged.Reasoning = &reasoning
		changed = true
	}
	control := merged.ReasoningControl
	if control == nil || control.Kind == "unknown" {
		control = fallback.ReasoningControl
	}
	allowEffort := reasoningControlAllowsEffort(control)
	allowEffortLevels := allowEffort && (control == nil || control.Kind == "unknown" || control.EffortValues == nil || reflect.DeepEqual(control.EffortValues, fallback.SupportedReasoningLevels))
	if allowEffort && (merged.Reasoning == nil || *merged.Reasoning) &&
		(control == nil || control.Kind == "unknown" || control.EffortValues == nil || slices.Contains(control.EffortValues, strings.TrimSpace(fallback.DefaultReasoningLevel))) &&
		(merged.SupportedReasoningLevels == nil || slices.Contains(merged.SupportedReasoningLevels, strings.TrimSpace(fallback.DefaultReasoningLevel))) &&
		strings.TrimSpace(merged.DefaultReasoningLevel) == "" && strings.TrimSpace(fallback.DefaultReasoningLevel) != "" {
		merged.DefaultReasoningLevel = strings.TrimSpace(fallback.DefaultReasoningLevel)
		changed = true
	}
	if allowEffortLevels && (merged.Reasoning == nil || *merged.Reasoning) && merged.SupportedReasoningLevels == nil && fallback.SupportedReasoningLevels != nil {
		merged.SupportedReasoningLevels = slices.Clone(fallback.SupportedReasoningLevels)
		changed = true
	}
	if merged.InputModalities == nil && fallback.InputModalities != nil && compatibleVisionDeclaration(primary, fallback.InputModalities) {
		merged.InputModalities = slices.Clone(fallback.InputModalities)
		changed = true
	}
	if merged.ContextWindow <= 0 && fallback.ContextWindow > 0 {
		merged.ContextWindow = fallback.ContextWindow
		// Keep the registry's context limits together. A direct upstream default
		// without an explicit maximum remains the conservative ceiling.
		if merged.MaxContextWindow <= 0 {
			merged.MaxContextWindow = fallback.MaxContextWindow
		}
		changed = true
	}
	if merged.MaxOutputTokens <= 0 && fallback.MaxOutputTokens > 0 && (merged.ContextLimitKind != "total" || merged.ContextWindow <= 0 || fallback.MaxOutputTokens <= merged.ContextWindow) {
		merged.MaxOutputTokens = fallback.MaxOutputTokens
		changed = true
	}
	a, b := reflect.ValueOf(&merged).Elem(), reflect.ValueOf(fallback)
	for _, name := range []string{"ProviderProfile", "SupportsVision", "SupportsPDF", "SupportsFunctionCalling", "SupportsPromptCaching", "ReasoningControl", "ContextLimitKind", "InputModalitiesStatus", "EndpointStatus", "EmbeddingDimensions", "ContextHint", "FamilyHint"} {
		x, y := a.FieldByName(name), b.FieldByName(name)
		if (name == "SupportsVision" || name == "SupportsPDF") && merged.InputModalities != nil && merged.InputModalitiesStatus != "partial" {
			modality, flag := "image", fallback.SupportsVision
			if name == "SupportsPDF" {
				modality, flag = "pdf", fallback.SupportsPDF
			}
			if flag != nil && *flag != slices.Contains(merged.InputModalities, modality) {
				continue
			}
		}
		if name == "ReasoningControl" && merged.Reasoning != nil && !*merged.Reasoning {
			continue
		}
		if name == "ReasoningControl" && primary.SupportedReasoningLevels != nil && len(primary.SupportedReasoningLevels) == 0 && fallback.ReasoningControl != nil && fallback.ReasoningControl.Kind == "effort" {
			continue
		}
		if name == "ReasoningControl" && fallback.ReasoningControl != nil && primary.SupportedReasoningLevels != nil {
			if !reasoningControlAllowsEffort(fallback.ReasoningControl) && len(primary.SupportedReasoningLevels) > 0 || fallback.ReasoningControl.Kind == "effort" && !reflect.DeepEqual(primary.SupportedReasoningLevels, fallback.ReasoningControl.EffortValues) {
				continue
			}
		}
		if name == "ContextLimitKind" && primary.ContextWindow > 0 {
			continue
		}
		if name == "InputModalitiesStatus" && !reflect.DeepEqual(merged.InputModalities, fallback.InputModalities) {
			continue
		}
		if name == "ReasoningControl" && merged.ReasoningControl != nil && merged.ReasoningControl.Kind == "unknown" && fallback.ReasoningControl != nil && fallback.ReasoningControl.Kind != "unknown" {
			x.Set(y)
			changed = true
		}
		if x.IsZero() && !y.IsZero() {
			x.Set(y)
			changed = true
		}
	}
	if primary.InputModalitiesStatus == "partial" && len(fallback.InputModalities) > 0 && fallback.InputModalitiesStatus != "partial" && compatibleVisionDeclaration(primary, fallback.InputModalities) {
		merged.InputModalities = slices.Clone(fallback.InputModalities)
		merged.InputModalitiesStatus = fallback.InputModalitiesStatus
		changed = true
	}
	before, after := referenceMetadataFieldValues(primary), referenceMetadataFieldValues(merged)
	for f, e := range fallback.FieldEvidence {
		if f == "task_limits" || strings.HasPrefix(f, "task_limits.") {
			continue // per-key evidence must not relabel retained native limits
		}
		if value, ok := after[f]; ok && !reflect.DeepEqual(before[f].Interface(), value.Interface()) {
			if merged.FieldEvidence == nil {
				merged.FieldEvidence = make(map[string]ModelCapabilityEvidence)
			}
			merged.FieldEvidence[f] = e
		}
	}
	referenceRecordRejectedCapabilityCandidates(&merged, fallback)
	return cloneUpstreamModelMetadata(merged), changed
}

// Clone even unchanged entries: callers must not mutate cached registry or
// native snapshots through a merged pointer, slice, map, or RawMessage.

func referenceMetadataFieldValues(m UpstreamModelMetadata) map[string]reflect.Value {
	v := reflect.ValueOf(m)
	result := make(map[string]reflect.Value)
	for i := 0; i < v.NumField(); i++ {
		name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
		result[name] = v.Field(i)
	}
	return result
}

func referenceRecordRejectedCapabilityCandidates(m *UpstreamModelMetadata, fallback UpstreamModelMetadata) {
	accepted, candidate := referenceMetadataFieldValues(*m), referenceMetadataFieldValues(fallback)
	record := func(field string, value any, reason string) {
		raw, err := json.Marshal(value)
		if err != nil {
			return
		}
		e := fallback.FieldEvidence[field]
		if strings.HasPrefix(field, "task_limits.") && e.Source == "" {
			e = fallback.FieldEvidence["task_limits"]
		}
		r := ModelCapabilityRejection{Field: field, Reason: reason, Value: raw, Evidence: e}
		for _, prior := range m.RejectedCandidates {
			if reflect.DeepEqual(prior, r) {
				return
			}
		}
		m.RejectedCandidates = append(m.RejectedCandidates, r)
	}
	for _, field := range []string{"task_type", "input_modalities", "output_modalities", "supported_methods", "context_window", "max_context_window", "max_output_tokens", "context_limit_kind", "reasoning", "reasoning_control", "supported_reasoning_levels", "default_reasoning_level", "supports_vision", "supports_pdf", "supports_function_calling", "supports_prompt_caching", "embedding_dimensions"} {
		v := candidate[field]
		if v.IsZero() || reflect.DeepEqual(v.Interface(), accepted[field].Interface()) {
			continue
		}
		reason := "higher_priority_declaration"
		if accepted[field].IsZero() {
			reason = "incompatible_related_declaration"
		}
		if field == "max_output_tokens" && m.ContextLimitKind == "total" && m.ContextWindow > 0 && fallback.MaxOutputTokens > m.ContextWindow {
			reason = "exceeds_total_context"
		}
		if (field == "supported_reasoning_levels" || field == "default_reasoning_level") && !reasoningControlAllowsEffort(m.ReasoningControl) {
			reason = "control_does_not_support_effort"
		}
		record(field, v.Interface(), reason)
	}
	keys := make([]string, 0, len(fallback.TaskLimits))
	for key := range fallback.TaskLimits {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !reflect.DeepEqual(m.TaskLimits[key], fallback.TaskLimits[key]) {
			record("task_limits."+key, fallback.TaskLimits[key], "higher_priority_declaration")
		}
	}
}

func referenceCapabilityObservationIndex(observation capabilitysources.Observation) map[string]UpstreamModelMetadata {
	// Do not promote CapabilityRecord.UnmarshalJSON: it would swallow the
	// registry's total context fields, which are deliberately separate from input.
	type observationRecord CapabilityRecord
	index := make(map[string]UpstreamModelMetadata)
	ambiguous := make(map[string]bool)
	for _, record := range observation.Records {
		body, err := json.Marshal(record)
		if err != nil {
			continue
		}
		var fields struct {
			observationRecord
			ContextWindow    int64  `json:"context_window"`
			ContextLimitKind string `json:"context_limit_kind"`
		}
		if json.Unmarshal(body, &fields) != nil {
			continue
		}
		profile, id := fields.Profile, fields.ModelID
		if profile == "" || id == "" || strings.ContainsRune(profile, 0) || strings.ContainsRune(id, 0) {
			continue
		}
		key := capabilityKey(profile, id)
		if _, exists := index[key]; exists {
			ambiguous[key] = true
			delete(index, key)
		}
		if ambiguous[key] {
			continue
		}
		metadata := UpstreamModelMetadata{ID: id, ProviderProfile: profile, InputModalities: fields.InputModalities, OutputModalities: fields.OutputModalities, Reasoning: fields.SupportsReasoning, SupportsVision: fields.SupportsVision, SupportsFunctionCalling: fields.SupportsFunctionCalling, SupportsPromptCaching: fields.SupportsPromptCaching}
		if fields.MaxInputTokens != nil {
			metadata.ContextWindow, metadata.ContextLimitKind = *fields.MaxInputTokens, "input"
		}
		if fields.ContextWindow > 0 {
			metadata.ContextWindow, metadata.ContextLimitKind = fields.ContextWindow, fields.ContextLimitKind
		}
		if fields.MaxOutputTokens != nil {
			metadata.MaxOutputTokens = *fields.MaxOutputTokens
		}
		if metadata.InputModalities != nil {
			metadata.InputModalitiesStatus = "complete"
		}
		switch fields.Mode {
		case "chat", "completion":
			metadata.TaskType = "text_generation"
		case "embedding", "rerank", "image_generation", "audio_generation", "video_generation", "transcription":
			metadata.TaskType = fields.Mode
		}
		metadata.ReasoningControl = normalizeReasoningControl(metadata)
		stampCapabilityEvidence(&metadata, observation.ID, observation.ContentHash, profile, id, "exact", "observed")
		for field, evidence := range metadata.FieldEvidence {
			evidence.ContentHash, evidence.ObservedAt = observation.ContentHash, observation.VerifiedAt
			metadata.FieldEvidence[field] = evidence
		}
		index[key] = metadata
	}
	return index
}

func TestCapabilityMergeEquivalentAndIsolated(t *testing.T) {
	yes, no := true, false
	minimum, maximum := int64(1), int64(100)
	full := UpstreamModelMetadata{
		ID: " sample ", DisplayName: " name ", Description: " description ", ProviderProfile: "openai",
		Category: "text", TaskType: "text_generation", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"},
		SupportedParameters: []string{"tools", "reasoning"}, SupportedMethods: []string{"responses"}, SupportedReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: "high",
		Reasoning: &yes, SupportsVision: &yes, SupportsPDF: &no, SupportsFunctionCalling: &yes, SupportsPromptCaching: &no,
		ReasoningControl: &ReasoningControl{Kind: "effort", SupportedKinds: []string{"effort"}, EffortValues: []string{"low", "high"}, ToggleValues: []bool{false}, BudgetMin: &minimum, BudgetMax: &maximum, Adaptive: &yes, Default: json.RawMessage(`"high"`)},
		ContextWindow:    100, MaxContextWindow: 200, MaxOutputTokens: 50, ContextLimitKind: "total", EmbeddingDimensions: 32,
		InputModalitiesStatus: "complete", EndpointStatus: "supported", ContextHint: "hint", FamilyHint: "family",
		TaskLimits:            map[string]json.RawMessage{"resolutions": json.RawMessage(`["1024"]`)},
		CodexToolCapabilities: map[string]json.RawMessage{"tool": json.RawMessage(`true`)},
		MissingFields:         []string{"missing"}, EstimatedFields: []string{"estimated"}, Conflicts: []string{"conflict"},
		RejectedCandidates: []ModelCapabilityRejection{{Field: "fixture", Value: json.RawMessage(`false`)}},
	}
	stampCapabilityEvidence(&full, "primary", "v1", "openai", "sample", "exact", "verified")
	variants := []UpstreamModelMetadata{{}, full, {Reasoning: &no, InputModalities: []string{}, OutputModalities: []string{}, SupportedMethods: []string{}, SupportedReasoningLevels: []string{}, ReasoningControl: &ReasoningControl{Kind: "unknown", EffortValues: []string{}}}, {ContextWindow: 10, ContextLimitKind: "total", InputModalities: []string{"text"}, InputModalitiesStatus: "partial", TaskType: "embedding", ReasoningControl: &ReasoningControl{Kind: "budget"}}, {SupportsVision: &no, SupportsPDF: &yes, SupportedReasoningLevels: []string{"medium"}, TaskLimits: map[string]json.RawMessage{"resolutions": json.RawMessage(`[]`), "max_duration_seconds": json.RawMessage(`10`)}}}
	// Each field is also exercised on its own, including explicit empty maps,
	// diagnostics, and equal values with different or absent provenance.
	value := reflect.ValueOf(full)
	for index := 0; index < value.NumField(); index++ {
		var one UpstreamModelMetadata
		reflect.ValueOf(&one).Elem().Field(index).Set(value.Field(index))
		variants = append(variants, one)
	}
	for primaryIndex, primary := range variants {
		for fallbackIndex, original := range variants {
			fallback := cloneUpstreamModelMetadata(original)
			for field, evidence := range fallback.FieldEvidence {
				evidence.Source = "fallback"
				fallback.FieldEvidence[field] = evidence
			}
			beforePrimary, beforeFallback := cloneUpstreamModelMetadata(primary), cloneUpstreamModelMetadata(fallback)
			for field, index := range metadataJSONFieldIndexes {
				require.Equal(t, reflect.DeepEqual(reflect.ValueOf(primary).Field(index).Interface(), reflect.ValueOf(fallback).Field(index).Interface()), metadataFieldEqual(&primary, &fallback, field), "comparison field=%s", field)
			}
			want, wantChanged := referenceMergeUpstreamModelMetadata(primary, fallback)
			got, changed := mergeUpstreamModelMetadata(primary, fallback)
			require.Equal(t, want, got, "primary=%d fallback=%d", primaryIndex, fallbackIndex)
			require.Equal(t, wantChanged, changed)
			wantAgain, wantAgainChanged := referenceMergeUpstreamModelMetadata(want, fallback)
			gotAgain, gotAgainChanged := mergeUpstreamModelMetadata(got, fallback)
			require.Equal(t, wantAgain, gotAgain, "repeated merge must preserve rejection order and deduplication")
			require.Equal(t, wantAgainChanged, gotAgainChanged)
			mutateCapabilityValue(reflect.ValueOf(&got).Elem())
			require.Equal(t, beforePrimary, primary, "result aliases primary")
			require.Equal(t, beforeFallback, fallback, "result aliases fallback")
			got, _ = mergeUpstreamModelMetadata(primary, fallback)
			untouched := cloneUpstreamModelMetadata(got)
			mutateCapabilityValue(reflect.ValueOf(&fallback).Elem())
			require.Equal(t, untouched, got, "fallback aliases result")
		}
	}
}

func mutateCapabilityValue(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			mutateCapabilityValue(value.Elem())
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			mutateCapabilityValue(value.Field(index))
		}
	case reflect.Slice:
		for index := 0; index < value.Len(); index++ {
			mutateCapabilityValue(value.Index(index))
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			item := reflect.New(value.Type().Elem()).Elem()
			item.Set(value.MapIndex(key))
			mutateCapabilityValue(item)
			value.SetMapIndex(key, item)
		}
	case reflect.String:
		value.SetString("mutated")
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.Int64:
		value.SetInt(value.Int() + 1)
	case reflect.Uint8:
		value.SetUint(33)
	}
}

func TestCapabilityObservationEquivalent(t *testing.T) {
	values := []any{nil, false, true, "", "text", "\x00", "\xff", int64(12), int64(-1), float64(1), 1.5, json.Number("12"), json.Number("1.0"), json.Number("1e2"), json.Number("9223372036854775808"), json.Number("01"), json.Number("+1"), json.Number(""), []any{}, []any{"text", nil}, []any{false}, []string{}, []string{"text"}, map[string]any{}, json.RawMessage(`null`), json.RawMessage(`{"kind":"effort","kind":"budget"}`), make(chan int)}
	fields := []string{"provider", "profile", "model_id", "mode", "context_window", "context_limit_kind", "max_input_tokens", "max_output_tokens", "supports_reasoning", "supports_vision", "supports_function_calling", "supports_prompt_caching", "input_modalities", "output_modalities", "supported_parameters", "control", "embedding_dimensions", "field_sources", "unknown", "PROFILE"}
	for _, field := range fields {
		for _, value := range values {
			row := map[string]any{"profile": "openai", "model_id": "sample", "context_window": int64(100)}
			row[field] = value
			observation := capabilitysources.Observation{ID: "fixture", ContentHash: "hash", VerifiedAt: "fixed", Records: []map[string]any{row}}
			require.Equal(t, referenceCapabilityObservationIndex(observation), capabilityObservationIndex(observation), "field=%s value=%#v", field, value)
		}
	}
	for _, count := range []int{252, 1000} {
		fixture := newCapabilityMixedFixture(t, count)
		for _, observation := range fixture.observations {
			require.Equal(t, referenceCapabilityObservationIndex(observation), capabilityObservationIndex(observation))
		}
	}
	duplicate := capabilitysources.Observation{Records: []map[string]any{{"profile": "openai", "model_id": "sample"}, {"profile": "openai", "model_id": "sample"}, {"profile": "openai", "model_id": "sample"}, {"profile": " openai ", "model_id": " sample "}}}
	require.Equal(t, referenceCapabilityObservationIndex(duplicate), capabilityObservationIndex(duplicate))
	require.Len(t, capabilityObservationIndex(duplicate), 1)
}

func TestCapabilityProjectionOwnsCachedFields(t *testing.T) {
	fixture := newCapabilityMixedFixture(t, 5)
	provider := &capabilityTestProvider{}
	enricher, err := NewCapabilityReadOnlyEnricher(provider, nil)
	require.NoError(t, err)
	_, err = fixture.pipeline(provider, enricher, fixture.observations)
	require.NoError(t, err)
	cached := make(map[string]map[string]UpstreamModelMetadata)
	for source, entry := range enricher.sources {
		cached[source] = make(map[string]UpstreamModelMetadata)
		for key, metadata := range entry.index {
			cached[source][key] = cloneUpstreamModelMetadata(metadata)
		}
	}
	snapshot := provider.CapabilitySnapshot()
	records := make(map[string]CapabilityRecord)
	for key, record := range snapshot.records {
		records[key] = cloneCapabilityRecord(record)
	}
	accountBefore, err := json.Marshal(fixture.account)
	require.NoError(t, err)
	for _, metadata := range enricher.Project(fixture.account, false) {
		mutateCapabilityValue(reflect.ValueOf(&metadata).Elem())
	}
	for source, entry := range enricher.sources {
		require.Equal(t, cached[source], entry.index)
	}
	require.Equal(t, records, snapshot.records)
	accountAfter, err := json.Marshal(fixture.account)
	require.NoError(t, err)
	require.Equal(t, accountBefore, accountAfter)
}

func referenceReasoningControlJSON(control ReasoningControl) ([]byte, error) {
	type plain ReasoningControl
	body, err := json.Marshal(plain(control))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if control.SupportedKinds != nil && len(control.SupportedKinds) == 0 {
		fields["supported_kinds"] = json.RawMessage(`[]`)
	}
	if control.EffortValues != nil && len(control.EffortValues) == 0 {
		fields["effort_values"] = json.RawMessage(`[]`)
	}
	if control.ToggleValues != nil && len(control.ToggleValues) == 0 {
		fields["toggle_values"] = json.RawMessage(`[]`)
	}
	return json.Marshal(fields)
}

func TestReasoningControlEncodingEquivalent(t *testing.T) {
	yes, no := true, false
	minimum, maximum := int64(1), int64(100)
	for _, stringsValue := range [][]string{nil, {}, {"effort", "<adaptive>"}} {
		for _, toggles := range [][]bool{nil, {}, {false, true}} {
			for _, raw := range []json.RawMessage{nil, {}, []byte(`null`), []byte(`"<high>"`), []byte(` { "b": 2, "a": 1 } `), []byte(`{"kind":"effort","kind":"budget"}`), []byte(`invalid`)} {
				for _, adaptive := range []*bool{nil, &yes, &no} {
					control := ReasoningControl{Kind: "effort", SupportedKinds: stringsValue, EffortValues: stringsValue, ToggleValues: toggles, BudgetMin: &minimum, BudgetMax: &maximum, Adaptive: adaptive, Default: raw}
					want, wantErr := referenceReasoningControlJSON(control)
					got, gotErr := control.MarshalJSON()
					require.Equal(t, wantErr == nil, gotErr == nil)
					if wantErr == nil {
						require.Equal(t, string(want), string(got))
					}
				}
			}
		}
	}
}

func TestCapabilityRejectionBufferOverflowEquivalent(t *testing.T) {
	primary := UpstreamModelMetadata{TaskLimits: make(map[string]json.RawMessage)}
	fallback := UpstreamModelMetadata{TaskLimits: make(map[string]json.RawMessage)}
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		primary.TaskLimits[key] = json.RawMessage(`1`)
		fallback.TaskLimits[key] = json.RawMessage(`2`)
	}
	want, wantChanged := referenceMergeUpstreamModelMetadata(primary, fallback)
	got, changed := mergeUpstreamModelMetadata(primary, fallback)
	require.Equal(t, want, got)
	require.Equal(t, wantChanged, changed)
	require.Len(t, got.RejectedCandidates, 12)
	again, _ := mergeUpstreamModelMetadata(got, fallback)
	require.Equal(t, got, again)
}
