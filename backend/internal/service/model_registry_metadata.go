package service

import (
	"slices"
	"sort"
	"strings"
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

func registryProfile(platform, profile string) string {
	switch profile {
	case "native":
		return platform
	case "google":
		return "gemini"
	case "vertex-gemini", "vertex-anthropic":
		return "vertex_ai"
	default:
		return profile
	}
}

func (s *ModelRegistrySnapshot) CatalogFacts(now time.Time) []catalog.RegistryModel {
	out := []catalog.RegistryModel{}
	profiles := map[string]string{}
	for _, preset := range s.presets {
		if prev, seen := profiles[preset.ProviderProfile]; !seen {
			profiles[preset.ProviderProfile] = preset.Platform
		} else if prev != preset.Platform {
			profiles[preset.ProviderProfile] = ""
		}
		profile := registryProfile(preset.Platform, preset.ProviderProfile)
		for _, model := range preset.Models {
			observed, _ := time.Parse(time.RFC3339Nano, model.VerifiedAt)
			if observed.IsZero() {
				observed, _ = time.Parse(time.RFC3339Nano, preset.CheckedAt)
			}
			modalities, actions := catalog.TaskPresentation(model.TaskType)
			facts := catalog.Facts{Context: knownCatalogLimit(model.ContextWindow), ContextKind: model.ContextLimitKind,
				Output: knownCatalogLimit(model.MaxOutputTokens), EmbeddingDimensions: knownCatalogLimit(model.EmbeddingDimensions)}
			if model.InputModalities != nil || model.OutputModalities != nil || len(modalities) > 0 {
				facts.Modalities = append(catalogModalities(model.InputModalities, model.OutputModalities), modalities...)
			}
			facts.InputModalities, facts.OutputModalities = catalogDirectionalModalities(model.InputModalities, model.OutputModalities)
			if model.MaxInputTokens > 0 {
				facts.Context, facts.ContextKind = &model.MaxInputTokens, "input"
			}
			e := catalog.Observation("approved_registry", catalogRegistryMaturity(model.Lifecycle), observed, time.Time{}, now)
			origin := catalog.Provenance{SourceKind: catalog.PlatformPreset, RecordKey: preset.Platform + "/" + preset.ProviderProfile + "/" + model.ID,
				Provider: preset.Platform, Profile: profile, ArtifactVersion: s.Version(), MatchRule: "exact_public_name", Trust: "reviewed", Scope: "public"}
			methods := slices.Clone(model.Methods)
			facts.SupportedMethods = methods
			out = append(out, catalog.RegistryModel{Provider: preset.Platform, Model: model.ID, Canonical: model.CanonicalModelID,
				DisplayName: model.DisplayName, Facts: catalog.ModelFacts{Facts: facts, Evidence: e, Origin: origin},
				Methods: methods, Capabilities: catalog.MethodCapabilities(preset.Platform, methods, e), Actions: actions})
		}
	}
	records := s.reviewed
	if records == nil && s.capabilities != nil {
		records = s.capabilities.records
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		record := cloneCapabilityRecord(records[key])
		platform := record.Provider
		if !domain.IsConcretePlatform(platform) {
			platform = profiles[record.Profile]
		}
		if platform == "" || record.ModelID == "" {
			continue
		}
		observed, _ := time.Parse(time.RFC3339Nano, record.VerifiedAt)
		modalities, actions := catalog.TaskPresentation(record.Mode)
		facts := catalog.Facts{Context: knownCatalogLimit(record.ContextWindow), ContextKind: record.ContextLimitKind,
			Output: record.MaxOutputTokens, EmbeddingDimensions: record.EmbeddingDimensions,
			Tools: record.SupportsFunctionCalling, Vision: record.SupportsVision, Reasoning: record.SupportsReasoning,
			Cache: record.SupportsPromptCaching}
		if record.InputModalities != nil || record.OutputModalities != nil || len(modalities) > 0 {
			facts.Modalities = append(catalogModalities(record.InputModalities, record.OutputModalities), modalities...)
		}
		facts.InputModalities, facts.OutputModalities = catalogDirectionalModalities(record.InputModalities, record.OutputModalities)
		if facts.Context == nil && record.MaxInputTokens != nil && *record.MaxInputTokens > 0 {
			facts.Context, facts.ContextKind = record.MaxInputTokens, "input"
		}
		suppressed := []string{}
		for _, field := range record.SuppressedFields {
			switch field {
			case "input_modalities", "output_modalities":
				suppressed = append(suppressed, "modalities")
			case "max_input_tokens", "context_limit_kind":
				field = "context_window"
			}
			suppressed = append(suppressed, field)
		}
		origin := catalog.Provenance{SourceKind: catalog.BuiltinReviewed, RecordKey: platform + "/" + record.Profile + "/" + record.ModelID,
			Provider: platform, Profile: registryProfile(platform, record.Profile), ArtifactVersion: s.Version(),
			MatchRule: "exact_public_name", Trust: "reviewed", Scope: "public"}
		e := catalog.Observation("approved_registry", "unknown", observed, time.Time{}, now)
		if strings.HasPrefix(record.Source, "litellm:") {
			origin.SourceKind, origin.Trust, origin.ArtifactVersion = catalog.ExternalReference, "reference", record.SourceContentHash
			e.Source = catalog.ExternalReference
		}
		fieldOrigins := reviewedFieldOrigins(origin, record, facts, now)
		methods := record.SupportedMethods
		facts.SupportedMethods = methods
		if methods == nil && record.Mode == "chat" && !slices.Contains(suppressed, "supported_methods") {
			switch platform {
			case "openai", "deepseek", "mistral":
				methods = []string{"chat_completions"}
			case "anthropic":
				methods = []string{"messages"}
			case "gemini":
				methods = []string{"generateContent"}
			}
		}
		out = append(out, catalog.RegistryModel{Provider: platform, Model: record.ModelID, Actions: actions,
			Aliases: record.Aliases, Methods: methods, Capabilities: catalog.MethodCapabilities(platform, methods, e),
			Facts: catalog.ModelFacts{Facts: facts, Evidence: e, Origin: origin, FieldOrigins: fieldOrigins, Suppressed: suppressed}})
	}
	return out
}

func reviewedFieldOrigins(origin catalog.Provenance, record CapabilityRecord, facts catalog.Facts, now time.Time) map[string]catalog.Provenance {
	fields := map[string]catalog.Provenance{}
	for field, source := range record.FieldSources {
		p := origin
		at, _ := time.Parse(time.RFC3339Nano, source.ObservedAt)
		if at.IsZero() {
			at, _ = time.Parse("2006-01-02", source.ObservedAt)
		}
		p.State = "known"
		p.ObservedAt = catalog.Observation(p.SourceKind, "unknown", at, time.Time{}, now).ObservedAt
		if strings.HasPrefix(source.Source, "litellm:") {
			p.SourceKind, p.Trust, p.ArtifactVersion = catalog.ExternalReference, "reference", source.ContentHash
		}
		if facts.ContextKind == "input" && record.ContextWindow == 0 {
			switch field {
			case "context_window":
				continue
			case "max_input_tokens":
				field = "context_window"
			}
		}
		fields[field] = p
	}
	return fields
}

func (s *ModelRegistrySnapshot) indexCatalogMethods() {
	priority := make(map[catalog.ModelIdentity]int)
	for _, entry := range s.CatalogFacts(time.Time{}) {
		if entry.Facts.Origin.SourceKind == catalog.ExternalReference {
			continue
		}
		blocked := slices.Contains(entry.Facts.Suppressed, "supported_methods")
		if entry.Methods == nil && !blocked {
			continue
		}
		id := catalog.NewModelIdentity(entry.Provider, entry.Model)
		rank := 1
		if entry.Facts.Origin.SourceKind == catalog.BuiltinReviewed {
			rank = 2
		}
		if rank < priority[id] {
			continue
		}
		methods := slices.Clone(entry.Methods)
		if blocked {
			methods = []string{}
		}
		slices.Sort(methods)
		if rank == priority[id] && !slices.Equal(s.methods[id], methods) {
			s.methodConflicts[id] = true
			continue
		}
		if rank > priority[id] {
			delete(s.methodConflicts, id)
		}
		priority[id], s.methods[id] = rank, methods
	}
}

func (s *ModelRegistrySnapshot) ReviewedMethods(provider, model string) []string {
	id := catalog.NewModelIdentity(strings.TrimSpace(provider), model)
	if s.methodConflicts[id] {
		return []string{}
	}
	return slices.Clone(s.methods[id])
}

func (s *ModelRegistrySnapshot) DescriptiveMethods(provider, model string) []string {
	if methods := s.ReviewedMethods(provider, model); methods != nil {
		return methods
	}
	return catalog.SupplementalMethods(provider, model)
}

// LegacyCatalogCanonicalNames retains the gateway's bare-name envelope. Its
// input is the already authorized Key/protocol view.
func (s *ModelRegistrySnapshot) LegacyCatalogCanonicalNames(platform string, models []string) map[string]string {
	visible := make(map[string]string, len(models))
	for _, model := range models {
		visible[model] = model
	}
	targets := make(map[string][]string)
	owners := make(map[string]string)
	for _, preset := range s.presets {
		if platform != PlatformComposite && preset.Platform != platform {
			continue
		}
		for _, model := range preset.Models {
			if visible[model.ID] == "" {
				continue
			}
			if old, ok := owners[model.ID]; ok && old != preset.Platform {
				owners[model.ID] = ""
			} else {
				owners[model.ID] = preset.Platform
			}
			if model.CanonicalModelID != "" && visible[model.CanonicalModelID] != "" {
				targets[model.ID] = append(targets[model.ID], model.CanonicalModelID)
			}
		}
	}
	for model, names := range targets {
		target := catalog.CanonicalName(model, names)
		if owners[model] != "" && owners[model] == owners[target] {
			visible[model] = target
		}
	}
	return visible
}

func (b PlazaCatalogBinding) CatalogFieldFacts(expiry, now time.Time) []catalog.ModelFacts {
	facts := b.CatalogModelFacts(expiry, now)
	for i := range facts {
		f := &facts[i]
		f.FieldOrigins = map[string]catalog.Provenance{}
		for field, e := range b.Metadata.FieldEvidence {
			if !AuthoritativeCatalogModelField(b.Metadata, field) {
				continue
			}
			kind, trust := catalog.AccountDiscovery, "observed"
			observed, _ := time.Parse(time.RFC3339Nano, e.ObservedAt)
			expires := expiry
			if e.Source == "account-declaration" {
				kind, trust, expires = catalog.AccountDeclaration, "declared", time.Time{}
			} else if observed.IsZero() {
				observed, _ = time.Parse(time.RFC3339Nano, b.EvidenceObservedAt)
			}
			if kind == catalog.AccountDiscovery && f.Source != "upstream_discovery" ||
				kind == catalog.AccountDeclaration && f.Source != "account_declaration" {
				continue
			}
			observation := catalog.Observation(kind, "unknown", observed, expires, now)
			state := "known"
			if observation.State == "stale" || b.DiscoveryState == "invalidated" || b.DiscoveryState == "stale" && kind == catalog.AccountDiscovery {
				state = "stale"
			}
			recordKey := "obs_" + catalog.Digest(struct {
				Account       int64
				Model, Source string
			}{b.AccountID, e.MatchedModelID, kind})[:32]
			origin := catalog.Provenance{State: state, SourceKind: kind, RecordKey: recordKey,
				ArtifactVersion: e.SourceVersion, Provider: b.Identity.Platform, Profile: e.ProviderProfile,
				MatchRule: e.MatchRule, Trust: trust, Scope: "account", ObservedAt: observation.ObservedAt, ExpiresAt: observation.ExpiresAt}
			if origin.ArtifactVersion == "" {
				origin.ArtifactVersion = e.ContentHash
			}
			switch field {
			case "reasoning":
				field = "supports_reasoning"
			case "task_type":
				field = "modalities"
			case "input_modalities":
				if b.Metadata.InputModalities != nil {
					f.InputModalities = catalogModalities(b.Metadata.InputModalities)
				}
			case "output_modalities":
				if b.Metadata.OutputModalities != nil {
					f.OutputModalities = catalogModalities(b.Metadata.OutputModalities)
				}
			}
			prior, exists := f.FieldOrigins[field]
			if !exists || catalog.Digest(origin) < catalog.Digest(prior) {
				f.FieldOrigins[field] = origin
			}
		}
	}
	return facts
}

func catalogDirectionalModalities(inputs, outputs []string) (input, output []string) {
	if inputs != nil {
		input = catalogModalities(inputs)
	}
	if outputs != nil {
		output = catalogModalities(outputs)
	}
	return input, output
}
