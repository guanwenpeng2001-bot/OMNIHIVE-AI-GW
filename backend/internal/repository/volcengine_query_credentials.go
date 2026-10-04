package repository

import (
	"context"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const cloudQuotaSnapshotKeysSQL = "ARRAY['volcengine_5h_used_percent','volcengine_5h_reset_at','volcengine_weekly_used_percent','volcengine_weekly_reset_at','volcengine_monthly_used_percent','volcengine_monthly_reset_at','volcengine_usage_updated_at','volcengine_daily_used_percent','volcengine_daily_reset_at','hunyuan_token_plan','hunyuan_usage_updated_at','byteplus_5h_used_percent','byteplus_5h_reset_at','byteplus_weekly_used_percent','byteplus_weekly_reset_at','byteplus_monthly_used_percent','byteplus_monthly_reset_at','byteplus_usage_updated_at']::text[]"

func cloudQuotaInvalidationSQL(credentialsExpression string) string {
	return "CASE WHEN platform IN ('volcengine','hunyuan','byteplus') AND credentials IS DISTINCT FROM " + credentialsExpression + " THEN " + cloudQuotaSnapshotKeysSQL + " ELSE ARRAY[]::text[] END"
}

func hasManagementQuerySecrets(credentials map[string]any) bool {
	for field := range credentials {
		if service.IsManagementQuerySecret(field) {
			return true
		}
	}
	return false
}

// New management keys must never enter inline account JSON, even on legacy
// accounts that have not opted into referenced inference credentials.
func createManagementQueryReference(ctx context.Context, database credentialSQL, encryptor service.SecretEncryptor, accountID, workspaceID int64, credentials map[string]any) (map[string]any, error) {
	inline, secrets := service.SplitCredentialSecrets(credentials)
	for field, value := range secrets {
		if !service.IsManagementQuerySecret(field) {
			continue
		}
		if value == nil {
			delete(secrets, field)
			continue
		}
		text, ok := value.(string)
		if !ok {
			return nil, service.ErrCredentialSecretInvalid
		}
		if strings.TrimSpace(text) == "" {
			delete(secrets, field)
		}
	}
	if len(secrets) == 0 {
		return inline, nil
	}
	metadata := service.CredentialSecret{Reference: uuid.NewString(), WorkspaceID: workspaceID, AccountID: accountID, Version: 1, UpdatedAt: time.Now().UTC()}
	ciphertext, err := service.EncryptCredentialSecret(encryptor, metadata, secrets)
	if err != nil {
		return nil, err
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO credential_secrets(reference,workspace_id,account_id,version,ciphertext,updated_at) VALUES($1,$2,$3,1,$4,$5)`, metadata.Reference, workspaceID, accountID, ciphertext, metadata.UpdatedAt); err != nil {
		return nil, err
	}
	inline[service.CredentialSecretReferenceKey], inline[service.CredentialSecretVersionKey] = metadata.Reference, metadata.Version
	if err := appendCredentialAudit(ctx, database, metadata, "convert", 0, secrets); err != nil {
		return nil, err
	}
	return inline, nil
}

// Called only within CreateWithAccountGroups' transaction. The initial INSERT
// contains configuration alone; the secret and reference publish atomically.
func (r *accountRepository) createAccountWithQuerySecrets(ctx context.Context, client *dbent.Client, account *service.Account) error {
	if !hasManagementQuerySecrets(account.Credentials) {
		return createAccountRecord(ctx, client, account)
	}
	if r.credentialEncryptor == nil {
		return service.ErrCredentialSecretInvalid
	}
	credentials := account.Credentials
	inline, _ := service.SplitCredentialSecrets(credentials)
	account.Credentials = inline
	defer func() { account.Credentials = credentials }()
	if err := createAccountRecord(ctx, client, account); err != nil {
		return err
	}
	persisted, err := createManagementQueryReference(ctx, client, r.credentialEncryptor, account.ID, account.WorkspaceID, credentials)
	if err != nil {
		return err
	}
	_, err = client.Account.UpdateOneID(account.ID).SetCredentials(persisted).Save(ctx)
	if err == nil {
		for _, key := range []string{service.CredentialSecretReferenceKey, service.CredentialSecretVersionKey} {
			if value, ok := persisted[key]; ok {
				credentials[key] = value
			}
		}
	}
	return err
}
