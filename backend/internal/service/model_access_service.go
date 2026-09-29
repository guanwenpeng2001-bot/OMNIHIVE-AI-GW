package service

import (
	"context"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
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
	KeyGroup        GroupModelAllowlist
	Generation      int64
	RevisionKnown   bool
	Key             []string
	Accounts        map[int64][]string
	AccountExcludes map[int64][]string
	KeyExclude      []string
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

func withModelAccessSnapshot(ctx context.Context, snapshot ModelAccessSnapshot) context.Context {
	ctx = context.WithValue(ctx, modelAccessSnapshotKey{}, snapshot)
	return WithVisibleModelKeyPolicy(ctx, snapshot.Key)
}
func modelAccessAccountAllows(ctx context.Context, accountID int64, model string) bool {
	if ctx == nil {
		return true
	}
	snapshot, _ := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot)
	for _, excluded := range snapshot.AccountExcludes[accountID] {
		if excluded == model {
			return false
		}
	}
	models := snapshot.Accounts[accountID]
	if models == nil {
		return true
	}
	for _, allowed := range models {
		if allowed == model {
			return true
		}
	}
	return false
}

// AttachModelAccessPolicy bypasses credential caches: every authenticated
// request sees committed policy revisions on every instance.
func (s *APIKeyService) AttachModelAccessPolicy(ctx context.Context, keyID int64) (context.Context, error) {
	reader, ok := s.apiKeyRepo.(ModelAccessPolicyReader)
	if !ok {
		return ctx, nil
	}
	snapshot, err := reader.ReadModelAccessSnapshot(ctx, keyID)
	if err != nil {
		return ctx, err
	}
	return withModelAccessSnapshot(ctx, snapshot), nil
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
