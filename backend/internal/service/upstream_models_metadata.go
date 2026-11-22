package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"

	"github.com/tidwall/gjson"
)

// Decode optional native facts independently. A malformed field remains unknown;
// it cannot erase a sibling fact or change ID/pagination validation. Temporary
// values avoid retaining the partial writes encoding/json makes on type errors.
func decodeUpstreamOptionalFields(body []byte, targets map[string]any) (invalid bool) {
	object := gjson.ParseBytes(body)
	for path, target := range targets {
		raw := object
		for _, key := range strings.Split(path, ".") {
			next := raw.Get(key)
			if !next.Exists() {
				// Retain encoding/json's case-insensitive field matching.
				raw.ForEach(func(name, value gjson.Result) bool {
					if strings.EqualFold(name.Str, key) {
						next = value
						return false
					}
					return true
				})
			}
			raw = next
		}
		if !raw.Exists() {
			continue
		}
		value := reflect.ValueOf(target).Elem()
		decoded := reflect.New(value.Type())
		if err := json.Unmarshal([]byte(raw.Raw), decoded.Interface()); err != nil {
			invalid = true
			continue
		}
		value.Set(decoded.Elem())
	}
	return invalid
}

func decodeUpstreamModelCapabilities(body []byte, entry *upstreamModelCapabilityEntry) bool {
	invalid := decodeUpstreamOptionalFields(body, map[string]any{
		"created_at":                 &entry.CreatedAt,
		"created":                    &entry.Created,
		"version":                    &entry.Version,
		"thinkingBudget":             &entry.ThinkingBudget,
		"tool_call":                  &entry.ToolCall,
		"supports_function_calling":  &entry.SupportsFunctionCalling,
		"supports_prompt_caching":    &entry.SupportsPromptCaching,
		"reasoning_control":          &entry.ReasoningControl,
		"context_limit_kind":         &entry.ContextLimitKind,
		"input_modalities_status":    &entry.InputModalitiesStatus,
		"endpoint_status":            &entry.EndpointStatus,
		"embedding_dimensions":       &entry.EmbeddingDimensions,
		"task_limits":                &entry.TaskLimits,
		"category":                   &entry.Category,
		"task_type":                  &entry.TaskType,
		"supported_methods":          &entry.SupportedMethods,
		"output_modalities":          &entry.OutputModalities,
		"supportedGenerationMethods": &entry.SupportedGenerationMethods,
		"inputTokenLimit":            &entry.InputTokenLimit,
		"outputTokenLimit":           &entry.OutputTokenLimit,
		"displayName":                &entry.DisplayNameCamel,
		"supportsThinking":           &entry.SupportsThinking,
		"supports_reasoning":         &entry.SupportsReasoning,
		"supports_vision":            &entry.SupportsVision,
		"thinking":                   &entry.Thinking,
		"supportsImages":             &entry.SupportsImages,
		"maxOutputTokens":            &entry.MaxOutputTokensCamel,
		"max_input_tokens":           &entry.MaxInputTokens,
		"max_tokens":                 &entry.MaxTokens,
		"capabilities.thinking":      &entry.Capabilities.Thinking,
		"capabilities.image_input":   &entry.Capabilities.ImageInput,
		"capabilities.pdf_input":     &entry.Capabilities.PDFInput,
		"capabilities.effort":        &entry.Capabilities.Effort,
		"display_name":               &entry.DisplayName,
		"description":                &entry.Description,
		"default_reasoning_level":    &entry.DefaultReasoningLevel,
		"supported_reasoning_levels": &entry.SupportedReasoningLevels,
		"reasoning_options":          &entry.ReasoningOptions,
		"input_modalities":           &entry.InputModalities,
		"modalities.input":           &entry.Modalities.Input,
		"modalities.output":          &entry.Modalities.Output,
		"context_window":             &entry.ContextWindow,
		"context_length":             &entry.ContextLength,
		"max_context_window":         &entry.MaxContextWindow,
		"max_output_tokens":          &entry.MaxOutputTokens,
		"limit.context":              &entry.Limit.Context,
		"limit.output":               &entry.Limit.Output,
	})
	var reasoning struct {
		Supported        *bool
		Type             string
		Valid            []json.RawMessage
		Default          string
		SupportedEfforts []json.RawMessage
		DefaultEffort    string
	}
	var rawReasoning json.RawMessage
	decodeUpstreamOptionalFields(body, map[string]any{"reasoning": &rawReasoning})
	raw := gjson.ParseBytes(rawReasoning)
	if raw.IsObject() {
		invalid = decodeUpstreamOptionalFields([]byte(raw.Raw), map[string]any{
			"supported": &reasoning.Supported, "type": &reasoning.Type,
			"effort.valid": &reasoning.Valid, "effort.default": &reasoning.Default,
			"supported_efforts": &reasoning.SupportedEfforts, "default_effort": &reasoning.DefaultEffort,
		}) || invalid
		if reasoning.Valid == nil {
			reasoning.Valid = reasoning.SupportedEfforts
		}
		if reasoning.Default == "" {
			reasoning.Default = reasoning.DefaultEffort
		}
		entry.Reasoning = reasoning.Supported
		// The native "only" declaration explicitly identifies reasoning support.
		// Unknown type values (including an empty object) do not imply false.
		if entry.Reasoning == nil && reasoning.Type == "only" {
			supported := true
			entry.Reasoning = &supported
		}
		if entry.SupportedReasoningLevels == nil {
			entry.SupportedReasoningLevels = reasoning.Valid
		}
		if entry.DefaultReasoningLevel == "" {
			entry.DefaultReasoningLevel = reasoning.Default
		}
	} else {
		invalid = decodeUpstreamOptionalFields(body, map[string]any{"reasoning": &entry.Reasoning}) || invalid
	}
	if entry.Reasoning == nil {
		invalid = decodeUpstreamOptionalFields(body, map[string]any{"supports_reasoning": &entry.Reasoning}) || invalid
	}
	return invalid
}

