package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type workspaceRepository struct{ database *sql.DB }

func NewWorkspaceRepository(database *sql.DB) service.WorkspaceRepository {
	return &workspaceRepository{database: database}
}

func (r *workspaceRepository) List(ctx context.Context, userID int64, globalAdmin bool) ([]service.Workspace, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT workspace.id, workspace.name, workspace.is_default,
		CASE WHEN $2 THEN 'admin' ELSE member.role END, workspace.created_at
		FROM workspaces workspace LEFT JOIN workspace_members member ON member.workspace_id=workspace.id AND member.user_id=$1
		WHERE $2 OR member.user_id IS NOT NULL ORDER BY workspace.id`, userID, globalAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workspaces := []service.Workspace{}
	for rows.Next() {
		var workspace service.Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.IsDefault, &workspace.Role, &workspace.CreatedAt); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func (r *workspaceRepository) Create(ctx context.Context, name string) (*service.Workspace, error) {
	workspace := &service.Workspace{Name: name, Role: "admin"}
	err := r.database.QueryRowContext(ctx, `INSERT INTO workspaces(name) VALUES($1) RETURNING id, created_at`, name).Scan(&workspace.ID, &workspace.CreatedAt)
	return workspace, err
}

func (r *workspaceRepository) Role(ctx context.Context, workspaceID, userID int64, globalAdmin bool) (string, error) {
	var role string
	err := r.database.QueryRowContext(ctx, `SELECT CASE WHEN $3 THEN 'admin' ELSE member.role END
		FROM workspaces workspace LEFT JOIN workspace_members member ON member.workspace_id=workspace.id AND member.user_id=$2
		WHERE workspace.id=$1 AND ($3 OR member.user_id IS NOT NULL)`, workspaceID, userID, globalAdmin).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", service.ErrWorkspaceNotFound
	}
	return role, err
}

func (r *workspaceRepository) Members(ctx context.Context, workspaceID int64) ([]service.WorkspaceMember, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT user_id, role FROM workspace_members WHERE workspace_id=$1 ORDER BY user_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

func (r *workspaceRepository) SetMember(ctx context.Context, workspaceID, userID int64, role string) error {
	result, err := r.database.ExecContext(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role)
		SELECT $1,id,$3 FROM users WHERE id=$2 AND deleted_at IS NULL
		ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=EXCLUDED.role,updated_at=NOW()`, workspaceID, userID, role)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return service.ErrWorkspaceNotFound
	}
	return err
}

func (r *workspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID int64) error {
	result, err := r.database.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return service.ErrWorkspaceNotFound
	}
	return err
}

func (r *workspaceRepository) ContainsResources(ctx context.Context, workspaceID int64, resource string, identifiers []int64) (bool, error) {
	var query string
	switch resource {
	case "users":
		query = `SELECT count(DISTINCT member.user_id) FROM workspace_members member JOIN users resource_user ON resource_user.id=member.user_id AND resource_user.deleted_at IS NULL WHERE member.workspace_id=$1 AND member.user_id=ANY($2)`
	case "accounts", "groups", "channels", "proxies", "api_keys", "channel_monitors", "channel_monitor_request_templates":
		query = fmt.Sprintf("SELECT count(DISTINCT id) FROM %s WHERE workspace_id=$1 AND id=ANY($2)", resource)
	case "scheduled_test_plans":
		query = `SELECT count(DISTINCT plan.id) FROM scheduled_test_plans plan JOIN accounts account ON account.id=plan.account_id WHERE account.workspace_id=$1 AND plan.id=ANY($2)`
	case "composite_model_routes":
		query = `SELECT count(DISTINCT route.id) FROM composite_model_routes route JOIN groups resource_group ON resource_group.id=route.group_id WHERE resource_group.workspace_id=$1 AND route.id=ANY($2)`
	default:
		return false, service.ErrWorkspaceNotFound
	}
	unique := map[int64]struct{}{}
	for _, identifier := range identifiers {
		if identifier <= 0 {
			return false, nil
		}
		unique[identifier] = struct{}{}
	}
	var count int
	err := r.database.QueryRowContext(ctx, query, workspaceID, pq.Array(identifiers)).Scan(&count)
	return count == len(unique), err
}
