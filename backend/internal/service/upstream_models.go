package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
)

const (
	upstreamModelsBodyLimit             int64 = 8 << 20
	modelsDevRegistryURL                      = "https://models.dev/api.json"
	modelsDevRegistryTTL                      = 6 * time.Hour
	UpstreamModelMetadataExtraKey             = "upstream_model_metadata"
	UpstreamModelMetadataIncompleteCode       = "upstream_model_metadata_incomplete"
	UpstreamModelMetadataPartialCode          = "upstream_model_metadata_partial"
)

type UpstreamModelMetadata struct {
	SupportsPDF             *bool                              `json:"supports_pdf,omitempty"`
	RejectedCandidates      []ModelCapabilityRejection         `json:"rejected_candidates,omitempty"`
	SupportsVision          *bool                              `json:"supports_vision,omitempty"`
	ProviderProfile         string                             `json:"provider_profile,omitempty"`
	SupportsFunctionCalling *bool                              `json:"supports_function_calling,omitempty"`
	SupportsPromptCaching   *bool                              `json:"supports_prompt_caching,omitempty"`
	ReasoningControl        *ReasoningControl                  `json:"reasoning_control,omitempty"`
	ContextLimitKind        string                             `json:"context_limit_kind,omitempty"`
	InputModalitiesStatus   string                             `json:"input_modalities_status,omitempty"`
	EndpointStatus          string                             `json:"endpoint_status,omitempty"`
	EmbeddingDimensions     int64                              `json:"embedding_dimensions,omitempty"`
	TaskLimits              map[string]json.RawMessage         `json:"task_limits,omitempty"`
	FieldEvidence           map[string]ModelCapabilityEvidence `json:"field_evidence,omitempty"`
	MetadataStatus          string                             `json:"metadata_status,omitempty"`
	MissingFields           []string                           `json:"missing_fields,omitempty"`
	EstimatedFields         []string                           `json:"estimated_fields,omitempty"`
	Conflicts               []string                           `json:"conflicts,omitempty"`
	ContextHint             string                             `json:"context_hint,omitempty"`
	FamilyHint              string                             `json:"family_hint,omitempty"`

	Category         string   `json:"category,omitempty"`
	TaskType         string   `json:"task_type,omitempty"`
	SupportedMethods []string `json:"supported_methods,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`

	ID                       string                     `json:"id"`
	DisplayName              string                     `json:"display_name,omitempty"`
	Description              string                     `json:"description,omitempty"`
	Reasoning                *bool                      `json:"reasoning,omitempty"`
	DefaultReasoningLevel    string                     `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels []string                   `json:"supported_reasoning_levels,omitempty"`
	InputModalities          []string                   `json:"input_modalities,omitempty"`
	ContextWindow            int64                      `json:"context_window,omitempty"`
	MaxContextWindow         int64                      `json:"max_context_window,omitempty"`
	MaxOutputTokens          int64                      `json:"max_output_tokens,omitempty"`
	CodexToolCapabilities    map[string]json.RawMessage `json:"codex_tool_capabilities,omitempty"`
}

type UpstreamModelMetadataSnapshot struct {
	Source   string                           `json:"source"`
	SyncedAt string                           `json:"synced_at"`
	Models   map[string]UpstreamModelMetadata `json:"models"`
}

type UpstreamModelCatalog struct {
	CandidateModels   []string              `json:"candidate_models"`
	DiscoveryComplete bool                  `json:"discovery_complete"`
	CapabilityStatus  string                `json:"capability_status"`
	Review            *AccountCatalogReview `json:"review,omitempty"`
	SchemaVersion     int                   `json:"schema_version"`
	EnrichmentVersion string                `json:"enrichment_version"`
	SourceGeneration  int64                 `json:"source_generation"`
	SourceVersions    map[string]string     `json:"source_versions"`
	AccountModelCatalogBaseline
	Applied          bool                    `json:"applied"`
	DiscoveryScope   string                  `json:"discovery_scope"`
	ProviderProfile  string                  `json:"provider_profile"`
	Complete         bool                    `json:"complete"`
	ObservedAt       string                  `json:"observed_at"`
	DiscoveredModels []string                `json:"discovered_models"`
	Diff             AccountModelCatalogDiff `json:"diff"`
	ExcludedModels   []ExcludedCatalogModel  `json:"excluded_models"`
	InvalidAliases   []string                `json:"invalid_aliases"`
	EligibleModels   []AccountCatalogModel   `json:"eligible_models"`
	snapshot         *AccountModelCatalogSnapshot

	CatalogSource   string                           `json:"catalog_source"`
	MetadataSources []string                         `json:"metadata_sources,omitempty"`
	Models          []string                         `json:"models"`
	Metadata        map[string]UpstreamModelMetadata `json:"metadata,omitempty"`
	Warnings        []UpstreamModelSyncWarning       `json:"warnings,omitempty"`
}

type UpstreamModelSyncWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type modelsDevProvider struct {
	Version string                    `json:"-"`
	Stale   bool                      `json:"-"`
	ID      string                    `json:"id"`
	Name    string                    `json:"name"`
	API     string                    `json:"api"`
	Models  map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ToolCall         *bool                      `json:"tool_call"`
	ReasoningControl *ReasoningControl          `json:"reasoning_control"`
	TaskType         string                     `json:"task_type"`
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Description      string                     `json:"description"`
	Reasoning        *bool                      `json:"reasoning"`
	ReasoningOptions []modelsDevReasoningOption `json:"reasoning_options"`
	Modalities       modelsDevModalities        `json:"modalities"`
	Limit            modelsDevLimit             `json:"limit"`
}

type modelsDevReasoningOption struct {
	Type    string          `json:"type"`
	Min     *int64          `json:"min"`
	Max     *int64          `json:"max"`
	Default json.RawMessage `json:"default"`
	Values  []any           `json:"values"`
}

type modelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelsDevLimit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

func (a *Account) SetUpstreamModelMetadataSnapshot(snapshot UpstreamModelMetadataSnapshot) {
	if a == nil {
		return
	}
	if a.Extra == nil {
		a.Extra = make(map[string]any)
	}
	a.Extra[UpstreamModelMetadataExtraKey] = snapshot
}

func (a *Account) GetUpstreamModelMetadataSnapshot() *UpstreamModelMetadataSnapshot {
	if a == nil || a.Extra == nil {
		return nil
	}
	raw, ok := a.Extra[UpstreamModelMetadataExtraKey]
	if !ok || raw == nil {
		return nil
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snapshot UpstreamModelMetadataSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil || len(snapshot.Models) == 0 {
		return nil
	}
	return &snapshot
}

func (a *Account) GetUpstreamModelMetadata(modelID string) (UpstreamModelMetadata, bool) {
	snapshot := a.GetUpstreamModelMetadataSnapshot()
	if snapshot == nil {
		return UpstreamModelMetadata{}, false
	}
	metadata, ok := snapshot.Models[strings.TrimSpace(modelID)]
	return metadata, ok
}

// UpstreamModelSyncErrorKind classifies model sync failures for safe HTTP mapping.
type UpstreamModelSyncErrorKind string

const (
	// UpstreamModelSyncErrorConfiguration means the account or server configuration cannot perform the sync.
	UpstreamModelSyncErrorConfiguration UpstreamModelSyncErrorKind = "configuration"
	UpstreamModelSyncErrorCredentials   UpstreamModelSyncErrorKind = "credentials"
	// UpstreamModelSyncErrorUnsupported means the account format is intentionally unsupported for live model sync.
	UpstreamModelSyncErrorUnsupported UpstreamModelSyncErrorKind = "unsupported"
	// UpstreamModelSyncErrorUpstream means the configured upstream failed or returned an unusable response.
	UpstreamModelSyncErrorUpstream UpstreamModelSyncErrorKind = "upstream"
	// UpstreamModelSyncErrorInternal means local persistence or service state failed after a valid upstream response.
	UpstreamModelSyncErrorInternal UpstreamModelSyncErrorKind = "internal"
)

// UpstreamModelSyncError keeps internal failure details wrapped while exposing a safe client message.
type UpstreamModelSyncError struct {
	PartialCatalog *UpstreamModelCatalog
	Kind           UpstreamModelSyncErrorKind
	Message        string
	StatusCode     int
	Err            error
}

func (e *UpstreamModelSyncError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *UpstreamModelSyncError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SafeMessage returns the sanitized message that can be sent to API clients.
func (e *UpstreamModelSyncError) SafeMessage() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "Failed to sync upstream models"
	}
	return e.Message
}

func newUpstreamModelSyncConfigError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorConfiguration, Message: message, Err: err}
}

func newUpstreamModelSyncUnsupportedError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUnsupported, Message: message, Err: err}
}

func newUpstreamModelSyncUpstreamError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUpstream, Message: message, Err: err}
}

func newUpstreamModelSyncInternalError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorInternal, Message: message, Err: err}
}

// FetchUpstreamSupportedModels retains the legacy live-ID discovery contract.
// Reviewed administration uses separate catalog projection and compare-and-swap.
func (s *AccountTestService) FetchUpstreamSupportedModels(ctx context.Context, account *Account) ([]string, error) {
	models, _, err := s.fetchUpstreamModelList(ctx, account)
	return models, err
}

// SyncUpstreamModelCatalog discovers and projects evidence without persisting account state.
func (s *AccountTestService) SyncUpstreamModelCatalog(ctx context.Context, account *Account) (*UpstreamModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	catalog, err := s.DiscoverModelCapabilities(context.WithValue(ctx, catalogDiscoveryKey{}, true), account, s.modelCapabilityProvider)
	if err != nil {
		var partial *UpstreamModelSyncError
		if errors.As(err, &partial) && partial.PartialCatalog != nil {
			catalog = partial.PartialCatalog
			catalog.AccountModelCatalogBaseline = account.ModelCatalogBaseline()
			catalog.CandidateModels = dedupeAndSortModelIDs(append(catalog.CandidateModels, catalog.DiscoveredModels...))
			catalog.DiscoveryComplete = false
			catalog.Complete = false
			catalog.CapabilityStatus = "incomplete"
			catalog.EligibleModels = []AccountCatalogModel{}
		}
		return nil, err
	}
	catalog.snapshot = projectAccountModelCatalog(account, catalog)
	return catalog, nil
}

// DiscoverModelCapabilities enriches native discoveries without persisting or authorizing models.
func (s *AccountTestService) DiscoverModelCapabilities(ctx context.Context, account *Account, provider ModelCapabilityProvider) (*UpstreamModelCatalog, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required for capability discovery", nil)
	}
	// Preview validation must not mutate the saved account's credentials.
	previewAccount := *account
	previewAccount.Credentials = make(map[string]any, len(account.Credentials))
	for name, value := range account.Credentials {
		previewAccount.Credentials[name] = value
	}
	account = &previewAccount
	if err := NormalizeHeaderOverrideCredentials(account.Credentials); err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid header overrides", err)
	}
	declarations, err := accountDeclaredCapabilities(account)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid model capability declaration", err)
	}
	models, body, err := s.fetchUpstreamModelList(ctx, account)
	if err != nil {
		return nil, err
	}
	catalog := &UpstreamModelCatalog{Models: models, CatalogSource: "live", Metadata: make(map[string]UpstreamModelMetadata)}
	if len(body) > 0 {
		_, directMetadata, parseErr := extractUpstreamModelCatalog(body, account != nil && account.IsGrok(), account.Platform)
		if parseErr == nil {
			catalog.Metadata = directMetadata
			if len(directMetadata) > 0 {
				catalog.MetadataSources = append(catalog.MetadataSources, "upstream")
			}
		}
	}

	// Capability enrichment also covers concrete model_mapping targets. Admins may
	// whitelist models that the live /models list omitted; those still need registry
	// metadata so Codex catalogs can advertise reasoning and modalities.
	enrichIDs := dedupeAndSortModelIDs(append(append([]string{}, models...), configuredUpstreamModelsForCapabilitySync(account)...))
	// Concrete mapping targets remain in the capability denominator even for an empty native list.
	capabilityIDs := enrichIDs
	profile := resolveCapabilityProfile(account)
	nativeVersion := capabilityHash(body)
	for _, id := range enrichIDs {
		m := catalog.Metadata[id]
		m.ID = id
		if m.DisplayName == "" {
			m.DisplayName = id
			if m.FieldEvidence == nil {
				m.FieldEvidence = make(map[string]ModelCapabilityEvidence)
			}
			m.FieldEvidence["display_name"] = ModelCapabilityEvidence{Source: "derived", SourceVersion: "id-label-v1", ProviderProfile: profile, MatchedModelID: id, MatchRule: "display_id", Confidence: "derived"}
		}
		m.ProviderProfile = profile
		m.ReasoningControl = normalizeReasoningControl(m)
		stampCapabilityEvidence(&m, "upstream", nativeVersion, profile, id, "native_id", "verified")
		catalog.Metadata[id] = m
	}
	// Account deployment facts are the first Tier 2 source, below native facts
	// and above public catalogs. A declaration never adds a discovered model.
	for _, id := range enrichIDs {
		if fallback, ok := declarations[id]; ok {
			merged, changed := mergeUpstreamModelMetadata(catalog.Metadata[id], fallback)
			catalog.Metadata[id] = merged
			if changed {
				catalog.MetadataSources = dedupeAndSortModelIDs(append(catalog.MetadataSources, "account-declaration"))
			}
		}
	}
	var snapshot *ModelCapabilitySnapshot
	if provider != nil {
		snapshot = provider.CapabilitySnapshot()
	}
	if snapshot == nil {
		snapshot, err = buildModelCapabilitySnapshot(nil)
		if err != nil {
			return nil, newUpstreamModelSyncInternalError("Failed to load capability baseline", err)
		}
	}
	catalog.SchemaVersion, catalog.EnrichmentVersion = AccountModelCatalogSchemaVersion, "capability-v2"
	catalog.SourceGeneration = snapshot.generation
	catalog.SourceVersions = map[string]string{"local_catalog": snapshot.Version(), "endpoint_rules": "endpoint-contract-v1", "heuristics": "name-rules-v1"}
	// A pin approves a concrete content version and monotonic generation; a
	// timestamp from this request must never make an old catalog look newer.
	var pin AccountCapabilityRelease
	if decodeCatalogExtra(account.Extra, ModelCapabilityReleaseExtraKey, &pin) && equalCatalogSourceVersions(pin.SourceVersions, catalog.SourceVersions) && pin.SourceGeneration > catalog.SourceGeneration {
		catalog.SourceGeneration = pin.SourceGeneration
	}
	for _, id := range enrichIDs {
		match := lookupCapabilityMetadata(snapshot, profile, id)
		if match.Status == "ambiguous" {
			m := catalog.Metadata[id]
			m.Conflicts = append(m.Conflicts, "ambiguous_identity")
			catalog.Metadata[id] = m
		}
		if match.Status != "matched" {
			continue
		}
		merged, changed := mergeUpstreamModelMetadata(catalog.Metadata[id], match.Metadata)
		catalog.Metadata[id] = merged
		if changed {
			catalog.MetadataSources = dedupeAndSortModelIDs(append(catalog.MetadataSources, "litellm"))
		}
	}

	if account != nil && account.IsCursor() {
		for id, fallback := range builtinCursorModelMetadata(enrichIDs) {
			fallback.InputModalitiesStatus = "partial"
			stampCapabilityEvidence(&fallback, "cursor-builtin", "cursor-builtin-v1", profile, id, "hosting_exact", "verified")
			merged, changed := mergeUpstreamModelMetadata(catalog.Metadata[id], fallback)
			catalog.Metadata[id] = merged
			if changed {
				catalog.MetadataSources = dedupeAndSortModelIDs(append(catalog.MetadataSources, "cursor-builtin"))
			}
		}
	}
	for _, id := range enrichIDs {
		m := catalog.Metadata[id]
		applyCapabilityEndpointFacts(account, &m)
		catalog.Metadata[id] = m
	}
	if upstreamCatalogNeedsRegistry(capabilityIDs, catalog.Metadata) {
		if registryMetadata, registryErr := s.fetchModelsDevMetadata(ctx, account, enrichIDs); len(registryMetadata) > 0 || registryErr == nil {
			if registryErr != nil {
				catalog.Warnings = append(catalog.Warnings, UpstreamModelSyncWarning{Code: "model_metadata_registry_stale", Message: registryErr.Error()})
			}
			for modelID, fallback := range registryMetadata {
				current := catalog.Metadata[modelID]
				merged, changed := mergeUpstreamModelMetadata(current, fallback)
				catalog.Metadata[modelID] = merged
				if changed {
					catalog.MetadataSources = dedupeAndSortModelIDs(append(catalog.MetadataSources, "models.dev"))
				}
			}
		} else {
			catalog.Warnings = append(catalog.Warnings, UpstreamModelSyncWarning{Code: "model_metadata_registry_unavailable", Message: registryErr.Error()})
			slog.Warn("upstream model capability metadata enrichment failed",
				"account_id", upstreamModelSyncAccountID(account),
				"platform", upstreamModelSyncPlatform(account),
				"error", registryErr,
			)
		}
	}

	for _, id := range enrichIDs {
		hints := inferModelCapabilityHints(id)
		for field, e := range hints.FieldEvidence {
			e.ProviderProfile = profile
			hints.FieldEvidence[field] = e
		}
		m, _ := mergeUpstreamModelMetadata(catalog.Metadata[id], hints)
		m.ReasoningControl = normalizeReasoningControl(m)
		applyCapabilityEndpointFacts(account, &m)
		status := evaluateModelMetadataCompleteness(m)
		m.MetadataStatus = status.Status
		m.MissingFields = status.MissingFields
		m.EstimatedFields = status.EstimatedFields
		m.Conflicts = status.Conflicts
		catalog.Metadata[id] = m
		for _, e := range m.FieldEvidence {
			if e.Source == "heuristic" {
				catalog.MetadataSources = dedupeAndSortModelIDs(append(catalog.MetadataSources, "heuristic"))
				break
			}
		}
	}
	if upstreamCatalogNeedsRegistry(capabilityIDs, catalog.Metadata) {
		catalog.Warnings = append([]UpstreamModelSyncWarning{{Code: UpstreamModelMetadataIncompleteCode, Message: "Native discovery is complete; some capability metadata is unknown."}}, catalog.Warnings...)
	}
	catalog.DiscoveredModels = dedupeAndSortModelIDs(models)
	// Only catalog projection can establish scoped completeness and eligibility.
	return catalog, nil
}

func upstreamModelSyncStatusCode(err error) int {
	var syncErr *UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		return syncErr.StatusCode
	}
	return 0
}

// Only explicit authentication failures invalidate the last successful catalog.
// A generic access denial can be scoped to model listing or billing.
func newUpstreamModelDiscoveryHTTPError(status int, body []byte) error {
	kind := UpstreamModelSyncErrorUpstream
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		kind = UpstreamModelSyncErrorUnsupported
	case http.StatusUnauthorized:
		kind = UpstreamModelSyncErrorCredentials
	case http.StatusForbidden:
		var payload struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &payload) == nil {
			for _, code := range []string{payload.Error.Code, payload.Error.Type} {
				switch code {
				case "invalid_api_key", "invalid_token", "token_expired", "token_revoked", "authentication_error":
					kind = UpstreamModelSyncErrorCredentials
				}
			}
		}
	}
	return &UpstreamModelSyncError{Kind: kind, StatusCode: status, Message: fmt.Sprintf("Upstream model list request failed with HTTP %d", status)}
}

func configuredUpstreamModelsForCapabilitySync(account *Account) []string {
	if account == nil {
		return nil
	}
	models := make([]string, 0)
	for _, mappedModel := range account.GetModelMapping() {
		mappedModel = strings.TrimSpace(mappedModel)
		if mappedModel == "" || strings.Contains(mappedModel, "*") {
			continue
		}
		models = append(models, mappedModel)
	}
	return dedupeAndSortModelIDs(models)
}

func capabilitySyncModelIDs(modelIDs []string) []string {
	filtered := make([]string, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" || isCodexDedicatedMediaModel(modelID) {
			continue
		}
		filtered = append(filtered, modelID)
	}
	return filtered
}

func upstreamModelSyncAccountID(account *Account) int64 {
	if account == nil {
		return 0
	}
	return account.ID
}

func upstreamModelSyncPlatform(account *Account) string {
	if account == nil {
		return ""
	}
	return account.Platform
}

func upstreamCatalogNeedsRegistry(models []string, metadata map[string]UpstreamModelMetadata) bool {
	for _, modelID := range models {
		modelID = strings.TrimSpace(modelID)
		model, ok := metadata[modelID]
		if !ok || evaluateModelMetadataCompleteness(model).Status != "complete" {
			return true
		}
	}
	return false
}

func upstreamModelMetadataIsUseful(metadata UpstreamModelMetadata) bool {
	return metadata.SupportsFunctionCalling != nil || metadata.SupportsPromptCaching != nil || metadata.ReasoningControl != nil || strings.TrimSpace(metadata.Category) != "" || strings.TrimSpace(metadata.TaskType) != "" ||
		strings.TrimSpace(metadata.DefaultReasoningLevel) != "" ||
		metadata.SupportedMethods != nil || metadata.OutputModalities != nil || strings.TrimSpace(metadata.DisplayName) != "" ||
		strings.TrimSpace(metadata.Description) != "" ||
		metadata.Reasoning != nil ||
		metadata.SupportedReasoningLevels != nil ||
		metadata.InputModalities != nil ||
		len(metadata.CodexToolCapabilities) > 0 ||
		metadata.ContextWindow > 0 ||
		metadata.MaxContextWindow > 0 ||
		metadata.MaxOutputTokens > 0
}

// The boolean reports accepted capability changes; rejection diagnostics alone
// do not mean the fallback supplied usable metadata.
func mergeUpstreamModelMetadata(primary, fallback UpstreamModelMetadata) (UpstreamModelMetadata, bool) {
	merged := cloneUpstreamModelMetadata(primary)
	changed := false
	for key, value := range fallback.TaskLimits {
		if _, exists := merged.TaskLimits[key]; exists {
			continue
		}
		if merged.TaskLimits == nil {
			merged.TaskLimits = make(map[string]json.RawMessage)
		}
		merged.TaskLimits[key] = slices.Clone(value)
		if merged.FieldEvidence == nil {
			merged.FieldEvidence = make(map[string]ModelCapabilityEvidence)
		}
		e, ok := fallback.FieldEvidence["task_limits."+key]
		if !ok {
			e = fallback.FieldEvidence["task_limits"]
		}
		merged.FieldEvidence["task_limits."+key] = e
		changed = true
	}
	if merged.Category == "" && fallback.Category != "" {
		merged.Category = fallback.Category
		changed = true
	}
	if merged.TaskType == "" && fallback.TaskType != "" {
		merged.TaskType = fallback.TaskType
		changed = true
	}
	if merged.OutputModalities == nil && fallback.OutputModalities != nil {
		merged.OutputModalities = slices.Clone(fallback.OutputModalities)
		changed = true
	}
	if merged.SupportedMethods == nil && fallback.SupportedMethods != nil {
		merged.SupportedMethods = slices.Clone(fallback.SupportedMethods)
		changed = true
	}
	for key, value := range fallback.CodexToolCapabilities {
		if _, exists := merged.CodexToolCapabilities[key]; exists {
			continue
		}
		if merged.CodexToolCapabilities == nil {
			merged.CodexToolCapabilities = make(map[string]json.RawMessage)
		}
		merged.CodexToolCapabilities[key] = slices.Clone(value)
		changed = true
	}
	if strings.TrimSpace(merged.ID) == "" && strings.TrimSpace(fallback.ID) != "" {
		merged.ID = strings.TrimSpace(fallback.ID)
		changed = true
	}
	if (strings.TrimSpace(merged.DisplayName) == "" || primary.FieldEvidence["display_name"].Source == "derived") && strings.TrimSpace(fallback.DisplayName) != "" {
		merged.DisplayName = strings.TrimSpace(fallback.DisplayName)
		changed = true
	}
	if strings.TrimSpace(merged.Description) == "" && strings.TrimSpace(fallback.Description) != "" {
		merged.Description = strings.TrimSpace(fallback.Description)
		changed = true
	}
	if merged.Reasoning == nil && fallback.Reasoning != nil {
		reasoning := *fallback.Reasoning
		merged.Reasoning = &reasoning
		changed = true
	}
	control := merged.ReasoningControl
	if control == nil || control.Kind == "unknown" {
		control = fallback.ReasoningControl
	}
	allowEffort := reasoningControlAllowsEffort(control)
	allowEffortLevels := allowEffort && (control == nil || control.Kind == "unknown" || control.EffortValues == nil || reflect.DeepEqual(control.EffortValues, fallback.SupportedReasoningLevels))
	if allowEffort && (merged.Reasoning == nil || *merged.Reasoning) &&
		(control == nil || control.Kind == "unknown" || control.EffortValues == nil || slices.Contains(control.EffortValues, strings.TrimSpace(fallback.DefaultReasoningLevel))) &&
		(merged.SupportedReasoningLevels == nil || slices.Contains(merged.SupportedReasoningLevels, strings.TrimSpace(fallback.DefaultReasoningLevel))) &&
		strings.TrimSpace(merged.DefaultReasoningLevel) == "" && strings.TrimSpace(fallback.DefaultReasoningLevel) != "" {
		merged.DefaultReasoningLevel = strings.TrimSpace(fallback.DefaultReasoningLevel)
		changed = true
	}
	if allowEffortLevels && (merged.Reasoning == nil || *merged.Reasoning) && merged.SupportedReasoningLevels == nil && fallback.SupportedReasoningLevels != nil {
		merged.SupportedReasoningLevels = slices.Clone(fallback.SupportedReasoningLevels)
		changed = true
	}
	if merged.InputModalities == nil && fallback.InputModalities != nil && compatibleVisionDeclaration(primary, fallback.InputModalities) {
		merged.InputModalities = slices.Clone(fallback.InputModalities)
		changed = true
	}
	if merged.ContextWindow <= 0 && fallback.ContextWindow > 0 {
		merged.ContextWindow = fallback.ContextWindow
		// Keep the registry's context limits together. A direct upstream default
		// without an explicit maximum remains the conservative ceiling.
		if merged.MaxContextWindow <= 0 {
			merged.MaxContextWindow = fallback.MaxContextWindow
		}
		changed = true
	}
	if merged.MaxOutputTokens <= 0 && fallback.MaxOutputTokens > 0 && (merged.ContextLimitKind != "total" || merged.ContextWindow <= 0 || fallback.MaxOutputTokens <= merged.ContextWindow) {
		merged.MaxOutputTokens = fallback.MaxOutputTokens
		changed = true
	}
	for _, name := range []string{"ProviderProfile", "SupportsVision", "SupportsPDF", "SupportsFunctionCalling", "SupportsPromptCaching", "ReasoningControl", "ContextLimitKind", "InputModalitiesStatus", "EndpointStatus", "EmbeddingDimensions", "ContextHint", "FamilyHint"} {
		if (name == "SupportsVision" || name == "SupportsPDF") && merged.InputModalities != nil && merged.InputModalitiesStatus != "partial" {
			modality, flag := "image", fallback.SupportsVision
			if name == "SupportsPDF" {
				modality, flag = "pdf", fallback.SupportsPDF
			}
			if flag != nil && *flag != slices.Contains(merged.InputModalities, modality) {
				continue
			}
		}
		if name == "ReasoningControl" && merged.Reasoning != nil && !*merged.Reasoning {
			continue
		}
		if name == "ReasoningControl" && primary.SupportedReasoningLevels != nil && len(primary.SupportedReasoningLevels) == 0 && fallback.ReasoningControl != nil && fallback.ReasoningControl.Kind == "effort" {
			continue
		}
		if name == "ReasoningControl" && fallback.ReasoningControl != nil && primary.SupportedReasoningLevels != nil {
			if !reasoningControlAllowsEffort(fallback.ReasoningControl) && len(primary.SupportedReasoningLevels) > 0 || fallback.ReasoningControl.Kind == "effort" && !reflect.DeepEqual(primary.SupportedReasoningLevels, fallback.ReasoningControl.EffortValues) {
				continue
			}
		}
		if name == "ContextLimitKind" && primary.ContextWindow > 0 {
			continue
		}
		if name == "InputModalitiesStatus" && !reflect.DeepEqual(merged.InputModalities, fallback.InputModalities) {
			continue
		}
		if mergeMetadataField(&merged, &fallback, name) {
			changed = true
		}
	}
	if primary.InputModalitiesStatus == "partial" && len(fallback.InputModalities) > 0 && fallback.InputModalitiesStatus != "partial" && compatibleVisionDeclaration(primary, fallback.InputModalities) {
		merged.InputModalities = slices.Clone(fallback.InputModalities)
		merged.InputModalitiesStatus = fallback.InputModalitiesStatus
		changed = true
	}
	// Snapshot the evidence map header before provenance updates, just as the
	// old reflected value copy did (including a nil header).
	after := merged
	for f, e := range fallback.FieldEvidence {
		if f == "task_limits" || strings.HasPrefix(f, "task_limits.") {
			continue // per-key evidence must not relabel retained native limits
		}
		if _, ok := metadataJSONFieldIndexes[f]; ok && !metadataFieldEqual(&primary, &after, f) {
			if merged.FieldEvidence == nil {
				merged.FieldEvidence = make(map[string]ModelCapabilityEvidence)
			}
			merged.FieldEvidence[f] = e
		}
	}
	recordRejectedCapabilityCandidates(&merged, &fallback)
	// Primary was cloned on entry, accepted fallback references were cloned at
	// assignment, and rejection values are newly marshaled owned buffers.
	return merged, changed
}

// Clone even unchanged entries: callers must not mutate cached registry or
// native snapshots through a merged pointer, slice, map, or RawMessage.
func cloneUpstreamModelMetadata(metadata UpstreamModelMetadata) UpstreamModelMetadata {
	for _, p := range []**bool{&metadata.SupportsVision, &metadata.SupportsPDF, &metadata.SupportsFunctionCalling, &metadata.SupportsPromptCaching} {
		if *p != nil {
			v := **p
			*p = &v
		}
	}
	metadata.ReasoningControl = cloneReasoningControl(metadata.ReasoningControl)
	metadata.FieldEvidence = cloneCapabilityEvidence(metadata.FieldEvidence)
	metadata.MissingFields = slices.Clone(metadata.MissingFields)
	metadata.EstimatedFields = slices.Clone(metadata.EstimatedFields)
	metadata.Conflicts = slices.Clone(metadata.Conflicts)
	metadata.RejectedCandidates = slices.Clone(metadata.RejectedCandidates)
	for i := range metadata.RejectedCandidates {
		metadata.RejectedCandidates[i].Value = slices.Clone(metadata.RejectedCandidates[i].Value)
	}
	if metadata.TaskLimits != nil {
		c := make(map[string]json.RawMessage)
		for k, v := range metadata.TaskLimits {
			c[k] = slices.Clone(v)
		}
		metadata.TaskLimits = c
	}

	if metadata.Reasoning != nil {
		value := *metadata.Reasoning
		metadata.Reasoning = &value
	}
	metadata.SupportedMethods = slices.Clone(metadata.SupportedMethods)
	metadata.OutputModalities = slices.Clone(metadata.OutputModalities)
	metadata.InputModalities = slices.Clone(metadata.InputModalities)
	metadata.SupportedReasoningLevels = slices.Clone(metadata.SupportedReasoningLevels)
	if metadata.CodexToolCapabilities != nil {
		cloned := make(map[string]json.RawMessage, len(metadata.CodexToolCapabilities))
		for key, value := range metadata.CodexToolCapabilities {
			cloned[key] = slices.Clone(value)
		}
		metadata.CodexToolCapabilities = cloned
	}
	return metadata
}

func (s *AccountTestService) fetchModelsDevMetadata(
	ctx context.Context,
	account *Account,
	modelIDs []string,
) (map[string]UpstreamModelMetadata, error) {
	if s == nil || s.httpUpstream == nil || account == nil {
		return nil, fmt.Errorf("model metadata registry is not configured")
	}
	registry, err := s.fetchModelsDevRegistry(ctx, account)
	if err != nil && len(registry) == 0 {
		return nil, err
	}
	provider, ok := matchModelsDevProvider(registry, upstreamModelRegistryBaseURL(account))
	if !ok {
		return nil, fmt.Errorf("no model metadata provider matches account base URL")
	}

	profile := resolveCapabilityProfile(account)
	if (profile == "grok-oauth" || profile == "antigravity" || profile == "openai-codex" || profile == "cursor" || profile == "commandcode") && provider.ID != profile {
		return nil, fmt.Errorf("registry_hosting_profile_mismatch: %s", profile)
	}
	metadata := make(map[string]UpstreamModelMetadata)
	byID, ambiguous := buildModelsDevIDIndex(provider)
	for _, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		model, found := provider.Models[modelID]
		rule := "exact"
		if !found {
			if ambiguous[modelID] {
				continue
			}
			model, found = byID[modelID]
			rule = "record_id"
		}
		if !found {
			continue
		}
		entry := upstreamMetadataFromModelsDevModel(modelID, model)
		entry.ProviderProfile = resolveCapabilityProfile(account)
		matchedID := strings.TrimSpace(model.ID)
		if matchedID == "" {
			matchedID = modelID
		}
		stampCapabilityEvidence(&entry, "models.dev", provider.Version, entry.ProviderProfile, matchedID, rule, "verified")
		if provider.Stale {
			for field, e := range entry.FieldEvidence {
				e.Stale = true
				entry.FieldEvidence[field] = e
			}
		}
		if upstreamModelMetadataIsUseful(entry) {
			metadata[modelID] = entry
		}
	}
	return metadata, err
}

// In-flight work is isolated by service and network egress. Waiters may cancel
// independently; the shared request has its own finite budget.
type modelsDevFlightKey struct {
	service *AccountTestService
	proxy   string
}
type modelsDevFlight struct {
	done     chan struct{}
	registry map[string]modelsDevProvider
	err      error
}

var modelsDevFlights = struct {
	sync.Mutex
	entries map[modelsDevFlightKey]*modelsDevFlight
}{entries: make(map[modelsDevFlightKey]*modelsDevFlight)}

const modelsDevOnlineBudget = 3 * time.Second
const modelsDevStaleGrace = 72 * time.Hour

func (s *AccountTestService) fetchModelsDevRegistry(ctx context.Context, account *Account) (map[string]modelsDevProvider, error) {
	if s == nil || account == nil {
		return nil, fmt.Errorf("registry_not_configured")
	}
	if strings.EqualFold(os.Getenv("MODEL_CAPABILITY_OFFLINE"), "true") || os.Getenv("MODEL_CAPABILITY_OFFLINE") == "1" {
		return nil, fmt.Errorf("registry_offline")
	}
	s.modelMetadataRegistryMu.Lock()
	cached, at := s.modelMetadataRegistry, s.modelMetadataRegistryAt
	s.modelMetadataRegistryMu.Unlock()
	if len(cached) > 0 && time.Since(at) < modelsDevRegistryTTL {
		return cached, nil
	}
	key := modelsDevFlightKey{s, upstreamModelsProxyURL(account)}
	modelsDevFlights.Lock()
	flight, ok := modelsDevFlights.entries[key]
	if !ok {
		// A previous flight may have published between our first cache read and
		// acquiring the flight mutex.
		s.modelMetadataRegistryMu.Lock()
		latest, loaded := s.modelMetadataRegistry, s.modelMetadataRegistryAt
		s.modelMetadataRegistryMu.Unlock()
		if len(latest) > 0 && time.Since(loaded) < modelsDevRegistryTTL {
			modelsDevFlights.Unlock()
			return latest, nil
		}
		flight = &modelsDevFlight{done: make(chan struct{})}
		modelsDevFlights.entries[key] = flight
		go func() {
			requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelsDevOnlineBudget)
			defer cancel()
			registry, err := s.downloadModelsDevRegistry(requestCtx, account)
			if err == nil {
				s.modelMetadataRegistryMu.Lock()
				s.modelMetadataRegistry = registry
				s.modelMetadataRegistryAt = time.Now()
				s.modelMetadataRegistryMu.Unlock()
			} else if len(cached) > 0 && time.Since(at) <= modelsDevRegistryTTL+modelsDevStaleGrace {
				registry = make(map[string]modelsDevProvider, len(cached))
				for k, v := range cached {
					v.Stale = true
					registry[k] = v
				}
			}
			modelsDevFlights.Lock()
			flight.registry = registry
			flight.err = err
			delete(modelsDevFlights.entries, key)
			close(flight.done)
			modelsDevFlights.Unlock()
		}()
	}
	modelsDevFlights.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.registry, flight.err
	}
}

func (s *AccountTestService) downloadModelsDevRegistry(ctx context.Context, account *Account) (map[string]modelsDevProvider, error) {
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("registry_not_configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevRegistryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("registry_empty_response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("model metadata registry returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamModelsBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > upstreamModelsBodyLimit {
		return nil, fmt.Errorf("model metadata registry response exceeds %d bytes", upstreamModelsBodyLimit)
	}
	var registry map[string]modelsDevProvider
	if err := json.Unmarshal(body, &registry); err != nil {
		return nil, fmt.Errorf("parse model metadata registry: %w", err)
	}
	if len(registry) == 0 {
		return nil, fmt.Errorf("model metadata registry is empty")
	}

	version := capabilityHash(body)
	for key, provider := range registry {
		provider.Version = version
		if provider.ID == "" {
			provider.ID = key
		}
		registry[key] = provider
	}
	return registry, nil
}

func upstreamMetadataFromModelsDevModel(modelID string, model modelsDevModel) UpstreamModelMetadata {
	levels := reasoningLevelsFromModelsDevOptions(model.ReasoningOptions)
	reasoning := model.Reasoning
	if reasoning == nil && len(levels) > 0 {
		inferred := true
		reasoning = &inferred
	}
	metadata := UpstreamModelMetadata{
		ID:                       strings.TrimSpace(modelID),
		OutputModalities:         model.Modalities.Output,
		DisplayName:              strings.TrimSpace(model.Name),
		Description:              strings.TrimSpace(model.Description),
		Reasoning:                reasoning,
		SupportedReasoningLevels: levels,
		InputModalities:          slices.Clone(model.Modalities.Input),
		SupportsFunctionCalling:  model.ToolCall,
		ReasoningControl:         model.ReasoningControl,
		ContextLimitKind:         "total",
		InputModalitiesStatus:    "complete",
		ContextWindow:            model.Limit.Context,
		MaxOutputTokens:          model.Limit.Output,
	}
	metadata.ReasoningControl = reasoningControlFromOptions(metadata, model.ReasoningOptions)
	projectReasoningEffort(&metadata)
	if model.TaskType != "" {
		metadata.TaskType = model.TaskType
	}
	if metadata.TaskType == "" && len(model.Modalities.Output) == 1 && model.Modalities.Output[0] == "text" {
		metadata.TaskType = "text_generation"
	}
	return metadata
}

func reasoningLevelsFromModelsDevOptions(options []modelsDevReasoningOption) []string {
	if options == nil {
		return nil
	}
	levels := make([]string, 0)
	for _, option := range options {
		if !strings.EqualFold(strings.TrimSpace(option.Type), "effort") {
			continue
		}
		for _, value := range option.Values {
			if value == nil {
				continue
			}
			if effort, ok := value.(string); ok {
				levels = append(levels, effort)
			}
		}
	}
	return normalizeReasoningLevels(levels)
}

func upstreamModelRegistryBaseURL(account *Account) string {
	if account == nil {
		return ""
	}
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL == "" {
		switch account.Platform {
		case PlatformMistral:
			return mistralDefaultBaseURL
		case PlatformHunyuan:
			return hunyuanCompatibilityBaseURL
		case PlatformVolcengine:
			return volcengineRuntimeBaseURL
		case PlatformQianfan:
			return qianfanDefaultBaseURL
		}
	}
	switch {
	case account.IsOpenAI() || account.IsCNProvider() || account.IsOpenCodeGo() || account.IsCommandCode():
		return account.GetOpenAIFormatBaseURL()
	case account.IsCursor():
		return "https://api2.cursor.sh"
	case account.IsGrok():
		return account.GetGrokBaseURL()
	case account.IsGemini():
		return account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	case account.IsAnthropic():
		return account.GetBaseURL()
	case account.Platform == PlatformAntigravity:
		return account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	default:
		return strings.TrimSpace(account.GetCredential("base_url"))
	}
}

func matchModelsDevProvider(registry map[string]modelsDevProvider, accountBaseURL string) (modelsDevProvider, bool) {
	if provider, ok := matchModelsDevProviderByAPIURL(registry, accountBaseURL); ok {
		return provider, true
	}
	// A matching API candidate with no winner means an ambiguous tie. Do not
	// hide that ambiguity by falling through to a host default.
	normalized := normalizeModelRegistryBaseURL(accountBaseURL)
	for _, provider := range registry {
		candidate := normalizeModelRegistryBaseURL(provider.API)
		if candidate != "" && (normalized == candidate || strings.HasPrefix(normalized, candidate+"/") || strings.HasPrefix(candidate, normalized+"/")) {
			return modelsDevProvider{}, false
		}
	}
	return matchModelsDevProviderByKnownHost(registry, accountBaseURL)
}

func matchModelsDevProviderByAPIURL(registry map[string]modelsDevProvider, accountBaseURL string) (modelsDevProvider, bool) {
	accountBaseURL = normalizeModelRegistryBaseURL(accountBaseURL)
	if accountBaseURL == "" {
		return modelsDevProvider{}, false
	}
	var best modelsDevProvider
	bestScore := -1
	ambiguous := false
	for _, provider := range registry {
		providerBaseURL := normalizeModelRegistryBaseURL(provider.API)
		if providerBaseURL == "" {
			continue
		}
		if accountBaseURL != providerBaseURL &&
			!strings.HasPrefix(accountBaseURL, providerBaseURL+"/") &&
			!strings.HasPrefix(providerBaseURL, accountBaseURL+"/") {
			continue
		}
		if len(providerBaseURL) > bestScore {
			best = provider
			bestScore = len(providerBaseURL)
			ambiguous = false
		} else if len(providerBaseURL) == bestScore {
			ambiguous = true
		}
	}
	return best, bestScore >= 0 && !ambiguous
}

// matchModelsDevProviderByKnownHost covers first-party hosts whose models.dev
// entries omit the `api` field (notably the official OpenAI provider). Custom
// compatible gateways must still match by API URL so same-named models are not
// cross-attributed across vendors.
func matchModelsDevProviderByKnownHost(registry map[string]modelsDevProvider, accountBaseURL string) (modelsDevProvider, bool) {
	host := modelRegistryHostname(accountBaseURL)
	if host == "" {
		return modelsDevProvider{}, false
	}
	providerID := ""
	switch host {
	case "api.openai.com", "chatgpt.com":
		providerID = "openai"
	case "api2.cursor.sh", "api.cursor.sh", "api.cursor.com":
		providerID = "cursor"
	case "opencode.ai":
		providerID = "opencode-go"
	case "api.x.ai":
		providerID = "xai"
	case "dashscope.aliyuncs.com":
		providerID = "alibaba-cn"
	case "dashscope-intl.aliyuncs.com":
		providerID = "alibaba"
	case "api.anthropic.com":
		providerID = "anthropic"
	case "generativelanguage.googleapis.com":
		providerID = "google"
	default:
		providerID = resolveCapabilityProfile(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": accountBaseURL}})
		if providerID == "" || strings.HasPrefix(providerID, "custom:") {
			return modelsDevProvider{}, false
		}
	}
	provider, ok := registry[providerID]
	if !ok || len(provider.Models) == 0 {
		return modelsDevProvider{}, false
	}
	if strings.TrimSpace(provider.ID) == "" {
		provider.ID = providerID
	}
	return provider, true
}

func modelRegistryHostname(raw string) string {
	normalized := normalizeModelRegistryBaseURL(raw)
	if normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(parsed.Hostname())), ".")
}

func normalizeModelRegistryBaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(strings.ToLower(path), "/models") {
		path = strings.TrimRight(path[:len(path)-len("/models")], "/")
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + path
}

func (s *AccountTestService) fetchUpstreamModelList(ctx context.Context, account *Account) ([]string, []byte, error) {
	if s == nil {
		return nil, nil, newUpstreamModelSyncConfigError("Account test service is not configured", nil)
	}
	if account == nil {
		return nil, nil, newUpstreamModelSyncConfigError("Account is required", nil)
	}

	if account.Platform == PlatformBedrock {
		return s.fetchBedrockModels(ctx, account)
	}
	if account.IsCursor() {
		if account.Type != AccountTypeAPIKey && account.Type != AccountTypeOAuth && account.Type != AccountTypeUpstream {
			return nil, nil, newUpstreamModelSyncUnsupportedError("Unsupported Cursor account type", nil)
		}
		key := "api_key"
		if account.Type == AccountTypeOAuth {
			key = "access_token"
		}
		if strings.TrimSpace(account.GetCredential(key)) == "" {
			return nil, nil, newUpstreamModelSyncConfigError("Cursor credentials are required", nil)
		}
		if catalogDiscovery, _ := ctx.Value(catalogDiscoveryKey{}).(bool); catalogDiscovery && account.Type == AccountTypeOAuth {
			return s.fetchCursorOAuthCatalog(ctx, account)
		}
		if s.cursorGatewayService == nil {
			return nil, nil, newUpstreamModelSyncConfigError("Cursor service is not configured", nil)
		}
		models, err := s.cursorGatewayService.AvailableModels(ctx, account)
		if err != nil {
			if cursorAuthFailure(err) {
				return nil, nil, &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorCredentials, Message: "Upstream rejected the account credentials", Err: err}
			}
			return nil, nil, newUpstreamModelSyncUpstreamError("Failed to request Cursor model list", err)
		}
		return models, nil, nil
	}
	if account.Platform == PlatformAntigravity && account.Type != AccountTypeAPIKey {
		if account.Type != AccountTypeOAuth {
			return nil, nil, newUpstreamModelSyncUnsupportedError("Unsupported Antigravity account type", nil)
		}
		if catalogDiscovery, _ := ctx.Value(catalogDiscoveryKey{}).(bool); catalogDiscovery {
			return s.fetchAntigravityCatalog(ctx, account)
		}
		models, err := s.fetchAntigravityOAuthUpstreamModels(ctx, account)
		return models, nil, err
	}

	if account.Platform == PlatformHunyuan || account.Platform == PlatformVolcengine {
		if account.Type != AccountTypeAPIKey {
			return nil, nil, newUpstreamModelSyncUnsupportedError("Model discovery requires an API key account", nil)
		}
		if strings.TrimSpace(account.GetCredential("api_key")) == "" {
			return nil, nil, newUpstreamModelSyncConfigError("API key is required", nil)
		}
		baseURL := strings.TrimSpace(account.GetCredential("base_url"))
		if baseURL != "" {
			if _, err := s.validateUpstreamBaseURL(baseURL); err != nil {
				return nil, nil, newUpstreamModelSyncConfigError("Invalid discovery base URL", err)
			}
		}
		if upstreamBillingProbeTargetIsOfficialAPI(baseURL) {
			presets, presetErr := PlatformModelPresets()
			if presetErr != nil {
				return nil, nil, newUpstreamModelSyncInternalError("Failed to load platform candidates", presetErr)
			}
			candidates := []string{}
			for _, preset := range presets {
				if preset.Platform == account.Platform {
					for _, model := range preset.Models {
						candidates = append(candidates, model.ID)
					}
				}
			}
			candidates = dedupeAndSortModelIDs(append(candidates, configuredUpstreamModelsForCapabilitySync(account)...))
			return nil, nil, &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUnsupported, Message: "Account-scoped native listing is unavailable; same-platform candidates require availability confirmation", PartialCatalog: &UpstreamModelCatalog{
				Models: []string{}, DiscoveredModels: []string{}, CandidateModels: candidates,
				CatalogSource: "platform-candidates", Metadata: map[string]UpstreamModelMetadata{},
				Warnings: []UpstreamModelSyncWarning{{Code: "upstream_model_discovery_candidates", Message: "Platform candidates are not evidence of account availability"}},
			}}
		}
	}

	if s.httpUpstream == nil {
		return nil, nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}

	req, err := s.buildUpstreamModelsRequest(ctx, account)
	if err != nil {
		return nil, nil, err
	}

	if discovery, _ := ctx.Value(catalogDiscoveryKey{}).(bool); discovery && account.Platform == PlatformAntigravity && account.Type == AccountTypeAPIKey {
		return s.fetchAntigravityAPIKeyCatalog(req, account)
	}
	if account.Platform == PlatformMistral {
		return s.fetchMistralModels(req, account)
	}
	if account.Platform == PlatformQianfan {
		return s.fetchQianfanModels(req, account)
	}
	if account.Platform == PlatformQianwen {
		return s.fetchQianwenModelPages(req, account)
	}

	return s.fetchCompleteModelPages(req, account)
}

func (s *AccountTestService) buildUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if descriptor, exists := domain.LookupPlatform(account.Platform); !exists || !descriptor.ModelCatalog {
		return nil, newUpstreamModelSyncUnsupportedError("Platform has no model catalog", nil)
	}
	switch {
	case account.Platform == PlatformQianfan:
		return buildQianfanRequest(ctx, account, "models", nil, s.validateUpstreamBaseURL)
	case account.Platform == PlatformHunyuan:
		return buildOpenAIAPIKeyModelsRequest(ctx, account, s.validateUpstreamBaseURL)
	case account.Platform == PlatformMistral:
		return s.buildMistralModelsRequest(ctx, account)
	case account.Platform == PlatformVolcengine:
		return buildOpenAIAPIKeyModelsRequest(ctx, account, s.validateUpstreamBaseURL)
	case account.Platform == PlatformAntigravity:
		return s.buildAntigravityAPIKeyModelsRequest(ctx, account)
	case account.IsGrok():
		return s.buildGrokUpstreamModelsRequest(ctx, account)
	case account.IsOpenAI() || account.IsCNProvider() || account.IsOpenCodeGo() || account.IsCommandCode():
		// 国产 OpenAI 兼容供应商（kimi/zhipu/deepseek）与 OpenCode Go
		// 复用 OpenAI /v1/models 探测。
		return s.buildOpenAIUpstreamModelsRequest(ctx, account)
	case account.IsGemini():
		return s.buildGeminiUpstreamModelsRequest(ctx, account)
	case account.IsAnthropic():
		return s.buildAnthropicUpstreamModelsRequest(ctx, account)
	default:
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported platform for upstream model sync: %s", account.Platform), nil,
		)
	}
}

func (s *AccountTestService) buildGrokUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required", nil)
	}

	var (
		authToken         string
		normalizedBaseURL string
		isOAuth           = account.IsGrokOAuth()
	)
	switch account.Type {
	case AccountTypeAPIKey:
		authToken = strings.TrimSpace(account.GetCredential("api_key"))
		if authToken == "" {
			return nil, newUpstreamModelSyncConfigError("No Grok API key is available", nil)
		}

		baseURL := strings.TrimSpace(account.GetCredential("base_url"))
		if baseURL == "" {
			baseURL = "https://api.x.ai"
		}
		validatedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid Grok base URL", err)
		}
		normalizedBaseURL = validatedBaseURL
	case AccountTypeOAuth:
		catalogDiscovery, _ := ctx.Value(catalogDiscoveryKey{}).(bool)
		accessToken := account.GetGrokAccessToken()
		var err error
		if !catalogDiscovery {
			if s.grokTokenProvider == nil {
				return nil, newUpstreamModelSyncConfigError("Grok token provider is not configured", nil)
			}
			accessToken, err = s.grokTokenProvider.GetAccessTokenForManualTest(ctx, account)
		}
		if err != nil {
			return nil, newUpstreamModelSyncUpstreamError("Failed to get Grok access token", err)
		}
		authToken = strings.TrimSpace(accessToken)
		if authToken == "" {
			return nil, newUpstreamModelSyncConfigError("No Grok access token is available", nil)
		}

		validator, err := grokBaseURLValidator(account, s.cfg)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid Grok base URL", err)
		}
		baseURL := account.GetGrokBaseURL()
		if s.settingService != nil {
			baseURL = s.settingService.ResolveGrokBaseURL(ctx, account)
		}
		validatedBaseURL, err := validator(baseURL)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid Grok base URL", err)
		}
		normalizedBaseURL = validatedBaseURL
	default:
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Grok account type for upstream model sync: %s", account.Type), nil,
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildOpenAIModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Grok model list URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)
	if isOAuth {
		// The shared HTTP transport adds the official CLI marker/version for the
		// exact proxy host. Keep the request builder aligned with the other Grok
		// probes and only forward account identity headers to that trusted host.
		applyGrokCLIHeaders(req.Header)
		if isGrokCLIProxyTarget(req.URL.String()) {
			if userID := strings.TrimSpace(account.GetCredential("sub")); userID != "" {
				req.Header.Set("X-UserID", userID)
			}
			if email := strings.TrimSpace(account.GetCredential("email")); email != "" {
				req.Header.Set("X-Email", email)
			}
		}
	}
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func (s *AccountTestService) buildAnthropicUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.IsBedrock() || account.Type == AccountTypeServiceAccount {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Anthropic account type for upstream model sync: %s", account.Type), nil,
		)
	}

	baseURL := "https://api.anthropic.com"
	authHeaderName := ""
	authHeaderValue := ""
	apiKeyAuthToken := ""
	betaHeader := ""

	if account.IsOAuth() {
		accessToken := strings.TrimSpace(account.GetCredential("access_token"))
		catalogDiscovery, _ := ctx.Value(catalogDiscoveryKey{}).(bool)
		if accessToken == "" && s.claudeTokenProvider != nil && !catalogDiscovery {
			token, tokenErr := s.claudeTokenProvider.GetAccessToken(ctx, account)
			if tokenErr != nil {
				return nil, newUpstreamModelSyncUpstreamError("Failed to get Anthropic access token", tokenErr)
			}
			accessToken = strings.TrimSpace(token)
		}
		if accessToken == "" {
			return nil, newUpstreamModelSyncConfigError("No Anthropic access token is available", nil)
		}
		authHeaderName = "Authorization"
		authHeaderValue = "Bearer " + accessToken
		betaHeader = claude.DefaultBetaHeader
	} else if account.Type == AccountTypeAPIKey {
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, newUpstreamModelSyncConfigError("No Anthropic API key is available", nil)
		}
		baseURL = account.GetBaseURL()
		if strings.TrimSpace(baseURL) == "" {
			baseURL = "https://api.anthropic.com"
		}
		apiKeyAuthToken = apiKey
		betaHeader = claude.APIKeyBetaHeader
	} else {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Anthropic account type for upstream model sync: %s", account.Type), nil,
		)
	}

	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Anthropic base URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildV1ModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Anthropic model list URL", err)
	}
	for key, value := range claude.DefaultHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", betaHeader)
	if authHeaderName != "" {
		req.Header.Set(authHeaderName, authHeaderValue)
	} else {
		// Ollama Cloud Anthropic 兼容端点按实际 base_url 强制 Bearer，其余保持
		// extra/default 行为。
		setAnthropicAPIKeyAuthHeader(req.Header, account, apiKeyAuthToken, normalizedBaseURL)
	}
	// 账号级请求头覆写：模型列表探测与真实转发保持一致的最终头
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func (s *AccountTestService) buildAntigravityAPIKeyModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Antigravity account type for upstream model sync: %s", account.Type), nil,
		)
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No Antigravity API key is available", nil)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(account.GetCredential("base_url")), "/")
	if baseURL == "" {
		return nil, newUpstreamModelSyncConfigError("Antigravity API-key base URL is required for upstream model sync", nil)
	}
	if !strings.HasSuffix(strings.ToLower(baseURL), "/antigravity") {
		return nil, newUpstreamModelSyncUnsupportedError(
			"Antigravity API-key upstream model sync requires a compatible gateway base URL ending in /antigravity; use Antigravity OAuth for official Cloud Code upstreams",
			nil,
		)
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Antigravity base URL", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildV1ModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Antigravity model list URL", err)
	}
	for key, value := range claude.DefaultHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", claude.APIKeyBetaHeader)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}

func (s *AccountTestService) buildOpenAIUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.IsOpenAIOAuth() {
		return s.buildOpenAIOAuthUpstreamModelsRequest(ctx, account)
	}
	return buildOpenAIAPIKeyModelsRequest(ctx, account, s.validateUpstreamBaseURL)
}

// buildOpenAIAPIKeyModelsRequest is shared by admin discovery and public model
// listing. Codex content negotiation is intentionally absent from this request.
func buildOpenAIAPIKeyModelsRequest(ctx context.Context, account *Account, validateBaseURL func(string) (string, error)) (*http.Request, error) {
	if account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported OpenAI account type for upstream model sync: %s", account.Type), nil,
		)
	}
	apiKey := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No OpenAI API key is available", nil)
	}

	// 协议感知：Anthropic 协议账号的凭证 base_url 指向 /anthropic 端点，模型
	// 列表同步需使用 OpenAI 格式 base（供应商 × 模式默认）。
	baseURL := account.GetOpenAIFormatBaseURL()
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com"
	}
	normalizedBaseURL, err := validateBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI base URL", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildOpenAIModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI model list URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	// Official DashScope's compatible-mode catalog omits native model families.
	// Keep custom relay endpoints intact; never redirect their credentials.
	if account.Platform == PlatformQianwen && req.URL.Scheme == "https" &&
		(req.URL.Host == "dashscope.aliyuncs.com" || req.URL.Host == "batch.dashscope.aliyuncs.com") {
		req.URL.Host = "dashscope.aliyuncs.com"
		req.URL.Path = "/api/v1/models"
		req.URL.RawPath = ""
		req.URL.RawQuery = "page_no=1&page_size=100&service_site=asia-pacific-china"
	}
	// 账号级请求头覆写：模型列表探测与真实转发保持一致的最终头
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

// buildOpenAIOAuthUpstreamModelsRequest uses ChatGPT's Codex model manifest.
// OAuth subscriptions do not expose the public Platform API /v1/models endpoint,
// so treating them like API-key accounts makes the admin sync button fail locally.
func (s *AccountTestService) buildOpenAIOAuthUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Failed to resolve OpenAI account credentials", err)
	}
	if !credentialAccount.IsOpenAIOAuth() {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported OpenAI account type for upstream model sync: %s", credentialAccount.Type), nil,
		)
	}

	modelsURL, err := buildCodexModelsManifestURL(
		chatgptCodexModelsURL,
		false,
		CodexCanonicalClientVersion(),
	)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI Codex model list URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL.String(), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI Codex model list request", err)
	}

	if credentialAccount.IsOpenAIAgentIdentity() {
		authHeaders, authErr := buildAgentIdentityAuthenticationHeaders(
			ctx,
			s.accountRepo,
			s.agentIdentityWS,
			&s.agentIdentityTaskMu,
			credentialAccount,
		)
		if authErr != nil {
			return nil, newUpstreamModelSyncUpstreamError("Failed to build OpenAI Agent Identity authentication", authErr)
		}
		for key, values := range authHeaders {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	} else {
		accessToken := strings.TrimSpace(credentialAccount.GetOpenAIAccessToken())
		if accessToken == "" {
			return nil, newUpstreamModelSyncConfigError("No OpenAI access token is available", nil)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	identity := resolveCodexOutboundIdentity(credentialAccount.GetOpenAIUserAgent())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", identity.originator)
	req.Header.Set("User-Agent", identity.userAgent)
	req.Header.Set("Version", identity.version)
	setOpenAIChatGPTAccountHeaders(req.Header, credentialAccount)
	credentialAccount.ApplyHeaderOverrides(req.Header)
	enforceCodexIdentityHeadersWithUA(req.Header, credentialAccount.GetOpenAIUserAgent())
	return req, nil
}

func (s *AccountTestService) buildGeminiUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = geminicli.AIStudioBaseURL
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Gemini base URL", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildGeminiModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Gemini model list URL", err)
	}
	req.Header.Set("Accept", "application/json")

	switch account.Type {
	case AccountTypeAPIKey:
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, newUpstreamModelSyncConfigError("No Gemini API key is available", nil)
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case AccountTypeOAuth:
		if strings.TrimSpace(account.GetCredential("project_id")) != "" {
			return nil, newUpstreamModelSyncUnsupportedError("Gemini Code Assist model listing is not supported by this sync button", nil)
		}
		catalogDiscovery, _ := ctx.Value(catalogDiscoveryKey{}).(bool)
		accessToken := account.GetCredential("access_token")
		var tokenErr error
		if !catalogDiscovery {
			if s.geminiTokenProvider == nil {
				return nil, newUpstreamModelSyncConfigError("Gemini token provider is not configured", nil)
			}
			accessToken, tokenErr = s.geminiTokenProvider.GetAccessToken(ctx, account)
		}
		if tokenErr != nil {
			return nil, newUpstreamModelSyncUpstreamError("Failed to get Gemini access token", tokenErr)
		}
		accessToken = strings.TrimSpace(accessToken)
		if accessToken == "" {
			return nil, newUpstreamModelSyncConfigError("No Gemini access token is available", nil)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	default:
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Gemini account type for upstream model sync: %s", account.Type), nil,
		)
	}

	return req, nil
}

func (s *AccountTestService) fetchAntigravityOAuthUpstreamModels(ctx context.Context, account *Account) ([]string, error) {
	if s.antigravityGatewayService == nil || s.antigravityGatewayService.GetTokenProvider() == nil {
		return nil, newUpstreamModelSyncConfigError("Antigravity token provider is not configured", nil)
	}

	accessToken, err := s.antigravityGatewayService.GetTokenProvider().GetAccessToken(ctx, account)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to get Antigravity access token", err)
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, newUpstreamModelSyncConfigError("No Antigravity access token is available", nil)
	}

	client, err := antigravity.NewClient(upstreamModelsProxyURL(account))
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Failed to configure Antigravity client", err)
	}
	modelsResp, _, err := client.FetchAvailableModels(
		ctx,
		accessToken,
		strings.TrimSpace(account.GetCredential("project_id")),
		resolveModelsListReadLimit(s.cfg),
	)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to fetch Antigravity available models", err)
	}
	if modelsResp == nil || len(modelsResp.Models) == 0 {
		return nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
	}

	models := make([]string, 0, len(modelsResp.Models))
	for modelID := range modelsResp.Models {
		models = append(models, strings.TrimSpace(modelID))
	}
	return dedupeAndSortModelIDs(models), nil
}

// Turn the RPC's ID-keyed map into the common catalog envelope without reducing
// ModelInfo to IDs. Prefer the raw object to preserve fields added upstream but
// not yet represented by the client type (and retain null/false presence).
func normalizeAntigravityUpstreamModels(infos map[string]antigravity.ModelInfo, raw map[string]any) ([]string, []byte, error) {
	keys := make([]string, 0, len(infos))
	for key := range infos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rawModels, _ := raw["models"].(map[string]any)
	models := make([]string, 0, len(keys))
	entries := make([]map[string]json.RawMessage, 0, len(keys))
	for _, key := range keys {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		var source any = infos[key]
		if value, ok := rawModels[key].(map[string]any); ok {
			source = value
		}
		encoded, err := json.Marshal(source)
		if err != nil {
			return nil, nil, err
		}
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &entry); err != nil {
			return nil, nil, err
		}
		entry["id"], _ = json.Marshal(id)
		models = append(models, id)
		entries = append(entries, entry)
	}
	body, err := json.Marshal(map[string]any{"models": entries})
	return dedupeAndSortModelIDs(models), body, err
}

func (s *AccountTestService) doUpstreamModelsRequest(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context()))
	if s.tlsFPProfileService == nil {
		return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	}
	return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
}

func upstreamModelsProxyURL(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

func buildV1ModelsURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/v1/models") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/v1") {
		return normalized + "/models"
	}
	return normalized + "/v1/models"
}

func buildOpenAIModelsURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/models")
}

func buildGeminiModelsURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/v1beta/models") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/v1beta") {
		return normalized + "/models"
	}
	return normalized + "/v1beta/models"
}

type upstreamModelEntry struct {
	ID           string          `json:"id"`
	Slug         string          `json:"slug"`
	Model        string          `json:"model"`
	ModelID      string          `json:"modelId"`
	ModelIDSnake string          `json:"model_id"`
	Name         string          `json:"name"`
	Meta         json.RawMessage `json:"_meta"`
}

type upstreamModelEntryMetadata struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Model        string `json:"model"`
	ModelID      string `json:"modelId"`
	ModelIDSnake string `json:"model_id"`
	Name         string `json:"name"`
}

type upstreamModelCapabilityEntry struct {
	ThinkingBudget          *int64                     `json:"thinkingBudget"`
	ToolCall                *bool                      `json:"tool_call"`
	SupportsFunctionCalling *bool                      `json:"supports_function_calling,omitempty"`
	SupportsPromptCaching   *bool                      `json:"supports_prompt_caching,omitempty"`
	ReasoningControl        *ReasoningControl          `json:"reasoning_control,omitempty"`
	ContextLimitKind        string                     `json:"context_limit_kind,omitempty"`
	InputModalitiesStatus   string                     `json:"input_modalities_status,omitempty"`
	EndpointStatus          string                     `json:"endpoint_status,omitempty"`
	EmbeddingDimensions     int64                      `json:"embedding_dimensions,omitempty"`
	TaskLimits              map[string]json.RawMessage `json:"task_limits,omitempty"`

	Category                   string   `json:"category"`
	TaskType                   string   `json:"task_type"`
	SupportedMethods           []string `json:"supported_methods"`
	OutputModalities           []string `json:"output_modalities"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	InputTokenLimit            int64    `json:"inputTokenLimit"`
	OutputTokenLimit           int64    `json:"outputTokenLimit"`
	DisplayNameCamel           string   `json:"displayName"`
	SupportsThinking           *bool    `json:"supportsThinking"`
	Thinking                   *bool    `json:"thinking"`
	SupportsImages             *bool    `json:"supportsImages"`
	MaxOutputTokensCamel       int64    `json:"maxOutputTokens"`
	MaxInputTokens             int64    `json:"max_input_tokens"`
	// Anthropic's max_tokens is an output limit. Antigravity's maxTokens
	// deliberately has no projection: its context/output semantics are unverified.
	MaxTokens    int64 `json:"max_tokens"`
	Capabilities struct {
		Thinking   upstreamSupportedCapability `json:"thinking"`
		ImageInput upstreamSupportedCapability `json:"image_input"`
		PDFInput   upstreamSupportedCapability `json:"pdf_input"`
		Effort     map[string]json.RawMessage  `json:"effort"`
	} `json:"capabilities"`

	upstreamModelEntry
	DisplayName              string                     `json:"display_name"`
	Description              string                     `json:"description"`
	Reasoning                *bool                      `json:"reasoning"`
	DefaultReasoningLevel    string                     `json:"default_reasoning_level"`
	SupportedReasoningLevels []json.RawMessage          `json:"supported_reasoning_levels"`
	ReasoningOptions         []modelsDevReasoningOption `json:"reasoning_options"`
	InputModalities          []string                   `json:"input_modalities"`
	Modalities               modelsDevModalities        `json:"modalities"`
	ContextWindow            int64                      `json:"context_window"`
	ContextLength            int64                      `json:"context_length"`
	MaxContextWindow         int64                      `json:"max_context_window"`
	MaxOutputTokens          int64                      `json:"max_output_tokens"`
	Limit                    modelsDevLimit             `json:"limit"`
}

