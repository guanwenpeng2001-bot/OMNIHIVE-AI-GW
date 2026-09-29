package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/capabilitysources"
)

const capabilityDisplayTTL = 7 * 24 * time.Hour

var capabilityDisplaySources = [...]string{"models.dev", "openrouter", "litellm"}

type capabilityDisplaySource struct {
	capture    capabilitysources.Capture
	index      map[string]UpstreamModelMetadata
	expires    time.Time
	retryAfter time.Time
	refreshing bool
}

// CapabilityReadOnlyEnricher owns public specification observations only. It has
// no repository, credentials, discovery, authorization or pricing write path.
// Keep one instance per service, not one per request.
type CapabilityReadOnlyEnricher struct {
	provider ModelCapabilityProvider
	baseline *ModelCapabilitySnapshot
	client   *http.Client
	mu       sync.Mutex
	sources  map[string]*capabilityDisplaySource
	now      func() time.Time
}

func NewCapabilityReadOnlyEnricher(provider ModelCapabilityProvider, client *http.Client) (*CapabilityReadOnlyEnricher, error) {
	baseline, err := buildModelCapabilitySnapshot(nil)
	if err != nil {
		return nil, err
	}
	return &CapabilityReadOnlyEnricher{provider: provider, baseline: baseline, client: client, sources: make(map[string]*capabilityDisplaySource), now: time.Now}, nil
}

// Project returns D union concrete mapping targets, including mapped-only rows.
// These rows are DISPLAY scope, never a list of authorized or discovered models.
// Do not persist them into extra, sync/review summaries or a catalog CAS payload.
// A cold/expired read immediately returns existing facts; collection is detached
// and bounded by the shared collector. Disabling online leaves reviewed facts on.
func (enricher *CapabilityReadOnlyEnricher) Project(account *Account, online bool) map[string]UpstreamModelMetadata {
	result := make(map[string]UpstreamModelMetadata)
	if account == nil {
		return result
	}
	// Read explicit targets, not GetModelMapping's platform defaults/cache.
	var ids []string
	for _, target := range configuredModelMapping(account) {
		target = strings.TrimSpace(target)
		if target != "" && !strings.ContainsAny(target, "*?") {
			ids = append(ids, target)
		}
	}
	var catalog AccountModelCatalogSnapshot
	if decodeCatalogExtra(account.Extra, UpstreamModelCatalogExtraKey, &catalog) {
		// Partial/stale observations remain in the coverage denominator. They
		// must not be mistaken for a current complete discovery or grant.
		ids = append(ids, catalog.DiscoveredModels...)
	}
	ids = dedupeAndSortModelIDs(ids)
	if len(ids) == 0 {
		return result
	}
	profile := resolveCapabilityProfile(account)
	stored := account.GetUpstreamModelMetadataSnapshot()
	// An invalid/stale declaration contributes no facts; it cannot block reads.
	declarations, _ := accountDeclaredCapabilities(account)
	snapshot := enricher.baseline
	if enricher.provider != nil {
		if current := enricher.provider.CapabilitySnapshot(); current != nil {
			snapshot = current
		}
	}
	indexes := enricher.displayIndexes(online)
	for _, id := range ids {
		metadata := UpstreamModelMetadata{ID: id, ProviderProfile: profile}
		if stored != nil && catalog.IdentityRevision == account.ModelCatalogState().IdentityRevision && !account.IsShadow() {
			if native, exists := stored.Models[id]; exists && native.ProviderProfile == profile {
				metadata = cloneUpstreamModelMetadata(native)
				metadata.ID = id
				if !catalog.Complete {
					for field, evidence := range metadata.FieldEvidence {
						evidence.Stale = true
						metadata.FieldEvidence[field] = evidence
					}
				}
			}
		}
		if metadata.FieldEvidence == nil {
			metadata.FieldEvidence = make(map[string]ModelCapabilityEvidence)
		}
		metadata.FieldEvidence["provider_profile"] = ModelCapabilityEvidence{
			Source: "resolved-profile", SourceVersion: strconv.FormatInt(account.ModelCatalogState().IdentityRevision, 10),
			ProviderProfile: profile, MatchedModelID: id, MatchRule: "account_deployment", Confidence: "verified",
		}
		if declaration, exists := declarations[id]; exists {
			metadata, _ = mergeUpstreamModelMetadata(metadata, declaration)
		}
		match := lookupCapabilityMetadata(snapshot, profile, id)
		if match.Status == "matched" {
			metadata, _ = mergeUpstreamModelMetadata(metadata, match.Metadata)
		}
		if match.Status == "ambiguous" {
			metadata.Conflicts = append(metadata.Conflicts, "ambiguous_identity")
		}
		for _, index := range indexes {
			if fallback, exists := index[capabilityKey(profile, id)]; exists {
				metadata, _ = mergeUpstreamModelMetadata(metadata, fallback)
			}
		}
		metadata.ReasoningControl = normalizeReasoningControl(metadata)
		status := evaluateModelMetadataCompleteness(metadata)
		metadata.MetadataStatus, metadata.MissingFields = status.Status, status.MissingFields
		metadata.EstimatedFields, metadata.Conflicts = status.EstimatedFields, status.Conflicts
		result[id] = metadata
	}
	return result
}

