package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

// AuthorizeKeyUse applies the same identity boundary to preflight and execution.
// Models are native names in the key's provider namespace, not an unqualified
// cross-provider alias set. KeyPolicy.Allows also resolves their stable IDs.
// The caller must additionally apply the shared model-access/routing policy.
func (s *Store) AuthorizeKeyUse(ctx context.Context, actor, w, p, key int64, action string, models []string) (identity.KeyUse, error) {
	var use identity.KeyUse
	if err := s.RequireResource(ctx, actor, w, p, "", "api_keys", key); err != nil {
		return use, err
	}
	access, err := s.Evaluate(ctx, actor, w, p, "")
	if err != nil {
		return use, err
	}
	var digest string
	var project bool
	err = s.db.QueryRowContext(ctx, `SELECT k.user_id,coalesce(k.group_id,0),coalesce(g.platform,''),c.credential_digest,c.status='project'
 FROM api_keys k JOIN users u ON u.id=k.user_id AND u.status='active' AND u.deleted_at IS NULL
 LEFT JOIN groups g ON g.id=k.group_id AND g.workspace_id=k.workspace_id AND g.status='active' AND g.deleted_at IS NULL
 JOIN api_key_credentials c ON c.api_key_id=k.id AND c.workspace_id=k.workspace_id
 WHERE k.id=$1 AND k.workspace_id=$2 AND k.deleted_at IS NULL AND k.status='active'
 AND (k.expires_at IS NULL OR k.expires_at>now())
 AND c.status IN ('current','project') AND c.revoked_at IS NULL AND (c.valid_until IS NULL OR c.valid_until>now())
 ORDER BY c.id DESC LIMIT 1`, key, w).Scan(&use.OwnerID, &use.GroupID, &use.Platform, &digest, &project)
	if errors.Is(err, sql.ErrNoRows) {
		return use, fault.NotFound
	}
	if err != nil {
		return use, err
	}
	if use.GroupID == 0 || use.Platform == "" {
		return use, fault.Forbidden
	}
	if use.OwnerID != actor && access.Require("keys.act_as") != nil {
		return use, fault.NotFound
	}
	if project {
		policy, err := s.ProjectCredential(ctx, digest)
		if err != nil {
			return use, err
		}
		if policy == nil || policy.Scope.WorkspaceID != w || policy.Scope.ProjectID != p || policy.Scope.KeyID != key {
			return use, fault.NotFound
		}
		if !policy.Allows(action, models) {
			return use, fault.Forbidden
		}
	} else {
		base, err := s.LegacyScope(ctx, w, key, use.OwnerID, digest)
		if err != nil {
			return use, err
		}
		if base.ProjectID != p {
			return use, fault.NotFound
		}
	}
	return use, nil
}
