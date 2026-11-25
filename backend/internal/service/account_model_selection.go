package service

import (
	"context"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

type ModelSelectionMode string

const (
	ModelSelectionInherit    ModelSelectionMode = "inherit"
	ModelSelectionRestricted ModelSelectionMode = "restricted"
	ModelSelectionDenyAll    ModelSelectionMode = "deny_all"
)

type ModelSelectionSource uint8

const (
	ModelSelectionAdmin ModelSelectionSource = iota
	ModelSelectionAccountWrite
	ModelSelectionConnection
	ModelSelectionRepair
)

const ModelSelectionRepairBackupExtraKey = "model_selection_v039_repair_backup"

var (
	ErrModelSelectionInvalid              = infraerrors.BadRequest("INVALID_MODEL_SELECTION", "Invalid account model selection")
	ErrModelSelectionConfirmationRequired = infraerrors.BadRequest("MODEL_SELECTION_CONFIRMATION_REQUIRED", "Explicitly confirm disabling all models")
	ErrModelSelectionReasonRequired       = infraerrors.BadRequest("MODEL_SELECTION_REASON_REQUIRED", "A reason is required to disable all models")
	ErrModelSelectionConflict             = infraerrors.Conflict("MODEL_SELECTION_CONFLICT", "Account model selection changed; reload and retry")
	ErrModelSelectionDenied               = infraerrors.Conflict("MODEL_SELECTION_DENIED", "Choose a model selection mode before changing this account's mapping")
)

// ModelSelectionCommand is populated by a trusted adapter. An empty Mode is a
// mapping-only edit. A nil ModelMapping preserves the map; an empty map clears it.
type ModelSelectionCommand struct {
	Mode                     ModelSelectionMode
	Models                   []string
	ModelMapping             map[string]string
	Reason                   string
	ExpectedRulesRevision    *int64
	ExpectedIdentityRevision *int64
	ActorID                  int64
	Source                   ModelSelectionSource
}

// ModelSelectionResult is a view of both configuration fields at one revision.
type ModelSelectionResult struct {
	Mode           ModelSelectionMode    `json:"mode"`
	Models         []string              `json:"models"`
	ModelMapping   map[string]string     `json:"model_mapping"`
	Scope          AccountModelAllowlist `json:"scope"`
	AutoAdded      []string              `json:"auto_added"`
	NotDiscovered  []string              `json:"not_discovered"`
	RulesRevision  int64                 `json:"rules_revision,string"`
	Changed        bool                  `json:"-"`
	MappingChanged bool                  `json:"-"`
}

// ModelSelectionRepository resolves a selection against the locked account.
// It commits both fields with the resulting revision, using the caller's
// transaction when present. For unchanged results it performs no write.
type ModelSelectionRepository interface {
	UpdateModelSelection(ctx context.Context, accountID int64, source ModelSelectionSource, resolve func(*Account) (ModelSelectionResult, error)) (ModelSelectionResult, error)
}

type AccountModelSelectionService struct {
	repository ModelSelectionRepository
}

func NewAccountModelSelectionService(repository ModelSelectionRepository) *AccountModelSelectionService {
	return &AccountModelSelectionService{repository: repository}
}

func (s *AccountModelSelectionService) SaveModelSelection(ctx context.Context, accountID int64, cmd ModelSelectionCommand) (ModelSelectionResult, error) {
	if s == nil || s.repository == nil {
		return ModelSelectionResult{}, infraerrors.InternalServer("MODEL_SELECTION_UNAVAILABLE", "Account model selection persistence is unavailable")
	}
	if cmd.Source == ModelSelectionAdmin && (cmd.ExpectedRulesRevision == nil || cmd.ActorID <= 0 || cmd.Mode == "") {
		return ModelSelectionResult{}, ErrModelSelectionInvalid
	}
	return s.repository.UpdateModelSelection(ctx, accountID, cmd.Source, func(account *Account) (ModelSelectionResult, error) {
		return ResolveLockedModelSelection(account, cmd)
	})
}

// SaveModelSelection uses the same repository as the account catalog endpoint.
func (s *AccountTestService) SaveModelSelection(ctx context.Context, accountID int64, cmd ModelSelectionCommand) (ModelSelectionResult, error) {
	repository, _ := s.accountRepo.(ModelSelectionRepository)
	return NewAccountModelSelectionService(repository).SaveModelSelection(ctx, accountID, cmd)
}

// ResolveModelSelectionMode decodes the public v1 selection request.
func ResolveModelSelectionMode(mode ModelSelectionMode, enabled *bool, models []string, confirmDenyAll bool) (ModelSelectionMode, error) {
	switch mode {
	case ModelSelectionInherit, ModelSelectionRestricted, ModelSelectionDenyAll:
		if enabled != nil && *enabled != (mode != ModelSelectionInherit) {
			return "", ErrModelSelectionInvalid
		}
		return mode, nil
	case "":
		if len(models) > 0 {
			return ModelSelectionRestricted, nil
		}
		if enabled == nil || !*enabled {
			return ModelSelectionInherit, nil
		}
		if !confirmDenyAll {
			return "", ErrModelSelectionConfirmationRequired
		}
		return ModelSelectionDenyAll, nil
	default:
		return "", ErrModelSelectionInvalid
	}
}

func modelSelectionScopeMode(scope AccountModelAllowlist) ModelSelectionMode {
	switch {
	case !scope.Enabled:
		return ModelSelectionInherit
	case len(scope.Models) == 0:
		return ModelSelectionDenyAll
	default:
		return ModelSelectionRestricted
	}
}

func AccountModelSelectionView(account *Account) ModelSelectionResult {
	var scope AccountModelAllowlist
	decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope)
	mode := modelSelectionScopeMode(scope)
	if legacyUpgradeFreeze(scope) {
		mode = ModelSelectionInherit
	}
	scope.Models = append([]string{}, scope.Models...)
	return ModelSelectionResult{
		Mode: mode, Scope: scope, Models: scope.Models,
		ModelMapping: copySelectionMapping(configuredModelMapping(account)),
		AutoAdded:    []string{}, NotDiscovered: []string{},
		RulesRevision: account.ModelCatalogState().RulesRevision,
	}
}

