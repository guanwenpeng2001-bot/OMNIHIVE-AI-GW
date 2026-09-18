package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"strings"
	"time"
)

// DefaultModelIDsForPlatform returns the existing platform preset, never a
// different provider's catalog. An unconfigured catalog is explicitly empty.
// Domestic provider candidates stay in PlatformModelPresets, separated by
// deployment and plan; they must not become this public Models fallback.
func DefaultModelIDsForPlatform(platform string) []string {
	if platform != PlatformComposite {
		d, ok := domain.LookupPlatform(platform)
		if !ok || !d.ModelCatalog {
			return []string{}
		}
	}
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAnthropic:
		return claude.DefaultModelIDs()
	case PlatformGrok:
		return xai.DefaultModelIDs()
	case PlatformCursor:
		return DefaultCursorModelIDs()
	case PlatformOpenCodeGo:
		return DefaultOpenCodeGoModelIDs()
	case PlatformQianwen:
		return DefaultQianwenModelIDs()
	case PlatformCommandCode:
		return DefaultCommandCodeModelIDs()
	case PlatformComposite:
		ids := make([]string, 0)
		seen := make(map[string]struct{})
		for _, concretePlatform := range domain.ConcretePlatforms() {
			for _, id := range DefaultModelIDsForPlatform(concretePlatform) {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return ids
	default:
		return []string{}
	}
}

//go:embed platform_model_presets.json
var platformModelPresetsJSON []byte

type PlatformPresetModel struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"display_name"`
	TaskType         string            `json:"task_type"`
	InputModalities  []string          `json:"input_modalities"`
	OutputModalities []string          `json:"output_modalities"`
	Methods          []string          `json:"methods"`
	Lifecycle        string            `json:"lifecycle"`
	ContextWindow    int64             `json:"context_window,omitempty"`
	MaxOutputTokens  int64             `json:"max_output_tokens,omitempty"`
	FieldSources     map[string]string `json:"field_sources"`
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
	var presets []PlatformModelPreset
	if err := json.Unmarshal(platformModelPresetsJSON, &presets); err != nil {
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
			seen[model.ID] = true
		}
	}
	for _, platform := range domain.ConcretePlatforms() {
		if !covered[platform] {
			return nil, fmt.Errorf("missing preset coverage: %s", platform)
		}
	}
	return presets, nil
}
