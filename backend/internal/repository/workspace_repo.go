package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type workspaceRepository struct{ database *sql.DB }

func NewWorkspaceRepository(database *sql.DB) service.WorkspaceRepository {
	return &workspaceRepository{database: database}
}

func (r *workspaceRepository) List(ctx context.Context, userID int64, _ bool) ([]service.Workspace, error) {
	rows, err := identitydb.New(r.database).Memberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]service.Workspace, 0, len(rows))
	for _, m := range rows {
		out = append(out, service.Workspace{ID: m.ID, Name: m.Name, IsDefault: m.IsDefault, CreatedAt: m.CreatedAt, Role: effectiveRole(m), EffectiveAdmin: m.EffectiveAdmin, MembershipRole: m.Role, RoleSource: m.RoleSource})
	}
	return out, nil
}

func effectiveRole(m identity.WorkspaceMembership) string {
	if m.EffectiveAdmin {
		return "admin"
	}
	if m.Role != nil {
		return *m.Role
	}
	return ""
}

func (r *workspaceRepository) DefaultWorkspaceID(ctx context.Context) (int64, error) {
	return domain.ResolveDefaultWorkspaceID(ctx, r.database)
}

func (r *workspaceRepository) Create(ctx context.Context, name string) (*service.Workspace, error) {
	workspace := &service.Workspace{Name: name, Role: "admin", EffectiveAdmin: true}
	err := r.database.QueryRowContext(ctx, `INSERT INTO workspaces(name) VALUES($1) RETURNING id, created_at`, name).Scan(&workspace.ID, &workspace.CreatedAt)
	return workspace, err
}

func (r *workspaceRepository) Role(ctx context.Context, workspaceID, userID int64, _ bool) (string, error) {
	m, err := identitydb.LookupMembership(ctx, r.database, userID, workspaceID)
	if errors.Is(err, fault.NotFound) {
		return "", service.ErrWorkspaceNotFound
	}
	return effectiveRole(m), err
}

func (r *workspaceRepository) Members(ctx context.Context, workspaceID int64) ([]service.WorkspaceMember, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT user_id, role FROM workspace_members WHERE workspace_id=$1 ORDER BY user_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	members := []service.WorkspaceMember{}
	for rows.Next() {
		var member service.WorkspaceMember
		if err := rows.Scan(&member.UserID, &member.Role); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *workspaceRepository) mutateMember(ctx context.Context, w int64, query string, args ...any) error {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, w).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrWorkspaceNotFound
		}
		return err
	}
	if actor, scoped := scope.From(ctx); scoped {
		if actor.WorkspaceID != w {
			return service.ErrWorkspaceNotFound
		}
		m, err := identitydb.LookupMembership(ctx, tx, actor.ActorID, w)
		if errors.Is(err, fault.NotFound) {
			return service.ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if !m.EffectiveAdmin {
			return service.ErrWorkspaceNotFound
		}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrWorkspaceNotFound
	}
	return tx.Commit()
}

func (r *workspaceRepository) SetMember(ctx context.Context, workspaceID, userID int64, role string) error {
	return r.mutateMember(ctx, workspaceID, `INSERT INTO workspace_members(workspace_id,user_id,role)
		SELECT $1,id,$3 FROM users WHERE id=$2 AND deleted_at IS NULL
		ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=EXCLUDED.role,inherits_global_role=false,updated_at=NOW()`, workspaceID, userID, role)

}

func (r *workspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID int64) error {
	return r.mutateMember(ctx, workspaceID, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)

}

func (r *workspaceRepository) ContainsResources(ctx context.Context, workspaceID int64, resource string, identifiers []int64) (bool, error) {
	var query string
	switch resource {
	case "users":
		query = `SELECT count(DISTINCT member.user_id) FROM workspace_members member JOIN users resource_user ON resource_user.id=member.user_id AND resource_user.deleted_at IS NULL WHERE member.workspace_id=$1 AND member.user_id=ANY($2)`
	case "accounts", "groups", "channels", "proxies", "proxy_groups", "api_keys", "channel_monitors", "channel_monitor_request_templates":
		query = fmt.Sprintf("SELECT count(DISTINCT id) FROM %s WHERE workspace_id=$1 AND id=ANY($2)", resource)
	case "scheduled_test_plans":
		query = `SELECT count(DISTINCT plan.id) FROM scheduled_test_plans plan JOIN accounts account ON account.id=plan.account_id WHERE account.workspace_id=$1 AND plan.id=ANY($2)`
	case "composite_model_routes":
		query = `SELECT count(DISTINCT route.id) FROM composite_model_routes route JOIN groups resource_group ON resource_group.id=route.group_id WHERE resource_group.workspace_id=$1 AND route.id=ANY($2)`
	default:
		return false, service.ErrWorkspaceNotFound
	}
	const maxResourceIDs = 1000
	if len(identifiers) > maxResourceIDs {
		return false, nil
	}
	if len(identifiers) == 0 {
		return true, nil
	}
	seen := make(map[int64]struct{}, len(identifiers))
	unique := make([]int64, 0, len(identifiers))
	for _, identifier := range identifiers {
		if identifier <= 0 {
			return false, nil
		}
		if _, exists := seen[identifier]; !exists {
			seen[identifier] = struct{}{}
			unique = append(unique, identifier)
		}
	}
	var count int
	err := r.database.QueryRowContext(ctx, query, workspaceID, pq.Array(unique)).Scan(&count)
	return count == len(unique), err
}
