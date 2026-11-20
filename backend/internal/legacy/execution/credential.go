package executioncompat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type Validator struct {
	DB     *sql.DB
	Scopes identity.GatewayScopes
}

type validatorKey struct{}

func (v Validator) Install(ctx context.Context) context.Context {
	return context.WithValue(ctx, validatorKey{}, v)
}

// Bind only the digest of the credential accepted by the native extractor.
func BindCredential(ctx context.Context, digest, action, clientIP string, project bool, group int64) context.Context {
	v, ok := ctx.Value(validatorKey{}).(Validator)
	if !ok {
		return ctx
	}
	return journal.WithReauthorization(ctx, func(ctx context.Context, frozen scope.Authorized, model string) error {
		if v.DB == nil || v.Scopes == nil {
			return journal.ErrScope
		}
		var current scope.Authorized
		modelNames := []string{model}
		var platform string
		if project {
			p, err := v.Scopes.ProjectCredential(ctx, digest)
			if errors.Is(err, fault.NotFound) || errors.Is(err, fault.Forbidden) {
				return journal.ErrScope
			}
			if err != nil {
				return err
			}
			if p == nil || !p.Allows(action, []string{model}) {
				return journal.ErrScope
			}
			current = p.Scope
			platform = p.Platform
			if platform != "" {
				modelNames = append(modelNames, catalog.StableID(platform, model))
			}
		} else {
			s, err := v.Scopes.LegacyScope(ctx, frozen.WorkspaceID, frozen.KeyID, frozen.KeyOwnerID, digest)
			if errors.Is(err, fault.NotFound) || errors.Is(err, fault.Forbidden) {
				return journal.ErrScope
			}
			if err != nil {
				return err
			}
			current = s
		}
		if current != frozen {
			return journal.ErrScope
		}
		var white, black, groupModels []byte
		err := v.DB.QueryRowContext(ctx, `SELECT k.ip_whitelist,k.ip_blacklist,g.model_allowlist FROM api_keys k JOIN users u ON u.id=k.user_id
 LEFT JOIN groups g ON g.id=k.group_id WHERE k.id=$1 AND k.workspace_id=$2 AND k.user_id=$3 AND coalesce(k.group_id,0)=$4
 AND ($6='' OR g.platform=$6)
 AND NOT EXISTS (SELECT 1 FROM model_access_rules r LEFT JOIN model_access_groups m
 ON m.id=r.access_group_id AND m.workspace_id=r.workspace_id WHERE r.api_key_id=k.id AND r.workspace_id=k.workspace_id
 AND (r.exclude ?| $5 OR (r.models IS NOT NULL AND NOT r.models ?| $5) OR (r.access_group_id IS NOT NULL AND NOT coalesce(m.models ?| $5,false))))
 AND k.deleted_at IS NULL AND u.deleted_at IS NULL AND k.status='active' AND u.status='active'
 AND (k.expires_at IS NULL OR k.expires_at>now()) AND (k.quota=0 OR k.quota_used<k.quota)
 AND (k.group_id IS NULL OR (g.deleted_at IS NULL AND g.status='active' AND
 (g.subscription_type='subscription' OR (NOT g.is_exclusive AND NOT u.restrict_public_groups)
 OR EXISTS(SELECT 1 FROM user_allowed_groups ug WHERE ug.user_id=u.id AND ug.group_id=g.id))))`,
			frozen.KeyID, frozen.WorkspaceID, frozen.KeyOwnerID, group, pq.Array(modelNames), platform).Scan(&white, &black, &groupModels)
		if err == sql.ErrNoRows {
			return journal.ErrScope
		}
		if err != nil {
			return err
		}
		var allowlist service.GroupModelAllowlist
		if len(groupModels) != 0 {
			if err := json.Unmarshal(groupModels, &allowlist); err != nil {
				return err
			}
			if !allowlist.Allows(model) {
				return journal.ErrScope
			}
		}
		var allow, deny []string
		if len(white) > 0 {
			if err = json.Unmarshal(white, &allow); err != nil {
				return err
			}
		}
		if len(black) > 0 {
			if err = json.Unmarshal(black, &deny); err != nil {
				return err
			}
		}
		if len(allow)+len(deny) > 0 {
			if allowed, _ := ip.CheckIPRestriction(clientIP, allow, deny); !allowed {
				return journal.ErrScope
			}
		}
		return nil
	})
}
