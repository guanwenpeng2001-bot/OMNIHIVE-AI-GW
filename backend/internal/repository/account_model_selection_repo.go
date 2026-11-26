package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"reflect"
	"slices"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func stripModelSelectionRepairBackup(extra map[string]any) map[string]any {
	if _, exists := extra[service.ModelSelectionRepairBackupExtraKey]; !exists {
		return extra
	}
	stripped := maps.Clone(extra)
	delete(stripped, service.ModelSelectionRepairBackupExtraKey)
	return stripped
}

func lockedModelSelectionAccount(ctx context.Context, executor sqlExecutor, accountID int64, source service.ModelSelectionSource) (*service.Account, []byte, error) {
	rows, err := executor.QueryContext(ctx, `SELECT workspace_id,COALESCE(credentials,'{}'::jsonb),COALESCE(extra,'{}'::jsonb),platform,type,proxy_id,parent_account_id FROM accounts
		WHERE id=$1 AND deleted_at IS NULL`+workspaceClause(ctx, "workspace_id")+` FOR UPDATE`, accountID)
	if err != nil {
		return nil, nil, err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		_ = rows.Close()
		return nil, nil, service.ErrAccountNotFound
	}
	var credentials, extra []byte
	account := &service.Account{ID: accountID}
	if err := rows.Scan(&account.WorkspaceID, &credentials, &extra, &account.Platform, &account.Type, &account.ProxyID, &account.ParentAccountID); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(credentials, &account.Credentials); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(extra, &account.Extra); err != nil {
		return nil, nil, err
	}
	if source == service.ModelSelectionAdmin || source == service.ModelSelectionAccountWrite {
		rows, err := executor.QueryContext(ctx, `SELECT account_id FROM provider_connections WHERE workspace_id=$1 AND account_id=$2 AND NOT legacy_dirty`, account.WorkspaceID, accountID)
		if err != nil {
			return nil, nil, err
		}
		managed := rows.Next()
		readErr := rows.Err()
		closeErr := rows.Close()
		if readErr != nil {
			return nil, nil, readErr
		}
		if closeErr != nil {
			return nil, nil, closeErr
		}
		if managed {
			return nil, nil, service.ErrAccountConnectionManaged
		}
	}
	return account, extra, nil
}

func modelSelectionCredentialMap(mapping map[string]string) map[string]any {
	credentials := make(map[string]any, len(mapping))
	for name, target := range mapping {
		credentials[name] = target
	}
	return credentials
}

func modelSelectionWritePatch(account *service.Account, result service.ModelSelectionResult) ([]byte, error) {
	next := *account
	next.Credentials = normalizeJSONMap(maps.Clone(account.Credentials))
	next.Extra = normalizeJSONMap(maps.Clone(account.Extra))
	if result.MappingChanged {
		next.Credentials["model_mapping"] = modelSelectionCredentialMap(result.ModelMapping)
	}
	if result.Changed {
		next.Extra[service.UpstreamModelAllowlistExtraKey] = result.Scope
		delete(next.Extra, service.ModelSelectionBaselineFrozenExtraKey)
	}
	original := *account
	if result.Changed {
		original.Extra = maps.Clone(account.Extra)
		previous := service.AccountModelSelectionView(account)
		if _, repaired := account.Extra[service.ModelSelectionRepairBackupExtraKey]; repaired {
			previous.Scope = result.Scope
		}
		if previous.Scope.Enabled != result.Scope.Enabled || !reflect.DeepEqual(previous.Scope.Models, result.Scope.Models) {
			original.Extra[service.UpstreamModelAllowlistExtraKey] = previous.Scope
		} else {
			original.Extra[service.UpstreamModelAllowlistExtraKey] = result.Scope
		}
	}
	service.AdvanceAccountModelCatalogRevisions(&original, &next)
	state := next.ModelCatalogState()
	state.RulesRevision = result.RulesRevision
	next.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	return json.Marshal(next.Extra)
}

func updateModelSelectionWithExecutor(ctx context.Context, executor sqlExecutor, accountID int64, source service.ModelSelectionSource, resolve func(*service.Account) (service.ModelSelectionResult, error)) (service.ModelSelectionResult, error) {
	account, _, err := lockedModelSelectionAccount(ctx, executor, accountID, source)
	if err != nil {
		return service.ModelSelectionResult{}, err
	}
	result, err := resolve(account)
	if err != nil {
		return service.ModelSelectionResult{}, err
	}
	if !result.Changed {
		return result, nil
	}
	extra, err := modelSelectionWritePatch(account, result)
	if err != nil {
		return service.ModelSelectionResult{}, err
	}
	var mapping []byte
	if result.MappingChanged {
		mapping, err = json.Marshal(result.ModelMapping)
		if err != nil {
			return service.ModelSelectionResult{}, err
		}
	}
	writeResult, err := executor.ExecContext(ctx, `UPDATE accounts SET
		credentials=CASE WHEN $2::boolean THEN jsonb_set(COALESCE(credentials,'{}'::jsonb),'{model_mapping}',COALESCE(NULLIF($3,'')::jsonb,'{}'::jsonb)) ELSE credentials END,
		extra=$4::jsonb,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`+workspaceClause(ctx, "workspace_id"), accountID, result.MappingChanged, string(mapping), string(extra))
	if err != nil {
		return service.ModelSelectionResult{}, err
	}
	affected, err := writeResult.RowsAffected()
	if err != nil {
		return service.ModelSelectionResult{}, err
	}
	if affected != 1 {
		return service.ModelSelectionResult{}, service.ErrModelSelectionConflict
	}
	payload := map[string]any{"rules_revision": result.RulesRevision}
	if err := enqueueSchedulerOutbox(ctx, executor, service.SchedulerOutboxEventAccountChanged, &accountID, nil, payload); err != nil {
		return service.ModelSelectionResult{}, err
	}
	return result, nil
}

