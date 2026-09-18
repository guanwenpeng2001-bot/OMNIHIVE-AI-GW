package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
)

// ReasoningControl describes actual wire controls; it never synthesizes effort values.
type ReasoningControl struct {
	Kind           string          `json:"kind"`
	SupportedKinds []string        `json:"supported_kinds,omitempty"`
	EffortValues   []string        `json:"effort_values,omitempty"`
	ToggleValues   []bool          `json:"toggle_values,omitempty"`
	BudgetMin      *int64          `json:"budget_min,omitempty"`
	BudgetMax      *int64          `json:"budget_max,omitempty"`
	Adaptive       *bool           `json:"adaptive,omitempty"`
	Default        json.RawMessage `json:"default,omitempty"`
}

// MarshalJSON preserves an explicit empty control domain instead of turning it
// into an unknown domain on a snapshot round trip.
func (control ReasoningControl) MarshalJSON() ([]byte, error) {
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

type ModelCapabilityEvidence struct {
	Source          string `json:"source"`
	SourceVersion   string `json:"source_version,omitempty"`
	ContentHash     string `json:"content_hash,omitempty"`
	ProviderProfile string `json:"provider_profile,omitempty"`
	MatchedModelID  string `json:"matched_model_id,omitempty"`
	MatchRule       string `json:"match_rule,omitempty"`
	ObservedAt      string `json:"observed_at,omitempty"`
	Confidence      string `json:"confidence"`
	Stale           bool   `json:"stale,omitempty"`
}
type ModelMetadataCompleteness struct {
	Status          string   `json:"metadata_status"`
	MissingFields   []string `json:"missing_fields,omitempty"`
	EstimatedFields []string `json:"estimated_fields,omitempty"`
	Conflicts       []string `json:"conflicts,omitempty"`
}

// Rejections describe discarded lower-priority facts, not conflicts among the
// accepted facts. They must not make an otherwise complete native model ineligible.
type ModelCapabilityRejection struct {
	Field    string                  `json:"field"`
	Reason   string                  `json:"reason"`
	Value    json.RawMessage         `json:"value"`
	Evidence ModelCapabilityEvidence `json:"evidence"`
}

func reasoningControlAllowsEffort(c *ReasoningControl) bool {
	return c == nil || c.Kind == "unknown" || c.Kind == "effort" || slices.Contains(c.SupportedKinds, "effort")
}

func recordRejectedCapabilityCandidates(m *UpstreamModelMetadata, fallback UpstreamModelMetadata) {
	accepted, candidate := metadataFieldValues(*m), metadataFieldValues(fallback)
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

func normalizeReasoningControl(m UpstreamModelMetadata) *ReasoningControl {
	if m.Reasoning != nil && !*m.Reasoning {
		return &ReasoningControl{Kind: "none"}
	}
	if m.ReasoningControl != nil {
		c := *cloneReasoningControl(m.ReasoningControl)
		switch c.Kind {
		case "none", "effort", "toggle", "budget", "adaptive", "fixed", "unknown":
		default:
			c.Kind = "unknown"
		}
		if c.EffortValues != nil {
			c.EffortValues = normalizeReasoningLevels(c.EffortValues)
		}
		return &c
	}
	if len(m.SupportedReasoningLevels) > 0 {
		return &ReasoningControl{Kind: "effort", EffortValues: normalizeReasoningLevels(m.SupportedReasoningLevels)}
	}
	if m.Reasoning != nil {
		return &ReasoningControl{Kind: "unknown"}
	}
	return nil
}

// V2 evaluates task-specific facts without conferring account authorization.
func evaluateModelMetadataCompleteness(m UpstreamModelMetadata) ModelMetadataCompleteness {
	r := ModelMetadataCompleteness{Status: "complete", Conflicts: slices.Clone(m.Conflicts)}
	required := []string{"id", "provider_profile", "task_type", "input_modalities", "output_modalities"}
	present := map[string]bool{"id": m.ID != "", "provider_profile": m.ProviderProfile != "", "task_type": m.TaskType != "", "input_modalities": len(m.InputModalities) > 0 && m.InputModalitiesStatus != "partial", "output_modalities": len(m.OutputModalities) > 0, "supported_methods": len(m.SupportedMethods) > 0 || m.EndpointStatus == "unsupported", "context_window": m.ContextWindow > 0, "max_output_tokens": m.MaxOutputTokens > 0, "reasoning": m.Reasoning != nil, "supports_function_calling": m.SupportsFunctionCalling != nil}
	present["context_limit_kind"] = m.ContextLimitKind == "input" || m.ContextLimitKind == "total"
	required = append(required, "supported_methods")
	switch m.TaskType {
	case "text_generation", "chat", "completion", "code_generation":
		required = append(required, "context_window", "context_limit_kind", "max_output_tokens", "reasoning", "supports_function_calling")
		if m.Reasoning != nil && *m.Reasoning {
			required = append(required, "reasoning_control")
			c := normalizeReasoningControl(m)
			present["reasoning_control"] = c != nil && c.Kind != "unknown" && c.Kind != "none"
			if c != nil {
				if len(c.Default) > 0 && string(c.Default) != "null" {
					switch c.Kind {
					case "effort":
						var value string
						if json.Unmarshal(c.Default, &value) != nil || !slices.Contains(c.EffortValues, value) {
							r.Conflicts = append(r.Conflicts, "reasoning_control.default")
						}
					case "budget":
						var value int64
						if json.Unmarshal(c.Default, &value) != nil || c.BudgetMin != nil && value < *c.BudgetMin || c.BudgetMax != nil && value > *c.BudgetMax {
							r.Conflicts = append(r.Conflicts, "reasoning_control.default")
						}
					case "toggle":
						var value bool
						if json.Unmarshal(c.Default, &value) != nil {
							r.Conflicts = append(r.Conflicts, "reasoning_control.default")
						}
					}
				}
				if c.Kind == "effort" && len(c.EffortValues) == 0 {
					present["reasoning_control"] = false
				}
				if c.Kind == "budget" && (c.BudgetMin == nil || c.BudgetMax == nil) {
					present["reasoning_control"] = false
				}
				if c.BudgetMin != nil && c.BudgetMax != nil && (*c.BudgetMin < 0 || *c.BudgetMax < *c.BudgetMin) {
					r.Conflicts = append(r.Conflicts, "reasoning_control.budget")
				}
			}
		}
	case "embedding":
		required = append(required, "context_window", "embedding_dimensions")
		present["embedding_dimensions"] = m.EmbeddingDimensions > 0
	case "rerank":
		required = append(required, "context_window")
	case "image_generation", "audio_generation", "speech", "transcription", "video_generation":
		// An unsupported adapter is an explicit endpoint fact. Supported media
		// requires the interface constraints supplied by that adapter/source.
		if m.EndpointStatus != "unsupported" {
			limits := []string{"output_formats"}
			switch m.TaskType {
			case "image_generation":
				limits = append(limits, "resolutions")
			case "transcription":
				limits = []string{"input_formats", "max_duration_seconds"}
			case "video_generation":
				limits = append(limits, "resolutions", "max_duration_seconds")
			}
			for _, key := range limits {
				field := "task_limits." + key
				required = append(required, field)
				present[field] = capabilityTaskLimitPresent(key, m.TaskLimits[key])
			}
		}
	default:
		present["task_type"] = false
	}
	if m.InputModalities != nil && m.InputModalitiesStatus != "partial" && m.SupportsVision != nil && *m.SupportsVision != slices.Contains(m.InputModalities, "image") {
		r.Conflicts = append(r.Conflicts, "input_modalities.vision")
	}
	if m.InputModalities != nil && m.InputModalitiesStatus != "partial" && m.SupportsPDF != nil && *m.SupportsPDF != slices.Contains(m.InputModalities, "pdf") {
		r.Conflicts = append(r.Conflicts, "input_modalities.pdf")
	}
	if m.ContextLimitKind == "total" && m.ContextWindow > 0 && m.MaxOutputTokens > m.ContextWindow {
		r.Conflicts = append(r.Conflicts, "max_output_tokens")
	}
	if m.DefaultReasoningLevel != "" && !slices.Contains(m.SupportedReasoningLevels, m.DefaultReasoningLevel) {
		r.Conflicts = append(r.Conflicts, "default_reasoning_level")
	}
	if m.Reasoning != nil && !*m.Reasoning && m.ReasoningControl != nil && m.ReasoningControl.Kind != "none" && m.ReasoningControl.Kind != "unknown" {
		r.Conflicts = append(r.Conflicts, "reasoning_control")
	}
	if !reasoningControlAllowsEffort(m.ReasoningControl) && (len(m.SupportedReasoningLevels) > 0 || m.DefaultReasoningLevel != "" || len(m.ReasoningControl.EffortValues) > 0) {
		r.Conflicts = append(r.Conflicts, "reasoning_control.effort")
	}
	if m.ReasoningControl != nil && m.ReasoningControl.Kind == "effort" && m.SupportedReasoningLevels != nil && !reflect.DeepEqual(m.SupportedReasoningLevels, m.ReasoningControl.EffortValues) {
		r.Conflicts = append(r.Conflicts, "reasoning_control.effort_values")
	}
	for _, f := range required {
		if !present[f] {
			r.MissingFields = append(r.MissingFields, f)
			continue
		}
		if f == "id" {
			continue
		}
		e, ok := m.FieldEvidence[f]
		if strings.HasPrefix(f, "task_limits.") && !ok {
			e, ok = m.FieldEvidence["task_limits"]
		}
		if f == "supported_methods" && m.EndpointStatus == "unsupported" {
			e, ok = m.FieldEvidence["endpoint_status"]
		}
		if !ok || e.Source == "" || e.SourceVersion == "" && e.ContentHash == "" || e.MatchRule == "" || e.Confidence == "" || e.ProviderProfile == "" || e.MatchedModelID == "" {
			r.MissingFields = append(r.MissingFields, f+".evidence")
		} else if e.Confidence == "estimated" {
			r.EstimatedFields = append(r.EstimatedFields, f)
		}
	}
	if len(r.EstimatedFields) > 0 {
		r.Status = "estimated"
	}
	if len(r.MissingFields) > 0 || len(r.Conflicts) > 0 {
		r.Status = "incomplete"
	}
	return r
}

func metadataFieldValues(m UpstreamModelMetadata) map[string]reflect.Value {
	v := reflect.ValueOf(m)
	result := make(map[string]reflect.Value)
	for i := 0; i < v.NumField(); i++ {
		name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
		result[name] = v.Field(i)
	}
	return result
}
func stampCapabilityEvidence(m *UpstreamModelMetadata, source, version, profile, matched, rule, confidence string) {
	if m.FieldEvidence == nil {
		m.FieldEvidence = make(map[string]ModelCapabilityEvidence)
	}
	for f, v := range metadataFieldValues(*m) {
		switch f {
		case "field_evidence", "metadata_status", "missing_fields", "estimated_fields", "conflicts", "rejected_candidates":
			continue
		}
		if v.IsZero() {
			continue
		}
		if _, ok := m.FieldEvidence[f]; !ok {
			m.FieldEvidence[f] = ModelCapabilityEvidence{Source: source, SourceVersion: version, ProviderProfile: profile, MatchedModelID: matched, MatchRule: rule, ObservedAt: time.Now().UTC().Format(time.RFC3339), Confidence: confidence}
		}
	}
}

func cloneCapabilityEvidence(in map[string]ModelCapabilityEvidence) map[string]ModelCapabilityEvidence {
	if in == nil {
		return nil
	}
	out := make(map[string]ModelCapabilityEvidence, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func reasoningControlFromOptions(m UpstreamModelMetadata, options []modelsDevReasoningOption) *ReasoningControl {
	if m.Reasoning != nil && !*m.Reasoning {
		return normalizeReasoningControl(m)
	}
	if m.ReasoningControl != nil {
		return normalizeReasoningControl(m)
	}
	var result *ReasoningControl
	for _, option := range options {
		kind := strings.ToLower(strings.TrimSpace(option.Type))
		if kind == "budget_tokens" {
			kind = "budget"
		}
		if kind != "effort" && kind != "toggle" && kind != "budget" && kind != "adaptive" && kind != "fixed" {
			continue
		}
		if result == nil {
			result = &ReasoningControl{Kind: kind}
		}
		result.SupportedKinds = append(result.SupportedKinds, kind)
		switch kind {
		case "effort":
			result.EffortValues = reasoningLevelsFromModelsDevOptions([]modelsDevReasoningOption{option})
		case "toggle":
			for _, value := range option.Values {
				if b, ok := value.(bool); ok {
					result.ToggleValues = append(result.ToggleValues, b)
				}
			}
		case "budget":
			result.BudgetMin = option.Min
			result.BudgetMax = option.Max
		case "adaptive":
			yes := true
			result.Adaptive = &yes
		}
		if len(option.Default) > 0 && result.Kind == kind {
			result.Default = slices.Clone(option.Default)
		}
	}
	if result != nil {
		return result
	}
	return normalizeReasoningControl(m)
}

// Endpoint facts use the existing forwarding contract only after a task is
// established; this does not discover, authorize or enable any model.

func cloneReasoningControl(in *ReasoningControl) *ReasoningControl {
	if in == nil {
		return nil
	}
	c := *in
	c.SupportedKinds = slices.Clone(in.SupportedKinds)
	c.EffortValues = slices.Clone(in.EffortValues)
	c.ToggleValues = slices.Clone(in.ToggleValues)
	c.Default = slices.Clone(in.Default)
	if in.BudgetMin != nil {
		v := *in.BudgetMin
		c.BudgetMin = &v
	}
	if in.BudgetMax != nil {
		v := *in.BudgetMax
		c.BudgetMax = &v
	}
	if in.Adaptive != nil {
		v := *in.Adaptive
		c.Adaptive = &v
	}
	return &c
}

func projectReasoningEffort(m *UpstreamModelMetadata) {
	c := m.ReasoningControl
	if c == nil || !reasoningControlAllowsEffort(c) || m.Reasoning != nil && !*m.Reasoning {
		return
	}
	if m.SupportedReasoningLevels == nil && c.EffortValues != nil {
		m.SupportedReasoningLevels = normalizeReasoningLevels(c.EffortValues)
	}
	if m.DefaultReasoningLevel == "" && c.Kind == "effort" && len(c.Default) > 0 {
		var value string
		if json.Unmarshal(c.Default, &value) == nil && slices.Contains(m.SupportedReasoningLevels, value) {
			m.DefaultReasoningLevel = value
		}
	}
}

func capabilityTaskLimitPresent(key string, raw json.RawMessage) bool {
	if key == "max_duration_seconds" {
		var n float64
		return json.Unmarshal(raw, &n) == nil && n > 0
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func compatibleVisionDeclaration(m UpstreamModelMetadata, modalities []string) bool {
	return (m.SupportsVision == nil || *m.SupportsVision == slices.Contains(modalities, "image")) &&
		(m.SupportsPDF == nil || *m.SupportsPDF == slices.Contains(modalities, "pdf"))
}
