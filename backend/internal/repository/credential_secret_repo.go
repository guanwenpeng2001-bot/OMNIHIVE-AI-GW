package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type credentialSecretRepository struct {
	database  *sql.DB
	encryptor service.SecretEncryptor
}

func NewCredentialSecretRepository(database *sql.DB, encryptor service.SecretEncryptor) service.CredentialSecretRepository {
	return &credentialSecretRepository{database: database, encryptor: encryptor}
}

type credentialSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readCredentialSecret(ctx context.Context, database credentialSQL, accountID int64) (*service.CredentialSecret, string, error) {
	rows, err := database.QueryContext(ctx, `SELECT reference, workspace_id, account_id, version, updated_at, revoked_at, ciphertext
		FROM credential_secrets WHERE account_id=$1`+workspaceClause(ctx, "workspace_id"), accountID)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, "", rows.Err()
		}
		return nil, "", service.ErrCredentialSecretNotFound
	}
	metadata := &service.CredentialSecret{}
	var ciphertext string
	if err := rows.Scan(&metadata.Reference, &metadata.WorkspaceID, &metadata.AccountID, &metadata.Version, &metadata.UpdatedAt, &metadata.RevokedAt, &ciphertext); err != nil {
		return nil, "", err
	}
	return metadata, ciphertext, nil
}

func (r *credentialSecretRepository) Metadata(ctx context.Context, accountID int64) (*service.CredentialSecret, error) {
	metadata, _, err := readCredentialSecret(ctx, r.database, accountID)
	return metadata, err
}