// name can be a display label when an explicit protocol ID is present. Keep
// malformed labels separate from identity, while rejecting malformed IDs.
func (entry *upstreamModelEntry) UnmarshalJSON(body []byte) error {
	type plain upstreamModelEntry
	var decoded struct {
		*plain
		Name json.RawMessage `json:"name"`
	}
	*entry = upstreamModelEntry{}
	decoded.plain = (*plain)(entry)
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	if len(decoded.Name) == 0 {
		return nil
	}
	err := json.Unmarshal(decoded.Name, &entry.Name)
	if err != nil && entry.ID == "" && entry.Slug == "" && entry.Model == "" && entry.ModelID == "" && entry.ModelIDSnake == "" {
		return err
	}
	return nil
}

func (card *MistralModelCard) UnmarshalJSON(body []byte) error {
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &identity); err != nil {
		return err
	}
	*card = MistralModelCard{ID: identity.ID}
	var capabilities map[string]json.RawMessage
	decodeUpstreamOptionalFields(body, map[string]any{
		"name": &card.Name, "description": &card.Description, "type": &card.Type,
		"root": &card.Root, "aliases": &card.Aliases, "archived": &card.Archived,
		"max_context_length": &card.MaxContextLength, "capabilities": &capabilities,
	})
	if card.MaxContextLength != nil && *card.MaxContextLength <= 0 {
		card.MaxContextLength = nil
	}
	if capabilities != nil {
		card.Capabilities = make(map[string]*bool, len(capabilities))
		for key, raw := range capabilities {
			var supported *bool
			if json.Unmarshal(raw, &supported) == nil {
				card.Capabilities[key] = supported
			}
		}
	}
	return nil
}

func (model *qianfanModel) UnmarshalJSON(body []byte) error {
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &identity); err != nil {
		return err
	}
	*model = qianfanModel{ID: identity.ID}
	decodeUpstreamOptionalFields(body, map[string]any{
		"object": &model.Object, "owned_by": &model.OwnedBy, "created": &model.Created,
		"type": &model.Type, "context_length": &model.ContextLength,
		"max_completions_tokens": &model.MaxCompletionsTokens, "max_tokens": &model.MaxTokens,
		"prompt_tokens": &model.PromptTokens, "architecture": &model.Architecture,
	})
	return nil
}

