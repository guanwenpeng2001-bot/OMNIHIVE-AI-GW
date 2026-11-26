package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

const (
	modelSelectionRepairReason       = "system_v039_model_selection_repair"
	modelSelectionRepairAdvisoryLock = migrationsAdvisoryLockID + 39
)

type modelSelectionRepairBackup struct {
	Scope              json.RawMessage `json:"scope"`
	ScopeExists        bool            `json:"scope_exists"`
	Frozen             json.RawMessage `json:"frozen"`
	FrozenExists       bool            `json:"frozen_exists"`
	Baseline           json.RawMessage `json:"baseline"`
	MappingFingerprint string          `json:"mapping_fingerprint"`
	Classification     string          `json:"classification"`
	AfterFingerprint   string          `json:"after_fingerprint"`
	AfterRevision      int64           `json:"after_rules_revision"`
	AfterIdentity      int64           `json:"after_identity_revision"`
	RepairedAt         string          `json:"repaired_at"`
	RollbackAt         string          `json:"rolled_back_at,omitempty"`
	RollbackCheckedAt  string          `json:"rollback_checked_at,omitempty"`
}

func selectionRepairJSON(raw map[string]any, key string) (json.RawMessage, bool, error) {
	value, exists := raw[key]
	if !exists {
		return nil, false, nil
	}
	encoded, err := json.Marshal(value)
	return encoded, true, err
}

func modelSelectionFingerprint(account *service.Account, result *service.ModelSelectionResult) (string, error) {
	var scope service.AccountModelAllowlist
	mapping := map[string]string{}
	baseline := account.ModelCatalogBaseline()
	if result == nil {
		scope = service.AccountModelSelectionView(account).Scope
		raw, exists := account.Credentials["model_mapping"]
		if exists {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return "", err
			}
			if string(encoded) != "null" {
				if err := json.Unmarshal(encoded, &mapping); err != nil {
					return "", err
				}
			}
		}
	} else {
		scope, mapping = result.Scope, result.ModelMapping
		baseline.RulesRevision = result.RulesRevision
	}
	frozenExists := false
	var frozen any
	if result == nil {
		frozen, frozenExists = account.Extra[service.ModelSelectionBaselineFrozenExtraKey]
	}
	fingerprint, err := json.Marshal(struct {
		Scope            service.AccountModelAllowlist `json:"scope"`
		Mapping          map[string]string             `json:"mapping"`
		FrozenExists     bool                          `json:"frozen_exists"`
		Frozen           any                           `json:"frozen"`
		IdentityRevision int64                         `json:"identity_revision"`
		RulesRevision    int64                         `json:"rules_revision"`
	}{scope, mapping, frozenExists, frozen, baseline.IdentityRevision, baseline.RulesRevision})
	return string(fingerprint), err
}

func classifyModelSelectionRepair(account *service.Account, rawExtra map[string]any) (string, service.ModelSelectionCommand, error) {
	var scope service.AccountModelAllowlist
	encodedScope, err := json.Marshal(rawExtra[service.UpstreamModelAllowlistExtraKey])
	if err != nil || json.Unmarshal(encodedScope, &scope) != nil {
		return "invalid_scope", service.ModelSelectionCommand{}, nil
	}
	if !scope.Enabled || len(scope.Models) != 0 {
		return "not_empty", service.ModelSelectionCommand{}, nil
	}
	if _, exists := rawExtra[service.ModelSelectionRepairBackupExtraKey]; exists {
		return "backed_up", service.ModelSelectionCommand{}, nil
	}
	if scope.ConfirmedBy != 0 || scope.ConfirmedAt != "" {
		return "admin_or_unknown", service.ModelSelectionCommand{}, nil
	}
	frozen, frozenValid := rawExtra[service.ModelSelectionBaselineFrozenExtraKey].(bool)
	classification := ""
	switch {
	case scope.Reason == "system_upgrade_baseline_freeze":
		classification = "system_upgrade_baseline_freeze"
	case frozenValid && frozen && scope.Reason == "":
		classification = "system_empty_mapping_freeze"
	default:
		return "admin_or_unknown", service.ModelSelectionCommand{}, nil
	}
	mapping := map[string]string{}
	if raw, exists := account.Credentials["model_mapping"]; exists && raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return "invalid_mapping", service.ModelSelectionCommand{}, nil
		}
		if err := json.Unmarshal(encoded, &mapping); err != nil {
			return "invalid_mapping", service.ModelSelectionCommand{}, nil
		}
	}
	command := service.ModelSelectionCommand{Mode: service.ModelSelectionInherit, Reason: modelSelectionRepairReason, Source: service.ModelSelectionRepair}
	if len(mapping) > 0 {
		command.Mode = service.ModelSelectionRestricted
		for name, target := range mapping {
			if name == target {
				command.Models = append(command.Models, target)
			}
		}
	}
	result, err := service.NormalizeModelSelection(scope, mapping, command)
	if err != nil {
		return "invalid_mapping", service.ModelSelectionCommand{}, nil
	}
	if len(result.Models) == 0 && command.Mode != service.ModelSelectionInherit {
		return "invalid_mapping", service.ModelSelectionCommand{}, nil
	}
	return classification, command, nil
}

