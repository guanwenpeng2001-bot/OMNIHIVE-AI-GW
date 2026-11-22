package service

import (
	"sort"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

// nativeModelCreatedAt accepts only explicit provider creation facts. Discovery,
// verification, account creation and registry load times are not model creation.
func nativeModelCreatedAt(entry upstreamModelCapabilityEntry) string {
	if entry.CreatedAt != "" {
		if created, err := time.Parse(time.RFC3339Nano, entry.CreatedAt); err == nil {
			return created.UTC().Format(time.RFC3339Nano)
		}
	}
	if entry.Created != nil && *entry.Created > 0 && *entry.Created <= 253402300799 {
		return time.Unix(*entry.Created, 0).UTC().Format(time.RFC3339)
	}
	return ""
}

// visibleModelMetadata fills identity facts from the already frozen approved
// registry without authorizing a candidate or doing an upstream read. Native
// facts persisted in the account specification take precedence.
func visibleModelMetadata(registry *ModelRegistrySnapshot, profile, model string, metadata UpstreamModelMetadata) UpstreamModelMetadata {
	metadata = cloneUpstreamModelMetadata(metadata)
	if registry != nil && profile != "" {
		approved := lookupCapabilityMetadata(registry.CapabilitySnapshot(), profile, model).Metadata
		for _, name := range []string{"CreatedAt", "ModelVersion"} {
			if mergeMetadataField(&metadata, &approved, name) {
				if metadata.FieldEvidence == nil {
					metadata.FieldEvidence = make(map[string]ModelCapabilityEvidence)
				}
				field := "created_at"
				if name == "ModelVersion" {
					field = "model_version"
				}
				metadata.FieldEvidence[field] = approved.FieldEvidence[field]
			}
		}
	}
	metadata.CreatedAtStatus = "unavailable"
	if metadata.CreatedAt != "" {
		if created, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt); err == nil {
			metadata.CreatedAt = created.UTC().Format(time.RFC3339Nano)
			metadata.CreatedAtStatus = "available"
		} else {
			metadata.CreatedAt = ""
			delete(metadata.FieldEvidence, "created_at")
		}
	}
	metadata.ModelVersionStatus = "unavailable"
	if metadata.ModelVersion != "" {
		metadata.ModelVersionStatus = "available"
	}
	return metadata
}