func (r *accountRepository) UpdateModelSelection(ctx context.Context, accountID int64, source service.ModelSelectionSource, resolve func(*service.Account) (service.ModelSelectionResult, error)) (service.ModelSelectionResult, error) {
	defer invalidateCatalogAfterWrite(ctx)
	baseCtx := ctx
	contextTx := dbent.TxFromContext(ctx)
	owned := contextTx == nil
	var changed bool
	wrappedResolve := func(account *service.Account) (service.ModelSelectionResult, error) {
		result, err := resolve(account)
		changed = result.Changed
		return result, err
	}
	if owned {
		transaction, err := r.client.Tx(ctx)
		if err != nil {
			return service.ModelSelectionResult{}, err
		}
		defer func() { _ = transaction.Rollback() }()
		ctx = dbent.NewTxContext(ctx, transaction)
		result, err := updateModelSelectionWithExecutor(ctx, transaction.Client(), accountID, source, wrappedResolve)
		if err != nil {
			return service.ModelSelectionResult{}, err
		}
		if err := transaction.Commit(); err != nil {
			return service.ModelSelectionResult{}, err
		}
		if changed {
			r.syncSchedulerAccountSnapshot(baseCtx, accountID)
		}
		return result, nil
	}
	return updateModelSelectionWithExecutor(ctx, contextTx.Client(), accountID, source, wrappedResolve)
}

// UpdateModelSelectionInSQL applies the unified writer to an existing native
// transaction. The caller remains responsible for its commit and other writes.
func UpdateModelSelectionInSQL(ctx context.Context, transaction *sql.Tx, accountID int64, source service.ModelSelectionSource, resolve func(*service.Account) (service.ModelSelectionResult, error)) (service.ModelSelectionResult, error) {
	return updateModelSelectionWithExecutor(ctx, transaction, accountID, source, resolve)
}

func mergeLockedAccountModelSelection(current, incoming *service.Account, command *service.ModelSelectionCommand) error {
	incoming.ModelSelectionResult = nil
	if command == nil {
		return nil
	}
	result, err := service.ResolveLockedModelSelection(current, *command)
	if err != nil {
		return err
	}
	if !result.Changed {
		return nil
	}
	if result.MappingChanged {
		incoming.Credentials["model_mapping"] = modelSelectionCredentialMap(result.ModelMapping)
	}
	incoming.Extra[service.UpstreamModelAllowlistExtraKey] = result.Scope
	delete(incoming.Extra, service.ModelSelectionBaselineFrozenExtraKey)
	incoming.ModelSelectionResult = &result
	return nil
}

func normalizeCreatedModelSelection(account *service.Account) error {
	account.Extra = stripModelSelectionRepairBackup(service.StripAccountModelCatalogExtra(account.Extra))
	return service.InitializeAccountModelSelection(account)
}

type bulkModelSelectionBaseline struct {
	id            int64
	rulesRevision int64
}

type bulkSelectionRulesAlreadyAdvanced struct{}

func lockBulkModelSelections(ctx context.Context, executor sqlExecutor, ids []int64, cmd *service.ModelSelectionCommand) ([]bulkModelSelectionBaseline, error) {
	if cmd == nil {
		return nil, nil
	}
	ids = uniquePositiveInt64s(ids)
	slices.Sort(ids)
	baselines := make([]bulkModelSelectionBaseline, 0, len(ids))
	for _, id := range ids {
		current, _, err := lockedModelSelectionAccount(ctx, executor, id, cmd.Source)
		if err != nil {
			return nil, err
		}
		baselines = append(baselines, bulkModelSelectionBaseline{id: id, rulesRevision: current.ModelCatalogState().RulesRevision})
	}
	return baselines, nil
}

// applyBulkModelSelections runs after the other updates in the same transaction.
// A protocol or identity edit may already have advanced the locked row's rules.
func (r *accountRepository) applyBulkModelSelections(ctx context.Context, baselines []bulkModelSelectionBaseline, updates service.AccountBulkUpdate) error {
	for _, baseline := range baselines {
		command := *updates.ModelSelectionCommand
		result, err := r.UpdateModelSelection(ctx, baseline.id, command.Source, func(current *service.Account) (service.ModelSelectionResult, error) {
			revision := current.ModelCatalogState().RulesRevision
			if command.ExpectedRulesRevision != nil {
				if *command.ExpectedRulesRevision != baseline.rulesRevision {
					return service.ModelSelectionResult{}, service.ErrModelSelectionConflict
				}
				command.ExpectedRulesRevision = &revision
			}
			result, err := service.ResolveLockedModelSelection(current, command)
			if err == nil && revision > baseline.rulesRevision {
				result.RulesRevision = revision
			}
			return result, err
		})
		if err != nil {
			return err
		}
		if updates.ModelSelectionResults != nil {
			updates.ModelSelectionResults[baseline.id] = result
		}
	}
	return nil
}
