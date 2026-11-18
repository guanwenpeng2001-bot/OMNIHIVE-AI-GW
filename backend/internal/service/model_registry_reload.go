package service

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
)

func (r *ModelRegistry) build(catalog, manifest, presets []byte, previous *modelcapabilities.ReleaseIdentity) (*ModelRegistrySnapshot, error) {
	identity, err := modelcapabilities.VerifyRelease(catalog, manifest, r.trustedPin, previous)
	if err != nil {
		return nil, err
	}
	var binding struct {
		PresetsSHA256 string `json:"presets_sha256"`
	}
	if err := json.Unmarshal(manifest, &binding); err != nil {
		return nil, err
	}
	presets = []byte(strings.ReplaceAll(string(presets), "\r\n", "\n"))
	if binding.PresetsSHA256 != capabilityHash(presets) {
		return nil, fmt.Errorf("untrusted model presets")
	}
	definitions, err := parsePlatformModelPresets(presets)
	if err != nil {
		return nil, err
	}
	capabilities, err := buildModelCapabilitySnapshotFromCatalog(nil, catalog)
	if err != nil {
		return nil, err
	}
	captures, err := presetFieldCaptures(presets)
	if err != nil {
		return nil, err
	}
	for _, preset := range definitions {
		profile := preset.ProviderProfile
		if profile == "native" {
			profile = preset.Platform
		}
		// These are reviewed registry namespace spellings, not fuzzy model aliases.
		switch profile {
		case "google":
			profile = "gemini"
		case "vertex-gemini", "vertex-anthropic":
			profile = "vertex_ai"
		}
		for _, model := range preset.Models {
			key := capabilityKey(profile, model.ID)
			record := capabilities.records[key]
			if record.ModelID == "" {
				record = CapabilityRecord{Provider: preset.Platform, Profile: profile, ModelID: model.ID, Source: "platform-preset", SourceContentHash: binding.PresetsSHA256}
			}
			fallback := CapabilityRecord{CreatedAt: model.CreatedAt, ModelVersion: model.ModelVersion, Mode: model.TaskType, SupportedMethods: model.Methods, ContextWindow: model.ContextWindow, ContextLimitKind: model.ContextLimitKind, Control: model.ReasoningControl, TaskLimits: model.TaskLimits, InputModalitiesStatus: model.InputModalitiesStatus, LiteLLMCapabilityFields: LiteLLMCapabilityFields{InputModalities: model.InputModalities, OutputModalities: model.OutputModalities}, FieldSources: make(map[string]CapabilityFieldSource)}
			if model.MaxInputTokens > 0 {
				limit := model.MaxInputTokens
				fallback.MaxInputTokens = &limit
			}
			if model.EmbeddingDimensions > 0 {
				dimensions := model.EmbeddingDimensions
				fallback.EmbeddingDimensions = &dimensions
			}
			if model.MaxOutputTokens > 0 {
				limit := model.MaxOutputTokens
				fallback.MaxOutputTokens = &limit
			}
			for _, field := range []string{"created_at", "model_version", "max_input_tokens", "max_output_tokens", "input_modalities", "output_modalities", "context_window", "context_limit_kind", "control", "mode", "supported_methods", "embedding_dimensions"} {
				fallback.FieldSources[field] = presetCapabilityFieldSource(preset, model, field, binding.PresetsSHA256, captures)
			}
			for name := range model.TaskLimits {
				fallback.FieldSources["task_limits."+name] = presetCapabilityFieldSource(preset, model, "task_limits."+name, binding.PresetsSHA256, captures)
			}
			record = fillCapabilityFields(record, fallback)
			if record.SupportedMethods == nil {
				record.SupportedMethods = append([]string(nil), model.Methods...)
			}
			capabilities.records[key] = record
		}
	}
	capabilities.aliases = buildCapabilityAliasIndex(capabilities.records)
	capabilities.version = identity.ManifestSHA256
	return &ModelRegistrySnapshot{release: identity, presets: definitions, capabilities: capabilities}, nil
}

