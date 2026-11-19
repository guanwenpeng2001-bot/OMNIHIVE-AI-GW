package service

import (
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
			evidence := catalog.Observation("approved_registry", catalogRegistryMaturity(model.Lifecycle), observed, time.Time{}, now)
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
			capabilities := []catalog.Capability{}
			for _, method := range model.Methods {
				action, protocol, bound := catalog.GatewayAction(method)
				if bound == "" || domain.PlatformAction(domain.ActionIdentity{Platform: preset.Platform, HostedAccountType: domain.HostedAccountType(preset.Platform)}, bound).Status != domain.ActionImplemented {
					continue
				}
				capabilities = append(capabilities, catalog.Capability{Action: action, Protocol: protocol, Mode: "unknown",
					Tools: "unknown", Stream: "unknown", Evidence: evidence, Limitations: []string{}})
			}
			out = append(out, catalog.RegistryModel{Provider: preset.Platform, Model: model.ID, Canonical: model.CanonicalModelID, DisplayName: model.DisplayName, Facts: catalog.ModelFacts{Facts: facts, Evidence: evidence}, Capabilities: capabilities})
		}
	}
	return out
}