func modelSelectionRepairMatchesConnection(ctx context.Context, tx *sql.Tx, account *service.Account) (bool, error) {
	var workspaceID int64
	var models []string
	// Activation locks the connection before the account. Read its committed
	// projection without acquiring the connection lock in reverse order.
	err := tx.QueryRowContext(ctx, `SELECT workspace_id,model_ids FROM provider_connections WHERE account_id=$1 AND NOT legacy_dirty`, account.ID).Scan(&workspaceID, pq.Array(&models))
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	mapping := service.AccountModelSelectionView(account).ModelMapping
	if workspaceID != account.WorkspaceID || len(mapping) != len(models) {
		return false, nil
	}
	for _, model := range models {
		target, exists := mapping[model]
		if !exists || target != model {
			return false, nil
		}
		delete(mapping, model)
	}
	return true, nil
}

func newModelSelectionRepairBackup(account *service.Account, rawExtra map[string]any, classification string, result service.ModelSelectionResult, now time.Time) (map[string]any, error) {
	scope, scopeExists, err := selectionRepairJSON(rawExtra, service.UpstreamModelAllowlistExtraKey)
	if err != nil {
		return nil, err
	}
	frozen, frozenExists, err := selectionRepairJSON(rawExtra, service.ModelSelectionBaselineFrozenExtraKey)
	if err != nil {
		return nil, err
	}
	baseline, _, err := selectionRepairJSON(rawExtra, service.UpstreamModelCatalogStateExtraKey)
	if err != nil {
		return nil, err
	}
	mappingFingerprint, valid := service.LegacyModelMappingFingerprint(account.Credentials)
	if !valid {
		return nil, fmt.Errorf("invalid mapping fingerprint")
	}
	afterFingerprint, err := modelSelectionFingerprint(account, &result)
	if err != nil {
		return nil, err
	}
	backup := modelSelectionRepairBackup{Scope: scope, ScopeExists: scopeExists, Frozen: frozen, FrozenExists: frozenExists, Baseline: baseline, MappingFingerprint: mappingFingerprint, Classification: classification, AfterFingerprint: afterFingerprint, AfterRevision: result.RulesRevision, AfterIdentity: result.Scope.IdentityRevision, RepairedAt: now.UTC().Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(backup)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func runAccountModelSelectionRepair(ctx context.Context, db *sql.DB, rollback bool) error {
	if db == nil {
		return errors.New("model selection repair database is nil")
	}
	connection, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, modelSelectionRepairAdvisoryLock); err != nil {
		return err
	}
	defer func() {
		_, _ = connection.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, modelSelectionRepairAdvisoryLock)
	}()
	if !rollback {
		if err := reportSkippedModelSelections(ctx, connection); err != nil {
			return err
		}
	}
	skipped := map[int64]bool{}
	for {
		processed, err := repairNextAccountModelSelection(ctx, connection, rollback, skipped)
		if err != nil {
			return err
		}
		if !processed {
			return nil
		}
	}
}

