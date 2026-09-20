package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	modelcapabilities "github.com/Wei-Shaw/sub2api/resources/model-capabilities"
	modelpricing "github.com/Wei-Shaw/sub2api/resources/model-pricing"
)

// LiteLLMCapabilityFields retains presence and source semantics. MaxTokens is
// deliberately not interpreted: LiteLLM entries use it for different limits.
// Vision is a partial observation, never a complete modality declaration.
type LiteLLMCapabilityFields struct {
	MaxInputTokens          *int64   `json:"max_input_tokens,omitempty"`
	MaxOutputTokens         *int64   `json:"max_output_tokens,omitempty"`
	MaxTokens               *int64   `json:"max_tokens,omitempty"`
	SupportsReasoning       *bool    `json:"supports_reasoning,omitempty"`
	SupportsVision          *bool    `json:"supports_vision,omitempty"`
	SupportsFunctionCalling *bool    `json:"supports_function_calling,omitempty"`
	SupportsPromptCaching   *bool    `json:"supports_prompt_caching,omitempty"`
	InputModalities         []string `json:"input_modalities,omitempty"`
	OutputModalities        []string `json:"output_modalities,omitempty"`
}

// CapabilityRecord contains specifications only; it cannot authorize a model
// or provide any billing or Codex-specific tool defaults.
type CapabilityRecord struct {
	ContextWindow         int64                      `json:"context_window,omitempty"`
	ContextLimitKind      string                     `json:"context_limit_kind,omitempty"`
	InputModalitiesStatus string                     `json:"input_modalities_status,omitempty"`
	TaskLimits            map[string]json.RawMessage `json:"task_limits,omitempty"`
	Aliases               []string                   `json:"aliases,omitempty"`
	Control               *ReasoningControl          `json:"control,omitempty"`
	SupportedMethods      []string                   `json:"supported_methods,omitempty"`
	EmbeddingDimensions   *int64                     `json:"embedding_dimensions,omitempty"`
	LiteLLMCapabilityFields
	Provider                string                           `json:"provider"`
	Profile                 string                           `json:"profile"`
	ModelID                 string                           `json:"model_id"`
	Region                  string                           `json:"region,omitempty"`
	Protocol                string                           `json:"protocol,omitempty"`
	Mode                    string                           `json:"mode,omitempty"`
	ReasoningControl        string                           `json:"reasoning_control,omitempty"`
	Source                  string                           `json:"source,omitempty"`
	VerifiedAt              string                           `json:"verified_at,omitempty"`
	SourceContentHash       string                           `json:"source_content_hash,omitempty"`
	ApplicableContentHashes []string                         `json:"applicable_content_hashes,omitempty"`
	FieldSources            map[string]CapabilityFieldSource `json:"field_sources,omitempty"`
}

type CapabilityFieldSource struct {
	Source      string `json:"source"`
	ContentHash string `json:"content_hash"`
}

type ModelCapabilityProvider interface {
	CapabilitySnapshot() *ModelCapabilitySnapshot
}

// ModelCapabilitySnapshot is immutable after publication. All exported accessors
// return values or deep copies, so a captured snapshot stays stable across reloads.
type ModelCapabilitySnapshot struct {
	generation  int64
	version     string
	contentHash string
	loadedAt    time.Time
	records     map[string]CapabilityRecord
	aliases     map[string][]string
}

type CapabilitySnapshot = ModelCapabilitySnapshot

func (s *ModelCapabilitySnapshot) Version() string {
	if s == nil {
		return ""
	}
	return s.version
}
func (s *ModelCapabilitySnapshot) ContentHash() string {
	if s == nil {
		return ""
	}
	return s.contentHash
}
func (s *ModelCapabilitySnapshot) LoadedAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.loadedAt
}
func (s *ModelCapabilitySnapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.records)
}

// LookupExact is case-sensitive and scope-preserving. No prefix, date, family,
// hosted alias or provider is guessed. P1 may add reviewed matching above this API.
func (s *ModelCapabilitySnapshot) LookupExact(profile, modelID string) (CapabilityRecord, bool) {
	if s == nil {
		return CapabilityRecord{}, false
	}
	r, ok := s.records[capabilityKey(profile, modelID)]
	if !ok {
		return CapabilityRecord{}, false
	}
	return cloneCapabilityRecord(r), true
}

