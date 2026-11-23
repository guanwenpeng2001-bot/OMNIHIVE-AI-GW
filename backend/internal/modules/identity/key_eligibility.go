package identity

import (
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type KeyState string

const (
	KeyActive         KeyState = "active"
	KeyDisabled       KeyState = "disabled"
	KeyExpired        KeyState = "expired"
	KeyQuotaExhausted KeyState = "quota_exhausted"
	KeyNoCredential   KeyState = "no_credential"
	KeyUnknown        KeyState = "unknown"
)

const (
	ProjectPolicyKey = "project_policy"
	LegacyGroupKey   = "legacy_group"
)

var ErrKeyAttribution = &fault.Fault{Status: 404, Reason: "KEY_ATTRIBUTION_CONFLICT", Message: "Key scope attribution is unavailable."}

func KeyLifecycle(status string, expiresAt *time.Time, now time.Time, exhausted, hasCredential bool) KeyState {
	switch {
	case status == "disabled" || status == "inactive":
		return KeyDisabled
	case status == "expired" || expiresAt != nil && !expiresAt.After(now):
		return KeyExpired
	case status == "quota_exhausted" || exhausted:
		return KeyQuotaExhausted
	case !hasCredential:
		return KeyNoCredential
	case status == "active":
		return KeyActive
	default:
		return KeyUnknown
	}
}

// ProjectKeyRow adds lifecycle data to the project management projection.
type ProjectKeyRow struct {
	Key
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	State     KeyState   `json:"state"`
	GroupID   *int64     `json:"group_id,string"`
	Kind      string     `json:"kind"`
}

// EffectiveKeyModels treats every non-nil list as a separate restriction.
// Identity resolution belongs to the bound catalog/group, so two lists may
// describe the same model using different names.
type EffectiveKeyModels struct {
	Ceiling, Rule, Named, Exclude []string
}

func IssuedModelCeiling(mode string, models []string) []string {
	switch mode {
	case "inherit":
		return nil
	case "allow_list":
		return append([]string{}, models...)
	default:
		return []string{}
	}
}

func IsStableModelID(id string) bool {
	if len(id) != 34 || !strings.HasPrefix(id, "m_") {
		return false
	}
	_, err := hex.DecodeString(id[2:])
	return err == nil
}

func (p EffectiveKeyModels) Allows(matches func(string) bool) bool {
	return p.AllowsWithExclusions(matches, matches)
}

func (p EffectiveKeyModels) AllowsWithExclusions(matches, excludes func(string) bool) bool {
	return (p.Ceiling == nil || slices.ContainsFunc(p.Ceiling, matches)) &&
		(p.Rule == nil || slices.ContainsFunc(p.Rule, matches)) &&
		(p.Named == nil || slices.ContainsFunc(p.Named, matches)) &&
		!slices.ContainsFunc(p.Exclude, excludes)
}

func (p EffectiveKeyModels) Restricted() bool {
	return p.Ceiling != nil || p.Rule != nil || p.Named != nil || len(p.Exclude) != 0
}

func (p EffectiveKeyModels) DeniesAll() bool {
	return p.Ceiling != nil && len(p.Ceiling) == 0 ||
		p.Rule != nil && len(p.Rule) == 0 || p.Named != nil && len(p.Named) == 0
}

// NativeModelMatch covers the stable ID and the public native name at the
// legacy gateway boundary, where the model's group/platform is already bound.
func NativeModelMatch(platform, model string) func(string) bool {
	var id string
	if platform != "" && model != "" {
		id = catalog.StableID(platform, model)
	}
	return func(entry string) bool {
		return entry != "" && (entry == model || entry == id)
	}
}

// AllowsIssuedKeyModel is used by credential-only compatibility callers.
// Model-policy consumers attach the current rules before authorizing a model.
func AllowsIssuedKeyModel(p *KeyPolicy, action string, models []string) bool {
	if p == nil || !slices.Contains(p.Actions, action) {
		return false
	}
	if KeyMetadataAction(action) {
		return true
	}
	ceiling := EffectiveKeyModels{Ceiling: IssuedModelCeiling(p.Mode, p.Models)}
	if len(models) == 0 {
		return ceiling.Ceiling == nil
	}
	for _, model := range models {
		if !ceiling.Allows(NativeModelMatch(p.Platform, model)) {
			return false
		}
	}
	return true
}

func KeyMetadataAction(action string) bool {
	return action == "models.read" || action == "usage.read" || action == "tasks.read"
}

func EffectiveKeyActions(issued []string, owner Access) ([]string, error) {
	allowed := slices.DeleteFunc(slices.Clone(issued), func(action string) bool {
		return action == "" || owner.Require(action) != nil
	})
	if len(allowed) == 0 {
		return nil, fault.Forbidden
	}
	return allowed, nil
}

// KeyScopeReader is implemented by the identity store while its shared
// GatewayScopes entry points are delegated during integration.
type KeyScopeReader interface {
	ReadProjectCredential(context.Context, string) (*KeyPolicy, error)
	ReadLegacyKeyScope(context.Context, int64, int64, int64, string) (scope.Authorized, error)
}

func ResolveProjectCredential(ctx context.Context, scopes GatewayScopes, digest string) (*KeyPolicy, error) {
	if reader, ok := scopes.(KeyScopeReader); ok {
		return reader.ReadProjectCredential(ctx, digest)
	}
	return scopes.ProjectCredential(ctx, digest)
}

func ResolveLegacyKeyScope(ctx context.Context, scopes GatewayScopes, w, key, owner int64, digest string) (scope.Authorized, error) {
	if reader, ok := scopes.(KeyScopeReader); ok {
		return reader.ReadLegacyKeyScope(ctx, w, key, owner, digest)
	}
	return scopes.LegacyScope(ctx, w, key, owner, digest)
}

// KeyAttributionJoins preserves Default attribution for unassigned legacy
// keys. Joined resources are checked even when their workspace disagrees.
const KeyAttributionJoins = `
 JOIN projects default_project ON default_project.workspace_id=k.workspace_id AND default_project.is_default
 LEFT JOIN project_key_policies issued ON issued.api_key_id=k.id
 LEFT JOIN project_resources key_resource ON key_resource.kind='api_keys' AND key_resource.resource_id=k.id
 JOIN projects key_project ON key_project.workspace_id=k.workspace_id
  AND key_project.id=coalesce(issued.project_id,key_resource.project_id,default_project.id)
  AND key_project.archived_at IS NULL`

const ValidKeyAttribution = `
 (key_resource.resource_id IS NULL OR key_resource.workspace_id=k.workspace_id)
 AND (issued.api_key_id IS NULL OR (issued.workspace_id,issued.project_id)=(key_resource.workspace_id,key_resource.project_id)
      AND key_resource.resource_id IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='api_keys' AND q.resource_id=k.id)`

const CurrentKeyCredential = `
 c.api_key_id=k.id AND c.workspace_id=k.workspace_id
 AND c.status=CASE WHEN issued.api_key_id IS NULL THEN 'current' ELSE 'project' END
 AND c.revoked_at IS NULL AND (c.valid_until IS NULL OR c.valid_until>now())`

// KeyGroupEntitlement assumes a workspace-checked group g and active owner u.
const KeyGroupEntitlement = `CASE WHEN g.subscription_type='subscription' THEN EXISTS (
 SELECT 1 FROM user_subscriptions s WHERE s.user_id=u.id AND s.group_id=g.id AND s.status='active'
   AND s.deleted_at IS NULL AND s.expires_at>now())
 ELSE (NOT g.is_exclusive AND NOT u.restrict_public_groups) OR EXISTS (
 SELECT 1 FROM user_allowed_groups ug WHERE ug.user_id=u.id AND ug.group_id=g.id) END`