func reportSkippedModelSelections(ctx context.Context, connection *sql.Conn) error {
	rows, err := connection.QueryContext(ctx, `SELECT workspace_id,id FROM accounts WHERE deleted_at IS NULL AND extra->'upstream_model_allowlist'->'enabled'='true'::jsonb
		AND COALESCE(extra->'upstream_model_allowlist'->'models','[]'::jsonb) IN ('[]'::jsonb,'null'::jsonb)
		AND NOT extra ? 'model_selection_v039_repair_backup' AND NOT (
			COALESCE(extra->'upstream_model_allowlist'->>'confirmed_by','0')='0' AND COALESCE(extra->'upstream_model_allowlist'->>'confirmed_at','')='' AND
			(COALESCE(extra->'upstream_model_allowlist'->>'reason','')='system_upgrade_baseline_freeze' OR
			 (COALESCE(extra->>'model_selection_baseline_frozen','false')='true' AND COALESCE(extra->'upstream_model_allowlist'->>'reason','')=''))
		) ORDER BY workspace_id,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var workspaceID, accountID int64
		if err := rows.Scan(&workspaceID, &accountID); err != nil {
			return err
		}
		logger.LegacyPrintf("repository.account_model_selection", "v039 repair requires review workspace=%d account=%d classification=admin_or_unknown", workspaceID, accountID)
	}
	return rows.Err()
}

// RepairAccountModelSelections repairs only the two documented system-freeze
// signatures. Every backup and repaired selection commits in one transaction.
func RepairAccountModelSelections(ctx context.Context, db *sql.DB) error {
	return runAccountModelSelectionRepair(ctx, db, false)
}

// RollbackAccountModelSelections restores only rows whose post-repair selection
// still matches the protected backup. Business edits are retained and reported.
func RollbackAccountModelSelections(ctx context.Context, db *sql.DB) error {
	return runAccountModelSelectionRepair(ctx, db, true)
}

func repairNextAccountModelSelection(ctx context.Context, connection *sql.Conn, rollback bool, skipped map[int64]bool) (bool, error) {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer transaction.Rollback()
	query := `SELECT id FROM accounts WHERE deleted_at IS NULL`
	if rollback {
		query += ` AND extra ? 'model_selection_v039_repair_backup' AND NOT extra->'model_selection_v039_repair_backup' ? 'rollback_checked_at'`
	} else {
		query += ` AND extra->'upstream_model_allowlist'->'enabled'='true'::jsonb
			AND COALESCE(extra->'upstream_model_allowlist'->'models','[]'::jsonb) IN ('[]'::jsonb,'null'::jsonb)
			AND NOT extra ? 'model_selection_v039_repair_backup' AND
			COALESCE(extra->'upstream_model_allowlist'->>'confirmed_by','0')='0' AND COALESCE(extra->'upstream_model_allowlist'->>'confirmed_at','')='' AND
			(extra->'upstream_model_allowlist'->>'reason'='system_upgrade_baseline_freeze' OR
			 (COALESCE(extra->>'model_selection_baseline_frozen','false')='true' AND COALESCE(extra->'upstream_model_allowlist'->>'reason','')=''))`
	}
	args := []any{}
	for id := range skipped {
		args = append(args, id)
		query += ` AND id <> $` + strconv.Itoa(len(args))
	}
	query += ` ORDER BY id LIMIT 1 FOR UPDATE`
	var accountID int64
	err = transaction.QueryRowContext(ctx, query, args...).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var rawCredentials, rawExtra []byte
	if err := transaction.QueryRowContext(ctx, `SELECT COALESCE(credentials,'{}'::jsonb),COALESCE(extra,'{}'::jsonb) FROM accounts WHERE id=$1`, accountID).Scan(&rawCredentials, &rawExtra); err != nil {
		return false, err
	}
	credentials, extra := map[string]any{}, map[string]any{}
	if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
		return false, err
	}
	if err := json.Unmarshal(rawExtra, &extra); err != nil {
		return false, err
	}
	account, _, err := lockedModelSelectionAccount(ctx, transaction, accountID, service.ModelSelectionRepair)
	if err != nil {
		return false, err
	}
	if rollback {
		return rollbackLockedAccountModelSelection(ctx, transaction, account, rawExtra)
	}
	classification, command, err := classifyModelSelectionRepair(account, extra)
	if err != nil {
		return false, err
	}
	if command.Mode != "" {
		match, err := modelSelectionRepairMatchesConnection(ctx, transaction, account)
		if err != nil {
			return false, err
		}
		if !match {
			classification = "managed_projection_mismatch"
			command.Mode = ""
		}
	}
	if command.Mode == "" {
		skipped[accountID] = true
		logger.LegacyPrintf("repository.account_model_selection", "v039 repair skipped workspace=%d account=%d classification=%s", account.WorkspaceID, accountID, classification)
		return true, transaction.Commit()
	}
	now := time.Now().UTC()
	_, err = UpdateModelSelectionInSQL(ctx, transaction, accountID, service.ModelSelectionRepair, func(current *service.Account) (service.ModelSelectionResult, error) {
		result, err := service.ResolveLockedModelSelection(current, command)
		if err != nil {
			return result, err
		}
		// This repair and its rollback only replace the scope. Keep legacy
		// credentials intact until an administrator actually edits selection.
		result.ModelMapping = service.AccountModelSelectionView(current).ModelMapping
		result.MappingChanged = false
		backup, err := newModelSelectionRepairBackup(current, extra, classification, result, now)
		if err != nil {
			return result, err
		}
		current.Extra[service.ModelSelectionRepairBackupExtraKey] = backup
		return result, nil
	})
	if err != nil {
		return false, err
	}
	logger.LegacyPrintf("repository.account_model_selection", "v039 repair applied workspace=%d account=%d classification=%s", account.WorkspaceID, accountID, classification)
	return true, transaction.Commit()
}

func rollbackLockedAccountModelSelection(ctx context.Context, transaction *sql.Tx, account *service.Account, rawExtra []byte) (bool, error) {
	raw := map[string]any{}
	if err := json.Unmarshal(rawExtra, &raw); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(raw[service.ModelSelectionRepairBackupExtraKey])
	if err != nil {
		return false, err
	}
	var backup modelSelectionRepairBackup
	if err := json.Unmarshal(encoded, &backup); err != nil {
		return false, err
	}
	currentFingerprint, err := modelSelectionFingerprint(account, nil)
	if err != nil {
		return false, err
	}
	state := account.ModelCatalogState()
	if currentFingerprint != backup.AfterFingerprint || state.RulesRevision != backup.AfterRevision || state.IdentityRevision != backup.AfterIdentity {
		backup.RollbackCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		account.Extra[service.ModelSelectionRepairBackupExtraKey] = backup
		extra, err := json.Marshal(account.Extra)
		if err != nil {
			return false, err
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE accounts SET extra=$2::jsonb,updated_at=NOW() WHERE id=$1`, account.ID, string(extra)); err != nil {
			return false, err
		}
		logger.LegacyPrintf("repository.account_model_selection", "v039 rollback skipped edited workspace=%d account=%d", account.WorkspaceID, account.ID)
		return true, transaction.Commit()
	}
	if !backup.ScopeExists {
		delete(account.Extra, service.UpstreamModelAllowlistExtraKey)
	} else {
		var value any
		if err := json.Unmarshal(backup.Scope, &value); err != nil {
			return false, err
		}
		account.Extra[service.UpstreamModelAllowlistExtraKey] = value
	}
	if !backup.FrozenExists {
		delete(account.Extra, service.ModelSelectionBaselineFrozenExtraKey)
	} else {
		var value any
		if err := json.Unmarshal(backup.Frozen, &value); err != nil {
			return false, err
		}
		account.Extra[service.ModelSelectionBaselineFrozenExtraKey] = value
	}
	state.RulesRevision++
	account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	backup.RollbackAt = time.Now().UTC().Format(time.RFC3339Nano)
	backup.RollbackCheckedAt = backup.RollbackAt
	account.Extra[service.ModelSelectionRepairBackupExtraKey] = backup
	extra, err := json.Marshal(account.Extra)
	if err != nil {
		return false, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE accounts SET extra=$2::jsonb,updated_at=NOW() WHERE id=$1`, account.ID, string(extra)); err != nil {
		return false, err
	}
	payload := map[string]any{"rules_revision": state.RulesRevision}
	if err := enqueueSchedulerOutbox(ctx, transaction, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, payload); err != nil {
		return false, err
	}
	logger.LegacyPrintf("repository.account_model_selection", "v039 rollback applied workspace=%d account=%d", account.WorkspaceID, account.ID)
	return true, transaction.Commit()
}