func (enricher *CapabilityReadOnlyEnricher) displayIndexes(online bool) []map[string]UpstreamModelMetadata {
	enricher.mu.Lock()
	defer enricher.mu.Unlock()
	now := enricher.now()
	indexes := make([]map[string]UpstreamModelMetadata, 0, len(capabilityDisplaySources))
	for _, source := range capabilityDisplaySources {
		entry := enricher.sources[source]
		if entry == nil {
			entry = &capabilityDisplaySource{}
			enricher.sources[source] = entry
		}
		if now.Before(entry.expires) {
			indexes = append(indexes, entry.index)
			continue
		}
		if online && !entry.refreshing && !now.Before(entry.retryAfter) {
			entry.refreshing = true
			previous := entry.capture
			go enricher.refresh(source, previous)
		}
	}
	return indexes
}

func (enricher *CapabilityReadOnlyEnricher) refresh(source string, previous capabilitysources.Capture) {
	capture, err := capabilitysources.Collect(context.Background(), enricher.client, source, &previous)
	var index map[string]UpstreamModelMetadata
	if err == nil {
		index = capabilityObservationIndex(capture.Observation)
	}
	enricher.mu.Lock()
	defer enricher.mu.Unlock()
	entry := enricher.sources[source]
	entry.refreshing = false
	if err != nil {
		// Avoid an outage turning every read into another download. Failure never
		// renews freshness, and expired network facts are not projected.
		entry.retryAfter = enricher.now().Add(time.Minute)
		return
	}
	entry.capture, entry.index = capture, index
	entry.expires = enricher.now().Add(capabilityDisplayTTL)
	entry.retryAfter = time.Time{}
}

// Convert only the common parser's capability whitelist. Registry namespaces
// remain exact: an OpenRouter ID does not prove a native OpenAI deployment fact.
func capabilityObservationIndex(observation capabilitysources.Observation) map[string]UpstreamModelMetadata {
	index := make(map[string]UpstreamModelMetadata)
	ambiguous := make(map[string]bool)
	for _, record := range observation.Records {
		fields, ok := decodeCapabilityObservation(record)
		if !ok {
			continue
		}
		profile, id := fields.Profile, fields.ModelID
		if profile == "" || id == "" || strings.ContainsRune(profile, 0) || strings.ContainsRune(id, 0) {
			continue
		}
		key := capabilityKey(profile, id)
		if _, exists := index[key]; exists {
			ambiguous[key] = true
			delete(index, key)
		}
		if ambiguous[key] {
			continue
		}
		metadata := UpstreamModelMetadata{ID: id, ProviderProfile: profile, InputModalities: fields.InputModalities, OutputModalities: fields.OutputModalities, Reasoning: fields.SupportsReasoning, SupportsVision: fields.SupportsVision, SupportsFunctionCalling: fields.SupportsFunctionCalling, SupportsPromptCaching: fields.SupportsPromptCaching}
		if fields.MaxInputTokens != nil {
			metadata.ContextWindow, metadata.ContextLimitKind = *fields.MaxInputTokens, "input"
		}
		if fields.ContextWindow > 0 {
			metadata.ContextWindow, metadata.ContextLimitKind = fields.ContextWindow, fields.ContextLimitKind
		}
		if fields.MaxOutputTokens != nil {
			metadata.MaxOutputTokens = *fields.MaxOutputTokens
		}
		if metadata.InputModalities != nil {
			metadata.InputModalitiesStatus = "complete"
		}
		switch fields.Mode {
		case "chat", "completion":
			metadata.TaskType = "text_generation"
		case "embedding", "rerank", "image_generation", "audio_generation", "video_generation", "transcription":
			metadata.TaskType = fields.Mode
		}
		metadata.ReasoningControl = normalizeReasoningControl(metadata)
		stampCapabilityEvidence(&metadata, observation.ID, observation.ContentHash, profile, id, "exact", "observed")
		for field, evidence := range metadata.FieldEvidence {
			evidence.ContentHash, evidence.ObservedAt = observation.ContentHash, observation.VerifiedAt
			metadata.FieldEvidence[field] = evidence
		}
		index[key] = metadata
	}
	return index
}

