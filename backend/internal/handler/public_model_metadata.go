package handler

import (
	"sort"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// Public observations preserve uncertainty instead of treating absent facts as zero.
type publicFact[Value comparable] struct {
	Guaranteed *Value   `json:"guaranteed"`
	Available  []Value  `json:"available"`
	Unknown    bool     `json:"unknown"`
	Sources    []string `json:"sources,omitempty"`
}
type publicLimit struct {
	Kind string `json:"kind,omitempty"`
	publicFact[int64]
	ObservedMinimum *int64 `json:"observed_minimum"`
}
type publicStringSet struct {
	Guaranteed *[]string `json:"guaranteed"`
	Available  []string  `json:"available"`
	Unknown    bool      `json:"unknown"`
	Sources    []string  `json:"sources,omitempty"`
}

type publicModalities = publicStringSet
type publicModelMetadata struct {
	InputContext            publicLimit       `json:"input_context"`
	TotalContext            publicLimit       `json:"total_context"`
	UnspecifiedContext      publicLimit       `json:"unspecified_context"`
	MaxOutputTokens         publicLimit       `json:"max_output_tokens"`
	EmbeddingDimensions     publicFact[int64] `json:"embedding_dimensions"`
	Reasoning               publicFact[bool]  `json:"reasoning"`
	ReasoningLevels         publicStringSet   `json:"supported_reasoning_levels"`
	SupportsVision          publicFact[bool]  `json:"supports_vision"`
	SupportsFunctionCalling publicFact[bool]  `json:"supports_function_calling"`
	SupportsPromptCaching   publicFact[bool]  `json:"supports_prompt_caching"`
	SupportsPDF             publicFact[bool]  `json:"supports_pdf"`
	InputModalities         publicStringSet   `json:"input_modalities"`
	OutputModalities        publicStringSet   `json:"output_modalities"`
}

// Each eligible binding keeps a position even when only general information is known.
type publicObservation[Value any] struct {
	value         *Value
	authoritative bool
	source        string
}

// The resolver has already matched the current account profile. Native facts and
// identity-bound declarations must also match this field's model/profile. Gateway
// evidence currently describes supported_methods only (account_protocol), not any
// model capability aggregated here; do not promote protocol support to model facts.
func authoritativePublicField(metadata service.UpstreamModelMetadata, field string) bool {
	evidence, exists := metadata.FieldEvidence[field]
	if !exists || evidence.Stale || metadata.ID == "" || metadata.ProviderProfile == "" ||
		evidence.MatchedModelID != metadata.ID || evidence.ProviderProfile != metadata.ProviderProfile {
		return false
	}
	switch evidence.Source {
	case "upstream":
		return evidence.MatchRule == "native_id"
	case "account-declaration":
		return evidence.MatchRule == "identity_bound_exact"
	default:
		return false
	}
}

func observePublicField[Value any](metadata service.UpstreamModelMetadata, field string, value *Value) publicObservation[Value] {
	return publicObservation[Value]{value: value, authoritative: authoritativePublicField(metadata, field), source: publicCapabilitySource(metadata.FieldEvidence[field].Source)}
}

// Publish source categories only, never arbitrary URLs, identities or evidence.
// Sources explain available observations; only Guaranteed establishes a binding
// guarantee, and Unknown remains true for reference-only or incomplete evidence.
func publicCapabilitySource(source string) string {
	switch source {
	case "upstream", "account-declaration", "models.dev", "litellm", "platform-preset", "registry":
		return source
	default:
		return "unavailable"
	}
}

func appendPublicSource(sources []string, source string) []string {
	if source == "" {
		return sources
	}
	for _, existing := range sources {
		if existing == source {
			return sources
		}
	}
	return append(sources, source)
}

// Identity serialization also uses this helper; retain its existing contract.
func aggregatePublicFact[Value comparable](values []*Value) publicFact[Value] {
	observations := make([]publicObservation[Value], len(values))
	for index, value := range values {
		observations[index] = publicObservation[Value]{value: value, authoritative: true}
	}
	return aggregatePublicObservedFact(observations)
}

func aggregatePublicObservedFact[Value comparable](values []publicObservation[Value]) publicFact[Value] {
	result := publicFact[Value]{Available: []Value{}, Unknown: len(values) == 0}
	seen := map[Value]bool{}
	for _, observation := range values {
		value := observation.value
		if !observation.authoritative {
			result.Unknown = true
		}
		if value == nil {
			result.Unknown = true
			continue
		}
		if !seen[*value] {
			result.Available = append(result.Available, *value)
			seen[*value] = true
		}
		result.Sources = appendPublicSource(result.Sources, observation.source)
	}
	sort.Strings(result.Sources)
	if !result.Unknown && len(result.Available) == 1 {
		value := result.Available[0]
		result.Guaranteed = &value
	}
	return result
}
func aggregatePublicBoolean(values []publicObservation[bool]) publicFact[bool] {
	result := aggregatePublicObservedFact(values)
	sort.Slice(result.Available, func(left, right int) bool { return !result.Available[left] && result.Available[right] })
	if !result.Unknown && len(result.Available) > 0 {
		guaranteed := result.Available[0]
		result.Guaranteed = &guaranteed
	}
	return result
}
func aggregatePublicLimit(values []publicObservation[int64]) publicLimit {
	result := publicLimit{publicFact: aggregatePublicObservedFact(values)}
	sort.Slice(result.Available, func(left, right int) bool { return result.Available[left] < result.Available[right] })
	if len(result.Available) > 0 {
		minimum := result.Available[0]
		result.ObservedMinimum = &minimum
		if !result.Unknown {
			result.Guaranteed = &minimum
		}
	}
	return result
}
func aggregatePublicContext(values []publicObservation[int64], kind string) publicLimit {
	result := aggregatePublicLimit(values)
	result.Kind = kind
	return result
}
func publicModelModalities(values []string) *[]string {
	if values == nil {
		return nil
	}
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "text", "image", "audio", "video", "pdf", "embedding":
		default:
			return nil
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return &result
}
func publicReasoningLevels(values []string) *[]string {
	if values == nil {
		return nil
	}
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "off", "disabled":
			value = "none"
		case "extra-high", "extra_high":
			value = "xhigh"
		case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		default:
			return nil
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return &result
}

func aggregatePublicModalities(values []publicObservation[[]string]) publicStringSet {
	return aggregatePublicStringSet(values, publicModelModalities)
}

func aggregatePublicStringSet(values []publicObservation[[]string], normalize func([]string) *[]string) publicStringSet {
	result := publicStringSet{Available: []string{}, Unknown: len(values) == 0}
	counts := map[string]int{}
	for _, observation := range values {
		if !observation.authoritative {
			result.Unknown = true
		}
		var normalized *[]string
		if observation.value != nil {
			normalized = normalize(*observation.value)
		}
		if normalized == nil {
			result.Unknown = true
			continue
		}
		result.Sources = appendPublicSource(result.Sources, observation.source)
		for _, value := range *normalized {
			counts[value]++
		}
	}
	intersection := []string{}
	for value, count := range counts {
		result.Available = append(result.Available, value)
		if count == len(values) {
			intersection = append(intersection, value)
		}
	}
	sort.Strings(result.Available)
	sort.Strings(result.Sources)
	sort.Strings(intersection)
	if !result.Unknown {
		result.Guaranteed = &intersection
	}
	return result
}
func positivePublicValue(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
func aggregatePublicMetadata(observations []service.UpstreamModelMetadata) publicModelMetadata {
	input, total, unspecified, output, dimensions := []publicObservation[int64]{}, []publicObservation[int64]{}, []publicObservation[int64]{}, []publicObservation[int64]{}, []publicObservation[int64]{}
	reasoning, vision, functions, caching, pdf := []publicObservation[bool]{}, []publicObservation[bool]{}, []publicObservation[bool]{}, []publicObservation[bool]{}, []publicObservation[bool]{}
	inputModalities, outputModalities := []publicObservation[[]string]{}, []publicObservation[[]string]{}
	reasoningLevels := []publicObservation[[]string]{}
	for _, observation := range observations {
		var inputValue, totalValue, unspecifiedValue *int64
		switch observation.ContextLimitKind {
		case "input":
			inputValue = positivePublicValue(observation.ContextWindow)
		case "total":
			totalValue = positivePublicValue(observation.ContextWindow)
		default:
			unspecifiedValue = positivePublicValue(observation.ContextWindow)
		}
		contextAuthoritative := authoritativePublicField(observation, "context_window")
		// A native limit with a registry-supplied kind is still general information.
		if observation.ContextLimitKind == "input" || observation.ContextLimitKind == "total" {
			contextAuthoritative = contextAuthoritative && authoritativePublicField(observation, "context_limit_kind")
		}
		contextSource := publicCapabilitySource(observation.FieldEvidence["context_window"].Source)
		input = append(input, publicObservation[int64]{value: inputValue, authoritative: contextAuthoritative, source: contextSource})
		total = append(total, publicObservation[int64]{value: totalValue, authoritative: contextAuthoritative, source: contextSource})
		unspecified = append(unspecified, publicObservation[int64]{value: unspecifiedValue, authoritative: contextAuthoritative, source: contextSource})
		output = append(output, observePublicField(observation, "max_output_tokens", positivePublicValue(observation.MaxOutputTokens)))
		dimensions = append(dimensions, observePublicField(observation, "embedding_dimensions", positivePublicValue(observation.EmbeddingDimensions)))
		reasoning = append(reasoning, observePublicField(observation, "reasoning", observation.Reasoning))
		callableLevels := observation.SupportedReasoningLevels
		if observation.ProviderProfile == "openai-codex" {
			// Ultra is a Codex client workflow, not a callable effort. Build a
			// separate slice so discovery and the native manifest retain it.
			// No remaining callable levels means unknown, not an empty guarantee.
			callableLevels = nil
			for _, level := range observation.SupportedReasoningLevels {
				if !strings.EqualFold(strings.TrimSpace(level), "ultra") {
					callableLevels = append(callableLevels, level)
				}
			}
		}
		reasoningLevels = append(reasoningLevels, observePublicField(observation, "supported_reasoning_levels", &callableLevels))
		vision = append(vision, observePublicField(observation, "supports_vision", observation.SupportsVision))
		functions = append(functions, observePublicField(observation, "supports_function_calling", observation.SupportsFunctionCalling))
		caching = append(caching, observePublicField(observation, "supports_prompt_caching", observation.SupportsPromptCaching))
		pdf = append(pdf, observePublicField(observation, "supports_pdf", observation.SupportsPDF))
		inputModalities = append(inputModalities, observePublicField(observation, "input_modalities", &observation.InputModalities))
		outputModalities = append(outputModalities, observePublicField(observation, "output_modalities", &observation.OutputModalities))
	}
	dimensionFact := aggregatePublicObservedFact(dimensions)
	sort.Slice(dimensionFact.Available, func(left, right int) bool { return dimensionFact.Available[left] < dimensionFact.Available[right] })
	return publicModelMetadata{
		InputContext:            aggregatePublicContext(input, "input"),
		TotalContext:            aggregatePublicContext(total, "total"),
		UnspecifiedContext:      aggregatePublicContext(unspecified, "unspecified"),
		MaxOutputTokens:         aggregatePublicLimit(output),
		EmbeddingDimensions:     dimensionFact,
		Reasoning:               aggregatePublicBoolean(reasoning),
		ReasoningLevels:         aggregatePublicStringSet(reasoningLevels, publicReasoningLevels),
		SupportsVision:          aggregatePublicBoolean(vision),
		SupportsFunctionCalling: aggregatePublicBoolean(functions),
		SupportsPromptCaching:   aggregatePublicBoolean(caching),
		SupportsPDF:             aggregatePublicBoolean(pdf),
		InputModalities:         aggregatePublicModalities(inputModalities),
		OutputModalities:        aggregatePublicModalities(outputModalities),
	}
}
