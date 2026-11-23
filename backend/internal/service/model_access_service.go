package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

var (
	ErrModelAccessNotFound  = infraerrors.NotFound("MODEL_ACCESS_NOT_FOUND", "model access resource not found")
	ErrModelAccessConflict  = infraerrors.Conflict("MODEL_ACCESS_CONFLICT", "model access group is referenced or revision changed")
	ErrModelAccessExpansion = infraerrors.BadRequest("MODEL_ACCESS_EXPANSION", "model access rules may only restrict the authorized public models")
)

// Models deliberately has no omitempty: null inherits and [] denies all.
type ModelAccessRule struct {
	AccessGroupID *int64   `json:"access_group_id"`
	Models        []string `json:"models"`
	Exclude       []string `json:"exclude"`
	Revision      int64    `json:"revision"`
}

type ModelAccessGroup struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Models     []string `json:"models"`
	Revision   int64    `json:"revision"`
	References int      `json:"references"`
}

type ModelAccessSnapshot struct {
	ProjectKeyPlatform  string
	KeyGroup            GroupModelAllowlist
	Generation          int64
	RevisionKnown       bool
	Key                 []string
	Accounts            map[int64][]string
	AccountExcludes     map[int64][]string
	KeyExclude          []string
	KeyRestriction      *identity.EffectiveKeyModels
	AccountRestrictions map[int64]identity.EffectiveKeyModels
	RestrictedKey       bool
}

type ModelAccessPolicyReader interface {
	ReadModelAccessSnapshot(context.Context, int64) (ModelAccessSnapshot, error)
}

type ModelAccessRepository interface {
	List(context.Context) ([]ModelAccessGroup, error)
	SaveGroup(context.Context, ModelAccessGroup) (*ModelAccessGroup, error)
	DeleteGroup(context.Context, int64, int64) error
	Rule(context.Context, string, int64, int64) (*ModelAccessRule, error)
	SaveRule(context.Context, string, int64, int64, ModelAccessRule) error
}

type ModelAccessService struct{ repository ModelAccessRepository }

func NewModelAccessService(repository ModelAccessRepository) *ModelAccessService {
	return &ModelAccessService{repository: repository}
}

func (s *ModelAccessService) List(ctx context.Context) ([]ModelAccessGroup, error) {
	return s.repository.List(ctx)
}
func (s *ModelAccessService) SaveGroup(ctx context.Context, group ModelAccessGroup) (*ModelAccessGroup, error) {
	group.Name = strings.TrimSpace(group.Name)
	if group.Name == "" || len([]rune(group.Name)) > 100 || group.Models == nil || !validAccessModels(group.Models) {
		return nil, ErrModelAccessExpansion
	}
	return s.repository.SaveGroup(ctx, group)
}
func (s *ModelAccessService) DeleteGroup(ctx context.Context, identifier, revision int64) error {
	return s.repository.DeleteGroup(ctx, identifier, revision)
}
func (s *ModelAccessService) Rule(ctx context.Context, resource string, identifier, owner int64) (*ModelAccessRule, error) {
	return s.repository.Rule(ctx, resource, identifier, owner)
}
func (s *ModelAccessService) SaveRule(ctx context.Context, resource string, identifier, owner int64, rule ModelAccessRule) error {
	if !validAccessModels(rule.Models) || !validAccessModels(rule.Exclude) {
		return ErrModelAccessExpansion
	}
	return s.repository.SaveRule(ctx, resource, identifier, owner, rule)
}

func validAccessModels(models []string) bool {
	for _, model := range models {
		if model == "" || model != strings.TrimSpace(model) || strings.ContainsAny(model, "*?\n\r") {
			return false
		}
	}
	return true
}