// Older native schemas use booleans, newer schemas use {"supported": bool}.
// Missing and null declarations both remain unknown, never false by default.
type upstreamSupportedCapability struct {
	Supported *bool                                  `json:"supported"`
	Types     map[string]upstreamSupportedCapability `json:"types,omitempty"`
}

func (capability *upstreamSupportedCapability) UnmarshalJSON(body []byte) error {
	if string(body) == "true" || string(body) == "false" || string(body) == "null" {
		return json.Unmarshal(body, &capability.Supported)
	}
	type plain upstreamSupportedCapability
	return json.Unmarshal(body, (*plain)(capability))
}

func extractUpstreamModelIDs(body []byte) ([]string, error) {
	return extractUpstreamModelIDsWithSelector(body, upstreamModelEntryID)
}

func extractGrokUpstreamModelIDs(body []byte) ([]string, error) {
	return extractUpstreamModelIDsWithSelector(body, grokUpstreamModelEntryID)
}

func extractUpstreamModelCatalog(body []byte, grok bool, platform ...string) ([]string, map[string]UpstreamModelMetadata, error) {
	entries, err := extractUpstreamModelRawEntries(body)
	if err != nil {
		return nil, nil, err
	}
	selectID := upstreamModelEntryID
	if grok {
		selectID = grokUpstreamModelEntryID
	}

	models := make([]string, 0, len(entries))
	metadata := make(map[string]UpstreamModelMetadata)
	for _, raw := range entries {
		// Discovery identity is independent of optional capability decoding.
		var identity upstreamModelEntry
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, nil, fmt.Errorf("invalid model identity: %w", err)
		}
		modelID := strings.TrimSpace(selectID(identity))
		if modelID == "" {
			continue
		}
		models = append(models, modelID)
		if len(platform) > 0 && platform[0] == PlatformMistral {
			var card MistralModelCard
			if err := json.Unmarshal(raw, &card); err != nil {
				metadata[modelID] = UpstreamModelMetadata{ID: modelID, Conflicts: []string{"invalid_optional_metadata"}}
				continue
			}
			metadata[modelID] = card.metadata(UpstreamModelMetadata{})
			continue
		}
		var capability upstreamModelCapabilityEntry
		if err := json.Unmarshal(raw, &capability); err != nil {
			metadata[modelID] = UpstreamModelMetadata{ID: modelID, Conflicts: []string{"invalid_optional_metadata"}}
			continue
		}
		// max_tokens has incompatible meanings across APIs. Only the Anthropic
		// model-list contract establishes that it is an output limit.
		if len(platform) == 0 || platform[0] != PlatformAnthropic {
			capability.MaxTokens = 0
		}
		entry := upstreamMetadataFromCapabilityEntry(modelID, capability)
		if len(platform) > 0 && platform[0] == PlatformQianfan {
			var model qianfanModel
			if err := json.Unmarshal(raw, &model); err == nil {
				if model.MaxCompletionsTokens != nil {
					entry.MaxOutputTokens = *model.MaxCompletionsTokens
				}
				var architecture struct {
					InputModalities  []string `json:"input_modalities"`
					OutputModalities []string `json:"output_modalities"`
				}
				if json.Unmarshal(model.Architecture, &architecture) == nil {
					entry.InputModalities = architecture.InputModalities
					entry.OutputModalities = architecture.OutputModalities
				}
			}
		}

		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err == nil {
			entry.CodexToolCapabilities = make(map[string]json.RawMessage)
			applyCodexToolCapabilities(entry.CodexToolCapabilities, fields, true)
			if len(entry.CodexToolCapabilities) == 0 {
				entry.CodexToolCapabilities = nil
			}
		}
		if upstreamModelMetadataIsUseful(entry) {
			metadata[modelID] = entry
		}
	}
	return dedupeAndSortModelIDs(models), metadata, nil
}

