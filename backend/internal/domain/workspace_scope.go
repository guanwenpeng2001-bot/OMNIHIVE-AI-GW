package domain

import (
	"context"
	"strconv"
)

// DefaultWorkspaceID preserves the ownership of resources created before delegation.
const DefaultWorkspaceID int64 = 1

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
	return DefaultWorkspaceID
}

// WorkspaceActorScope isolates write replay while preserving legacy default-domain keys.
func WorkspaceActorScope(ctx context.Context, actor string) string {
	if workspaceID, scoped := WorkspaceScope(ctx); scoped && workspaceID != DefaultWorkspaceID {
		return actor + ":workspace:" + strconv.FormatInt(workspaceID, 10)
	}
	return actor
}
