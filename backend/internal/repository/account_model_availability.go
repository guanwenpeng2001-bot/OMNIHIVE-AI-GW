package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

// Lock first, then sample database time: a transaction waiting across midnight
// must spend the new UTC window, not its transaction-start window.
func lockModelAvailabilityAccount(ctx context.Context, tx *dbent.Tx, accountID int64) (*service.Account, time.Time, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, accountID)
	if err != nil {
		return nil, time.Time{}, err
	}
	found := rows.Next()
	readErr := rows.Err()
	_ = rows.Close()
	if readErr != nil {
		return nil, time.Time{}, readErr
	}
	if !found {
		return nil, time.Time{}, service.ErrAccountNotFound
	}
	rows, err = tx.QueryContext(ctx, `SELECT clock_timestamp()`)
	if err != nil {
		return nil, time.Time{}, err
	}
	var now time.Time
	if !rows.Next() {
		_ = rows.Close()
		return nil, now, fmt.Errorf("database clock unavailable")
	}
	err = rows.Scan(&now)
	_ = rows.Close()
	if err != nil {
		return nil, now, err
	}
	account, err := tx.Client().Account.Get(ctx, accountID)
	if err != nil {
		return nil, now, err
	}
	return accountEntityToService(account), now.UTC(), nil
}

func availabilityCredentialAccount(ctx context.Context, tx *dbent.Tx, account *service.Account) (*service.Account, error) {
	if !account.IsShadow() {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *account.ParentAccountID)
	if err != nil {
		return nil, err
	}
	found := rows.Next()
	readErr := rows.Err()
	_ = rows.Close()
	if readErr != nil {
		return nil, readErr
	}
	if !found {
		return nil, service.ErrAccountModelCatalogConflict
	}
	parent, err := tx.Client().Account.Get(ctx, *account.ParentAccountID)
	if err != nil {
		return nil, err
	}
	credential := accountEntityToService(parent)
	if !credential.IsOpenAIOAuth() || credential.IsShadow() || !credential.IsCredentialUsableForShadow() || credential.ModelCatalogState().Result == "credentials_invalid" {
		return nil, service.ErrAccountModelCatalogConflict
	}
	return credential, nil
}