func extractUpstreamModelRawEntries(body []byte) ([]json.RawMessage, error) {
	var response struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &response); err == nil && (response.Data != nil || response.Models != nil) {
		entries := make([]json.RawMessage, 0, len(response.Data)+len(response.Models))
		entries = append(entries, response.Data...)
		entries = append(entries, response.Models...)
		return entries, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil || entries == nil {
		return nil, fmt.Errorf("parse upstream model catalog: %w", err)
	}
	return entries, nil
}

func upstreamMetadataFromCapabilityEntry(modelID string, entry upstreamModelCapabilityEntry) UpstreamModelMetadata {
	var levels []string
	if entry.SupportedReasoningLevels != nil {
		levels = reasoningLevelsFromRawEntries(entry.SupportedReasoningLevels)
	} else if entry.ReasoningOptions != nil {
		levels = reasoningLevelsFromModelsDevOptions(entry.ReasoningOptions)
	} else if entry.Capabilities.Effort != nil {
		// Project only explicitly supported native effort values. A thinking
		// toggle or budget alone cannot establish an effort list or default.
		var supported *bool
		_ = json.Unmarshal(entry.Capabilities.Effort["supported"], &supported)
		if supported != nil && !*supported {
			levels = []string{}
		} else {
			for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
				var capability upstreamSupportedCapability
				if json.Unmarshal(entry.Capabilities.Effort[level], &capability) == nil && capability.Supported != nil {
					if levels == nil {
						levels = []string{}
					}
					if *capability.Supported {
						levels = append(levels, level)
					}
				}
			}
		}
	}
	reasoning := entry.Reasoning
	if reasoning == nil {
		reasoning = entry.SupportsThinking
	}
	if reasoning == nil {
		reasoning = entry.Thinking
	}
	if reasoning == nil {
		reasoning = entry.Capabilities.Thinking.Supported
	}
	if reasoning == nil && len(levels) > 0 {
		inferred := len(levels) != 1 || levels[0] != "none"
		reasoning = &inferred
	}
	modalities := entry.InputModalities
	if modalities == nil {
		modalities = entry.Modalities.Input
	}
	images := entry.SupportsImages
	if images == nil {
		images = entry.Capabilities.ImageInput.Supported
	}
	if modalities == nil && (images != nil || entry.Capabilities.PDFInput.Supported != nil) {
		modalities = []string{"text"}
		if images != nil && *images {
			modalities = append(modalities, "image")
		}
		if pdf := entry.Capabilities.PDFInput.Supported; pdf != nil && *pdf {
			modalities = append(modalities, "pdf")
		}
	}
	if modalities != nil {
		modalities = slices.Clone(modalities)
	}
	contextWindow := firstPositiveUpstreamLimit(entry.ContextWindow, entry.MaxContextWindow, entry.ContextLength, entry.Limit.Context, entry.InputTokenLimit, entry.MaxInputTokens)
	maxOutputTokens := firstPositiveUpstreamLimit(entry.MaxOutputTokens, entry.MaxOutputTokensCamel, entry.Limit.Output, entry.OutputTokenLimit, entry.MaxTokens)
	defaultReasoningLevel := normalizeReasoningLevel(entry.DefaultReasoningLevel)
	displayName := strings.TrimSpace(entry.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(entry.DisplayNameCamel)
	}
	if displayName == "" && strings.TrimSpace(entry.Name) != "" && strings.TrimSpace(entry.Name) != modelID {
		displayName = strings.TrimSpace(entry.Name)
	}
	metadata := UpstreamModelMetadata{
		ID:                       modelID,
		Category:                 strings.TrimSpace(entry.Category),
		TaskType:                 strings.TrimSpace(entry.TaskType),
		SupportedMethods:         entry.SupportedMethods,
		OutputModalities:         entry.OutputModalities,
		DisplayName:              displayName,
		Description:              strings.TrimSpace(entry.Description),
		Reasoning:                reasoning,
		DefaultReasoningLevel:    defaultReasoningLevel,
		SupportedReasoningLevels: levels,
		InputModalities:          modalities,
		ContextWindow:            contextWindow,
		MaxContextWindow:         firstPositiveUpstreamLimit(entry.MaxContextWindow),
		MaxOutputTokens:          maxOutputTokens,
	}
	if metadata.SupportedMethods == nil {
		metadata.SupportedMethods = entry.SupportedGenerationMethods
	}
	if metadata.OutputModalities == nil {
		metadata.OutputModalities = entry.Modalities.Output
	}
	if metadata.TaskType == "" && len(metadata.OutputModalities) == 1 && metadata.OutputModalities[0] == "text" && !slices.Contains(metadata.InputModalities, "audio") {
		metadata.TaskType = "text_generation"
	}
	metadata.SupportsVision = images
	metadata.SupportsPDF = entry.Capabilities.PDFInput.Supported
	metadata.SupportsFunctionCalling = entry.SupportsFunctionCalling
	if metadata.SupportsFunctionCalling == nil {
		metadata.SupportsFunctionCalling = entry.ToolCall
	}
	metadata.SupportsPromptCaching = entry.SupportsPromptCaching
	metadata.ReasoningControl = entry.ReasoningControl
	if metadata.ReasoningControl == nil && reasoning != nil && *reasoning && entry.Capabilities.Thinking.Types != nil {
		c := &ReasoningControl{Kind: "unknown"}
		for _, native := range []struct{ name, kind string }{{"adaptive", "adaptive"}, {"enabled", "budget"}} {
			if supported := entry.Capabilities.Thinking.Types[native.name].Supported; supported != nil && *supported {
				if c.Kind == "unknown" {
					c.Kind = native.kind
				}
				c.SupportedKinds = append(c.SupportedKinds, native.kind)
			}
		}
		if slices.Contains(c.SupportedKinds, "adaptive") {
			yes := true
			c.Adaptive = &yes
		}
		if len(levels) > 0 {
			c.SupportedKinds = append(c.SupportedKinds, "effort")
			c.EffortValues = slices.Clone(levels)
		}
		metadata.ReasoningControl = c
	}
	if metadata.ReasoningControl == nil && entry.ThinkingBudget != nil && metadata.Reasoning != nil && *metadata.Reasoning {
		value, _ := json.Marshal(*entry.ThinkingBudget)
		metadata.ReasoningControl = &ReasoningControl{Kind: "budget", Default: value}
	}
	metadata.ReasoningControl = reasoningControlFromOptions(metadata, entry.ReasoningOptions)
	projectReasoningEffort(&metadata)
	metadata.ContextLimitKind = entry.ContextLimitKind
	if metadata.ContextLimitKind == "" && metadata.ContextWindow > 0 {
		metadata.ContextLimitKind = "total"
		if entry.ContextWindow <= 0 && entry.MaxContextWindow <= 0 && entry.ContextLength <= 0 && entry.Limit.Context <= 0 {
			metadata.ContextLimitKind = "input"
		}
	}
	metadata.InputModalitiesStatus = entry.InputModalitiesStatus
	if metadata.InputModalitiesStatus == "" && modalities != nil {
		metadata.InputModalitiesStatus = "complete"
		if entry.InputModalities == nil && entry.Modalities.Input == nil {
			metadata.InputModalitiesStatus = "partial"
		}
	}
	metadata.EndpointStatus = entry.EndpointStatus
	metadata.EmbeddingDimensions = entry.EmbeddingDimensions
	metadata.TaskLimits = entry.TaskLimits
	return cloneUpstreamModelMetadata(metadata)
}