// ResolveModelAccessRule only intersects. A named set cannot grant a model
// absent from the routing group; binding still checks the original public name.
func ResolveModelAccessRule(rule ModelAccessRule, named []string) []string {
	models := rule.Models
	if rule.AccessGroupID != nil {
		if models == nil {
			models = named
		} else {
			models = intersectAccessModels(models, named)
		}
	}
	if models == nil {
		return nil
	}
	result := []string{}
	excluded := make(map[string]bool, len(rule.Exclude))
	for _, model := range rule.Exclude {
		excluded[model] = true
	}
	for _, model := range models {
		if !excluded[model] {
			result = append(result, model)
		}
	}
	return result
}
func intersectAccessModels(models, allowed []string) []string {
	permitted := make(map[string]bool, len(allowed))
	for _, model := range allowed {
		permitted[model] = true
	}
	result := []string{}
	for _, model := range models {
		if permitted[model] {
			result = append(result, model)
		}
	}
	return result
}

type modelAccessSnapshotKey struct{}

type authenticatedModelAccessKey struct{}
type authenticatedModelAccess struct {
	keyID              int64
	service            *APIKeyService
	reader             ModelAccessPolicyReader
	attachmentAttempts int
}

// Model rules alone do not authenticate a key. Retry authorization bypasses
// credential caches and reloads the user's full allowed-group membership.
// Billing checks remain with the middleware, including its read-only exceptions.
func (access authenticatedModelAccess) authorize(ctx context.Context, groupID *int64, checkGroup bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := access.service.apiKeyRepo.GetByID(ctx, access.keyID)
	if err != nil {
		return err
	}
	if key == nil || key.ID != access.keyID || (!key.IsActive() && key.Status != StatusAPIKeyExpired && key.Status != StatusAPIKeyQuotaExhausted) {
		return ErrAPIKeyNotFound
	}
	if userID, ok := ctx.Value(ctxkey.UserID).(int64); ok && userID != key.UserID {
		return ErrAPIKeyNotFound
	}
	if checkGroup && catalogGroupID(groupID) != catalogGroupID(key.GroupID) {
		return ErrGroupNotAllowed
	}
	if access.service.userRepo == nil {
		return ErrUserNotActive
	}
	user, err := access.service.userRepo.GetByID(ctx, key.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.ID != key.UserID || !user.IsActive() {
		return ErrUserNotActive
	}
	current := *key
	current.User = user
	if key.GroupID != nil {
		if access.service.groupRepo == nil {
			return ErrGroupNotAllowed
		}
		current.Group, err = access.service.groupRepo.GetByIDLite(ctx, *key.GroupID)
		if err != nil {
			return err
		}
	}
	if _, unavailable := current.groupUnavailableReason(); unavailable {
		return ErrGroupNotAllowed
	}
	return ctx.Err()
}

func withModelAccessSnapshot(ctx context.Context, snapshot ModelAccessSnapshot) context.Context {
	ctx = context.WithValue(ctx, modelAccessSnapshotKey{}, snapshot)
	return WithVisibleModelKeyPolicy(ctx, snapshot.visibleKeyModels())
}

func modelAccessAccountAllows(ctx context.Context, accountID int64, model string) bool {
	if ctx == nil {
		return true
	}
	snapshot, _ := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot)
	match := identity.NativeModelMatch("", model)
	return snapshot.AllowsCatalogAccountModel(accountID, match, match)
}

// AttachModelAccessPolicy bypasses credential caches: every authenticated
// request sees committed policy revisions on every instance.
func (s *APIKeyService) AttachModelAccessPolicy(ctx context.Context, keyID int64) (context.Context, error) {
	reader, ok := s.apiKeyRepo.(ModelAccessPolicyReader)
	if !ok {
		return ctx, nil
	}
	access := authenticatedModelAccess{keyID: keyID, service: s, reader: reader}
	_, preFrozen := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision)
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return ctx, err
		}
		snapshot, err := reader.ReadModelAccessSnapshot(ctx, keyID)
		if canceled := ctx.Err(); canceled != nil {
			return ctx, canceled
		}
		if err == nil {
			if attempt > 1 {
				if err := access.authorize(ctx, nil, false); err != nil {
					return ctx, err
				}
			}
			access.attachmentAttempts = attempt
			ctx = withModelAccessSnapshot(ctx, snapshot)
			if keyID > 0 {
				ctx = context.WithValue(ctx, authenticatedModelAccessKey{}, access)
			}
			return ctx, ctx.Err()
		}
		if keyID <= 0 || preFrozen || attempt >= visibleModelResolveMaxAttempts || !errors.Is(err, ErrVisibleModelRevisionChanged) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ctx, err
		}
	}
}

