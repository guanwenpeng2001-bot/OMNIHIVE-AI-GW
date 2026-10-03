package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Runs inside BulkUpdate's transaction before its first write. Lock all target
// special-auth rows, including inline ones, to serialize concurrent conversion
// to a reference or auth-mode changes with the subsequent JSONB merge.
func validateBulkCopilotNousCredentials(ctx context.Context, database credentialSQL, ids []int64, incoming map[string]any) error {
	_, relevant := incoming["auth_mode"]
	for key := range incoming {
		relevant = relevant || service.IsSensitiveCredentialKey(key)
	}
	if !relevant {
		return nil
	}
	rows, err := database.QueryContext(ctx, `SELECT platform, credentials FROM accounts
		WHERE id = ANY($1) AND deleted_at IS NULL AND platform IN ('github_copilot', 'nous_research')`+workspaceClause(ctx, "workspace_id")+` ORDER BY id FOR UPDATE`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		account := &service.Account{}
		var raw []byte
		if err := rows.Scan(&account.Platform, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &account.Credentials); err != nil {
			return err
		}
		if err := service.ValidateCopilotNousBulkCredentials(account, incoming); err != nil {
			return err
		}
	}
	return rows.Err()
}