func writeAvailabilityAudit(ctx context.Context, tx *dbent.Tx, accountID, actor int64, action string, now time.Time, extra map[string]any) error {
	log := &service.AuditLog{CreatedAt: now, Action: action, ActorRole: "admin", StatusCode: 200,
		Path: "/api/v1/admin/accounts/" + strconv.FormatInt(accountID, 10) + "/model-availability", Extra: extra}
	if actor > 0 {
		log.ActorUserID = &actor
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (`+auditLogInsertColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, auditLogInsertValues(log)...)
	return err
}

// Preserve other still-valid declarations when this dedicated writer advances
// rules CAS; a prior configuration change must never be rebased back to valid.
func advanceAvailabilityRules(account *service.Account, declarations []service.ModelAvailabilityDeclaration) service.AccountModelCatalogState {
	state := account.ModelCatalogState()
	for index := range declarations {
		if declarations[index].IdentityRevision == state.IdentityRevision && declarations[index].RulesRevision == state.RulesRevision {
			declarations[index].RulesRevision++
		}
	}
	state.RulesRevision++
	return state
}

func persistAvailabilityPatch(ctx context.Context, tx *dbent.Tx, accountID int64, patch map[string]any, invalidate bool) error {
	encoded, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||$2::jsonb, updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, accountID, string(encoded)); err != nil {
		return err
	}
	if invalidate {
		return enqueueSchedulerOutbox(dbent.NewTxContext(ctx, tx), tx.Client(), service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil)
	}
	return nil
}

func (repository *accountRepository) ChangeModelAvailability(ctx context.Context, accountID int64, change service.ModelAvailabilityChange, actor int64, revoke bool) error {
	transaction, err := repository.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	account, now, err := lockModelAvailabilityAccount(ctx, transaction, accountID)
	if err != nil {
		return err
	}
	if err := service.ValidateModelAvailabilityChange(account, change, actor, revoke, now); err != nil {
		return err
	}
	declarations := account.ModelAvailabilityDeclarations()
	index := slices.IndexFunc(declarations, func(declaration service.ModelAvailabilityDeclaration) bool {
		return declaration.Model == change.Model && declaration.Endpoint == change.Endpoint
	})
	if revoke && index < 0 {
		return service.ErrModelAvailabilityNotFound
	}
	if !revoke && index < 0 && len(declarations) >= 512 {
		return service.ErrModelAvailabilityInvalid
	}
	state := advanceAvailabilityRules(account, declarations)
	if revoke {
		declarations[index].RevokedAt = &now
	} else {
		credential, err := availabilityCredentialAccount(ctx, transaction, account)
		if err != nil {
			return err
		}
		declaration := service.ModelAvailabilityDeclaration{AccountID: accountID, Model: change.Model, Endpoint: change.Endpoint,
			IdentityRevision: state.IdentityRevision, RulesRevision: state.RulesRevision, Reason: change.Reason, ConfirmedBy: actor,
			ConfirmedAt: now, ExpiresAt: change.ExpiresAt, EvidenceKind: service.ModelEvidenceAdministrator}
		if credential != nil {
			declaration.CredentialAccountID, declaration.CredentialIdentityRevision = credential.ID, credential.ModelCatalogState().IdentityRevision
		}
		if index < 0 {
			declarations = append(declarations, declaration)
		} else {
			declarations[index] = declaration
		}
	}
	action := "admin.account.model_availability.declare"
	if revoke {
		action = "admin.account.model_availability.revoke"
	}
	if err := writeAvailabilityAudit(ctx, transaction, accountID, actor, action, now, map[string]any{"account_id": accountID, "model": change.Model, "endpoint": change.Endpoint, "reason": change.Reason, "identity_revision": state.IdentityRevision, "rules_revision": state.RulesRevision, "expires_at": change.ExpiresAt}); err != nil {
		return err
	}
	if err := persistAvailabilityPatch(ctx, transaction, accountID, map[string]any{service.ModelAvailabilityExtraKey: declarations, service.UpstreamModelCatalogStateExtraKey: state}, true); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	repository.syncSchedulerAccountSnapshot(ctx, accountID)
	return nil
}

func decodeModelProbeBudget(account *service.Account) (service.ModelProbeBudget, error) {
	var budget service.ModelProbeBudget
	if value, exists := account.Extra[service.ModelProbeBudgetExtraKey]; exists {
		encoded, err := json.Marshal(value)
		if err != nil {
			return budget, err
		}
		if err := json.Unmarshal(encoded, &budget); err != nil {
			return budget, err
		}
		// Corrupt state is never treated as an empty budget.
		if budget.Revision < 0 || budget.DailyUsed < 0 || budget.MonthlyUsed < 0 || budget.MediaDailyUsed < 0 || budget.MediaMonthlyUsed < 0 || budget.Month == "" || budget.Day == "" {
			return budget, service.ErrModelProbeBudgetExceeded
		}
	}
	return budget, nil
}

func (repository *accountRepository) ReserveModelProbe(ctx context.Context, accountID int64, baseline service.AccountModelCatalogBaseline, model, endpoint string) (*service.ModelProbeAttempt, error) {
	transaction, err := repository.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	account, now, err := lockModelAvailabilityAccount(ctx, transaction, accountID)
	if err != nil {
		return nil, err
	}
	change := service.ModelAvailabilityChange{AccountModelCatalogBaseline: baseline, Model: model, Endpoint: endpoint, Reason: "bounded model probe", Confirmed: true}
	if err := service.ValidateModelAvailabilityChange(account, change, 1, false, now); err != nil {
		return nil, err
	}
	// A missing, partial, stale or failed discovery is not an empty catalog.
	snapshot := account.GetUpstreamModelCatalog()
	if snapshot == nil || snapshot.IdentityRevision != baseline.IdentityRevision || slices.Contains(snapshot.DiscoveredModels, model) || account.ModelCatalogState().Result == "credentials_invalid" || account.ModelCatalogState().Result == "failed" || !account.IsActive() || !account.Schedulable || (account.ExpiresAt != nil && !now.Before(*account.ExpiresAt)) {
		return nil, service.ErrModelAvailabilityInvalid
	}
	credential, err := availabilityCredentialAccount(ctx, transaction, account)
	if err != nil {
		return nil, err
	}
	if credential != nil && (snapshot.CredentialAccountID != credential.ID || snapshot.CredentialIdentityRevision != credential.ModelCatalogState().IdentityRevision) {
		return nil, service.ErrAccountModelCatalogConflict
	}
	observed, err := time.Parse(time.RFC3339Nano, snapshot.ObservedAt)
	if err != nil || observed.After(now) || !now.Before(observed.AddDate(0, 1, 0)) {
		return nil, service.ErrModelAvailabilityInvalid
	}
	budget, err := decodeModelProbeBudget(account)
	if err != nil {
		return nil, err
	}
	attempt := service.ModelProbeAttempt{ID: uuid.NewString(), AccountID: accountID, Model: model, Endpoint: endpoint, IdentityRevision: baseline.IdentityRevision, RulesRevision: baseline.RulesRevision, ReservedAt: now}
	if credential != nil {
		attempt.CredentialAccountID, attempt.CredentialIdentityRevision = credential.ID, credential.ModelCatalogState().IdentityRevision
	}
	if err := budget.Reserve(attempt); err != nil {
		return nil, err
	}
	if err := persistAvailabilityPatch(ctx, transaction, accountID, map[string]any{service.ModelProbeBudgetExtraKey: budget}, false); err != nil {
		return nil, err
	}
	if err := writeAvailabilityAudit(ctx, transaction, accountID, 0, "account.model_probe.reserve", now, map[string]any{"account_id": accountID, "attempt": attempt, "budget_revision": budget.Revision}); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (repository *accountRepository) ClaimModelProbe(ctx context.Context, accountID int64, attemptID string) (*service.ModelProbeAttempt, error) {
	transaction, err := repository.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	account, now, err := lockModelAvailabilityAccount(ctx, transaction, accountID)
	if err != nil {
		return nil, err
	}
	budget, err := decodeModelProbeBudget(account)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(budget.Attempts, func(attempt service.ModelProbeAttempt) bool {
		return attempt.ID == attemptID && attempt.AccountID == accountID
	})
	if index < 0 {
		return nil, service.ErrModelProbeAttemptClaimed
	}
	attempt := &budget.Attempts[index]
	state := account.ModelCatalogState()
	if attempt.ClaimedAt != nil || attempt.CompletedAt != nil || budget.Month != now.Format("2006-01") || attempt.ReservedAt.UTC().Format("2006-01-02") != now.Format("2006-01-02") || attempt.ReservedAt.Before(now.Add(-5*time.Minute)) {
		return nil, service.ErrModelProbeAttemptClaimed
	}
	if attempt.IdentityRevision != state.IdentityRevision || attempt.RulesRevision != state.RulesRevision || state.Result == "credentials_invalid" || !account.IsActive() || !account.Schedulable || (account.ExpiresAt != nil && !now.Before(*account.ExpiresAt)) {
		return nil, service.ErrAccountModelCatalogConflict
	}
	credential, err := availabilityCredentialAccount(ctx, transaction, account)
	if err != nil {
		return nil, err
	}
	if credential != nil && (attempt.CredentialAccountID != credential.ID || attempt.CredentialIdentityRevision != credential.ModelCatalogState().IdentityRevision) {
		return nil, service.ErrAccountModelCatalogConflict
	}
	attempt.ClaimedAt = &now
	budget.Revision++
	if err := persistAvailabilityPatch(ctx, transaction, accountID, map[string]any{service.ModelProbeBudgetExtraKey: budget}, false); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (repository *accountRepository) CompleteModelProbe(ctx context.Context, accountID int64, attemptID string, outcome service.ModelProbeOutcome) error {
	if outcome != service.ModelProbeSucceeded && outcome != service.ModelProbeMissing && outcome != service.ModelProbeTemporary {
		return service.ErrModelAvailabilityInvalid
	}
	transaction, err := repository.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	account, now, err := lockModelAvailabilityAccount(ctx, transaction, accountID)
	if err != nil {
		return err
	}
	budget, err := decodeModelProbeBudget(account)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(budget.Attempts, func(attempt service.ModelProbeAttempt) bool {
		return attempt.ID == attemptID && attempt.AccountID == accountID
	})
	if index < 0 {
		return service.ErrModelProbeAttemptClaimed
	}
	attempt := &budget.Attempts[index]
	if attempt.CompletedAt != nil || attempt.ClaimedAt == nil || budget.Month != now.Format("2006-01") {
		return service.ErrModelProbeAttemptClaimed
	}
	attempt.CompletedAt, attempt.Outcome = &now, string(outcome)
	budget.Revision++
	patch := map[string]any{service.ModelProbeBudgetExtraKey: budget}
	state := account.ModelCatalogState()
	invalidate := false
	credentialValid := true
	if account.IsShadow() {
		credential, credentialErr := availabilityCredentialAccount(ctx, transaction, account)
		if credentialErr != nil {
			return credentialErr
		}
		credentialValid = credential.ID == attempt.CredentialAccountID && credential.ModelCatalogState().IdentityRevision == attempt.CredentialIdentityRevision
	}
	// Late results are recorded for audit, but cannot revive a changed identity.
	if outcome != service.ModelProbeTemporary && credentialValid && attempt.IdentityRevision == state.IdentityRevision && attempt.RulesRevision == state.RulesRevision && state.Result != "credentials_invalid" {
		declarations := account.ModelAvailabilityDeclarations()
		declarationIndex := slices.IndexFunc(declarations, func(declaration service.ModelAvailabilityDeclaration) bool {
			return declaration.Model == attempt.Model && declaration.Endpoint == attempt.Endpoint
		})
		if outcome == service.ModelProbeSucceeded && (declarationIndex >= 0 || len(declarations) < 512) {
			state = advanceAvailabilityRules(account, declarations)
			expires := now.AddDate(0, 1, 0)
			declaration := service.ModelAvailabilityDeclaration{AccountID: accountID, Model: attempt.Model, Endpoint: attempt.Endpoint, IdentityRevision: state.IdentityRevision, RulesRevision: state.RulesRevision, Reason: "successful bounded probe", ConfirmedAt: now, ExpiresAt: &expires, EvidenceKind: service.ModelEvidenceProbe}
			declaration.CredentialAccountID, declaration.CredentialIdentityRevision = attempt.CredentialAccountID, attempt.CredentialIdentityRevision
			if declarationIndex < 0 {
				declarations = append(declarations, declaration)
			} else {
				declarations[declarationIndex] = declaration
			}
			invalidate = true
		} else if outcome == service.ModelProbeMissing && declarationIndex >= 0 {
			state = advanceAvailabilityRules(account, declarations)
			declarations[declarationIndex].RevokedAt = &now
			invalidate = true
		}
		if invalidate {
			patch[service.ModelAvailabilityExtraKey], patch[service.UpstreamModelCatalogStateExtraKey] = declarations, state
		}
	}
	if err := writeAvailabilityAudit(ctx, transaction, accountID, 0, "account.model_probe.complete", now, map[string]any{"account_id": accountID, "attempt_id": attemptID, "outcome": outcome}); err != nil {
		return err
	}
	if err := persistAvailabilityPatch(ctx, transaction, accountID, patch, invalidate); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	if invalidate {
		repository.syncSchedulerAccountSnapshot(ctx, accountID)
	}
	return nil
}