func knownCatalogLimit(n int64) *int64 {
	if n <= 0 {
		return nil
	}
	return &n
}

func catalogModalities(groups ...[]string) []string {
	result := []string{}
	for _, values := range groups {
		for _, value := range values {
			switch value {
			case "text", "image", "audio", "video", "pdf", "embedding", "3d":
				result = append(result, value)
			}
		}
	}
	return result
}

func (b PlazaCatalogBinding) CatalogModelFacts(expires, now time.Time) []catalog.ModelFacts {
	m := b.Metadata
	fields := []struct {
		name  string
		apply func(*catalog.Facts) bool
	}{
		{"context_window", func(f *catalog.Facts) bool {
			f.Context = knownCatalogLimit(m.ContextWindow)
			if AuthoritativeCatalogModelField(m, "context_limit_kind") {
				f.ContextKind = m.ContextLimitKind
			}
			return f.Context != nil
		}},
		{"max_output_tokens", func(f *catalog.Facts) bool { f.Output = knownCatalogLimit(m.MaxOutputTokens); return f.Output != nil }},
		{"embedding_dimensions", func(f *catalog.Facts) bool {
			f.EmbeddingDimensions = knownCatalogLimit(m.EmbeddingDimensions)
			return f.EmbeddingDimensions != nil
		}},
		{"supports_function_calling", func(f *catalog.Facts) bool { f.Tools = m.SupportsFunctionCalling; return f.Tools != nil }},
		{"supports_vision", func(f *catalog.Facts) bool { f.Vision = m.SupportsVision; return f.Vision != nil }},
		{"reasoning", func(f *catalog.Facts) bool { f.Reasoning = m.Reasoning; return f.Reasoning != nil }},
		{"supports_prompt_caching", func(f *catalog.Facts) bool { f.Cache = m.SupportsPromptCaching; return f.Cache != nil }},
		{"supports_pdf", func(f *catalog.Facts) bool { f.PDF = m.SupportsPDF; return f.PDF != nil }},
		{"input_modalities", func(f *catalog.Facts) bool {
			f.Modalities = append(f.Modalities, catalogModalities(m.InputModalities)...)
			return m.InputModalities != nil
		}},
		{"task_type", func(f *catalog.Facts) bool {
			modalities, _ := catalog.TaskPresentation(m.TaskType)
			f.Modalities = append(f.Modalities, modalities...)
			return len(modalities) > 0
		}},
		{"output_modalities", func(f *catalog.Facts) bool {
			f.Modalities = append(f.Modalities, catalogModalities(m.OutputModalities)...)
			return m.OutputModalities != nil
		}},
	}
	byEvidence := make(map[string]catalog.ModelFacts)
	for _, field := range fields {
		if !AuthoritativeCatalogModelField(m, field.name) {
			continue
		}
		e := m.FieldEvidence[field.name]
		observed, _ := time.Parse(time.RFC3339Nano, e.ObservedAt)
		if observed.IsZero() && e.Source == "upstream" {
			observed, _ = time.Parse(time.RFC3339Nano, b.EvidenceObservedAt)
		}
		source := "upstream_discovery"
		if e.Source == "account-declaration" {
			source = "account_declaration"
		}
		evidence := catalog.Observation(source, "unknown", observed, expires, now)
		key := catalog.Digest(evidence)
		f, ok := byEvidence[key]
		if !ok {
			f = catalog.ModelFacts{Facts: catalog.Facts{Modalities: []string{}}, Evidence: evidence}
		}
		if field.apply(&f.Facts) {
			byEvidence[key] = f
		}
	}
	out := make([]catalog.ModelFacts, 0, len(byEvidence))
	for _, facts := range byEvidence {
		out = append(out, facts)
	}
	return out
}