func firstPositiveUpstreamLimit(limits ...int64) int64 {
	for _, limit := range limits {
		if limit > 0 {
			return limit
		}
	}
	return 0
}

func reasoningLevelsFromRawEntries(entries []json.RawMessage) []string {
	levels := make([]string, 0, len(entries))
	for _, raw := range entries {
		var effort string
		if err := json.Unmarshal(raw, &effort); err == nil {
			levels = append(levels, effort)
			continue
		}
		var level struct {
			Effort string `json:"effort"`
		}
		if err := json.Unmarshal(raw, &level); err == nil {
			levels = append(levels, level.Effort)
		}
	}
	return normalizeReasoningLevels(levels)
}

func normalizeReasoningLevels(levels []string) []string {
	seen := make(map[string]struct{}, len(levels))
	normalized := make([]string, 0, len(levels))
	for _, level := range levels {
		level = normalizeReasoningLevel(level)
		if level == "" {
			continue
		}
		if _, exists := seen[level]; exists {
			continue
		}
		seen[level] = struct{}{}
		normalized = append(normalized, level)
	}
	return normalized
}

func normalizeReasoningLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "off", "disabled":
		return "none"
	case "extra-high", "extra_high":
		return "xhigh"
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return level
	default:
		return ""
	}
}

func normalizeCodexInputModalities(modalities []string) []string {
	if modalities == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(modalities))
	normalized := make([]string, 0, len(modalities))
	for _, modality := range modalities {
		modality = strings.ToLower(strings.TrimSpace(modality))
		if modality != "text" && modality != "image" {
			continue
		}
		if _, exists := seen[modality]; exists {
			continue
		}
		seen[modality] = struct{}{}
		normalized = append(normalized, modality)
	}
	return normalized
}

