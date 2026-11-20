package postgres

import (
	"context"
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

// Resource allocation crosses project boundaries. Evaluate projects.edit at
// workspace scope so a grant on just the destination cannot move a group.
func (s *Store) SetGroupProject(ctx context.Context, actor, w, p, group int64, bind bool, requestID string) (int64, error) {
	var dest int64
	err := s.write(ctx, actor, w, 0, "projects.edit", func(tx *sql.Tx, _ identity.Access) error {
		var def int64
		err := tx.QueryRowContext(ctx, `SELECT d.id FROM projects p JOIN projects d ON d.workspace_id=p.workspace_id AND d.is_default
			WHERE p.workspace_id=$1 AND p.id=$2 AND (NOT $3 OR p.archived_at IS NULL)`, w, p, bind).Scan(&def)
		if err != nil {
			return found(err)
		}
		var locked int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
			AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine WHERE kind='groups' AND resource_id=$2) FOR UPDATE`, w, group).Scan(&locked); err != nil {
			return found(err)
		}
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT project_id FROM project_resources WHERE kind='groups' AND workspace_id=$1 AND resource_id=$2),$3)`, w, group, def).Scan(&current); err != nil {
			return err
		}
		dest = p
		action := "project.group.bind"
		if !bind {
			dest, action = def, "project.group.unbind"
		}
		if current != def && current != p {
			return &fault.Fault{Status: 409, Reason: "GROUP_PROJECT_CONFLICT", Message: "Unbind the group from its current project first."}
		}
		if current == dest {
			return nil
		}
		var used bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys WHERE group_id=$1 AND deleted_at IS NULL)`, group).Scan(&used); err != nil {
			return err
		}
		if used {
			return &fault.Fault{Status: 409, Reason: "GROUP_IN_USE", Message: "Move or remove existing keys before reassigning the group."}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('groups',$1,$2,$3)
			ON CONFLICT(kind,resource_id) DO UPDATE SET project_id=excluded.project_id`, group, w, dest); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET scope_version=scope_version+1 WHERE id=$1`, w); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,action,request_id,status_code,extra) VALUES($1,$2,$3,200,
			jsonb_build_object('workspace_id',$4::bigint,'group_id',$5::bigint,'from_project_id',$6::bigint,'to_project_id',$7::bigint))`, actor, action, requestID, w, group, current, dest)
		return err
	})
	return dest, err
}
