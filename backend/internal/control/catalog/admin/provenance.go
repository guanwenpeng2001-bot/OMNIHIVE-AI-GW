package admin

import (
	"encoding/json"
	"regexp"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type fieldObservation struct {
	Field           string     `json:"field"`
	State           string     `json:"state"`
	SourceKind      string     `json:"source_kind,omitempty"`
	ArtifactVersion string     `json:"artifact_version,omitempty"`
	ObservedAt      *time.Time `json:"observed_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Value           any        `json:"value,omitempty"`
}

type commonFacts struct {
	Context             *int64   `json:"context_tokens,string"`
	ContextKind         string   `json:"context_kind"`
	Output              *int64   `json:"max_output_tokens,string"`
	EmbeddingDimensions *int64   `json:"embedding_dimensions,string"`
	Tools               *bool    `json:"tools"`
	Vision              *bool    `json:"vision"`
	Reasoning           *bool    `json:"reasoning"`
	Cache               *bool    `json:"prompt_caching"`
	PDF                 *bool    `json:"pdf"`
	Modalities          []string `json:"modalities"`
}

func projectFacts(f *catalog.Facts) *commonFacts {
	if f == nil {
		return nil
	}
	return &commonFacts{
		Context: f.Context, ContextKind: f.ContextKind, Output: f.Output, EmbeddingDimensions: f.EmbeddingDimensions,
		Tools: f.Tools, Vision: f.Vision, Reasoning: f.Reasoning, Cache: f.Cache, PDF: f.PDF, Modalities: modalities(f.Modalities),
	}
}

var artifactDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func projectProvenance(records []catalog.Provenance) []fieldObservation {
	out := make([]fieldObservation, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		value, known := projectFieldValue(record.Field, record.Value)
		if !known || record.SourceKind != "" && !sourceKind(record.SourceKind) {
			continue
		}
		p := fieldObservation{Field: record.Field, State: record.State, SourceKind: record.SourceKind,
			ObservedAt: record.ObservedAt, ExpiresAt: record.ExpiresAt, Value: value}
		switch p.SourceKind {
		case catalog.BuiltinReviewed, catalog.PlatformPreset, catalog.BuiltinSupplement, catalog.PiAIReference:
			if artifactDigest.MatchString(record.ArtifactVersion) {
				p.ArtifactVersion = record.ArtifactVersion
			}
		}
		key := catalog.Digest(p)
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

type contextWindow struct {
	Tokens int64  `json:"tokens,string"`
	Basis  string `json:"basis"`
}

func projectFieldValue(field string, value any) (any, bool) {
	switch field {
	case "context_window":
		// BE-3's context observation uses a private struct. Decode only its
		// numeric token count and recognized basis for the wire projection.
		var window contextWindow
		if raw, err := json.Marshal(value); err == nil && json.Unmarshal(raw, &window) == nil && window.Tokens > 0 {
			switch window.Basis {
			case "input", "total", "unspecified":
				return window, true
			}
		}
	case "max_output_tokens", "embedding_dimensions":
		if number, ok := value.(int64); ok {
			return strconv.FormatInt(number, 10), true
		}
	case "supports_function_calling", "supports_vision", "supports_reasoning", "supports_prompt_caching", "supports_pdf":
		if supported, ok := value.(bool); ok {
			return supported, true
		}
	case "modalities", "input_modalities", "output_modalities":
		if values, ok := value.([]string); ok {
			return modalities(values), true
		}
	case "supported_methods":
		if values, ok := value.([]string); ok {
			methods := make([]string, 0, len(values))
			for _, method := range values {
				if _, _, adapter := catalog.GatewayAction(method); adapter != "" || service.ModelAvailabilityEndpointValid(method) {
					methods = append(methods, method)
				}
			}
			return methods, true
		}
	case "reference_price":
		return nil, true
	default:
		return nil, false
	}
	return nil, true
}

func modalities(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch value {
		case "text", "image", "audio", "video", "pdf", "embedding", "3d":
			out = append(out, value)
		}
	}
	return out
}