func capabilityKey(profile, modelID string) string { return profile + "\x00" + modelID }
func capabilityHash(body []byte) string            { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }

func cloneCapabilityRecord(r CapabilityRecord) CapabilityRecord {
	// Clone pointer/slice fields directly; JSON omitempty would lose explicit empty lists.
	c := r
	c.Aliases = append([]string(nil), r.Aliases...)
	if r.SupportedMethods != nil {
		c.SupportedMethods = append([]string{}, r.SupportedMethods...)
	}
	if r.EmbeddingDimensions != nil {
		dimensions := *r.EmbeddingDimensions
		c.EmbeddingDimensions = &dimensions
	}
	c.Control = cloneReasoningControl(r.Control)
	if r.TaskLimits != nil {
		c.TaskLimits = make(map[string]json.RawMessage, len(r.TaskLimits))
		for name, value := range r.TaskLimits {
			c.TaskLimits[name] = append(json.RawMessage(nil), value...)
		}
	}
	src, dst := reflect.ValueOf(r.LiteLLMCapabilityFields), reflect.ValueOf(&c.LiteLLMCapabilityFields).Elem()
	for i := 0; i < src.NumField(); i++ {
		v := src.Field(i)
		if v.IsNil() {
			continue
		}
		if v.Kind() == reflect.Pointer {
			p := reflect.New(v.Type().Elem())
			p.Elem().Set(v.Elem())
			dst.Field(i).Set(p)
		} else {
			dst.Field(i).Set(reflect.MakeSlice(v.Type(), v.Len(), v.Len()))
			reflect.Copy(dst.Field(i), v)
		}
	}
	c.ApplicableContentHashes = append([]string(nil), r.ApplicableContentHashes...)
	c.FieldSources = make(map[string]CapabilityFieldSource, len(r.FieldSources))
	for k, v := range r.FieldSources {
		c.FieldSources[k] = v
	}
	return c
}

func validateCapabilityRecord(r CapabilityRecord) error {
	if strings.TrimSpace(r.ModelID) == "" || strings.ContainsRune(r.ModelID, 0) || strings.ContainsRune(r.Profile, 0) {
		return fmt.Errorf("invalid model identity")
	}
	for name, v := range map[string]*int64{"max_input_tokens": r.MaxInputTokens, "max_output_tokens": r.MaxOutputTokens, "max_tokens": r.MaxTokens, "embedding_dimensions": r.EmbeddingDimensions} {
		if v != nil && *v <= 0 {
			return fmt.Errorf("%s: %s must be positive", r.ModelID, name)
		}
	}
	// Input and output limits are independent; comparing them would incorrectly
	// treat input as the total context window.
	for _, list := range [][]string{r.InputModalities, r.OutputModalities, r.SupportedMethods} {
		for _, v := range list {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("%s: empty modality or method", r.ModelID)
			}
		}
	}
	switch r.ReasoningControl {
	case "", "none", "effort", "toggle", "budget", "adaptive", "fixed", "unknown":
	default:
		return fmt.Errorf("invalid reasoning control: %s", r.ModelID)
	}
	if r.SupportsReasoning != nil && !*r.SupportsReasoning && r.ReasoningControl != "" && r.ReasoningControl != "none" && r.ReasoningControl != "unknown" {
		return fmt.Errorf("conflicting reasoning: %s", r.ModelID)
	}
	if r.Control != nil {
		c := r.Control
		if r.SupportsReasoning != nil && !*r.SupportsReasoning && c.Kind != "none" && c.Kind != "unknown" {
			return fmt.Errorf("conflicting reasoning control: %s", r.ModelID)
		}
		switch c.Kind {
		case "none", "effort", "toggle", "budget", "adaptive", "fixed", "unknown":
		default:
			return fmt.Errorf("invalid control kind")
		}
		if c.Kind == "effort" && len(c.EffortValues) == 0 {
			return fmt.Errorf("effort values missing")
		}
		if c.Kind == "budget" && (c.BudgetMin == nil || c.BudgetMax == nil || *c.BudgetMin < 0 || *c.BudgetMax < *c.BudgetMin) {
			return fmt.Errorf("invalid reasoning budget")
		}
	}

	return nil
}

func hasCapabilityFields(r CapabilityRecord) bool {
	v := reflect.ValueOf(r.LiteLLMCapabilityFields)
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsNil() {
			return true
		}
	}
	return false
}