// ResolveLockedModelSelection is also used by account mutations which already
// hold the row lock. The caller combines its revision with other rule changes.
func ResolveLockedModelSelection(account *Account, cmd ModelSelectionCommand) (ModelSelectionResult, error) {
	if account == nil {
		return ModelSelectionResult{}, ErrAccountNotFound
	}
	state := account.ModelCatalogState()
	if cmd.ExpectedRulesRevision != nil && (*cmd.ExpectedRulesRevision < 0 || *cmd.ExpectedRulesRevision == math.MaxInt64) {
		return ModelSelectionResult{}, ErrModelSelectionInvalid
	}
	if (cmd.ExpectedRulesRevision != nil && *cmd.ExpectedRulesRevision != state.RulesRevision) ||
		(cmd.ExpectedIdentityRevision != nil && *cmd.ExpectedIdentityRevision != state.IdentityRevision) {
		return ModelSelectionResult{}, ErrModelSelectionConflict
	}
	view := AccountModelSelectionView(account)
	result, err := NormalizeModelSelection(view.Scope, view.ModelMapping, cmd)
	if err != nil {
		return ModelSelectionResult{}, err
	}
	result.RulesRevision = state.RulesRevision
	if result.Changed {
		if state.RulesRevision == math.MaxInt64 {
			return ModelSelectionResult{}, ErrModelSelectionConflict
		}
		result.RulesRevision++
		result.Scope.IdentityRevision = state.IdentityRevision
		result.Scope.ConfirmedBy = cmd.ActorID
		result.Scope.ConfirmedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if result.Mode == ModelSelectionRestricted {
		snapshot := account.GetUpstreamModelCatalog()
		for _, model := range result.Models {
			if snapshot == nil || snapshot.IdentityRevision != state.IdentityRevision || !slices.Contains(snapshot.DiscoveredModels, model) {
				result.NotDiscovered = append(result.NotDiscovered, model)
			}
		}
	}
	return result, nil
}

// NormalizeModelSelection computes both stored fields without reading storage,
// modifying its arguments, or changing the revision.
func NormalizeModelSelection(currentScope AccountModelAllowlist, currentMapping map[string]string, cmd ModelSelectionCommand) (ModelSelectionResult, error) {
	currentScope.Models = append([]string{}, currentScope.Models...)
	result := ModelSelectionResult{
		Mode: modelSelectionScopeMode(currentScope), Scope: currentScope, Models: currentScope.Models,
		ModelMapping: copySelectionMapping(currentMapping), AutoAdded: []string{}, NotDiscovered: []string{},
	}

	if cmd.ModelMapping != nil {
		for name, target := range cmd.ModelMapping {
			if !validModelSelectionName(name) || !exactConfiguredModel(target) || !validModelSelectionName(target) {
				return ModelSelectionResult{}, ErrModelSelectionInvalid
			}
		}
		result.MappingChanged = !maps.Equal(currentMapping, cmd.ModelMapping)
		result.ModelMapping = copySelectionMapping(cmd.ModelMapping)
	}

	mode := cmd.Mode
	models := cmd.Models
	reason := strings.TrimSpace(cmd.Reason)
	mappingEdit := mode == ""
	if mappingEdit {
		if !result.MappingChanged {
			return result, nil
		}
		if result.Mode == ModelSelectionDenyAll {
			return ModelSelectionResult{}, ErrModelSelectionDenied
		}
		mode = result.Mode
		if currentScope.Enabled {
			models = currentScope.Models
		} else {
			models = nil
		}
		if len(result.ModelMapping) > 0 {
			mode = ModelSelectionRestricted
		}
	}

	switch mode {
	case ModelSelectionInherit:
		if !mappingEdit && len(models) != 0 {
			return ModelSelectionResult{}, ErrModelSelectionInvalid
		}
		models = []string{}
	case ModelSelectionDenyAll:
		if reason == "" {
			return ModelSelectionResult{}, ErrModelSelectionReasonRequired
		}
		if cmd.ActorID <= 0 || len(models) != 0 || len(cmd.ModelMapping) != 0 {
			return ModelSelectionResult{}, ErrModelSelectionInvalid
		}
		models = []string{}
	case ModelSelectionRestricted:
		var err error
		models, result.AutoAdded, err = restrictedModelSelection(models, result.ModelMapping)
		if err != nil {
			return ModelSelectionResult{}, err
		}
	default:
		return ModelSelectionResult{}, ErrModelSelectionInvalid
	}

	enabled := mode != ModelSelectionInherit
	result.Mode, result.Models = mode, models
	result.Changed = result.MappingChanged || currentScope.Enabled != enabled || !slices.Equal(currentScope.Models, models) || (reason != "" && currentScope.Reason != reason)
	if result.Changed {
		result.Scope.Enabled, result.Scope.Models = enabled, models
		if reason != "" {
			result.Scope.Reason = reason
		} else if !mappingEdit || result.Scope.Reason == "" {
			result.Scope.Reason = "管理端模型范围调整"
		}
	}
	return result, nil
}

func restrictedModelSelection(models []string, mapping map[string]string) ([]string, []string, error) {
	members := make(map[string]bool, len(models)+len(mapping))
	autoAdded := []string{}
	for _, model := range models {
		if !exactConfiguredModel(model) || !validModelSelectionName(model) {
			return nil, nil, ErrModelSelectionInvalid
		}
		members[model] = true
	}
	for _, target := range mapping {
		if !exactConfiguredModel(target) || !validModelSelectionName(target) {
			return nil, nil, ErrModelSelectionInvalid
		}
		if !members[target] {
			members[target] = true
			autoAdded = append(autoAdded, target)
		}
	}
	if len(members) == 0 {
		return nil, nil, ErrModelSelectionInvalid
	}
	selected := make([]string, 0, len(members))
	for model := range members {
		selected = append(selected, model)
	}
	slices.Sort(selected)
	slices.Sort(autoAdded)
	return selected, autoAdded, nil
}

func validModelSelectionName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && strings.IndexFunc(name, unicode.IsControl) < 0
}

