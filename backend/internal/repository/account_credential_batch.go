package repository

import (
	"context"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type storedCredentialSecret struct {
	metadata   service.CredentialSecret
	ciphertext string
}

// One query for the whole ID set, on the caller's transaction/client. Neither
// query arguments nor returned ciphertext/credentials are logged.
func readCredentialSecretsBatch(ctx context.Context, database credentialSQL, accountIDs []int64) (map[int64]storedCredentialSecret, error) {
	secrets := make(map[int64]storedCredentialSecret, len(accountIDs))
	if len(accountIDs) == 0 {
		return secrets, nil
	}
	rows, err := database.QueryContext(ctx, `SELECT reference, workspace_id, account_id, version, updated_at, revoked_at, ciphertext
		FROM credential_secrets WHERE account_id = ANY($1)`+workspaceClause(ctx, "workspace_id"), pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var record storedCredentialSecret
		m := &record.metadata
		if err := rows.Scan(&m.Reference, &m.WorkspaceID, &m.AccountID, &m.Version, &m.UpdatedAt, &m.RevokedAt, &record.ciphertext); err != nil {
			return nil, err
		}
		if _, duplicate := secrets[m.AccountID]; duplicate {
			return nil, service.ErrCredentialSecretInvalid
		}
		secrets[m.AccountID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return secrets, nil
}

// Validate the persisted pointer before decrypting. A row/secret race is a
// conflict, never permission to combine a stale revision with newer material.
func (r *accountRepository) applyStoredCredentialSecret(ctx context.Context, account *service.Account, metadata *service.CredentialSecret, ciphertext string) error {
	reference, referenced := service.CredentialReference(account.Credentials)
	version := service.CredentialVersion(account.Credentials)
	if !referenced || reference == "" || version < 1 {
		return service.ErrCredentialSecretInvalid
	}
	if metadata == nil || metadata.AccountID != account.ID || metadata.WorkspaceID != account.WorkspaceID || metadata.Reference != reference {
		return service.ErrCredentialSecretNotFound
	}
	if metadata.Version != version {
		return service.ErrCredentialSecretConflict
	}
	for field := range account.Credentials {
		if service.IsSensitiveCredentialKey(field) {
			return service.ErrCredentialSecretInvalid
		}
	}
	secrets, err := service.ResolveCredentialSecret(ctx, r.credentialEncryptor, *metadata, ciphertext)
	if err != nil {
		return err
	}
	resolved := make(map[string]any, len(account.Credentials)+len(secrets))
	for field, value := range account.Credentials {
		resolved[field] = value
	}
	for field, value := range secrets {
		// Encrypted configuration/reference injection must not overwrite the
		// identity that was just checked or bypass Account.MarshalJSON redaction.
		if !service.IsSensitiveCredentialKey(field) {
			return service.ErrCredentialSecretInvalid
		}
		resolved[field] = value
	}
	resolved[service.CredentialSecretVersionKey] = metadata.Version
	account.Credentials = resolved
	return nil
}

func credentialReadUnavailable(err error) bool {
	return errors.Is(err, service.ErrCredentialSecretNotFound) || errors.Is(err, service.ErrCredentialSecretInvalid) ||
		errors.Is(err, service.ErrCredentialSecretRevoked) || errors.Is(err, service.ErrCredentialSecretConflict)
}

// General reads are all-or-nothing. Candidate scans may omit an unusable row,
// but never return its unresolved credentials or fall back to inline secrets.
// Database/transport errors always fail the whole read, including scans.
func (r *accountRepository) resolveAccountCredentialsBatch(ctx context.Context, database credentialSQL, accounts []*service.Account, skipUnavailable bool) ([]*service.Account, error) {
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		account.AttachEgress(r.egressRuntime)
		account.CredentialRevision = credentialRevision(account.Credentials)
		if _, referenced := service.CredentialReference(account.Credentials); referenced {
			ids = append(ids, account.ID)
		}
	}
	records, err := readCredentialSecretsBatch(ctx, database, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*service.Account, 0, len(accounts))
	for _, account := range accounts {
		if _, referenced := service.CredentialReference(account.Credentials); referenced {
			record, found := records[account.ID]
			var metadata *service.CredentialSecret
			if found {
				metadata = &record.metadata
			}
			if err := r.applyStoredCredentialSecret(ctx, account, metadata, record.ciphertext); err != nil {
				if skipUnavailable && credentialReadUnavailable(err) {
					continue
				}
				return nil, err
			}
		}
		out = append(out, account)
	}
	return out, nil
}
