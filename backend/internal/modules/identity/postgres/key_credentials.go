package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

// ReadProjectCredential is the project implementation of GatewayScopes.
func (s *Store) ReadProjectCredential(ctx context.Context, digest string) (*identity.KeyPolicy, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var policy identity.KeyPolicy
	var attributed bool
	a := &policy.Scope
	err = tx.QueryRowContext(ctx, `SELECT k.id,k.user_id,k.workspace_id,key_project.id,issued.actions,issued.model_mode,issued.models,
 ARRAY(SELECT ug.group_id FROM user_allowed_groups ug WHERE ug.user_id=k.user_id),
 coalesce(k.group_id,0),coalesce(g.platform,''),rpm.rpm_override,coalesce((`+identity.ValidKeyAttribution+`),false)
 FROM api_keys k`+identity.KeyAttributionJoins+`
 JOIN api_key_credentials c ON `+identity.CurrentKeyCredential+`
 LEFT JOIN groups g ON g.id=k.group_id AND g.workspace_id=k.workspace_id AND g.deleted_at IS NULL
 LEFT JOIN user_group_rate_multipliers rpm ON rpm.user_id=k.user_id AND rpm.group_id=k.group_id
 WHERE c.credential_digest=$1 AND c.status='project' AND k.deleted_at IS NULL
 AND (NOT issued.member_bound OR issued.issued_membership IS NOT NULL)`, digest).
		Scan(&a.KeyID, &a.KeyOwnerID, &a.WorkspaceID, &a.ProjectID, pq.Array(&policy.Actions), &policy.Mode,
			pq.Array(&policy.Models), pq.Array(&policy.AllowedGroups), &policy.GroupID, &policy.Platform, &policy.GroupRPMOverride, &attributed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !attributed {
		return nil, identity.ErrKeyAttribution
	}
	owner, err := evaluate(ctx, tx, a.KeyOwnerID, a.WorkspaceID, a.ProjectID, "")
	if err != nil {
		return nil, err
	}
	policy.Actions, err = identity.EffectiveKeyActions(policy.Actions, owner)
	if err != nil {
		return nil, err
	}
	a.ActorID, a.FundingUserID, a.Version = a.KeyOwnerID, a.KeyOwnerID, owner.Version
	return &policy, tx.Commit()
}

// ReadLegacyKeyScope retains grace authentication and the established Default
// rule for legacy credentials without an explicit mapping.
func (s *Store) ReadLegacyKeyScope(ctx context.Context, w, key, owner int64, digest string) (scope.Authorized, error) {
	a := scope.Authorized{WorkspaceID: w, KeyID: key, ActorID: owner, KeyOwnerID: owner, FundingUserID: owner}
	err := s.db.QueryRowContext(ctx, `SELECT p.id,w.scope_version
 FROM api_keys k`+identity.KeyAttributionJoins+`
 JOIN workspaces w ON w.id=k.workspace_id JOIN projects p ON p.id=key_project.id
 WHERE k.id=$1 AND k.workspace_id=$2 AND k.user_id=$3 AND k.deleted_at IS NULL
 AND issued.api_key_id IS NULL AND `+identity.ValidKeyAttribution+`
 AND (p.is_default OR EXISTS (
   SELECT 1 FROM users u LEFT JOIN workspace_members m ON m.workspace_id=w.id AND m.user_id=u.id
   WHERE u.id=k.user_id AND u.deleted_at IS NULL AND u.status='active' AND `+legacyProjectPermission+`))
 AND EXISTS (SELECT 1 FROM api_key_credentials c WHERE c.api_key_id=k.id AND c.workspace_id=k.workspace_id
   AND c.credential_digest=$4 AND c.revoked_at IS NULL AND c.status IN ('current','grace')
   AND (c.valid_until IS NULL OR c.valid_until>now()))`, key, w, owner, digest).Scan(&a.ProjectID, &a.Version)
	return a, found(err)
}
