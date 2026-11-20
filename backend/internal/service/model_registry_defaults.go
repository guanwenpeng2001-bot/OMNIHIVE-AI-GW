package service

import (
	"encoding/json"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/claude"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/xai"
	"maps"
	"slices"
)

// RegistryPresetModels exposes admin candidates and display metadata only.
// It must never be substituted for a credential's discovered/eligible model set.
func RegistryPresetModels(platform string) []PlatformPresetModel {
	result := []PlatformPresetModel{}
	seen := make(map[string]bool)
	for _, preset := range NewModelRegistry().Snapshot().presets {
		if preset.Platform != platform {
			continue
		}
		for _, model := range preset.Models {
			if !seen[model.ID] {
				model.InputModalities = slices.Clone(model.InputModalities)
				model.OutputModalities = slices.Clone(model.OutputModalities)
				model.Methods = slices.Clone(model.Methods)
				model.FieldSources = maps.Clone(model.FieldSources)
				model.ReasoningControl = cloneReasoningControl(model.ReasoningControl)
				if model.TaskLimits != nil {
					limits := make(map[string]json.RawMessage, len(model.TaskLimits))
					for name, value := range model.TaskLimits {
						limits[name] = slices.Clone(value)
					}
					model.TaskLimits = limits
				}
				result = append(result, model)
				seen[model.ID] = true
			}
		}
	}
	return result
}

func ClaudePresetModels() []claude.Model {
	result := []claude.Model{}
	for _, model := range RegistryPresetModels(PlatformAnthropic) {
		result = append(result, claude.Model{ID: model.ID, Type: "model", DisplayName: model.DisplayName})
	}
	return result
}

func OpenAIPresetModels() []openai.Model {
	result := []openai.Model{}
	for _, model := range RegistryPresetModels(PlatformOpenAI) {
		result = append(result, openai.Model{ID: model.ID, Object: "model", OwnedBy: "openai", Type: "model", DisplayName: model.DisplayName, CatalogSource: "preset"})
	}
	return result
}

func GeminiPresetModels() []geminicli.Model {
	result := []geminicli.Model{}
	for _, model := range RegistryPresetModels(PlatformGemini) {
		result = append(result, geminicli.Model{ID: model.ID, Type: "model", DisplayName: model.DisplayName})
	}
	return result
}

func GrokPresetModels() []xai.Model {
	result := []xai.Model{}
	for _, model := range RegistryPresetModels(PlatformGrok) {
		result = append(result, xai.Model{ID: model.ID, Object: "model", OwnedBy: "xai", Type: "model", DisplayName: model.DisplayName})
	}
	return result
}

func AntigravityPresetModels() []antigravity.ClaudeModel {
	result := []antigravity.ClaudeModel{}
	for _, model := range RegistryPresetModels(PlatformAntigravity) {
		result = append(result, antigravity.ClaudeModel{ID: model.ID, Type: "model", DisplayName: model.DisplayName})
	}
	return result
}

func CursorPresetModels() []cursor.AvailableModel {
	protocols := make(map[string]cursor.AvailableModel)
	for _, model := range cursor.ProtocolVariantModels() {
		protocols[model.ID] = model
	}
	result := []cursor.AvailableModel{}
	for _, model := range RegistryPresetModels(PlatformCursor) {
		protocol, exists := protocols[model.ID]
		if !exists {
			protocol = cursor.AvailableModel{ID: model.ID, RunSlug: model.ID}
		}
		result = append(result, protocol)
	}
	return result
}

// GoogleOnePresetModels applies the existing retired Code Assist protocol
// restriction after reading candidates. It does not expand that credential type.
func GoogleOnePresetModels() []geminicli.Model {
	result := []geminicli.Model{}
	for _, model := range GeminiPresetModels() {
		switch model.ID {
		case geminicli.GoogleOneLegacyFlashModel, geminicli.GoogleOneFlashModel, geminicli.GoogleOneProModel:
			result = append(result, model)
		}
	}
	return result
}

func GoogleOneModelMapping() map[string]string {
	// This pre-existing credential restriction cannot be expanded or revoked by
	// a candidate-only data reload. Discovery/eligibility still applies separately.
	return map[string]string{
		geminicli.GoogleOneLegacyFlashModel: geminicli.GoogleOneLegacyFlashModel,
		geminicli.GoogleOneFlashModel:       geminicli.GoogleOneFlashModel,
		geminicli.GoogleOneProModel:         geminicli.GoogleOneProModel,
	}
}