func AuthoritativeCatalogModelField(metadata UpstreamModelMetadata, field string) bool {
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

func catalogRegistryMaturity(lifecycle string) string {
	switch lifecycle {
	case "active":
		return "stable"
	case "preview":
		return "experimental"
	case "deprecated", "retired":
		return "deprecated"
	default:
		return "unknown"
	}
}

func (s *ModelRegistrySnapshot) CatalogFacts(now time.Time) []catalog.RegistryModel {
	out := []catalog.RegistryModel{}
	for _, preset := range s.presets {
		for _, model := range preset.Models {
			observed, _ := time.Parse(time.RFC3339Nano, model.VerifiedAt)
			if observed.IsZero() {
				observed, _ = time.Parse(time.RFC3339Nano, preset.CheckedAt)
			}
			evidence := catalog.Observation(catalog.SourcePreset, catalogRegistryMaturity(model.Lifecycle), observed, time.Time{}, now)
			metadata := lookupCapabilityMetadata(s.capabilities, preset.ProviderProfile, model.ID).Metadata
			facts := catalog.Facts{
				Context: knownCatalogLimit(model.ContextWindow), ContextKind: model.ContextLimitKind,
				Output: knownCatalogLimit(model.MaxOutputTokens), EmbeddingDimensions: knownCatalogLimit(model.EmbeddingDimensions),
				Tools: metadata.SupportsFunctionCalling, Vision: metadata.SupportsVision, Reasoning: metadata.Reasoning,
				Cache: metadata.SupportsPromptCaching, PDF: metadata.SupportsPDF,
				Modalities: catalogModalities(model.InputModalities, model.OutputModalities),
			}
			if model.MaxInputTokens > 0 {
				facts.Context, facts.ContextKind = &model.MaxInputTokens, "input"
			}
			taskModalities, taskActions := catalog.TaskPresentation(model.TaskType)
			facts.Modalities = append(facts.Modalities, taskModalities...)
			capabilities := []catalog.Capability{}
			for _, method := range model.Methods {
				action, protocol, bound := catalog.GatewayAction(method)
				if bound == "" || domain.PlatformAction(domain.ActionIdentity{Platform: preset.Platform, HostedAccountType: domain.HostedAccountType(preset.Platform)}, bound).Status != domain.ActionImplemented {
					continue
				}
				capabilities = append(capabilities, catalog.Capability{Action: action, Protocol: protocol, Mode: "unknown",
					Tools: "unknown", Stream: "unknown", Evidence: evidence, Limitations: []string{}})
			}
			projected := catalog.ModelFacts{Facts: facts, Evidence: evidence}
			catalog.AttachProvenance(&projected, s.Version(), "approved", "verified_registry_snapshot")
			// Limits and methods above come from the preset. These optional
			// flags came from the capability lookup and retain that field origin.
			for field, metadataField := range map[string]string{"tools": "supports_function_calling", "vision": "supports_vision", "reasoning": "reasoning", "prompt_caching": "supports_prompt_caching", "pdf": "supports_pdf"} {
				provenance, present := projected.Provenance[field]
				if !present {
					continue
				}
				provenance.SourceKind = catalog.SourceBuiltin
				if metadata.FieldEvidence[metadataField].Source == "platform-preset" {
					provenance.SourceKind = catalog.SourcePreset
				}
				provenance.SelectedReason = "verified_capability_registry"
				projected.Provenance[field] = provenance
			}
			out = append(out, catalog.RegistryModel{Provider: preset.Platform, Model: model.ID, Canonical: model.CanonicalModelID, DisplayName: model.DisplayName, Facts: projected, Capabilities: capabilities, Actions: taskActions})
		}
	}
	// Capability-only exact records also belong to the approved platform
	// inventory. Presets retain precedence, including explicit unknown facts.
	seen := map[string]bool{}
	profiles := map[string]string{}
	for _, preset := range s.presets {
		platform, exists := profiles[preset.ProviderProfile]
		if !exists {
			profiles[preset.ProviderProfile] = preset.Platform
		} else if platform != preset.Platform {
			// Labels such as "native" are reused across platforms. They
			// cannot override the record's concrete provider identity.
			profiles[preset.ProviderProfile] = ""
		}
	}
	for _, model := range out {
		seen[catalog.StableID(model.Provider, model.Model)] = true
	}
	if s.capabilities != nil {
		keys := make([]string, 0, len(s.capabilities.records))
		for key := range s.capabilities.records {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			record := cloneCapabilityRecord(s.capabilities.records[key])
			platform := profiles[record.Profile]
			if platform == "" && domain.IsConcretePlatform(record.Provider) {
				platform = record.Provider
			}
			id := catalog.StableID(platform, record.ModelID)
			if platform == "" || record.ModelID == "" || seen[id] {
				continue
			}
			seen[id] = true
			observed, _ := time.Parse(time.RFC3339Nano, record.VerifiedAt)
			modalities, actions := catalog.TaskPresentation(record.Mode)
			facts := catalog.Facts{Context: knownCatalogLimit(record.ContextWindow), ContextKind: record.ContextLimitKind,
				Output: record.MaxOutputTokens, EmbeddingDimensions: record.EmbeddingDimensions,
				Tools: record.SupportsFunctionCalling, Vision: record.SupportsVision, Reasoning: record.SupportsReasoning,
				Cache: record.SupportsPromptCaching, Modalities: append(catalogModalities(record.InputModalities, record.OutputModalities), modalities...)}
			if facts.Context == nil && record.MaxInputTokens != nil && *record.MaxInputTokens > 0 {
				facts.Context, facts.ContextKind = record.MaxInputTokens, "input"
			}
			source := catalog.SourceBuiltin
			if record.Source == "platform-preset" {
				source = catalog.SourcePreset
			}
			evidence := catalog.Observation(source, "unknown", observed, time.Time{}, now)
			capabilities := []catalog.Capability{}
			for _, method := range record.SupportedMethods {
				action, protocol, adapter := catalog.GatewayAction(method)
				if adapter != "" && domain.PlatformAction(domain.ActionIdentity{Platform: platform, HostedAccountType: domain.HostedAccountType(platform)}, adapter).Status == domain.ActionImplemented {
					capabilities = append(capabilities, catalog.Capability{Action: action, Protocol: protocol, Mode: "unknown", Tools: "unknown", Stream: "unknown", Limitations: []string{}, Evidence: evidence})
				}
			}
			out = append(out, catalog.RegistryModel{Provider: platform, Model: record.ModelID, Actions: actions, Capabilities: capabilities,
				Facts: catalog.ModelFacts{Facts: facts, Evidence: evidence}})
		}
	}
	for i := range out {
		if out[i].Facts.Provenance == nil {
			catalog.AttachProvenance(&out[i].Facts, s.Version(), "approved", "verified_registry_snapshot")
		}
		for j := range out[i].Capabilities {
			capability := &out[i].Capabilities[j]
			capability.Provenance = map[string]catalog.Provenance{"protocol": {SourceKind: capability.Source, SourceRevision: s.Version(), FieldPath: "protocol", Approval: "approved", SelectedReason: "verified_registry_snapshot"}}
		}
	}
	return out
}
