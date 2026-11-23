package service

import (
	"context"
	"errors"
	"slices"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

// ReadRunSubscription bypasses the gateway cache for selected-key checks.
func (s *SubscriptionService) ReadRunSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	if s == nil || s.userSubRepo == nil {
		return nil, errors.New("subscription repository unavailable")
	}
	sub, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	copy := *sub
	return &copy, nil
}

func ModelAccessFromContext(ctx context.Context) (ModelAccessSnapshot, bool) {
	snapshot, ok := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot)
	return snapshot, ok
}

func (s ModelAccessSnapshot) keyModels() identity.EffectiveKeyModels {
	if s.KeyRestriction != nil {
		return *s.KeyRestriction
	}
	return identity.EffectiveKeyModels{Rule: s.Key, Exclude: s.KeyExclude}
}

func (s ModelAccessSnapshot) AllowsKeyModel(model string, matches, excludes func(string) bool) bool {
	return model != "" && s.KeyGroup.Allows(model) && s.keyModels().AllowsWithExclusions(matches, excludes)
}

func (s ModelAccessSnapshot) DeniesAllKeyModels() bool {
	return s.keyModels().DeniesAll()
}

func (s ModelAccessSnapshot) AllowsCatalogAccountModel(id int64, matches, excludes func(string) bool) bool {
	rule, ok := s.AccountRestrictions[id]
	if !ok {
		rule = identity.EffectiveKeyModels{Rule: s.Accounts[id], Exclude: s.AccountExcludes[id]}
	}
	return rule.AllowsWithExclusions(matches, excludes)
}

// visibleKeyModels adapts the decision to the legacy finite-list resolver.
// Native entries can prove a match against a stable ID in another list.
func (s ModelAccessSnapshot) visibleKeyModels() []string {
	if s.KeyRestriction == nil {
		return s.Key
	}
	p := s.keyModels()
	if p.Ceiling == nil && p.Rule == nil && p.Named == nil {
		return nil
	}
	out := []string{}
	added := map[string]bool{}
	for _, entries := range [][]string{p.Ceiling, p.Rule, p.Named} {
		for _, entry := range entries {
			if !p.Allows(identity.NativeModelMatch(s.ProjectKeyPlatform, entry)) {
				continue
			}
			if s.RestrictedKey && s.ProjectKeyPlatform != "" && !identity.IsStableModelID(entry) {
				entry = catalog.StableID(s.ProjectKeyPlatform, entry)
			}
			if !added[entry] {
				added[entry] = true
				out = append(out, entry)
			}
		}
	}
	return out
}

// WithIssuedKeyPolicy supports adapters that already hold an authenticated
// credential policy. The current database snapshot takes precedence.
func WithIssuedKeyPolicy(ctx context.Context, issued *identity.KeyPolicy) context.Context {
	if issued == nil {
		return ctx
	}
	snapshot, _ := ModelAccessFromContext(ctx)
	if snapshot.RestrictedKey {
		return ctx
	}
	policy := snapshot.keyModels()
	policy.Ceiling = identity.IssuedModelCeiling(issued.Mode, issued.Models)
	snapshot.ProjectKeyPlatform = issued.Platform
	snapshot.RestrictedKey = true
	snapshot.KeyRestriction = &policy
	return withModelAccessSnapshot(ctx, snapshot)
}

func AllowsAuthenticatedKeyModels(ctx context.Context, issued *identity.KeyPolicy, action string, models []string) bool {
	if _, ok := ModelAccessFromContext(ctx); !ok {
		return identity.AllowsIssuedKeyModel(issued, action, models)
	}
	if issued == nil || !slices.Contains(issued.Actions, action) {
		return false
	}
	if identity.KeyMetadataAction(action) {
		return true
	}
	snapshot, _ := ModelAccessFromContext(WithIssuedKeyPolicy(ctx, issued))
	if len(models) == 0 {
		return !snapshot.keyModels().Restricted()
	}
	for _, model := range models {
		match := identity.NativeModelMatch(snapshot.ProjectKeyPlatform, model)
		if !snapshot.AllowsKeyModel(model, match, match) {
			return false
		}
	}
	return true
}

func AuthenticatedKeyNeedsModel(ctx context.Context) bool {
	snapshot, _ := ModelAccessFromContext(ctx)
	return snapshot.keyModels().Restricted()
}

func AuthenticatedKeyDeniesAll(ctx context.Context) bool {
	snapshot, _ := ModelAccessFromContext(ctx)
	return snapshot.DeniesAllKeyModels()
}
