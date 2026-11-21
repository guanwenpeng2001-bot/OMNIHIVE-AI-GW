package postgres

import (
	"context"
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/lib/pq"
)

// Member administration and its reads use the same workspace-wide capability.
// Check it in the snapshot from which membership and grant versions are read.
func (s *Store) memberRead(ctx context.Context, actor, workspace int64) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	if _, err := evaluate(ctx, tx, actor, workspace, 0, "members.grants.write"); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (s *Store) Members(ctx context.Context, actor, workspace int64, page identity.Page) ([]identity.Member, error) {
	tx, err := s.memberRead(ctx, actor, workspace)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT m.user_id,u.email,u.username,m.role,u.status,m.grant_version
 FROM workspace_members m JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL
 WHERE m.workspace_id=$1 AND m.user_id>$2 AND m.created_at<=$3 ORDER BY m.user_id LIMIT $4`,
		workspace, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []identity.Member{}
	for rows.Next() {
		var m identity.Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Username, &m.Role, &m.Status, &m.Version); err != nil {
			return nil, err
		}
		// Grant endpoints identify the member by the user's ID.
		m.ID = m.UserID
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, tx.Commit()
}

func (s *Store) MemberGrants(ctx context.Context, actor, workspace, user int64) (identity.MemberGrants, error) {
	var g identity.MemberGrants
	tx, err := s.memberRead(ctx, actor, workspace)
	if err != nil {
		return g, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `WITH grants AS (
 SELECT g.project_id,g.action FROM project_grants g
 JOIN projects p ON p.workspace_id=g.workspace_id AND p.id=g.project_id AND p.archived_at IS NULL
 WHERE g.workspace_id=$1 AND g.user_id=$2)
 SELECT m.user_id,m.grant_version,m.updated_at,
 ARRAY(SELECT id::text FROM (SELECT DISTINCT project_id AS id FROM grants) projects ORDER BY id),
 ARRAY(SELECT DISTINCT action FROM grants ORDER BY action)
 FROM workspace_members m JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL
 WHERE m.workspace_id=$1 AND m.user_id=$2`, workspace, user).Scan(
		&g.MemberID, &g.Version, &g.EffectiveAt, pq.Array(&g.ProjectIDs), pq.Array(&g.Actions))
	if err != nil {
		return g, found(err)
	}
	g.EffectiveAt = g.EffectiveAt.UTC()
	return g, tx.Commit()
}