func fillCapabilityFields(primary, fallback CapabilityRecord) CapabilityRecord {
	if primary.FieldSources == nil {
		primary.FieldSources = make(map[string]CapabilityFieldSource)
	}
	a, b := reflect.ValueOf(&primary.LiteLLMCapabilityFields).Elem(), reflect.ValueOf(fallback.LiteLLMCapabilityFields)
	for i := 0; i < a.NumField(); i++ {
		if a.Field(i).IsNil() && !b.Field(i).IsNil() {
			a.Field(i).Set(b.Field(i))
			name := strings.Split(a.Type().Field(i).Tag.Get("json"), ",")[0]
			primary.FieldSources[name] = fallback.FieldSources[name]
		}
	}
	if primary.Mode == "" && fallback.Mode != "" {
		primary.Mode = fallback.Mode
		primary.FieldSources["mode"] = fallback.FieldSources["mode"]
	}
	if primary.ContextWindow == 0 && primary.MaxInputTokens == nil && fallback.ContextWindow > 0 {
		primary.ContextWindow, primary.ContextLimitKind = fallback.ContextWindow, fallback.ContextLimitKind
		primary.FieldSources["context_window"] = fallback.FieldSources["context_window"]
		primary.FieldSources["context_limit_kind"] = fallback.FieldSources["context_limit_kind"]
	}
	if primary.Control == nil && fallback.Control != nil {
		primary.Control = cloneReasoningControl(fallback.Control)
		primary.FieldSources["control"] = fallback.FieldSources["control"]
	}
	if primary.SupportedMethods == nil && fallback.SupportedMethods != nil {
		primary.SupportedMethods = append([]string(nil), fallback.SupportedMethods...)
		primary.FieldSources["supported_methods"] = fallback.FieldSources["supported_methods"]
	}
	if primary.EmbeddingDimensions == nil && fallback.EmbeddingDimensions != nil {
		primary.EmbeddingDimensions = fallback.EmbeddingDimensions
		primary.FieldSources["embedding_dimensions"] = fallback.FieldSources["embedding_dimensions"]
	}
	if primary.InputModalitiesStatus == "" {
		primary.InputModalitiesStatus = fallback.InputModalitiesStatus
	}
	if primary.TaskLimits == nil && len(fallback.TaskLimits) > 0 {
		primary.TaskLimits = make(map[string]json.RawMessage)
	}
	for name, value := range fallback.TaskLimits {
		if _, exists := primary.TaskLimits[name]; !exists {
			primary.TaskLimits[name] = append(json.RawMessage(nil), value...)
			primary.FieldSources["task_limits."+name] = fallback.FieldSources["task_limits."+name]
		}
	}
	return primary
}

func parseCapabilityRecords(body []byte, source string) (map[string]CapabilityRecord, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("empty capability catalog")
	}
	records := make(map[string]CapabilityRecord)
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	hash := capabilityHash(body)
	for _, id := range keys {
		if id == "sample_spec" {
			continue
		}
		var raw struct {
			LiteLLMCapabilityFields
			Provider string `json:"litellm_provider"`
			Mode     string `json:"mode"`
		}
		if err := json.Unmarshal(entries[id], &raw); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		r := CapabilityRecord{LiteLLMCapabilityFields: raw.LiteLLMCapabilityFields, Provider: raw.Provider, Profile: raw.Provider, ModelID: id, Mode: raw.Mode, Source: source, SourceContentHash: hash, FieldSources: make(map[string]CapabilityFieldSource)}
		if err := validateCapabilityRecord(r); err != nil {
			return nil, err
		}
		if !hasCapabilityFields(r) {
			continue
		}
		v := reflect.ValueOf(r.LiteLLMCapabilityFields)
		for i := 0; i < v.NumField(); i++ {
			if !v.Field(i).IsNil() {
				name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
				r.FieldSources[name] = CapabilityFieldSource{source, hash}
			}
		}
		records[capabilityKey(r.Profile, id)] = r
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("catalog contains no capability fields")
	}
	return records, nil
}

// buildModelCapabilitySnapshot never reads custom pricing overrides. Supplements
// are version-bound; runtime fills before embed and boolean false is never missing.
func buildModelCapabilitySnapshot(runtime []byte) (*ModelCapabilitySnapshot, error) {
	return buildModelCapabilitySnapshotFromCatalog(runtime, modelcapabilities.JSON())
}