// Consume only exact confirmed field captures from the signed preset bundle.
// Model-wide review timestamps cannot establish a refreshed sibling field.
func presetFieldCaptures(body []byte) (map[string]CapabilityFieldSource, error) {
	var scopes []struct {
		Platform string `json:"platform"`
		Profile  string `json:"provider_profile"`
		Coverage struct {
			Review struct {
				Records []struct {
					ID         string `json:"exact_id"`
					Field      string `json:"method_or_field"`
					Decision   string `json:"decision"`
					URL        string `json:"evidence_url"`
					Hash       string `json:"capture_sha256"`
					ObservedAt string `json:"observed_at"`
				} `json:"records"`
			} `json:"official_review"`
		} `json:"coverage_scope"`
	}
	if err := json.Unmarshal(body, &scopes); err != nil {
		return nil, err
	}
	result := make(map[string]CapabilityFieldSource)
	for _, scope := range scopes {
		for _, record := range scope.Coverage.Review.Records {
			if record.Decision != "confirmed" || record.ID == "" || record.Field == "" || !strings.HasPrefix(record.URL, "https://") {
				continue
			}
			if _, err := hex.DecodeString(record.Hash); err != nil || len(record.Hash) != 64 {
				return nil, fmt.Errorf("invalid preset field capture hash")
			}
			if _, err := time.Parse(time.RFC3339Nano, record.ObservedAt); err != nil {
				return nil, fmt.Errorf("invalid preset field capture date: %w", err)
			}
			key := strings.Join([]string{scope.Platform, scope.Profile, record.ID, record.Field, record.URL}, "\x00")
			evidence := CapabilityFieldSource{Source: record.URL, ContentHash: record.Hash, ObservedAt: record.ObservedAt, Confidence: "verified"}
			if previous, exists := result[key]; exists && previous != evidence {
				return nil, fmt.Errorf("ambiguous preset field capture: %s/%s", record.ID, record.Field)
			}
			result[key] = evidence
		}
	}
	return result, nil
}

func presetCapabilityFieldSource(preset PlatformModelPreset, model PlatformPresetModel, field, bundleHash string, captures map[string]CapabilityFieldSource) CapabilityFieldSource {
	switch field {
	case "mode":
		field = "task_type"
	case "supported_methods":
		field = "methods"
	case "control":
		field = "reasoning_control"
	}
	url := model.FieldSources[field]
	if url == "" && strings.HasPrefix(field, "task_limits.") {
		url = model.FieldSources["task_limits"]
	}
	key := strings.Join([]string{preset.Platform, preset.ProviderProfile, model.ID, field, url}, "\x00")
	if evidence, ok := captures[key]; ok {
		return evidence
	}
	// This legacy source explicitly describes the model-level captured registry
	// observation. It remains observed, never fresh official verification.
	if url == "https://models.dev/api.json" {
		return CapabilityFieldSource{Source: url, ContentHash: model.SourceContentHash, ObservedAt: model.VerifiedAt, Confidence: "observed"}
	}
	if url != "" {
		return CapabilityFieldSource{Source: url, Confidence: "observed"}
	}
	return CapabilityFieldSource{Source: "platform-preset", ContentHash: bundleHash, Confidence: "observed"}
}

// ReloadApprovedLocalPackage accepts only a local directory containing the
// approved catalog, manifest and presets. Its pin is never read from that directory.
// Each instance must receive these same bytes; a notification cannot copy them.
// Changing content requires a new reviewed generation and independently deployed
// compiled trust pin. Rollback never disables the anti-downgrade check.
func (r *ModelRegistry) ReloadApprovedLocalPackage(directory string) (*ModelRegistrySnapshot, error) {
	if !filepath.IsAbs(directory) || strings.Contains(directory, "://") || strings.HasPrefix(directory, `\\`) {
		return nil, fmt.Errorf("an absolute local package directory is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	bodies := make([][]byte, 3)
	for index, name := range []string{"catalog.json", "release-manifest.json", "platform_model_presets.json"} {
		file, err := os.Open(filepath.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("read approved package: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(file, (32<<20)+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(body) > 32<<20 {
			return nil, fmt.Errorf("approved package file too large")
		}
		bodies[index] = body
	}
	var previous *modelcapabilities.ReleaseIdentity
	if current := r.Snapshot(); current != nil {
		previous = &current.release
	}
	candidate, err := r.build(bodies[0], bodies[1], bodies[2], previous)
	if err != nil {
		return nil, err
	}
	r.current.Store(candidate)
	return candidate, nil
}
