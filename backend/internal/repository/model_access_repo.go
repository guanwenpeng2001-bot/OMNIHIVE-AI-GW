package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	identitypostgres "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type modelAccessRepository struct{ database *sql.DB }

func NewModelAccessRepository(database *sql.DB) service.ModelAccessRepository {
	return &modelAccessRepository{database: database}
}

func modelAccessError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrModelAccessNotFound
	}
	var postgres *pq.Error
	if errors.As(err, &postgres) && (postgres.Code == "23503" || postgres.Code == "23505" || postgres.Code == "23001") {
		return service.ErrModelAccessConflict
	}
	return err
}
func (r *modelAccessRepository) List(ctx context.Context) ([]service.ModelAccessGroup, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT access.id,access.name,access.models,access.revision,(SELECT count(*) FROM model_access_rules rule WHERE rule.access_group_id=access.id) FROM model_access_groups access WHERE access.workspace_id=$1 ORDER BY access.name,access.id`, domain.ResourceWorkspaceID(ctx))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	groups := []service.ModelAccessGroup{}
	for rows.Next() {
		var group service.ModelAccessGroup
		var models []byte
		if err = rows.Scan(&group.ID, &group.Name, &models, &group.Revision, &group.References); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(models, &group.Models); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}
func (r *modelAccessRepository) SaveGroup(ctx context.Context, group service.ModelAccessGroup) (*service.ModelAccessGroup, error) {
	models, err := json.Marshal(group.Models)
	if err != nil {
		return nil, err
	}
	if group.ID == 0 {
		err = r.database.QueryRowContext(ctx, `INSERT INTO model_access_groups(workspace_id,name,models) VALUES($1,$2,$3) RETURNING id,revision`, domain.ResourceWorkspaceID(ctx), group.Name, models).Scan(&group.ID, &group.Revision)
	} else {
		err = r.database.QueryRowContext(ctx, `UPDATE model_access_groups SET name=$1,models=$2,revision=revision+1,updated_at=NOW() WHERE id=$3 AND workspace_id=$4 AND revision=$5 RETURNING revision`, group.Name, models, group.ID, domain.ResourceWorkspaceID(ctx), group.Revision).Scan(&group.Revision)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrModelAccessConflict
		}
	}
	return &group, modelAccessError(err)
}
func (r *modelAccessRepository) DeleteGroup(ctx context.Context, identifier, revision int64) error {
	result, err := r.database.ExecContext(ctx, `DELETE FROM model_access_groups WHERE id=$1 AND workspace_id=$2 AND revision=$3`, identifier, domain.ResourceWorkspaceID(ctx), revision)
	if err != nil {
		return modelAccessError(err)
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return service.ErrModelAccessConflict
	}
	return err
}
func accessResource(resource string) (string, string, error) {
	switch resource {
	case "accounts":
		return "accounts", "account_id", nil
	case "keys":
		return "api_keys", "api_key_id", nil
	}
	return "", "", service.ErrModelAccessNotFound
}

func lockAccessResource(ctx context.Context, tx *sql.Tx, resource string, identifier, owner int64) error {
	table, _, err := accessResource(resource)
	if err != nil {
		return err
	}
	ownerFilter := ""
	if owner != 0 {
		if resource != "keys" {
			return service.ErrModelAccessNotFound
		}
		ownerFilter = " AND user_id=$3"
	}
	args := []any{identifier, domain.ResourceWorkspaceID(ctx)}
	if owner != 0 {
		args = append(args, owner)
	}
	var found int64
	err = tx.QueryRowContext(ctx, fmt.Sprintf("SELECT id FROM %s WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL%s FOR UPDATE", table, ownerFilter), args...).Scan(&found)
	return modelAccessError(err)
}
func readAccessRule(ctx context.Context, tx *sql.Tx, resource string, identifier int64) (*service.ModelAccessRule, error) {
	_, column, err := accessResource(resource)
	if err != nil {
		return nil, err
	}
	rule := &service.ModelAccessRule{Exclude: []string{}}
	var models, exclude []byte
	err = tx.QueryRowContext(ctx, fmt.Sprintf("SELECT access_group_id,models,exclude,revision FROM model_access_rules WHERE %s=$1 AND workspace_id=$2", column), identifier, domain.ResourceWorkspaceID(ctx)).Scan(&rule.AccessGroupID, &models, &exclude, &rule.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return rule, nil
	}
	if err != nil {
		return nil, err
	}
	if len(models) > 0 {
		if err = json.Unmarshal(models, &rule.Models); err != nil {
			return nil, err
		}
	}
	if err = json.Unmarshal(exclude, &rule.Exclude); err != nil {
		return nil, err
	}
	return rule, nil
}
func (r *modelAccessRepository) Rule(ctx context.Context, resource string, identifier, owner int64) (*service.ModelAccessRule, error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockAccessResource(ctx, tx, resource, identifier, owner); err != nil {
		return nil, err
	}
	rule, err := readAccessRule(ctx, tx, resource, identifier)
	if err != nil {
		return nil, err
	}
	return rule, tx.Commit()
}
func (r *modelAccessRepository) SaveRule(ctx context.Context, resource string, identifier, owner int64, rule service.ModelAccessRule) error {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockAccessResource(ctx, tx, resource, identifier, owner); err != nil {
		return err
	}
	existing, err := readAccessRule(ctx, tx, resource, identifier)
	if err != nil {
		return err
	}
	if existing.Revision != rule.Revision {
		return service.ErrModelAccessConflict
	}
	if resource == "keys" {
		var mode, platform string
		var legacy []string
		err := tx.QueryRowContext(ctx, `SELECT p.model_mode,p.models,coalesce(g.platform,'') FROM project_key_policies p JOIN api_keys k ON k.id=p.api_key_id AND k.workspace_id=p.workspace_id LEFT JOIN groups g ON g.id=k.group_id AND g.workspace_id=k.workspace_id WHERE p.api_key_id=$1 AND p.workspace_id=$2 FOR SHARE OF p`, identifier, domain.ResourceWorkspaceID(ctx)).Scan(&mode, pq.Array(&legacy), &platform)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			rule, err = preserveLegacyKeyModelBoundary(rule, platform, mode, legacy)
			if err != nil {
				return err
			}
		}
	}
	if rule.AccessGroupID != nil {
		var encoded []byte
		err = tx.QueryRowContext(ctx, `SELECT models FROM model_access_groups WHERE id=$1 AND workspace_id=$2 FOR SHARE`, *rule.AccessGroupID, domain.ResourceWorkspaceID(ctx)).Scan(&encoded)
		if err != nil {
			return modelAccessError(err)
		}
		var named []string
		if err = json.Unmarshal(encoded, &named); err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, model := range named {
			allowed[model] = true
		}
		for _, model := range rule.Models {
			if !allowed[model] {
				return service.ErrModelAccessExpansion
			}
		}
	}
	if resource == "keys" && rule.Models != nil {
		var encoded []byte
		err = tx.QueryRowContext(ctx, `SELECT resource_group.model_allowlist FROM api_keys api_key LEFT JOIN groups resource_group ON resource_group.id=api_key.group_id WHERE api_key.id=$1`, identifier).Scan(&encoded)
		if err != nil {
			return err
		}
		if len(encoded) > 0 {
			var allowlist service.GroupModelAllowlist
			if err = json.Unmarshal(encoded, &allowlist); err != nil {
				return err
			}
			for _, model := range rule.Models {
				if !allowlist.Allows(model) {
					return service.ErrModelAccessExpansion
				}
			}
		}
	}
	if resource == "keys" {
		if err := identitypostgres.WriteKeyModelRule(ctx, tx, domain.ResourceWorkspaceID(ctx), identifier, rule); err != nil {
			if errors.Is(err, fault.Conflict) {
				return service.ErrModelAccessConflict
			}
			return err
		}
		return tx.Commit()
	}
	var models any
	if rule.Models != nil {
		encoded, marshalErr := json.Marshal(rule.Models)
		if marshalErr != nil {
			return marshalErr
		}
		models = encoded
	}
	if rule.Exclude == nil {
		rule.Exclude = []string{}
	}
	exclude, err := json.Marshal(rule.Exclude)
	if err != nil {
		return err
	}
	_, column, _ := accessResource(resource)
	// The upsert re-checks the revision after waiting on any concurrent writer,
	// so a stale writer cannot silently overwrite a newer committed rule.
	result, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO model_access_rules(workspace_id,%s,access_group_id,models,exclude) VALUES($1,$2,$3,$4,$5) ON CONFLICT(%s) DO UPDATE SET access_group_id=EXCLUDED.access_group_id,models=EXCLUDED.models,exclude=EXCLUDED.exclude,revision=model_access_rules.revision+1,updated_at=NOW() WHERE model_access_rules.revision=$6`, column, column), domain.ResourceWorkspaceID(ctx), identifier, rule.AccessGroupID, models, exclude, rule.Revision)
	if err != nil {
		return modelAccessError(err)
	}
	if affected, affectErr := result.RowsAffected(); affectErr == nil && affected == 0 {
		return service.ErrModelAccessConflict
	}
	return tx.Commit()
}

