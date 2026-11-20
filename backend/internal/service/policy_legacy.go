package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

// LegacyRoutingIntent returns the original typed matching and fallback fields for
// shadow comparison. Callers use Group.ModelAllowlist.Allows and
// CompositeModelRoute.EffectiveTargets, including legacy single-target defaults.
func LegacyRoutingIntent(ctx context.Context, db *sql.DB, workspace, project, group int64) (Group, []CompositeModelRoute, error) {
	var g Group
	routes := []CompositeModelRoute{}
	rows, err := db.QueryContext(ctx, `SELECT source_kind,fields FROM routing_legacy_intents WHERE workspace_id=$1 AND project_id=$2 AND format_version=1 AND
 (source_kind='group' AND source_id=$3 OR source_kind='composite' AND (fields->>'group_id')::bigint=$3) ORDER BY source_kind,source_id`, workspace, project, group)
	if err != nil {
		return g, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var raw []byte
		if err := rows.Scan(&kind, &raw); err != nil {
			return g, nil, err
		}
		if kind == "group" {
			if err := json.Unmarshal(raw, &g); err != nil {
				return g, nil, err
			}
			continue
		}
		var r CompositeModelRoute
		if err := json.Unmarshal(raw, &r); err != nil {
			return g, nil, err
		}
		routes = append(routes, r)
	}
	if err := rows.Err(); err != nil {
		return g, nil, err
	}
	if g.ID != group || g.ID <= 0 {
		return g, nil, fault.NotFound
	}
	return g, routes, nil
}

// The old mixed MCP handler stores these user preferences in process memory.
// Import only a reviewed export from its actual principal before retiring it.
type LegacyRoutingPreferences struct {
	Strategy    string      `json:"strategy_mode"`
	ZDR         bool        `json:"zdr_enabled"`
	Fallback    bool        `json:"allow_fallback"`
	PromptCache bool        `json:"auto_prompt_cache"`
	Region      string      `json:"region_preference"`
	Providers   []string    `json:"provider_whitelist"`
	Budget      json.Number `json:"monthly_budget_usd"`
	Threshold   int         `json:"alert_threshold_percent"`
	Updated     time.Time   `json:"updated_at"`
}

func ImportRoutingPreferences(ctx context.Context, db *sql.DB, user, workspace, project int64, v LegacyRoutingPreferences) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `INSERT INTO routing_legacy_intents(source_kind,source_id,workspace_id,project_id,fields)
 SELECT 'user_preferences',user_id,workspace_id,project_id,$4::jsonb FROM routing_account_preference_owners
 WHERE user_id=$1 AND workspace_id=$2 AND project_id=$3
 ON CONFLICT(source_kind,source_id) DO UPDATE SET workspace_id=excluded.workspace_id,project_id=excluded.project_id,fields=excluded.fields,captured_at=now()`, user, workspace, project, string(raw))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return fault.Forbidden
	}
	return err
}
