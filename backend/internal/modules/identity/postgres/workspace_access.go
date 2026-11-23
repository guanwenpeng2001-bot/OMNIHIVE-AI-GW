package postgres

import (
	"context"
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/lib/pq"
)

const workspaceMembership = `SELECT w.id,w.name,w.is_default,w.created_at,m.role,
 CASE WHEN m.id IS NULL THEN NULL WHEN m.inherits_global_role THEN 'inherited' ELSE 'delegated' END,
 coalesce(` + administrator + `,false)` + membership + ` WHERE ` + memberFilter

func scanMembership(row interface{ Scan(...any) error }) (identity.WorkspaceMembership, error) {
	var m identity.WorkspaceMembership
	err := row.Scan(&m.ID, &m.Name, &m.IsDefault, &m.CreatedAt, &m.Role, &m.RoleSource, &m.EffectiveAdmin)
	return m, found(err)
}

// LookupMembership reads actual membership and effective authority in the caller's snapshot.
func LookupMembership(ctx context.Context, q queryer, actor, w int64) (identity.WorkspaceMembership, error) {
	return scanMembership(q.QueryRowContext(ctx, workspaceMembership+" AND w.id=$2", actor, w))
}

func (s *Store) Memberships(ctx context.Context, actor int64) ([]identity.WorkspaceMembership, error) {
	rows, err := s.db.QueryContext(ctx, workspaceMembership+" ORDER BY w.id", actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.WorkspaceMembership{}
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) WorkspaceAccess(ctx context.Context, actor, w int64) (identity.WorkspaceAccess, error) {
	var out identity.WorkspaceAccess
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, err := evaluate(ctx, tx, actor, w, 0, "")
	if err != nil {
		return out, err
	}
	m, err := LookupMembership(ctx, tx, actor, w)
	if err != nil {
		return out, err
	}
	out = identity.WorkspaceAccess{ActorID: a.ActorID, WorkspaceID: w, Role: m.Role, RoleSource: m.RoleSource, EffectiveAdmin: m.EffectiveAdmin, Actions: a.Actions, Version: a.Version, ExpiresAt: a.ExpiresAt}
	if a.Require("members.grants.write") == nil {
		var acts []string
		err = tx.QueryRowContext(ctx, `SELECT ARRAY(SELECT action FROM identity_actions WHERE category IN `+grantCategories+` AND action=ANY($1) ORDER BY action)`, pq.Array(a.Actions)).Scan(pq.Array(&acts))
		if err != nil {
			return out, err
		}
		out.GrantableActions = &acts
	}
	return out, tx.Commit()
}

func (s *Store) ProjectAccess(ctx context.Context, actor, w, p int64) (identity.ProjectAccess, error) {
	var out identity.ProjectAccess
	a, err := s.Evaluate(ctx, actor, w, p, "")
	if err != nil {
		return out, err
	}
	out.Access = a
	err = s.db.QueryRowContext(ctx, `SELECT ARRAY(SELECT action FROM identity_actions WHERE (category='key' OR action='models.read') AND action=ANY($1) ORDER BY action)`, pq.Array(a.Actions)).Scan(pq.Array(&out.IssuableKeyActions))
	return out, err
}
