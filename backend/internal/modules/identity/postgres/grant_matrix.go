package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"slices"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/lib/pq"
)

func memberVersion(ctx context.Context, tx *sql.Tx, w, user int64, lock bool) (identity.GrantVersion, error) {
	var v identity.GrantVersion
	query := `SELECT m.user_id,m.grant_version,m.updated_at FROM workspace_members m
 JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL WHERE m.workspace_id=$1 AND m.user_id=$2`
	if lock {
		query += " FOR UPDATE OF m"
	}
	err := tx.QueryRowContext(ctx, query, w, user).Scan(&v.MemberID, &v.Version, &v.EffectiveAt)
	v.EffectiveAt = v.EffectiveAt.UTC()
	return v, found(err)
}

func readMatrix(ctx context.Context, tx *sql.Tx, w, user int64) ([]identity.ProjectGrant, error) {
	rows, err := tx.QueryContext(ctx, `SELECT g.project_id,array_agg(g.action ORDER BY g.action)
 FROM project_grants g JOIN projects p ON p.id=g.project_id AND p.workspace_id=g.workspace_id
 WHERE g.workspace_id=$1 AND g.user_id=$2 AND p.archived_at IS NULL GROUP BY g.project_id ORDER BY g.project_id`, w, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []identity.ProjectGrant{}
	for rows.Next() {
		var r identity.ProjectGrant
		if err = rows.Scan(&r.ProjectID, pq.Array(&r.Actions)); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func rectangular(rows []identity.ProjectGrant) bool {
	for i := 1; i < len(rows); i++ {
		if !slices.Equal(rows[0].Actions, rows[i].Actions) {
			return false
		}
	}
	return true
}

const grantCategories = "('member','project','key')"

func validateMatrix(ctx context.Context, tx *sql.Tx, a identity.Access, rows []identity.ProjectGrant, actions []string) error {
	ids := make(map[int64]bool, len(rows))
	all := map[string]bool{}
	for _, act := range actions {
		all[act] = true
	}
	for _, r := range rows {
		if r.ProjectID <= 0 || ids[r.ProjectID] || r.Actions == nil {
			return fault.Invalid("/project_grants")
		}
		ids[r.ProjectID] = true
		for _, act := range r.Actions {
			all[act] = true
		}
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE workspace_id=$1 AND archived_at IS NULL AND id=ANY($2)`, a.WorkspaceID, pq.Array(slices.Collect(maps.Keys(ids)))).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return fault.NotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_actions WHERE category IN `+grantCategories+` AND action=ANY($1)`, pq.Array(slices.Collect(maps.Keys(all)))).Scan(&count); err != nil {
		return err
	}
	if count != len(all) {
		return fault.Invalid("/actions")
	}
	for act := range all {
		if err := a.Require(act); err != nil {
			return err
		}
	}
	return nil
}

// Apply only new and removed pairs. A request approval passes replace=false
// to preserve every existing pair, including other actions on the same project.
func applyMatrix(ctx context.Context, tx *sql.Tx, a identity.Access, v identity.GrantVersion, rows []identity.ProjectGrant, replace bool) (identity.GrantVersion, []identity.ProjectGrant, error) {
	payload, err := json.Marshal(rows)
	if err != nil {
		return v, nil, err
	}
	const desired = `WITH desired AS (SELECT DISTINCT project_id,act AS action
 FROM jsonb_to_recordset($3::jsonb) AS r(project_id bigint,actions jsonb)
 CROSS JOIN LATERAL jsonb_array_elements_text(r.actions) act) `
	var protected bool
	err = tx.QueryRowContext(ctx, desired+`SELECT EXISTS (SELECT FROM desired d JOIN projects p ON p.id=d.project_id
 JOIN workspaces w ON w.id=p.workspace_id WHERE w.id=$1 AND w.is_default AND p.is_default AND NOT EXISTS
 (SELECT FROM project_grants g WHERE g.workspace_id=$1 AND g.user_id=$2 AND (g.project_id,g.action)=(d.project_id,d.action)))`, a.WorkspaceID, v.MemberID, string(payload)).Scan(&protected)
	if err != nil {
		return v, nil, err
	}
	if protected {
		return v, nil, fault.Invalid("/project_grants")
	}
	if replace {
		var dropsProtected bool
		err = tx.QueryRowContext(ctx, desired+`SELECT EXISTS(SELECT FROM project_grants g
        JOIN projects p ON p.workspace_id=g.workspace_id AND p.id=g.project_id JOIN workspaces w ON w.id=p.workspace_id
        WHERE w.id=$1 AND w.is_default AND p.is_default AND g.user_id=$2 AND NOT EXISTS
        (SELECT FROM desired d WHERE (d.project_id,d.action)=(g.project_id,g.action)))`, a.WorkspaceID, v.MemberID, string(payload)).Scan(&dropsProtected)
		if err != nil {
			return v, nil, err
		}
		if dropsProtected {
			return v, nil, &fault.Fault{Status: 409, Reason: "PROTECTED_DEFAULT_GRANTS", Message: "Historical shared Default grants require owner review."}
		}
	}
	var removed int64
	if replace {
		res, err := tx.ExecContext(ctx, desired+`DELETE FROM project_grants g USING projects p,workspaces w
 WHERE w.id=$1 AND g.workspace_id=w.id AND g.user_id=$2 AND p.workspace_id=w.id AND p.id=g.project_id
 AND p.archived_at IS NULL AND NOT(w.is_default AND p.is_default)
 AND NOT EXISTS(SELECT FROM desired d WHERE (d.project_id,d.action)=(g.project_id,g.action))`, a.WorkspaceID, v.MemberID, string(payload))
		if err != nil {
			return v, nil, err
		}
		removed, err = res.RowsAffected()
		if err != nil {
			return v, nil, err
		}
	}
	added := []identity.ProjectGrant{}
	result, err := tx.QueryContext(ctx, desired+`, added AS (INSERT INTO project_grants(workspace_id,user_id,project_id,action)
 SELECT $1,$2,project_id,action FROM desired ON CONFLICT DO NOTHING RETURNING project_id,action)
 SELECT project_id,array_agg(action ORDER BY action) FROM added GROUP BY project_id ORDER BY project_id`, a.WorkspaceID, v.MemberID, string(payload))
	if err != nil {
		return v, nil, err
	}
	defer result.Close()
	for result.Next() {
		var row identity.ProjectGrant
		if err = result.Scan(&row.ProjectID, pq.Array(&row.Actions)); err != nil {
			return v, nil, err
		}
		added = append(added, row)
	}
	if err = result.Err(); err != nil {
		return v, nil, err
	}
	if removed != 0 || len(added) != 0 {
		err = tx.QueryRowContext(ctx, `UPDATE workspace_members SET grant_version=grant_version+1,updated_at=now()
 WHERE workspace_id=$1 AND user_id=$2 RETURNING grant_version,updated_at`, a.WorkspaceID, v.MemberID).Scan(&v.Version, &v.EffectiveAt)
	}
	v.EffectiveAt = v.EffectiveAt.UTC()
	return v, added, err
}

func (s *Store) SetGrants(ctx context.Context, actor, w, user, expected int64, projects []int64, actions []string) (identity.GrantVersion, error) {
	if len(projects) > 256 {
		return identity.GrantVersion{}, fault.Invalid("/project_ids")
	}
	if len(actions) > 256 {
		return identity.GrantVersion{}, fault.Invalid("/actions")
	}
	rows := make([]identity.ProjectGrant, 0, len(projects))
	for _, id := range projects {
		rows = append(rows, identity.ProjectGrant{ProjectID: id, Actions: actions})
	}
	return s.replaceGrants(ctx, actor, w, user, expected, rows, actions, true)
}

func (s *Store) SetExactGrants(ctx context.Context, actor, w, user, expected int64, rows []identity.ProjectGrant) (identity.GrantVersion, error) {
	return s.replaceGrants(ctx, actor, w, user, expected, rows, nil, false)
}

func (s *Store) replaceGrants(ctx context.Context, actor, w, user, expected int64, rows []identity.ProjectGrant, actions []string, legacy bool) (identity.GrantVersion, error) {
	var out identity.GrantVersion
	err := s.write(ctx, actor, w, 0, "members.grants.write", func(tx *sql.Tx, a identity.Access) error {
		v, err := memberVersion(ctx, tx, w, user, true)
		if err != nil {
			return err
		}
		if v.Version != expected {
			return fault.Stale(v.Version)
		}
		if legacy {
			current, err := readMatrix(ctx, tx, w, user)
			if err != nil {
				return err
			}
			if !rectangular(current) {
				return &fault.Fault{Status: 409, Reason: "EXACT_GRANTS_REQUIRED", Message: "Submit the exact grants for each project."}
			}
		}
		if rows == nil {
			return fault.Invalid("/project_grants")
		}
		if err = validateMatrix(ctx, tx, a, rows, actions); err != nil {
			return err
		}
		out, _, err = applyMatrix(ctx, tx, a, v, rows, true)
		return err
	})
	return out, err
}