func buildModelCapabilitySnapshotFromCatalog(runtime, supplement []byte) (*ModelCapabilitySnapshot, error) {
	baseline := modelpricing.JSON()
	records, err := parseCapabilityRecords(baseline, "litellm:embed")
	if err != nil {
		return nil, err
	}
	contentHash := capabilityHash(baseline)
	if runtime != nil {
		live, err := parseCapabilityRecords(runtime, "litellm:runtime")
		if err != nil {
			return nil, err
		}
		for key, r := range live {
			if old, ok := records[key]; ok {
				r = fillCapabilityFields(r, old)
			}
			records[key] = r
		}
		contentHash = capabilityHash(runtime)
	}
	var catalog struct {
		SchemaVersion int                `json:"schema_version"`
		GeneratedAt   string             `json:"generated_at"`
		Records       []CapabilityRecord `json:"records"`
	}
	if err := json.Unmarshal(supplement, &catalog); err != nil {
		return nil, err
	}
	if catalog.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported capability schema")
	}
	if _, err := time.Parse(time.RFC3339, catalog.GeneratedAt); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, r := range catalog.Records {
		if err := validateCapabilityRecord(r); err != nil {
			return nil, err
		}
		key := capabilityKey(r.Profile, r.ModelID)
		if seen[key] {
			return nil, fmt.Errorf("duplicate supplement: %s", r.ModelID)
		}
		seen[key] = true
		if r.Provider == "" || r.Profile == "" || r.Source == "" || len(r.SourceContentHash) != 64 {
			return nil, fmt.Errorf("missing supplement provenance: %s", r.ModelID)
		}
		if _, err := time.Parse("2006-01-02", r.VerifiedAt); err != nil {
			return nil, err
		}
		// The independently pinned release approves exact profile/model identities.
		// Historical input hashes are provenance, never a live pricing predicate.
		r.FieldSources = make(map[string]CapabilityFieldSource)
		v := reflect.ValueOf(r.LiteLLMCapabilityFields)
		for i := 0; i < v.NumField(); i++ {
			if !v.Field(i).IsNil() {
				name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
				r.FieldSources[name] = CapabilityFieldSource{r.Source, r.SourceContentHash}
			}
		}
		if old, ok := records[key]; ok {
			r = fillCapabilityFields(r, old)
		}
		records[key] = r
	}
	version := capabilityHash([]byte(contentHash + ":" + capabilityHash(baseline) + ":" + capabilityHash(supplement)))
	releasedAt, _ := time.Parse(time.RFC3339, catalog.GeneratedAt)
	return &ModelCapabilitySnapshot{generation: releasedAt.Unix(), version: version, contentHash: contentHash, loadedAt: time.Now(), records: records, aliases: buildCapabilityAliasIndex(records)}, nil
}

func (s *PricingService) CapabilitySnapshot() *ModelCapabilitySnapshot {
	if s == nil {
		return nil
	}
	if s.registrySnapshot != nil {
		return s.registrySnapshot.CapabilitySnapshot()
	}
	if s.registry != nil {
		return s.registry.CapabilitySnapshot()
	}
	return NewModelRegistry().CapabilitySnapshot()
}

func (s *PricingService) buildCapabilityCandidate(body []byte, declaredHash string) *ModelCapabilitySnapshot {
	if declaredHash != "" && !strings.EqualFold(declaredHash, capabilityHash(body)) {
		logger.LegacyPrintf("service.pricing", "[Capabilities] Hash mismatch; retaining previous snapshot")
		return nil
	}
	candidate, err := buildModelCapabilitySnapshot(body)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Capabilities] Invalid candidate; retaining previous snapshot: %v", err)
		return nil
	}
	previous := s.CapabilitySnapshot()
	if previous != nil {
		for key, old := range previous.records {
			next, exists := candidate.records[key]
			if !exists {
				continue
			} // Deletions do not authorize or discover any model.
			for field, source := range old.FieldSources {
				if source.Source == "litellm:runtime" && next.FieldSources[field].Source != "litellm:runtime" {
					logger.LegacyPrintf("service.pricing", "[Capabilities] Field regression for %s/%s; retaining previous snapshot", old.ModelID, field)
					return nil
				}
			}
		}
	}
	return candidate
}