func credentialRevision(credentials map[string]any) string {
	payload, _ := json.Marshal(credentials)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// accountRevisionMatches accepts the reviewed row timestamp either in the exact
// serialized form the API served or any RFC3339 representation of the same
// instant, so a session timezone cannot turn a fresh review into a conflict.
func accountRevisionMatches(updatedAt time.Time, revision string) bool {
	if updatedAt.UTC().Format(time.RFC3339Nano) == revision {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, revision)
	return err == nil && parsed.Equal(updatedAt)
}

func lockCredentialAccount(ctx context.Context, database credentialSQL, accountID int64) (int64, map[string]any, time.Time, error) {
	rows, err := database.QueryContext(ctx, `SELECT workspace_id, credentials, updated_at FROM accounts
		WHERE id=$1 AND deleted_at IS NULL`+workspaceClause(ctx, "workspace_id")+` FOR UPDATE`, accountID)
	if err != nil {
		return 0, nil, time.Time{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if rows.Err() != nil {
			return 0, nil, time.Time{}, rows.Err()
		}
		return 0, nil, time.Time{}, service.ErrAccountNotFound
	}
	var workspaceID int64
	var payload []byte
	var updatedAt time.Time
	if err := rows.Scan(&workspaceID, &payload, &updatedAt); err != nil {
		return 0, nil, time.Time{}, err
	}
	credentials := map[string]any{}
	if err := json.Unmarshal(payload, &credentials); err != nil {
		return 0, nil, time.Time{}, err
	}
	return workspaceID, credentials, updatedAt, nil
}

func (r *credentialSecretRepository) Convert(ctx context.Context, accountID int64, revision string) (*service.CredentialSecret, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	workspaceID, credentials, updatedAt, err := lockCredentialAccount(ctx, transaction, accountID)
	if err != nil {
		return nil, err
	}
	if _, referenced := service.CredentialReference(credentials); referenced {
		metadata, _, err := readCredentialSecret(ctx, transaction, accountID)
		return metadata, err // conversion is idempotent; never writes a second secret
	}
	if !accountRevisionMatches(updatedAt, revision) {
		return nil, service.ErrCredentialSecretConflict
	}
	inline, secrets := service.SplitCredentialSecrets(credentials)
	if len(secrets) == 0 {
		return nil, service.ErrCredentialSecretInvalid
	}
	metadata := &service.CredentialSecret{Reference: uuid.NewString(), AccountID: accountID, WorkspaceID: workspaceID, Version: 1, UpdatedAt: time.Now().UTC()}
	ciphertext, err := service.EncryptVerifiedCredential(ctx, r.encryptor, *metadata, secrets)
	if err != nil {
		return nil, err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO credential_secrets(reference,workspace_id,account_id,version,ciphertext,updated_at,api_key_digest)
		VALUES($1,$2,$3,1,$4,$5,$6)`, metadata.Reference, workspaceID, accountID, ciphertext, metadata.UpdatedAt, service.AccountAPIKeyDigest(secrets)); err != nil {
		return nil, err
	}
	inline[service.CredentialSecretReferenceKey] = metadata.Reference
	inline[service.CredentialSecretVersionKey] = metadata.Version
	if err := writeReferencedAccount(ctx, transaction, accountID, inline); err != nil {
		return nil, err
	}
	if err := appendCredentialAudit(ctx, transaction, *metadata, "convert", 0, secrets); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	service.InvalidateModelCatalogCaches()
	return metadata, nil
}

func InstallCredentialVersion(ctx context.Context, tx *sql.Tx, metadata service.CredentialSecret) error {
	workspace, current, _, err := lockCredentialAccount(ctx, tx, metadata.AccountID)
	if err != nil {
		return err
	}
	if workspace != metadata.WorkspaceID {
		return service.ErrCredentialSecretNotFound
	}
	inline, _ := service.SplitCredentialSecrets(current)
	inline[service.CredentialSecretReferenceKey] = metadata.Reference
	inline[service.CredentialSecretVersionKey] = metadata.Version
	return writeReferencedAccount(ctx, tx, metadata.AccountID, inline)
}

func writeReferencedAccount(ctx context.Context, database credentialSQL, accountID int64, credentials map[string]any) error {
	payload, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, `UPDATE accounts SET credentials=$2::jsonb, updated_at=NOW(),
		extra=COALESCE(extra,'{}'::jsonb)-'upstream_billing_probe'-'ollama_cloud_usage_snapshot' - `+cloudQuotaInvalidationSQL("$2::jsonb")+`
		WHERE id=$1`+workspaceClause(ctx, "workspace_id"), accountID, string(payload))
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type,account_id,payload,created_at)
		VALUES($1,$2,'{}'::jsonb,NOW())`, service.SchedulerOutboxEventAccountChanged, accountID)
	return err
}

func (r *credentialSecretRepository) Rotate(ctx context.Context, accountID, expectedVersion int64, incoming map[string]any) (*service.CredentialSecret, error) {
	return r.mutate(ctx, accountID, expectedVersion, incoming, false)
}

func (r *credentialSecretRepository) Revoke(ctx context.Context, accountID, expectedVersion int64) (*service.CredentialSecret, error) {
	return r.mutate(ctx, accountID, expectedVersion, nil, true)
}

func (r *credentialSecretRepository) mutate(ctx context.Context, accountID, expectedVersion int64, incoming map[string]any, revoke bool) (*service.CredentialSecret, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	_, credentials, _, err := lockCredentialAccount(ctx, transaction, accountID)
	if err != nil {
		return nil, err
	}
	metadata, ciphertext, err := readCredentialSecret(ctx, transaction, accountID)
	if err != nil {
		return nil, err
	}
	if revoke && metadata.RevokedAt != nil && metadata.Version == expectedVersion+1 {
		return metadata, nil
	}
	if metadata.RevokedAt != nil {
		return nil, service.ErrCredentialSecretRevoked
	}
	if metadata.Version != expectedVersion {
		return nil, service.ErrCredentialSecretConflict
	}
	var digest string
	action := "rotate"
	if revoke {
		action = "revoke"
		now := time.Now().UTC()
		metadata.RevokedAt = &now
		ciphertext = "" // irreversible erasure; no historical plaintext recovery
	} else {
		secrets, err := service.ResolveCredentialSecret(ctx, r.encryptor, *metadata, ciphertext)
		if err != nil {
			return nil, err
		}
		for field, value := range incoming {
			if !service.IsSensitiveCredentialKey(field) {
				return nil, service.ErrCredentialSecretInvalid
			}
			if service.IsManagementQuerySecret(field) {
				if value == nil {
					delete(secrets, field)
					continue
				}
				text, ok := value.(string)
				if !ok {
					return nil, service.ErrCredentialSecretInvalid
				}
				if strings.TrimSpace(text) == "" {
					continue
				}
			}
			secrets[field] = value
		}
		digest = service.AccountAPIKeyDigest(secrets)
		candidate := *metadata
		candidate.Version++
		ciphertext, err = service.EncryptCredentialSecret(r.encryptor, candidate, secrets)
		if err != nil {
			return nil, err
		}
	}
	if err := updateSecretCAS(ctx, transaction, metadata, expectedVersion, ciphertext, digest); err != nil {
		return nil, err
	}
	credentials[service.CredentialSecretVersionKey] = metadata.Version
	if err := writeReferencedAccount(ctx, transaction, accountID, credentials); err != nil {
		return nil, err
	}
	if err := appendCredentialAudit(ctx, transaction, *metadata, action, expectedVersion, incoming); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	service.InvalidateModelCatalogCaches()
	return metadata, nil
}

func updateSecretCAS(ctx context.Context, database credentialSQL, metadata *service.CredentialSecret, expectedVersion int64, ciphertext, digest string) error {
	result, err := database.ExecContext(ctx, `UPDATE credential_secrets SET ciphertext=$1,api_key_digest=$7,version=version+1,revoked_at=$2,updated_at=NOW()
		WHERE reference=$3 AND account_id=$4 AND workspace_id=$5 AND version=$6 AND revoked_at IS NULL`, ciphertext, metadata.RevokedAt, metadata.Reference, metadata.AccountID, metadata.WorkspaceID, expectedVersion, digest)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrCredentialSecretConflict
	}
	metadata.Version = expectedVersion + 1
	metadata.UpdatedAt = time.Now().UTC()
	return nil
}

func appendCredentialAudit(ctx context.Context, database credentialSQL, metadata service.CredentialSecret, action string, previousVersion int64, changed map[string]any) error {
	actor := service.ConfigurationActor(ctx)
	extra, err := json.Marshal(map[string]any{"workspace_id": metadata.WorkspaceID, "account_id": metadata.AccountID, "reference": metadata.Reference,
		"previous_version": previousVersion, "version": metadata.Version, "diff": service.ConfigurationDiff(nil, changed)})
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,actor_role,action,request_id,status_code,extra)
		VALUES(NULLIF($1,0),$2,$3,$4,200,$5::jsonb)`, actor.UserID, actor.Role, "credential_secret."+action, actor.RequestID, string(extra))
	return err
}

// prepareReferencedCredentials runs under the account row lock and in the same
// transaction as the account write. A converted account never falls back inline.
func prepareReferencedCredentials(ctx context.Context, database credentialSQL, encryptor service.SecretEncryptor, accountID, workspaceID int64, current, incoming map[string]any) (map[string]any, error) {
	reference, referenced := service.CredentialReference(current)
	incomingReference, incomingReferenced := service.CredentialReference(incoming)
	if !referenced {
		if incomingReferenced {
			return nil, service.ErrCredentialSecretInvalid
		}
		if hasCredentialSecrets(incoming) {
			return createManagementQueryReference(ctx, database, encryptor, accountID, workspaceID, incoming)
		}
		return incoming, nil
	}
	if incomingReferenced && reference != incomingReference {
		return nil, service.ErrCredentialSecretNotFound
	}
	metadata, ciphertext, err := readCredentialSecret(ctx, database, accountID)
	if err != nil {
		return nil, err
	}
	if metadata.Reference != reference || metadata.WorkspaceID != workspaceID {
		return nil, service.ErrCredentialSecretNotFound
	}
	secrets, err := service.ResolveCredentialSecret(ctx, encryptor, *metadata, ciphertext)
	if err != nil {
		return nil, err
	}
	inline, incomingSecrets := service.SplitCredentialSecrets(incoming)
	changed := map[string]any{}
	for field, value := range incomingSecrets {
		if service.IsManagementQuerySecret(field) {
			if value == nil {
				if _, exists := secrets[field]; exists {
					changed[field] = nil
					delete(secrets, field)
				}
				continue
			}
			text, ok := value.(string)
			if !ok {
				return nil, service.ErrCredentialSecretInvalid
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
		}
		if !reflect.DeepEqual(secrets[field], value) {
			changed[field] = value
			secrets[field] = value
		}
	}
	if len(changed) > 0 {
		expectedVersion := service.CredentialVersion(incoming)
		if expectedVersion == 0 && service.ExpectedCredentialRevision(ctx) != "" {
			expectedVersion = metadata.Version
		}
		if expectedVersion != metadata.Version {
			return nil, service.ErrCredentialSecretConflict
		}
		candidate := *metadata
		candidate.Version++
		ciphertext, err := service.EncryptCredentialSecret(encryptor, candidate, secrets)
		if err != nil {
			return nil, err
		}
		if err := updateSecretCAS(ctx, database, metadata, expectedVersion, ciphertext, service.AccountAPIKeyDigest(secrets)); err != nil {
			return nil, err
		}
		if err := appendCredentialAudit(ctx, database, *metadata, "update", expectedVersion, changed); err != nil {
			return nil, err
		}
	}
	inline[service.CredentialSecretReferenceKey] = reference
	inline[service.CredentialSecretVersionKey] = metadata.Version
	return inline, nil
}

func (r *accountRepository) bulkUpdateCredentialSecrets(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if r.client == nil || r.credentialEncryptor == nil {
		return 0, service.ErrCredentialSecretInvalid
	}
	tx := dbent.TxFromContext(ctx)
	owned := false
	if tx == nil {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
			return 0, err
		}
		if tx != nil {
			owned = true
			defer func() { _ = tx.Rollback() }()
			ctx = dbent.NewTxContext(ctx, tx)
		}
	}
	client := clientFromContext(ctx, r.client)
	if _, err := lockLegacyAccountConfigurations(ctx, client, ids); err != nil {
		return 0, err
	}
	if err := validateBulkCopilotNousCredentials(ctx, client, ids, updates.Credentials); err != nil {
		return 0, err
	}
	inline, secrets := service.SplitCredentialSecrets(updates.Credentials)
	updates.Credentials = inline
	if _, err := r.BulkUpdate(ctx, ids, updates); err != nil {
		return 0, err
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	var count int64
	for _, id := range slices.Compact(ids) {
		_, current, _, err := lockCredentialAccount(ctx, client, id)
		if errors.Is(err, service.ErrAccountNotFound) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if current == nil {
			current = map[string]any{}
		}
		maps.Copy(current, secrets)
		if err = r.UpdateCredentials(ctx, id, current); err != nil {
			return 0, err
		}
		count++
	}
	if owned {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
	}
	return count, nil
}