// The first v1 edit must not implicitly erase a historical project restriction.
// Fold that bound into the canonical rule before neutralizing the old columns.
// A later explicit edit can change the now-visible canonical rule under its CAS.
func preserveLegacyKeyModelBoundary(rule service.ModelAccessRule, platform, mode string, legacy []string) (service.ModelAccessRule, error) {
	if mode == "deny_all" {
		if len(rule.Models) > 0 {
			return rule, service.ErrModelAccessExpansion
		}
		rule.Models = []string{}
		return rule, nil
	}
	if mode != "allow_list" {
		return rule, nil
	}
	if rule.Models == nil {
		rule.Models = append([]string{}, legacy...)
		return rule, nil
	}
	canonical := func(model string) string {
		if platform == "" || len(model) == 34 && model[:2] == "m_" {
			return model
		}
		return catalog.StableID(platform, model)
	}
	for _, model := range rule.Models {
		if !slices.ContainsFunc(legacy, func(bound string) bool { return canonical(bound) == canonical(model) }) {
			return rule, service.ErrModelAccessExpansion
		}
	}
	return rule, nil
}

// One SQL statement takes a consistent snapshot of all applicable rules and
// named sets. No scheduler or credential cache can retain an old policy.
func readModelAccessSnapshot(ctx context.Context, executor sqlExecutor, keyID int64) (service.ModelAccessSnapshot, error) {
	snapshot := service.ModelAccessSnapshot{Accounts: map[int64][]string{}, AccountExcludes: map[int64][]string{}}
	generation, err := readAccessGeneration(ctx, executor)
	if err != nil {
		return snapshot, err
	}
	snapshot.Generation, snapshot.RevisionKnown = generation, true
	// Scoped callers read their own workspace only; an unscoped global view lists
	// accounts across workspaces, so it must load every workspace's rules.
	rows, err := executor.QueryContext(ctx, `SELECT rule.account_id,rule.api_key_id,rule.access_group_id,rule.models,rule.exclude,access.models FROM model_access_rules rule LEFT JOIN model_access_groups access ON access.id=rule.access_group_id AND access.workspace_id=rule.workspace_id WHERE `+workspacePredicate(ctx, "rule.workspace_id")+` AND (rule.account_id IS NOT NULL OR rule.api_key_id=$1)`, keyID)
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var accountID, key sql.NullInt64
		var rule service.ModelAccessRule
		var models, exclude, namedBytes []byte
		if err = rows.Scan(&accountID, &key, &rule.AccessGroupID, &models, &exclude, &namedBytes); err != nil {
			return snapshot, err
		}
		if len(models) > 0 {
			if err = json.Unmarshal(models, &rule.Models); err != nil {
				return snapshot, err
			}
		}
		if err = json.Unmarshal(exclude, &rule.Exclude); err != nil {
			return snapshot, err
		}
		var named []string
		if rule.AccessGroupID != nil {
			if len(namedBytes) == 0 {
				return snapshot, service.ErrModelAccessNotFound
			}
			if err = json.Unmarshal(namedBytes, &named); err != nil {
				return snapshot, err
			}
		}
		resolved := service.ResolveModelAccessRule(rule, named)
		if accountID.Valid {
			snapshot.Accounts[accountID.Int64] = resolved
			snapshot.AccountExcludes[accountID.Int64] = rule.Exclude
		} else {
			snapshot.Key = resolved
			snapshot.KeyExclude = rule.Exclude
		}
	}
	if err := rows.Err(); err != nil {
		return snapshot, err
	}
	_ = rows.Close()
	if keyID != 0 {
		groupRows, readErr := executor.QueryContext(ctx, `SELECT resource_group.model_allowlist, CASE WHEN project.api_key_id IS NOT NULL THEN coalesce(resource_group.platform,'') ELSE '' END,coalesce(project.model_mode,''),coalesce(project.models,ARRAY[]::text[])
 FROM api_keys api_key LEFT JOIN groups resource_group ON resource_group.id=api_key.group_id AND resource_group.workspace_id=api_key.workspace_id
 LEFT JOIN project_key_policies project ON project.api_key_id=api_key.id AND project.workspace_id=api_key.workspace_id
 WHERE api_key.id=$1 AND api_key.workspace_id=$2 AND api_key.deleted_at IS NULL`, keyID, domain.ResourceWorkspaceID(ctx))
		if readErr != nil {
			return snapshot, readErr
		}
		if !groupRows.Next() {
			readErr = groupRows.Err()
			_ = groupRows.Close()
			if readErr != nil {
				return snapshot, readErr
			}
			return snapshot, service.ErrModelAccessNotFound
		}
		var encoded []byte
		var legacyMode string
		var legacyModels []string
		readErr = groupRows.Scan(&encoded, &snapshot.ProjectKeyPlatform, &legacyMode, pq.Array(&legacyModels))
		_ = groupRows.Close()
		if readErr != nil {
			return snapshot, readErr
		}
		if len(encoded) > 0 {
			if readErr = json.Unmarshal(encoded, &snapshot.KeyGroup); readErr != nil {
				return snapshot, readErr
			}
		}
		if legacyMode != "" {
			_, models, excludes, _, ruleErr := identitypostgres.EffectiveKeyModelRule(ctx, executor, domain.ResourceWorkspaceID(ctx), keyID, snapshot.ProjectKeyPlatform, legacyMode, legacyModels)
			if ruleErr != nil {
				return snapshot, ruleErr
			}
			snapshot.Key, snapshot.KeyExclude = models, excludes
		}
	}
	latest, err := readAccessGeneration(ctx, executor)
	if err != nil {
		return snapshot, err
	}
	if latest != generation {
		return snapshot, service.ErrVisibleModelRevisionChanged
	}
	return snapshot, nil
}

func readAccessGeneration(ctx context.Context, executor sqlExecutor) (int64, error) {
	rows, err := executor.QueryContext(ctx, `SELECT generation FROM model_catalog_generations WHERE scope='catalog'`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var generation int64
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return 0, err
		}
		return 0, service.ErrVisibleModelRevisionChanged
	}
	if err = rows.Scan(&generation); err != nil {
		return 0, err
	}
	return generation, rows.Err()
}
func (r *apiKeyRepository) ReadModelAccessSnapshot(ctx context.Context, keyID int64) (service.ModelAccessSnapshot, error) {
	return readModelAccessSnapshot(ctx, r.sql, keyID)
}
func (r *accountRepository) ReadModelAccessSnapshot(ctx context.Context, keyID int64) (service.ModelAccessSnapshot, error) {
	return readModelAccessSnapshot(ctx, r.sql, keyID)
}
