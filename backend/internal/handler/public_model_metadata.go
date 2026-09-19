package handler

import (
	"bytes"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const publicModelMetadataContextKey = "public_model_metadata"

// publicModelMetadata is a specification whitelist, deliberately excluding
// identity, accounts, evidence, source URLs, diagnostics, pricing and routing.
// Pointer-to-slice fields preserve unknown versus explicitly empty arrays.
type publicModelMetadata struct {
	ContextWindow           int64     `json:"context_window,omitempty"`
	ContextLimitKind        string    `json:"context_limit_kind,omitempty"`
	MaxOutputTokens         int64     `json:"max_output_tokens,omitempty"`
	InputModalities         *[]string `json:"input_modalities,omitempty"`
	OutputModalities        *[]string `json:"output_modalities,omitempty"`
	Reasoning               *bool     `json:"reasoning,omitempty"`
	SupportsVision          *bool     `json:"supports_vision,omitempty"`
	SupportsFunctionCalling *bool     `json:"supports_function_calling,omitempty"`
	SupportsPromptCaching   *bool     `json:"supports_prompt_caching,omitempty"`
	SupportsPDF             *bool     `json:"supports_pdf,omitempty"`
	EmbeddingDimensions     int64     `json:"embedding_dimensions,omitempty"`
}

func publicMetadataFields(metadata service.UpstreamModelMetadata) (map[string]json.RawMessage, error) {
	projection := publicModelMetadata{
		ContextWindow:   max(0, metadata.ContextWindow),
		MaxOutputTokens: max(0, metadata.MaxOutputTokens), Reasoning: metadata.Reasoning,
		SupportsVision: metadata.SupportsVision, SupportsFunctionCalling: metadata.SupportsFunctionCalling,
		SupportsPromptCaching: metadata.SupportsPromptCaching, SupportsPDF: metadata.SupportsPDF,
		EmbeddingDimensions: max(0, metadata.EmbeddingDimensions),
	}
	if metadata.ContextLimitKind == "input" || metadata.ContextLimitKind == "total" {
		projection.ContextLimitKind = metadata.ContextLimitKind
	}
	projection.InputModalities = publicModelModalities(metadata.InputModalities)
	projection.OutputModalities = publicModelModalities(metadata.OutputModalities)
	body, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	err = json.Unmarshal(body, &fields)
	return fields, err
}

func publicModelModalities(modalities []string) *[]string {
	if modalities == nil {
		return nil
	}
	for _, modality := range modalities {
		switch modality {
		case "text", "image", "audio", "video", "pdf", "embedding":
		default:
			return nil // Do not publish arbitrary provider-supplied text as a modality.
		}
	}
	return &modalities
}

func projectPublicModelMetadata(models any, metadata map[string][]service.UpstreamModelMetadata) (any, error) {
	body, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		var modelID string
		if err := json.Unmarshal(entry["id"], &modelID); err != nil {
			return nil, err
		}
		var common map[string]json.RawMessage
		for index, observation := range metadata[modelID] {
			fields, err := publicMetadataFields(observation)
			if err != nil {
				return nil, err
			}
			if index == 0 {
				common = fields
				continue
			}
			for field, value := range common {
				if !bytes.Equal(value, fields[field]) {
					delete(common, field)
				}
			}
		}
		// Context magnitude has no public meaning without its agreed limit kind.
		if common["context_window"] == nil || common["context_limit_kind"] == nil {
			delete(common, "context_window")
			delete(common, "context_limit_kind")
		}
		if len(common) > 0 {
			encoded, err := json.Marshal(common)
			if err != nil {
				return nil, err
			}
			entry["capabilities"] = encoded
		}
	}
	return entries, nil
}
