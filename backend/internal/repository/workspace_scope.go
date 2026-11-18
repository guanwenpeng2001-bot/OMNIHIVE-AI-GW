package repository

import (
	"context"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// workspacePredicate is used by raw SQL paths alongside Ent's WorkspaceMixin.
// The column comes only from repository code and the value is a typed integer.
func workspacePredicate(ctx context.Context, column string) string {
	if workspaceID, scoped := domain.WorkspaceScope(ctx); scoped {
		return column + " = " + strconv.FormatInt(workspaceID, 10)
	}
	return "TRUE"
}

func workspaceClause(ctx context.Context, column string) string {
	if _, scoped := domain.WorkspaceScope(ctx); !scoped {
		return ""
	}
	return " AND " + workspacePredicate(ctx, column)
}

func workspaceUsagePredicate(ctx context.Context, keyColumn string) string {
	if _, scoped := domain.WorkspaceScope(ctx); !scoped {
		return "TRUE"
	}
	return keyColumn + " IN (SELECT id FROM api_keys WHERE " + workspacePredicate(ctx, "workspace_id") + ")"
}

func workspaceUsageConditions(ctx context.Context) []string {
	if _, scoped := domain.WorkspaceScope(ctx); scoped {
		return []string{workspaceUsagePredicate(ctx, "api_key_id")}
	}
	return make([]string, 0, 9)
}

func workspaceGroupPredicate(ctx context.Context, groupColumn string) string {
	if _, scoped := domain.WorkspaceScope(ctx); !scoped {
		return "TRUE"
	}
	return groupColumn + " IN (SELECT id FROM groups WHERE " + workspacePredicate(ctx, "workspace_id") + ")"
}

func workspaceMemberClause(ctx context.Context, userColumn string) string {
	if _, scoped := domain.WorkspaceScope(ctx); !scoped {
		return ""
	}
	return " AND " + userColumn + " IN (SELECT user_id FROM workspace_members WHERE " + workspacePredicate(ctx, "workspace_id") + ")"
}
