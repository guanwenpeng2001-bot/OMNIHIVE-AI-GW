package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

// WriteKeyModelRule is the sole model-rule command for v1 edits and v2 issuance.
// Callers authorize and lock the key in this same transaction. The old project
// columns are neutral compatibility fields, never a second editable model list.
func WriteKeyModelRule(ctx context.Context, tx *sql.Tx, workspace, key int64, rule identity.ModelRule) error {
	var models any
	if rule.Models != nil {
		encoded, err := json.Marshal(rule.Models)
		if err != nil {
			return err
		}
		models = string(encoded)
	}
	if rule.Exclude == nil {
		rule.Exclude = []string{}
	}
	excluded, err := json.Marshal(rule.Exclude)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO model_access_rules(workspace_id,api_key_id,access_group_id,models,exclude) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(api_key_id) DO UPDATE SET access_group_id=EXCLUDED.access_group_id,models=EXCLUDED.models,exclude=EXCLUDED.exclude,revision=model_access_rules.revision+1,updated_at=now()
 WHERE model_access_rules.workspace_id=EXCLUDED.workspace_id AND model_access_rules.revision=$6`, workspace, key, rule.AccessGroupID, models, string(excluded), rule.Revision)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return fault.Conflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE project_key_policies SET model_mode='inherit',models=ARRAY[]::text[] WHERE api_key_id=$1 AND workspace_id=$2`, key, workspace)
	return err
}

type ModelRuleQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// EffectiveKeyModelRule preserves restrictive historical conflicts until an
// explicit canonical write converges them. Every reader uses this interpretation.
func EffectiveKeyModelRule(ctx context.Context, db ModelRuleQueryer, workspace, key int64, platform, legacyMode string, legacyModels []string) (mode string, models, excludes []string, source string, err error) {
	mode, source = "inherit", "model_access_rules"
	rows, err := db.QueryContext(ctx, `SELECT r.access_group_id,r.models,r.exclude,g.models FROM model_access_rules r LEFT JOIN model_access_groups g ON g.id=r.access_group_id AND g.workspace_id=r.workspace_id WHERE r.workspace_id=$1 AND r.api_key_id=$2`, workspace, key)
	if err != nil {
		return mode, nil, nil, source, err
	}
	defer rows.Close()
	normalize := func(values []string) []string {
		if values == nil {
			return nil
		}
		out := make([]string, 0, len(values))
		for _, model := range values {
			if platform != "" && !catalogModelID(model) {
				model = catalog.StableID(platform, model)
			}
			out = append(out, model)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	var rule identity.ModelRule
	if rows.Next() {
		var raw, excluded, namedRaw []byte
		if err = rows.Scan(&rule.AccessGroupID, &raw, &excluded, &namedRaw); err != nil {
			return mode, nil, nil, source, err
		}
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &rule.Models); err != nil {
				return mode, nil, nil, source, err
			}
		}
		if err = json.Unmarshal(excluded, &rule.Exclude); err != nil {
			return mode, nil, nil, source, err
		}
		var named []string
		if rule.AccessGroupID != nil {
			if len(namedRaw) == 0 {
				return mode, nil, nil, source, fault.NotFound
			}
			if err = json.Unmarshal(namedRaw, &named); err != nil {
				return mode, nil, nil, source, err
			}
		}
		rule.Models, rule.Exclude, named = normalize(rule.Models), normalize(rule.Exclude), normalize(named)
		models = identity.ResolveModelRule(rule, named)
		excludes = rule.Exclude
	} else {
		source = "legacy_only"
	}
	if err = rows.Err(); err != nil {
		return mode, nil, nil, source, err
	}
	models, excludes = normalize(models), normalize(excludes)
	if legacyMode == "deny_all" {
		models = []string{}
		source = "legacy_intersection"
	}
	if legacyMode == "allow_list" {
		legacy := normalize(legacyModels)
		if models == nil {
			models = legacy
		} else {
			models = slices.DeleteFunc(models, func(model string) bool { return !slices.Contains(legacy, model) })
		}
		source = "legacy_intersection"
	}
	if models != nil {
		models = slices.DeleteFunc(models, func(model string) bool { return slices.Contains(excludes, model) })
		mode = "allow_list"
		if len(models) == 0 {
			mode = "deny_all"
		}
	}
	return mode, models, excludes, source, nil
}
