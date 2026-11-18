package repository

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func ProvideAccountRepositoryWithSecrets(client *dbent.Client, database *sql.DB, cache service.SchedulerCache, encryptor service.SecretEncryptor, runtime *service.ProxyGroupRuntime) service.AccountRepository {
	repository := newAccountRepositoryWithSQL(client, database, cache)
	repository.credentialEncryptor = encryptor
	repository.egressRuntime = runtime
	return repository
}

func ProvideAdminAccountRepositoryWithSecrets(client *dbent.Client, database *sql.DB, cache service.SchedulerCache, encryptor service.SecretEncryptor, runtime *service.ProxyGroupRuntime) service.AdminAccountRepository {
	repository := newAccountRepositoryWithSQL(client, database, cache)
	repository.credentialEncryptor = encryptor
	repository.egressRuntime = runtime
	return repository
}

func (r *accountRepository) resolveAccountCredentials(ctx context.Context, database credentialSQL, account *service.Account) error {
	account.AttachEgress(r.egressRuntime)
	account.CredentialRevision = credentialRevision(account.Credentials)
	_, referenced := service.CredentialReference(account.Credentials)
	if !referenced {
		return nil
	}
	metadata, ciphertext, err := readCredentialSecret(ctx, database, account.ID)
	if err != nil {
		return err
	}
	return r.applyStoredCredentialSecret(ctx, account, metadata, ciphertext)
}

// UpdateCredentialsCAS preserves the expected read revision even if a caller
// mutated its in-memory credentials before asking for persistence.
func (r *accountRepository) UpdateCredentialsCAS(ctx context.Context, account *service.Account, credentials map[string]any) error {
	if account == nil {
		return service.ErrAccountNilInput
	}
	return r.UpdateCredentials(service.WithCredentialRevision(ctx, account.CredentialRevision), account.ID, credentials)
}

func (r *accountRepository) prepareCredentialUpdate(ctx context.Context, client *dbent.Client, accountID int64, credentials map[string]any) (map[string]any, error) {
	workspaceID, current, _, err := lockCredentialAccount(ctx, client, accountID)
	if err != nil {
		return nil, err
	}
	if revision := service.ExpectedCredentialRevision(ctx); revision != "" && revision != credentialRevision(current) {
		slog.WarnContext(ctx, "credential_update_cas_conflict", "account_id", accountID)
		return nil, service.ErrCredentialSecretConflict
	}
	prepared, err := prepareReferencedCredentials(ctx, client, r.credentialEncryptor, accountID, workspaceID, current, credentials)
	if errors.Is(err, service.ErrCredentialSecretConflict) {
		slog.WarnContext(ctx, "credential_update_cas_conflict", "account_id", accountID)
	}
	return prepared, err
}

// mutateReferencedOAuthIfUnchanged locks the account row, resolves the
// referenced secret and compares the full credential document (and proxy) with
// the snapshot the caller based its upstream attempt on. apply only runs on an
// exact match, inside the same transaction, so concurrent writers lose cleanly.
func (r *accountRepository) mutateReferencedOAuthIfUnchanged(ctx context.Context, accountID int64, platform string, expected map[string]any, proxyID *int64, proxyGroupID *int64, checkProxy bool, apply func(lockedCtx context.Context, repository *accountRepository, current *service.Account) (bool, error)) (bool, error) {
	transaction, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = transaction.Rollback() }()
	lockedCtx := dbent.NewTxContext(ctx, transaction)
	client := transaction.Client()
	if _, _, _, err := lockCredentialAccount(lockedCtx, client, accountID); err != nil {
		return false, err
	}
	entity, err := client.Account.Get(lockedCtx, accountID)
	if err != nil {
		return false, err
	}
	current := accountEntityToService(entity)
	if err := r.resolveAccountCredentials(lockedCtx, client, current); err != nil {
		return false, err
	}
	if current.Platform != platform || current.Type != service.AccountTypeOAuth || credentialRevision(current.Credentials) != credentialRevision(expected) || (checkProxy && !reflect.DeepEqual(proxyID, current.ProxyID)) || ((checkProxy || platform == service.PlatformCursor) && !reflect.DeepEqual(proxyGroupID, current.ProxyGroupID)) {
		slog.WarnContext(ctx, "oauth_refresh_credential_cas_conflict", "account_id", accountID)
		return false, nil
	}
	repository := &accountRepository{client: client, sql: client, credentialEncryptor: r.credentialEncryptor}
	applied, err := apply(lockedCtx, repository, current)
	if err != nil || !applied {
		return false, err
	}
	if err := transaction.Commit(); err != nil {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, accountID)
	return true, nil
}

func (r *accountRepository) updateReferencedOAuthCredentials(ctx context.Context, accountID int64, platform string, expected map[string]any, proxyID *int64, proxyGroupID *int64, checkProxy bool, incoming map[string]any) (bool, error) {
	return r.mutateReferencedOAuthIfUnchanged(ctx, accountID, platform, expected, proxyID, proxyGroupID, checkProxy, func(lockedCtx context.Context, repository *accountRepository, current *service.Account) (bool, error) {
		if err := repository.UpdateCredentials(service.WithCredentialRevision(lockedCtx, current.CredentialRevision), accountID, incoming); err != nil {
			return false, err
		}
		return true, nil
	})
}

// markOAuthErrorLocked mirrors the status-only assignments of the plaintext CAS
// variants; credential (and proxy) equality is already enforced by the resolved
// document comparison, and the scheduler publication shares the transaction.
func (r *accountRepository) markOAuthErrorLocked(ctx context.Context, accountID int64, errorMsg string, activeOnly bool) (bool, error) {
	query := `UPDATE accounts SET status=$1, error_message=$2, schedulable=FALSE, updated_at=NOW() WHERE id=$3 AND deleted_at IS NULL`
	args := []any{service.StatusError, errorMsg, accountID}
	if activeOnly {
		query += ` AND status=$4`
		args = append(args, service.StatusActive)
	}
	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}

func (r *accountRepository) markOAuthTempUnschedulableLocked(ctx context.Context, accountID int64, until time.Time, reason string, activeOnly bool) (bool, error) {
	query := `UPDATE accounts SET temp_unschedulable_until=$1, temp_unschedulable_reason=$2, updated_at=NOW()
		WHERE id=$3 AND deleted_at IS NULL AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until < $1)`
	args := []any{until, reason, accountID}
	if activeOnly {
		query += ` AND status=$4`
		args = append(args, service.StatusActive)
	}
	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}