type ModelAccessImpact struct {
	Models          []string `json:"models"`
	FullyEnumerable bool     `json:"fully_enumerable"`
}

func (s *ModelAccessService) Preview(ctx context.Context, resource string, identifier, owner int64, rule ModelAccessRule, gateway *GatewayService, keys *APIKeyService) (ModelAccessImpact, error) {
	impact := ModelAccessImpact{Models: []string{}, FullyEnumerable: true}
	if _, err := s.Rule(ctx, resource, identifier, owner); err != nil {
		return impact, err
	}
	if !validAccessModels(rule.Models) || !validAccessModels(rule.Exclude) {
		return impact, ErrModelAccessExpansion
	}
	var named []string
	if rule.AccessGroupID != nil {
		groups, err := s.List(ctx)
		if err != nil {
			return impact, err
		}
		found := false
		for _, group := range groups {
			if group.ID == *rule.AccessGroupID {
				named = group.Models
				found = true
				break
			}
		}
		if !found {
			return impact, ErrModelAccessNotFound
		}
	}
	reader, ok := gateway.accountRepo.(ModelAccessPolicyReader)
	if !ok {
		return impact, ErrModelAccessNotFound
	}
	snapshot, err := reader.ReadModelAccessSnapshot(ctx, 0)
	if err != nil {
		return impact, err
	}
	policy := ResolveModelAccessRule(rule, named)
	var groupIDs []*int64
	platform := ""
	if resource == "keys" {
		key, err := keys.GetByID(ctx, identifier)
		if err != nil {
			return impact, err
		}
		groupIDs = append(groupIDs, key.GroupID)
		if key.Group != nil {
			platform = key.Group.Platform
		}
		snapshot.Key = policy
		snapshot.KeyExclude = rule.Exclude
	} else {
		account, err := gateway.accountRepo.GetByID(ctx, identifier)
		if err != nil {
			return impact, err
		}
		platform = account.Platform
		snapshot.Accounts[identifier] = policy
		snapshot.AccountExcludes[identifier] = rule.Exclude
		delete(snapshot.AccountRestrictions, identifier)
		for _, groupID := range account.GroupIDs {
			groupID := groupID
			groupIDs = append(groupIDs, &groupID)
		}
		if len(groupIDs) == 0 {
			groupIDs = append(groupIDs, nil)
		}
	}
	ctx = withModelAccessSnapshot(ctx, snapshot)
	members := map[string]bool{}
	for _, groupID := range groupIDs {
		groupPlatform := platform
		if groupID != nil {
			group, err := gateway.groupRepo.GetByIDLite(ctx, *groupID)
			if err != nil {
				return impact, err
			}
			if group == nil {
				return impact, ErrModelAccessNotFound
			}
			groupPlatform = group.Platform
		}
		view, err := gateway.VisibleModels(ctx, groupID, groupPlatform)
		if err != nil {
			return impact, err
		}
		impact.FullyEnumerable = impact.FullyEnumerable && view.FullyEnumerable
		for _, model := range view.Models {
			if resource == "keys" {
				members[model] = true
			} else {
				for _, binding := range view.Bindings[model] {
					if binding.AccountID == identifier {
						members[model] = true
					}
				}
			}
		}
	}
	for model := range members {
		impact.Models = append(impact.Models, model)
	}
	sort.Strings(impact.Models)
	return impact, nil
}