func copySelectionMapping(mapping map[string]string) map[string]string {
	if mapping == nil {
		return map[string]string{}
	}
	return maps.Clone(mapping)
}

// ModelSelectionCommandFromCredentials separates an explicit mapping edit from
// a credential replacement. Persisted or refreshed credentials are not intent.
func ModelSelectionCommandFromCredentials(credentials map[string]any, actorID int64) (*ModelSelectionCommand, error) {
	var mapping map[string]string
	switch raw := credentials["model_mapping"].(type) {
	case nil:
		return nil, nil
	case map[string]string:
		if raw == nil {
			return nil, nil
		}
		mapping = maps.Clone(raw)
	case map[string]any:
		if raw == nil {
			return nil, nil
		}
		mapping = make(map[string]string, len(raw))
		for name, value := range raw {
			target, ok := value.(string)
			if !ok {
				return nil, ErrModelSelectionInvalid
			}
			mapping[name] = target
		}
	default:
		return nil, ErrModelSelectionInvalid
	}
	return &ModelSelectionCommand{ModelMapping: mapping, ActorID: actorID, Source: ModelSelectionAccountWrite}, nil
}

// InitializeAccountModelSelection is called by the insert path after dropping
// reserved extra fields supplied by external callers.
func InitializeAccountModelSelection(account *Account) error {
	cmd := account.ModelSelectionCommand
	if cmd == nil {
		var err error
		cmd, err = ModelSelectionCommandFromCredentials(account.Credentials, 0)
		if err != nil {
			return err
		}
		if cmd == nil {
			cmd = &ModelSelectionCommand{Source: ModelSelectionAccountWrite}
		}
	}
	if cmd.Mode == "" {
		initial := *cmd
		initial.Mode = ModelSelectionInherit
		if len(initial.ModelMapping) > 0 {
			initial.Mode = ModelSelectionRestricted
		}
		cmd = &initial
	}
	result, err := ResolveLockedModelSelection(account, *cmd)
	if err != nil {
		return err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[UpstreamModelAllowlistExtraKey] = result.Scope
	if cmd.ModelMapping != nil {
		if account.Credentials == nil {
			account.Credentials = make(map[string]any)
		}
		account.Credentials["model_mapping"] = result.ModelMapping
	}
	// Creation establishes the initial baseline rather than editing a rule.
	result.RulesRevision = 0
	account.ModelSelectionResult = &result
	return nil
}

func preserveCurrentModelMapping(incoming, current map[string]any) map[string]any {
	mapping, hasCurrent := current["model_mapping"]
	_, hasIncoming := incoming["model_mapping"]
	if !hasCurrent && !hasIncoming {
		return incoming
	}
	if hasCurrent && !hasIncoming && incoming == nil {
		return map[string]any{"model_mapping": mapping}
	}
	result := maps.Clone(incoming)
	if hasCurrent {
		result["model_mapping"] = mapping
	} else {
		delete(result, "model_mapping")
	}
	return result
}

// ModelSelectionDeniesAll preserves the historical system-freeze exception.
func (account *Account) ModelSelectionDeniesAll() bool {
	if account == nil {
		return false
	}
	var scope AccountModelAllowlist
	switch raw := account.Extra[UpstreamModelAllowlistExtraKey].(type) {
	case nil:
		return false
	case AccountModelAllowlist:
		scope = raw
	case map[string]any:
		if enabled, _ := raw["enabled"].(bool); !enabled {
			return false
		}
		switch models := raw["models"].(type) {
		case []any:
			if len(models) != 0 {
				return false
			}
		case []string:
			if len(models) != 0 {
				return false
			}
		}
		if !decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope) {
			return false
		}
	default:
		if !decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope) {
			return false
		}
	}
	return scope.Enabled && len(scope.Models) == 0 && !legacyUpgradeFreeze(scope)
}
