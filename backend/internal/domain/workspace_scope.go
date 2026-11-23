package domain

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync"
)

// Deprecated: historical seed ID for compatibility with callers being migrated.
// Use ResolveDefaultWorkspaceID or the request-bound ResolvedDefaultWorkspaceID.
const DefaultWorkspaceID int64 = 1

type workspaceReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func ResolveDefaultWorkspaceID(ctx context.Context, db workspaceReader) (int64, error) {
	rows, err := db.QueryContext(ctx, "SELECT id FROM workspaces WHERE is_default")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, sql.ErrNoRows
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return 0, err
	}
	if id <= 0 {
		return 0, errors.New("invalid default workspace")
	}
	if rows.Next() {
		return 0, errors.New("multiple default workspaces")
	}
	return id, rows.Err()
}

type defaultWorkspaceKey struct{}

func WithDefaultWorkspaceID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, defaultWorkspaceKey{}, id)
}

// WithDefaultWorkspaceResolver resolves lazily and shares the result for one request.
func WithDefaultWorkspaceResolver(ctx context.Context, resolve func(context.Context) (int64, error)) context.Context {
	return context.WithValue(ctx, defaultWorkspaceKey{}, sync.OnceValues(func() (int64, error) { return resolve(ctx) }))
}
func ResolvedDefaultWorkspaceID(ctx context.Context) int64 {
	if resolve, ok := ctx.Value(defaultWorkspaceKey{}).(func() (int64, error)); ok {
		id, err := resolve()
		if err == nil {
			return id
		}
		return 0
	}
	id, _ := ctx.Value(defaultWorkspaceKey{}).(int64)
	return id
}

type workspaceScopeKey struct{}
type workspaceAdministrationKey struct{}

func WithWorkspaceAdministration(ctx context.Context) context.Context {
	return context.WithValue(ctx, workspaceAdministrationKey{}, true)
}

func WorkspaceAdministration(ctx context.Context) bool {
	authorized, _ := ctx.Value(workspaceAdministrationKey{}).(bool)
	return authorized
}

// WithWorkspaceScope constrains resource access after membership authorization.
// An absent scope is reserved for existing global administrator and system paths.
func WithWorkspaceScope(ctx context.Context, workspaceID int64) context.Context {
	return context.WithValue(ctx, workspaceScopeKey{}, workspaceID)
}

func WorkspaceScope(ctx context.Context) (int64, bool) {
	workspaceID, scoped := ctx.Value(workspaceScopeKey{}).(int64)
	return workspaceID, scoped
}

func ResourceWorkspaceID(ctx context.Context) int64 {
	if workspaceID, scoped := WorkspaceScope(ctx); scoped {
		return workspaceID
	}
	return ResolvedDefaultWorkspaceID(ctx)
}

// WorkspaceActorScope isolates write replay while preserving legacy default-domain keys.
func WorkspaceActorScope(ctx context.Context, actor string) string {
	if workspaceID, scoped := WorkspaceScope(ctx); scoped && workspaceID != ResolvedDefaultWorkspaceID(ctx) {
		return actor + ":workspace:" + strconv.FormatInt(workspaceID, 10)
	}
	return actor
}
