package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"strings"
	"time"
)

// DefaultModelIDsForPlatform cannot establish a public model set without
// credential evidence. Administrative defaults use ModelRegistrySnapshot instead.
func DefaultModelIDsForPlatform(platform string) []string { return []string{} }

//go:embed platform_model_presets.json
var platformModelPresetsJSON []byte

type PlatformPresetModel struct {
	CreatedAt    string `json:"created_at,omitempty"`
	ModelVersion string `json:"model_version,omitempty"`

	CanonicalModelID      string                     `json:"canonical_model_id,omitempty"`
	ContextLimitKind      string                     `json:"context_limit_kind,omitempty"`
	MaxInputTokens        int64                      `json:"max_input_tokens,omitempty"`
	InputModalitiesStatus string                     `json:"input_modalities_status,omitempty"`
	ReasoningControl      *ReasoningControl          `json:"reasoning_control,omitempty"`
	EmbeddingDimensions   int64                      `json:"embedding_dimensions,omitempty"`
	TaskLimits            map[string]json.RawMessage `json:"task_limits,omitempty"`
	RetirementDate        string                     `json:"retirement_date,omitempty"`
	SourceContentHash     string                     `json:"source_content_hash,omitempty"`
	VerifiedAt            string                     `json:"verified_at,omitempty"`
	ID                    string                     `json:"id"`
	DisplayName           string                     `json:"display_name"`
	TaskType              string                     `json:"task_type"`
	InputModalities       []string                   `json:"input_modalities"`
	OutputModalities      []string                   `json:"output_modalities"`
	Methods               []string                   `json:"methods"`
	Lifecycle             string                     `json:"lifecycle"`
	ContextWindow         int64                      `json:"context_window,omitempty"`
	MaxOutputTokens       int64                      `json:"max_output_tokens,omitempty"`
	FieldSources          map[string]string          `json:"field_sources"`
}

type PlatformModelPreset struct {
	Platform        string                `json:"platform"`
	ProviderProfile string                `json:"provider_profile"`
	Revision        string                `json:"revision"`
	CoverageStatus  string                `json:"coverage_status"`
	CheckedAt       string                `json:"checked_at"`
	SourceRefs      []string              `json:"source_refs"`
	Gaps            []string              `json:"gaps,omitempty"`
	Models          []PlatformPresetModel `json:"models"`
}

// PlatformModelPresets returns validated candidates, never account authorization.
func PlatformModelPresets() ([]PlatformModelPreset, error) {
	return NewModelRegistry().Snapshot().Presets(), nil
}

func parsePlatformModelPresets(body []byte) ([]PlatformModelPreset, error) {
	var presets []PlatformModelPreset
	if err := json.Unmarshal(body, &presets); err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	profiles := map[string]bool{}
	for _, preset := range presets {
		if !domain.IsConcretePlatform(preset.Platform) || preset.ProviderProfile == "" || preset.Revision == "" {
			return nil, fmt.Errorf("invalid platform preset identity")
		}
		if _, err := time.Parse(time.RFC3339Nano, preset.CheckedAt); err != nil {
			return nil, err
		}
		switch preset.CoverageStatus {
		case "complete", "partial", "unconfigured":
		default:
			return nil, fmt.Errorf("invalid preset coverage")
		}
		profile := preset.Platform + ":" + preset.ProviderProfile
		if preset.CoverageStatus != "unconfigured" && (len(preset.Models) == 0 || len(preset.SourceRefs) == 0) {
			return nil, fmt.Errorf("configured preset has no candidates or sources: %s", profile)
		}
		if profiles[profile] {
			return nil, fmt.Errorf("duplicate preset profile %s", profile)
		}
		profiles[profile], covered[preset.Platform] = true, true
		seen := map[string]bool{}
		for _, model := range preset.Models {
			if strings.TrimSpace(model.ID) == "" || seen[model.ID] || model.DisplayName == "" || model.TaskType == "" || model.Lifecycle == "" || len(model.FieldSources) == 0 {
				return nil, fmt.Errorf("invalid model in %s", profile)
			}
			if model.CreatedAt != "" {
				if _, err := time.Parse(time.RFC3339Nano, model.CreatedAt); err != nil {
					return nil, fmt.Errorf("invalid model creation time: %w", err)
				}
			}
			seen[model.ID] = true
		}
	}
	for _, platform := range domain.ConcretePlatforms() {
		// Table providers may use manual declarations without vendor presets.
		if IsCompatibleProvider(platform) {
			continue
		}
		if !covered[platform] {
			return nil, fmt.Errorf("missing preset coverage: %s", platform)
		}
	}
	return presets, nil
}