var renamePricingFile = os.Rename

// Replace a complete file in its own directory so readers cannot see half JSON.
func writePricingFileAtomically(path string, body []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pricing-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err = file.Chmod(0644); err != nil {
		_ = file.Close()
		return err
	}
	if _, err = file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	const maxAttempts = 5
	for attempt := 1; ; attempt++ {
		err = renamePricingFile(name, path)
		// Windows readers may temporarily deny replacement. Keep POSIX and
		// all other errors unchanged; never remove the destination to retry.
		// Win32 ERROR_ACCESS_DENIED = 5, ERROR_SHARING_VIOLATION = 32.
		if err == nil || runtime.GOOS != "windows" ||
			(!errors.Is(err, syscall.Errno(5)) && !errors.Is(err, syscall.Errno(32))) {
			return err
		}
		if attempt == maxAttempts {
			return fmt.Errorf("replace pricing file after %d attempts: %w", attempt, err)
		}
		// Four bounded backoffs: 25 + 50 + 75 + 100 = 250 ms total.
		time.Sleep(time.Duration(attempt) * 25 * time.Millisecond)
	}
}

// Invalid capability fields cannot change the pre-existing price admission rules.
func unmarshalPricingEntry(body []byte, entry *LiteLLMRawEntry) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	for _, name := range []string{"max_input_tokens", "max_output_tokens", "max_tokens", "supports_reasoning", "supports_vision", "supports_function_calling", "input_modalities", "output_modalities"} {
		delete(fields, name)
	}
	priceBody, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(priceBody, entry)
}

func (s *PricingService) persistPricingDownload(path string, body []byte, candidate *ModelCapabilitySnapshot) error {
	s.stageCapabilityReview(path, body, candidate)
	return writePricingFileAtomically(path, body)
}

func (s *PricingService) stageCapabilityReview(path string, body []byte, candidate *ModelCapabilitySnapshot) {
	// Collection never updates the registry. Human review and a new pinned local
	// package are required even when the remote content hash matches.
	if candidate != nil {
		previous := s.CapabilitySnapshot()
		changes := []string{}
		observed, _ := parseCapabilityRecords(body, "pending-review")
		for identity, record := range observed {
			if !reflect.DeepEqual(previous.records[identity], record) {
				changes = append(changes, identity)
			}
		}
		sort.Strings(changes)
		pending, err := json.Marshal(struct {
			Status            string          `json:"status"`
			BaseGeneration    int64           `json:"base_generation"`
			ContentHash       string          `json:"content_hash"`
			Input             json.RawMessage `json:"input"`
			ChangedIdentities []string        `json:"changed_identities"`
		}{"pending_review", previous.generation, capabilityHash(body), body, changes})
		if err == nil {
			err = writePricingFileAtomically(path+".capabilities.pending.json", pending)
		}
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Capabilities] Cannot stage pending review: %v", err)
		}
	}
}

// MarshalJSON emits the public schema and preserves explicit empty declarations.
func (r CapabilityRecord) MarshalJSON() ([]byte, error) {
	type plain CapabilityRecord
	body, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if r.Control != nil {
		fields["reasoning_control"], err = json.Marshal(r.Control)
		if err != nil {
			return nil, err
		}
		delete(fields, "control")
	}
	for name, values := range map[string][]string{
		"aliases": r.Aliases, "supported_methods": r.SupportedMethods,
		"input_modalities": r.InputModalities, "output_modalities": r.OutputModalities,
		"applicable_content_hashes": r.ApplicableContentHashes,
	} {
		if values != nil && len(values) == 0 {
			fields[name] = json.RawMessage(`[]`)
		}
	}
	return json.Marshal(fields)
}

// UnmarshalJSON accepts string and structured reasoning controls without changing
// the string field used by existing callers.
func (r *CapabilityRecord) UnmarshalJSON(body []byte) error {
	type plain CapabilityRecord
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	control := fields["reasoning_control"]
	if len(control) > 0 && control[0] == '{' {
		delete(fields, "reasoning_control")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var value plain
	if err = json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	*r = CapabilityRecord(value)
	if len(control) > 0 && control[0] == '{' {
		var structured ReasoningControl
		if err = json.Unmarshal(control, &structured); err != nil {
			return err
		}
		r.Control = &structured
		r.ReasoningControl = structured.Kind
	}
	return nil
}
