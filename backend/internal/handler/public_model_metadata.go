package handler

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Public observations preserve uncertainty instead of treating absent facts as zero.
type publicFact[Value comparable] struct {
	Guaranteed *Value  `json:"guaranteed"`
	Available  []Value `json:"available"`
	Unknown    bool    `json:"unknown"`
}
type publicLimit struct {
	Kind string `json:"kind,omitempty"`
	publicFact[int64]
	ObservedMinimum *int64 `json:"observed_minimum"`
}
type publicModalities struct {
	Guaranteed *[]string `json:"guaranteed"`
	Available  []string  `json:"available"`
	Unknown    bool      `json:"unknown"`
}
type publicModelMetadata struct {
	InputContext            publicLimit       `json:"input_context"`
	TotalContext            publicLimit       `json:"total_context"`
	UnspecifiedContext      publicLimit       `json:"unspecified_context"`
	MaxOutputTokens         publicLimit       `json:"max_output_tokens"`
	EmbeddingDimensions     publicFact[int64] `json:"embedding_dimensions"`
	Reasoning               publicFact[bool]  `json:"reasoning"`
	SupportsVision          publicFact[bool]  `json:"supports_vision"`
	SupportsFunctionCalling publicFact[bool]  `json:"supports_function_calling"`
	SupportsPromptCaching   publicFact[bool]  `json:"supports_prompt_caching"`
	SupportsPDF             publicFact[bool]  `json:"supports_pdf"`
	InputModalities         publicModalities  `json:"input_modalities"`
	OutputModalities        publicModalities  `json:"output_modalities"`
}

func aggregatePublicFact[Value comparable](values []*Value) publicFact[Value] {
	result := publicFact[Value]{Available: []Value{}, Unknown: len(values) == 0}
	seen := map[Value]bool{}
	for _, value := range values {
		if value == nil {
			result.Unknown = true
			continue
		}
		if !seen[*value] {
			result.Available = append(result.Available, *value)
			seen[*value] = true
		}
	}
	if !result.Unknown && len(result.Available) == 1 {
		value := result.Available[0]
		result.Guaranteed = &value
	}
	return result
}
func aggregatePublicBoolean(values []*bool) publicFact[bool] {
	result := aggregatePublicFact(values)
	sort.Slice(result.Available, func(left, right int) bool { return !result.Available[left] && result.Available[right] })
	if !result.Unknown && len(result.Available) > 0 {
		guaranteed := result.Available[0]
		result.Guaranteed = &guaranteed
	}
	return result
}
func aggregatePublicLimit(values []*int64) publicLimit {
	result := publicLimit{publicFact: aggregatePublicFact(values)}
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
func aggregatePublicContext(values []*int64, kind string) publicLimit {
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
func aggregatePublicModalities(values [][]string) publicModalities {
	result := publicModalities{Available: []string{}, Unknown: len(values) == 0}
	counts := map[string]int{}
	for _, value := range values {
		normalized := publicModelModalities(value)
		if normalized == nil {
			result.Unknown = true
			continue
		}
		for _, modality := range *normalized {
			counts[modality]++
		}
	}
	intersection := []string{}
	for modality, count := range counts {
		result.Available = append(result.Available, modality)
		if count == len(values) {
			intersection = append(intersection, modality)
		}
	}
	sort.Strings(result.Available)
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
	input, total, unspecified, output, dimensions := []*int64{}, []*int64{}, []*int64{}, []*int64{}, []*int64{}
	reasoning, vision, functions, caching, pdf := []*bool{}, []*bool{}, []*bool{}, []*bool{}, []*bool{}
	inputModalities, outputModalities := [][]string{}, [][]string{}
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
		input = append(input, inputValue)
		total = append(total, totalValue)
		unspecified = append(unspecified, unspecifiedValue)
		output = append(output, positivePublicValue(observation.MaxOutputTokens))
		dimensions = append(dimensions, positivePublicValue(observation.EmbeddingDimensions))
		reasoning = append(reasoning, observation.Reasoning)
		vision = append(vision, observation.SupportsVision)
		functions = append(functions, observation.SupportsFunctionCalling)
		caching = append(caching, observation.SupportsPromptCaching)
		pdf = append(pdf, observation.SupportsPDF)
		inputModalities = append(inputModalities, observation.InputModalities)
		outputModalities = append(outputModalities, observation.OutputModalities)
	}
	dimensionFact := aggregatePublicFact(dimensions)
	sort.Slice(dimensionFact.Available, func(left, right int) bool { return dimensionFact.Available[left] < dimensionFact.Available[right] })
	return publicModelMetadata{
		InputContext:            aggregatePublicContext(input, "input"),
		TotalContext:            aggregatePublicContext(total, "total"),
		UnspecifiedContext:      aggregatePublicContext(unspecified, "unspecified"),
		MaxOutputTokens:         aggregatePublicLimit(output),
		EmbeddingDimensions:     dimensionFact,
		Reasoning:               aggregatePublicBoolean(reasoning),
		SupportsVision:          aggregatePublicBoolean(vision),
		SupportsFunctionCalling: aggregatePublicBoolean(functions),
		SupportsPromptCaching:   aggregatePublicBoolean(caching),
		SupportsPDF:             aggregatePublicBoolean(pdf),
		InputModalities:         aggregatePublicModalities(inputModalities),
		OutputModalities:        aggregatePublicModalities(outputModalities),
	}
}