func extractUpstreamModelIDsWithSelector(body []byte, selectID func(upstreamModelEntry) string) ([]string, error) {
	var response struct {
		Data   []upstreamModelEntry `json:"data"`
		Models []upstreamModelEntry `json:"models"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		var arrayResponse []upstreamModelEntry
		if arrayErr := json.Unmarshal(body, &arrayResponse); arrayErr != nil {
			return nil, fmt.Errorf("parse upstream model list: %w", err)
		}

		models := make([]string, 0, len(arrayResponse))
		for _, entry := range arrayResponse {
			models = append(models, selectID(entry))
		}
		return dedupeAndSortModelIDs(models), nil
	}

	models := make([]string, 0, len(response.Data)+len(response.Models))
	for _, entry := range response.Data {
		models = append(models, selectID(entry))
	}
	for _, entry := range response.Models {
		models = append(models, selectID(entry))
	}

	if len(models) == 0 {
		var arrayResponse []upstreamModelEntry
		if err := json.Unmarshal(body, &arrayResponse); err == nil {
			for _, entry := range arrayResponse {
				models = append(models, selectID(entry))
			}
		}
	}

	return dedupeAndSortModelIDs(models), nil
}

func upstreamModelEntryID(entry upstreamModelEntry) string {
	if modelID := strings.TrimSpace(entry.ID); modelID != "" {
		return modelID
	}
	if slug := strings.TrimSpace(entry.Slug); slug != "" {
		return slug
	}
	// Gemini's resource-name field uses models/<id>; explicit IDs remain exact.
	return strings.TrimPrefix(strings.TrimSpace(entry.Name), "models/")
}

func grokUpstreamModelEntryID(entry upstreamModelEntry) string {
	candidates := []string{
		entry.Model,
		entry.ModelID,
		entry.ModelIDSnake,
		entry.ID,
		entry.Slug,
	}
	if len(entry.Meta) > 0 {
		var meta upstreamModelEntryMetadata
		if err := json.Unmarshal(entry.Meta, &meta); err == nil {
			candidates = append(candidates,
				meta.Model,
				meta.ModelID,
				meta.ModelIDSnake,
				meta.ID,
				meta.Slug,
				meta.Name,
			)
		}
	}
	// `name` is a display label in the Grok catalog, so keep it as the final
	// compatibility fallback rather than preferring it over protocol model IDs.
	candidates = append(candidates, entry.Name)
	for _, candidate := range candidates {
		modelID := strings.TrimSpace(candidate)
		if modelID != "" {
			return modelID
		}
	}
	return ""
}

func dedupeAndSortModelIDs(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	sort.Strings(result)
	return result
}

// Preserve explicit empty declarations across persistence. Nil remains omitted,
// while [] continues to mean that the source explicitly supports no values.
func (m UpstreamModelMetadata) MarshalJSON() ([]byte, error) {
	type plain UpstreamModelMetadata
	return MarshalModelCapabilityLists(plain(m), map[string][]string{"supported_methods": m.SupportedMethods, "input_modalities": m.InputModalities, "output_modalities": m.OutputModalities, "supported_reasoning_levels": m.SupportedReasoningLevels})
}

// MarshalModelCapabilityLists shares the nil/empty presence contract between
// validated account declarations and persisted capability metadata.
func MarshalModelCapabilityLists(value any, lists map[string][]string) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for key, list := range lists {
		if list != nil && len(list) == 0 {
			fields[key] = json.RawMessage(`[]`)
		}
	}
	return json.Marshal(fields)
}

// ReadOnlyModelCapabilities is a display projection, separate from discovery,
// review digests, CAS and pricing. Offline disables only public network refresh.
func (service *AccountTestService) ReadOnlyModelCapabilities(account *Account) map[string]UpstreamModelMetadata {
	if service == nil || service.capabilityReadOnlyEnricher == nil {
		return map[string]UpstreamModelMetadata{}
	}
	offline := strings.EqualFold(os.Getenv("MODEL_CAPABILITY_OFFLINE"), "true") || os.Getenv("MODEL_CAPABILITY_OFFLINE") == "1"
	return service.capabilityReadOnlyEnricher.Project(account, !offline)
}

// Both paths belong to the saved compatible /antigravity gateway. Its v1beta
// route uses APIKeyAuthWithSubscriptionGoogle, which accepts x-goog-api-key.
// This does not extend OAuth/CloudCode credentials to another host or endpoint.
func (service *AccountTestService) fetchAntigravityAPIKeyCatalog(messagesRequest *http.Request, account *Account) ([]string, []byte, error) {
	geminiRequest := messagesRequest.Clone(messagesRequest.Context())
	geminiRequest.URL.Path = strings.TrimSuffix(messagesRequest.URL.Path, "/v1/models") + "/v1beta/models"
	geminiRequest.Header.Del("x-api-key")
	geminiRequest.Header.Del("anthropic-version")
	geminiRequest.Header.Del("anthropic-beta")
	geminiRequest.Header.Set("x-goog-api-key", strings.TrimSpace(account.GetCredential("api_key")))
	models := []string{}
	entries := []json.RawMessage{}
	var failed error
	successfulPaths := 0
	for _, request := range []*http.Request{messagesRequest, geminiRequest} {
		ids, body, err := service.fetchCompleteModelPages(request, account)
		if err != nil {
			failed = err
			if len(body) == 0 {
				continue
			}
		}
		rawEntries, err := extractUpstreamModelRawEntries(body)
		if err != nil {
			failed = err
			continue
		}
		successfulPaths++
		models = append(models, ids...)
		entries = append(entries, rawEntries...)
	}
	merged, err := json.Marshal(map[string]any{"data": entries})
	if err != nil {
		return nil, nil, err
	}
	models = dedupeAndSortModelIDs(models)
	if failed != nil {
		if successfulPaths == 0 {
			return nil, nil, failed
		}
		failure := &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUpstream, Message: "Antigravity dual-path catalog is incomplete; previous snapshot retained", Err: failed}
		if successfulPaths > 0 {
			_, metadata, parseErr := extractUpstreamModelCatalog(merged, false, PlatformAntigravity)
			if parseErr == nil {
				failure.PartialCatalog = &UpstreamModelCatalog{Models: []string{}, DiscoveredModels: models, Metadata: metadata, CatalogSource: "partial", Complete: false, Warnings: []UpstreamModelSyncWarning{{Code: "upstream_model_discovery_partial", Message: "A catalog path failed; partial results are preview-only and cannot be applied"}}}
			}
		}
		return nil, nil, failure
	}
	return models, merged, nil
}

// The caller checks related-field compatibility before assigning. Pointer
// fields always acquire independent ownership; scalar fields need only a copy.
func mergeMetadataField(merged, fallback *UpstreamModelMetadata, field string) bool {
	switch field {
	case "ProviderProfile":
		if merged.ProviderProfile == "" && fallback.ProviderProfile != "" {
			merged.ProviderProfile = fallback.ProviderProfile
			return true
		}
	case "SupportsVision":
		if merged.SupportsVision == nil && fallback.SupportsVision != nil {
			flag := *fallback.SupportsVision
			merged.SupportsVision = &flag
			return true
		}
	case "SupportsPDF":
		if merged.SupportsPDF == nil && fallback.SupportsPDF != nil {
			flag := *fallback.SupportsPDF
			merged.SupportsPDF = &flag
			return true
		}
	case "SupportsFunctionCalling":
		if merged.SupportsFunctionCalling == nil && fallback.SupportsFunctionCalling != nil {
			flag := *fallback.SupportsFunctionCalling
			merged.SupportsFunctionCalling = &flag
			return true
		}
	case "SupportsPromptCaching":
		if merged.SupportsPromptCaching == nil && fallback.SupportsPromptCaching != nil {
			flag := *fallback.SupportsPromptCaching
			merged.SupportsPromptCaching = &flag
			return true
		}
	case "ReasoningControl":
		if fallback.ReasoningControl != nil && (merged.ReasoningControl == nil || merged.ReasoningControl.Kind == "unknown" && fallback.ReasoningControl.Kind != "unknown") {
			merged.ReasoningControl = cloneReasoningControl(fallback.ReasoningControl)
			return true
		}
	case "ContextLimitKind":
		if merged.ContextLimitKind == "" && fallback.ContextLimitKind != "" {
			merged.ContextLimitKind = fallback.ContextLimitKind
			return true
		}
	case "InputModalitiesStatus":
		if merged.InputModalitiesStatus == "" && fallback.InputModalitiesStatus != "" {
			merged.InputModalitiesStatus = fallback.InputModalitiesStatus
			return true
		}
	case "EndpointStatus":
		if merged.EndpointStatus == "" && fallback.EndpointStatus != "" {
			merged.EndpointStatus = fallback.EndpointStatus
			return true
		}
	case "EmbeddingDimensions":
		if merged.EmbeddingDimensions == 0 && fallback.EmbeddingDimensions != 0 {
			merged.EmbeddingDimensions = fallback.EmbeddingDimensions
			return true
		}
	case "ContextHint":
		if merged.ContextHint == "" && fallback.ContextHint != "" {
			merged.ContextHint = fallback.ContextHint
			return true
		}
	case "FamilyHint":
		if merged.FamilyHint == "" && fallback.FamilyHint != "" {
			merged.FamilyHint = fallback.FamilyHint
			return true
		}
	}
	return false
}

// Invalid optional fields can hide endpoint facts used for eligibility. Keep
// discovered IDs, but never authorize a destructive diff from malformed facts.
func validateUpstreamModelMetadata(metadata map[string]UpstreamModelMetadata) error {
	for _, entry := range metadata {
		if slices.Contains(entry.Conflicts, "invalid_optional_metadata") {
			return newUpstreamModelSyncUpstreamError("Invalid optional model metadata", nil)
		}
	}
	return nil
}

// fetchCompleteModelPages preserves validated pages when a later page fails.
// All continuation URLs stay on the original origin and redirects are disabled.
func (service *AccountTestService) fetchCompleteModelPages(request *http.Request, account *Account) ([]string, []byte, error) {
	entries := []json.RawMessage{}
	seen := map[string]bool{}
	seenModels := map[string]bool{}
	validatedPages := 0
	origin := *request.URL
	limit := resolveModelsListReadLimit(service.cfg)
	var consumed int64
	assemble := func(cause error) ([]string, []byte, error) {
		object := ""
		if account.Platform == PlatformQianfan {
			object = "list"
		}
		merged, _ := json.Marshal(struct {
			Data   []json.RawMessage `json:"data"`
			Object string            `json:"object,omitempty"`
		}{entries, object})
		models, metadata, parseErr := extractUpstreamModelCatalog(merged, account.IsGrok(), account.Platform)
		if parseErr != nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Invalid model entries", parseErr)
		}
		if cause == nil {
			cause = validateUpstreamModelMetadata(metadata)
		}
		if cause == nil {
			return models, merged, nil
		}
		if validatedPages == 0 {
			return nil, nil, cause
		}
		return models, merged, &UpstreamModelSyncError{
			Kind: UpstreamModelSyncErrorUpstream, Message: "Model discovery is partial; previous availability must be retained", Err: cause,
			PartialCatalog: &UpstreamModelCatalog{Models: []string{}, DiscoveredModels: models, CandidateModels: models,
				Metadata: metadata, CatalogSource: "partial", Warnings: []UpstreamModelSyncWarning{{Code: "upstream_model_discovery_partial", Message: "Model discovery contains incomplete pages or invalid entries; previous availability must be retained"}}},
		}
	}
	for page := 0; page < 100; page++ {
		if seen[request.URL.String()] {
			return assemble(newUpstreamModelSyncUpstreamError("Model pagination did not advance", nil))
		}
		seen[request.URL.String()] = true
		response, err := service.doUpstreamModelsRequest(request, upstreamModelsProxyURL(account), account)
		if err != nil || response == nil || response.Body == nil {
			return assemble(newUpstreamModelSyncUpstreamError("Failed to request model page", err))
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, limit-consumed+1))
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return assemble(newUpstreamModelDiscoveryHTTPError(response.StatusCode, body))
		}
		consumed += int64(len(body))
		if readErr != nil || consumed > limit {
			return assemble(newUpstreamModelSyncUpstreamError(fmt.Sprintf("Model catalog response exceeds %d bytes or is unreadable", limit), readErr))
		}
		if account.Platform == PlatformMistral {
			if _, err := parseMistralModelsResponse(response.StatusCode, body); err != nil {
				return assemble(err)
			}
		}
		if account.Platform == PlatformQianfan {
			pageResponse := *response
			pageResponse.Body = io.NopCloser(bytes.NewReader(body))
			if _, err := parseQianfanModelsResponse(&pageResponse); err != nil {
				return assemble(err)
			}
		}
		selector := upstreamModelEntryID
		if account.IsGrok() {
			selector = grokUpstreamModelEntryID
		}
		if _, err := extractCatalogModelIDsWithSelector(body, selector); err != nil {
			return assemble(newUpstreamModelSyncUpstreamError("Invalid model page", err))
		}
		pageEntries, err := extractUpstreamModelRawEntries(body)
		if err != nil {
			return assemble(newUpstreamModelSyncUpstreamError("Invalid model page", err))
		}
		validatedPages++
		for _, entry := range pageEntries {
			var identity upstreamModelEntry
			_ = json.Unmarshal(entry, &identity)
			modelID := selector(identity)
			if !seenModels[modelID] {
				entries = append(entries, entry)
				seenModels[modelID] = true
			}
		}
		var paging struct {
			HasMore       bool   `json:"has_more"`
			LastID        string `json:"last_id"`
			NextPageToken string `json:"nextPageToken"`
			NextCursor    string `json:"next_cursor"`
			Next          string `json:"next"`
		}
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
			if err := json.Unmarshal(body, &paging); err != nil {
				return assemble(newUpstreamModelSyncUpstreamError("Invalid model pagination", err))
			}
		}
		next := request.Clone(request.Context())
		cursor, parameter := paging.NextPageToken, "pageToken"
		if paging.NextCursor != "" {
			cursor, parameter = paging.NextCursor, "cursor"
		}
		if paging.Next != "" {
			link, err := url.Parse(paging.Next)
			if err != nil {
				return assemble(newUpstreamModelSyncUpstreamError("Invalid next model page", err))
			}
			// A bare next value is a cursor; relative paths and queries are links.
			if !link.IsAbs() && link.Host == "" && !strings.ContainsAny(paging.Next, "/?") {
				cursor, parameter = paging.Next, "cursor"
			} else {
				next.URL = request.URL.ResolveReference(link)
				if !strings.EqualFold(next.URL.Scheme, origin.Scheme) || !strings.EqualFold(next.URL.Host, origin.Host) || next.URL.User != nil || next.URL.Fragment != "" {
					return assemble(newUpstreamModelSyncUpstreamError("Next model page must have the same origin", nil))
				}
			}
		} else if paging.HasMore && cursor == "" {
			cursor, parameter = paging.LastID, "after"
			if account.IsAnthropic() {
				parameter = "after_id"
			}
			if cursor == "" {
				return assemble(newUpstreamModelSyncUpstreamError("Missing next model page cursor", nil))
			}
		}
		if paging.Next == "" && cursor == "" {
			return assemble(nil)
		}
		if len(pageEntries) == 0 {
			return assemble(newUpstreamModelSyncUpstreamError("Model pagination did not advance", nil))
		}
		if cursor != "" {
			query := next.URL.Query()
			query.Set(parameter, cursor)
			next.URL.RawQuery = query.Encode()
		}
		request = next
	}
	return assemble(newUpstreamModelSyncUpstreamError("Model pagination exceeded page limit", nil))
}