// Do not embed CapabilityRecord's custom unmarshaler: total context and input
// limits are separate observations. The fast path covers the common parser's
// whitelist; all other representations retain encoding/json's exact behavior.
type capabilityObservationRecord CapabilityRecord

type capabilityObservationFields struct {
	capabilityObservationRecord
	ContextWindow    int64  `json:"context_window"`
	ContextLimitKind string `json:"context_limit_kind"`
}

func decodeCapabilityObservation(record map[string]any) (capabilityObservationFields, bool) {
	if fields, ok := decodeCommonCapabilityObservation(record); ok {
		return fields, true
	}
	body, err := json.Marshal(record)
	if err != nil {
		return capabilityObservationFields{}, false
	}
	var fields capabilityObservationFields
	err = json.Unmarshal(body, &fields)
	return fields, err == nil
}

func decodeCommonCapabilityObservation(record map[string]any) (capabilityObservationFields, bool) {
	var fields capabilityObservationFields
	for key, value := range record {
		switch key {
		case "provider", "profile", "model_id", "mode", "context_limit_kind":
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok || !utf8.ValidString(text) {
				return fields, false
			}
			switch key {
			case "provider":
				fields.Provider = text
			case "profile":
				fields.Profile = text
			case "model_id":
				fields.ModelID = text
			case "mode":
				fields.Mode = text
			case "context_limit_kind":
				fields.ContextLimitKind = text
			}
		case "max_input_tokens", "max_output_tokens", "context_window":
			if value == nil {
				continue
			}
			// The collector emits int64; json.Number retains exact integer
			// syntax and range checks for observations decoded with UseNumber.
			var number int64
			switch value := value.(type) {
			case int64:
				number = value
			case json.Number:
				parsed, err := value.Int64()
				if err != nil || !json.Valid([]byte(value)) {
					return fields, false
				}
				number = parsed
			default:
				return fields, false
			}
			switch key {
			case "max_input_tokens":
				fields.MaxInputTokens = &number
			case "max_output_tokens":
				fields.MaxOutputTokens = &number
			case "context_window":
				fields.ContextWindow = number
			}
		case "supports_reasoning", "supports_vision", "supports_function_calling", "supports_prompt_caching":
			if value == nil {
				continue
			}
			flag, ok := value.(bool)
			if !ok {
				return fields, false
			}
			switch key {
			case "supports_reasoning":
				fields.SupportsReasoning = &flag
			case "supports_vision":
				fields.SupportsVision = &flag
			case "supports_function_calling":
				fields.SupportsFunctionCalling = &flag
			case "supports_prompt_caching":
				fields.SupportsPromptCaching = &flag
			}
		case "input_modalities", "output_modalities", "supported_parameters":
			if value == nil {
				continue
			}
			items, ok := value.([]any)
			if !ok {
				return fields, false
			}
			var texts []string
			if items != nil {
				texts = make([]string, len(items))
			}
			for index, item := range items {
				// JSON null in a string slice decodes to its zero value.
				if item == nil {
					continue
				}
				text, ok := item.(string)
				if !ok || !utf8.ValidString(text) {
					return fields, false
				}
				texts[index] = text
			}
			switch key {
			case "input_modalities":
				fields.InputModalities = texts
			case "output_modalities":
				fields.OutputModalities = texts
			}
		default:
			// This includes case-insensitive JSON field aliases and unknown
			// values which may fail marshaling; never partially accept a row.
			return fields, false
		}
	}
	return fields, true
}
