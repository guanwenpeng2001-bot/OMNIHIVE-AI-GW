package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type volcengineCredentialCapture struct{ value *string }

type rejectPlainQuotaSecrets struct{}

func (rejectPlainQuotaSecrets) ConvertValue(value any) (driver.Value, error) {
	converted, err := driver.DefaultParameterConverter.ConvertValue(value)
	if err != nil {
		return nil, err
	}
	var text string
	switch v := converted.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	}
	if strings.Contains(text, "management-secret") || strings.Contains(text, "management-access") || strings.Contains(text, "inference-secret") {
		return nil, fmt.Errorf("plaintext secret reached SQL")
	}
	return converted, nil
}

func (c volcengineCredentialCapture) Match(value driver.Value) bool {
	text, ok := value.(string)
	if ok {
		*c.value = text
	}
	return ok
}

func quotaCredentialMap() map[string]any {
	return map[string]any{"api_key": "inference-secret", "volcengine_access_key_id": "management-access", "volcengine_secret_access_key": "management-secret", "account_mode": "coding", "tencent_query_secret_id": "management-access", "tencent_query_secret_key": "management-secret", "usage_query_token": "management-secret", "byteplus_access_key_id": "management-access", "byteplus_secret_access_key": "management-secret"}
}

func TestVolcengineQueryReferenceCreateEncryptsAndFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	}()
	enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
	require.NoError(t, err)
	var reference, ciphertext, audit string
	mock.ExpectExec("INSERT INTO credential_secrets").WithArgs(volcengineCredentialCapture{&reference}, int64(7), int64(17), volcengineCredentialCapture{&ciphertext}, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO audit_logs").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "credential_secret.convert", sqlmock.AnyArg(), volcengineCredentialCapture{&audit}).WillReturnResult(sqlmock.NewResult(1, 1))
	inline, err := prepareReferencedCredentials(context.Background(), db, enc, 17, 7, map[string]any{}, quotaCredentialMap())
	require.NoError(t, err)
	for _, key := range []string{"api_key", "volcengine_access_key_id", "volcengine_secret_access_key", "tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"} {
		require.NotContains(t, inline, key)
	}
	require.Equal(t, reference, inline[service.CredentialSecretReferenceKey])
	require.NotContains(t, ciphertext, "management-secret")
	require.NotContains(t, audit, "management-secret")
	require.NotContains(t, audit, "management-access")
	resolved, err := service.ResolveCredentialSecret(context.Background(), enc, service.CredentialSecret{Reference: reference, WorkspaceID: 7, AccountID: 17, Version: 1}, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "management-secret", resolved["volcengine_secret_access_key"])
	require.Equal(t, "inference-secret", resolved["api_key"])
	_, err = prepareReferencedCredentials(context.Background(), db, nil, 17, 7, nil, quotaCredentialMap())
	require.ErrorIs(t, err, service.ErrCredentialSecretInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVolcengineQueryReferenceUpdateClearAndRetain(t *testing.T) {
	for _, operation := range []string{"omit", "blank", "rotate", "clear", "conflict"} {
		t.Run(operation, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, db.Close())
			}()
			enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
			require.NoError(t, err)
			metadata := service.CredentialSecret{Reference: "quota-reference", WorkspaceID: 7, AccountID: 17, Version: 3}
			_, secrets := service.SplitCredentialSecrets(quotaCredentialMap())
			ciphertext, err := service.EncryptCredentialSecret(enc, metadata, secrets)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT reference, workspace_id, account_id, version, updated_at, revoked_at, ciphertext").WithArgs(int64(17)).WillReturnRows(sqlmock.NewRows([]string{"reference", "workspace_id", "account_id", "version", "updated_at", "revoked_at", "ciphertext"}).AddRow(metadata.Reference, 7, 17, 3, time.Now(), nil, ciphertext))
			current := map[string]any{service.CredentialSecretReferenceKey: metadata.Reference, service.CredentialSecretVersionKey: int64(3), "account_mode": "coding"}
			incoming := maps.Clone(current)
			switch operation {
			case "blank":
				for _, field := range []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"} {
					incoming[field] = " "
				}
				incoming["volcengine_secret_access_key"] = ""
			case "rotate", "conflict":
				for _, field := range []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"} {
					incoming[field] = "rotated"
				}
				incoming["volcengine_secret_access_key"] = "rotated"
			case "clear":
				for _, field := range []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"} {
					incoming[field] = nil
				}
				incoming["volcengine_secret_access_key"] = nil
				incoming["volcengine_access_key_id"] = nil
			}
			if operation == "rotate" || operation == "clear" || operation == "conflict" {
				affected := int64(1)
				if operation == "conflict" {
					affected = 0
				}
				mock.ExpectExec("UPDATE credential_secrets SET ciphertext").WithArgs(volcengineCredentialCapture{&ciphertext}, nil, metadata.Reference, int64(17), int64(7), int64(3), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, affected))
				if operation != "conflict" {
					mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
					metadata.Version = 4
				}
			}
			out, err := prepareReferencedCredentials(context.Background(), db, enc, 17, 7, current, incoming)
			if operation == "conflict" {
				require.ErrorIs(t, err, service.ErrCredentialSecretConflict)
				require.NoError(t, mock.ExpectationsWereMet())
				return
			}
			require.NoError(t, err)
			require.Equal(t, metadata.Reference, out[service.CredentialSecretReferenceKey])
			require.NotContains(t, out, "volcengine_secret_access_key")
			resolved, err := service.ResolveCredentialSecret(context.Background(), enc, metadata, ciphertext)
			require.NoError(t, err)
			require.Equal(t, "inference-secret", resolved["api_key"])
			projected := dto.AccountFromService(&service.Account{ID: 17, Platform: "azure_openai", Type: "apikey", Credentials: resolved})
			for _, response := range []any{projected, dto.AccountListItemFromAccount(projected)} {
				encoded, encodeErr := json.Marshal(response)
				require.NoError(t, encodeErr)
				require.NotContains(t, string(encoded), "inference-secret")
				require.NotContains(t, string(encoded), "management-secret")
				require.NotContains(t, string(encoded), "management-access")
				require.NotContains(t, string(encoded), "rotated")
			}
			require.Equal(t, operation != "clear", projected.CredentialsStatus["has_usage_query_token"])

			for _, field := range []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"} {
				require.Equal(t, operation != "clear", projected.CredentialsStatus["has_"+field])
				require.NotContains(t, out, field)
				switch operation {
				case "clear":
					require.NotContains(t, resolved, field)
				case "rotate":
					require.Equal(t, "rotated", resolved[field])
				default:
					require.Equal(t, quotaCredentialMap()[field], resolved[field])
				}
			}
			switch operation {
			case "clear":
				require.NotContains(t, resolved, "volcengine_secret_access_key")
				require.NotContains(t, resolved, "volcengine_access_key_id")
			case "rotate":
				require.Equal(t, "rotated", resolved["volcengine_secret_access_key"])
			default:
				require.Equal(t, "management-secret", resolved["volcengine_secret_access_key"])
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVolcengineQueryCreateTransactionRollback(t *testing.T) {
	// Both public create entrypoints must enclose secret creation in the same
	// transaction. A secret-store failure must leave no persisted account row.
	for _, withGroups := range []bool{false, true} {
		t.Run(map[bool]string{false: "Create", true: "CreateWithGroups"}[withGroups], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, db.Close())
			}()
			enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			repo := newAccountRepositoryWithSQL(client, db, nil)
			repo.credentialEncryptor = enc
			a := &service.Account{Name: "quota-account", Platform: service.PlatformVolcengine, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: quotaCredentialMap()}
			mock.ExpectBegin()
			mock.ExpectQuery(`INSERT INTO "accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
			mock.ExpectExec("INSERT INTO credential_secrets").WillReturnError(errors.New("synthetic secret store unavailable"))
			mock.ExpectRollback()
			if withGroups {
				err = repo.CreateWithAccountGroups(context.Background(), a, nil)
			} else {
				err = repo.Create(context.Background(), a)
			}
			require.ErrorContains(t, err, "synthetic secret store unavailable")
			require.Equal(t, "management-secret", a.Credentials["volcengine_secret_access_key"])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVolcengineQuerySecretsRejectBulkInlineWrite(t *testing.T) {
	repo := newAccountRepositoryWithSQL(nil, nil, nil)
	_, err := repo.BulkUpdate(context.Background(), []int64{17}, service.AccountBulkUpdate{Credentials: map[string]any{"volcengine_secret_access_key": "secret"}})
	require.ErrorIs(t, err, service.ErrCredentialSecretInvalid)
	// No plaintext accidentally included in the generic serialized config.
	inline, _ := service.SplitCredentialSecrets(quotaCredentialMap())
	raw, err := json.Marshal(inline)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "management-secret")
}

func TestVolcengineQueryCreateAtomicSuccess(t *testing.T) {
	for _, withGroups := range []bool{false, true} {
		t.Run(fmt.Sprint(withGroups), func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.ValueConverterOption(rejectPlainQuotaSecrets{}))
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, db.Close())
			}()
			enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			repo := newAccountRepositoryWithSQL(client, db, nil)
			repo.credentialEncryptor = enc
			a := &service.Account{Name: "quota-account", Platform: service.PlatformVolcengine, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: quotaCredentialMap()}
			mock.ExpectBegin()
			if withGroups {
				mock.ExpectQuery("SELECT id FROM groups").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
			}
			mock.ExpectQuery(`INSERT INTO "accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
			mock.ExpectExec("INSERT INTO credential_secrets").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec(`UPDATE "accounts"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT .* FROM "accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
			if withGroups {
				mock.ExpectQuery(`SELECT .* FROM "groups"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectExec(`INSERT INTO "account_groups"`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			if withGroups {
				err = repo.CreateWithAccountGroups(context.Background(), a, []service.AccountGroup{{GroupID: 99, Priority: 2}})
			} else {
				err = repo.Create(context.Background(), a)
			}
			require.NoError(t, err)
			require.NotEmpty(t, a.Credentials[service.CredentialSecretReferenceKey])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestInferenceCredentialCreateAtomic(t *testing.T) {
	for _, withGroups := range []bool{false, true} {
		t.Run(fmt.Sprint(withGroups), func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.ValueConverterOption(rejectPlainQuotaSecrets{}))
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, db.Close())
			}()
			enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			repo := newAccountRepositoryWithSQL(client, db, nil)
			repo.credentialEncryptor = enc
			a := &service.Account{Name: "quota-account", Platform: service.PlatformVolcengine, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "inference-secret"}}
			mock.ExpectBegin()
			if withGroups {
				mock.ExpectQuery("SELECT id FROM groups").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
			}
			mock.ExpectQuery(`INSERT INTO "accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
			mock.ExpectExec("INSERT INTO credential_secrets").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec(`UPDATE "accounts"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT .* FROM "accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
			if withGroups {
				mock.ExpectQuery(`SELECT .* FROM "groups"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectExec(`INSERT INTO "account_groups"`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			if withGroups {
				err = repo.CreateWithAccountGroups(context.Background(), a, []service.AccountGroup{{GroupID: 99, Priority: 2}})
			} else {
				err = repo.Create(context.Background(), a)
			}
			require.NoError(t, err)
			require.NotEmpty(t, a.Credentials[service.CredentialSecretReferenceKey])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVolcengineUpdateCredentialsEncryptionFailureRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	}()
	enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newAccountRepositoryWithSQL(client, db, nil)
	repo.credentialEncryptor = enc
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT workspace_id, credentials, updated_at FROM accounts").WithArgs(int64(17)).WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "credentials", "updated_at"}).AddRow(7, []byte(`{"api_key":"old-inference"}`), time.Now()))
	mock.ExpectExec("INSERT INTO credential_secrets").WillReturnError(errors.New("synthetic secret write failure"))
	mock.ExpectRollback()
	err = repo.UpdateCredentials(context.Background(), 17, quotaCredentialMap())
	require.ErrorContains(t, err, "synthetic secret write failure")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVolcengineSnapshotConfigurationPreservation(t *testing.T) {
	current := map[string]any{"volcengine_5h_used_percent": 70.0, "volcengine_monthly_reset_at": "current-reset"}
	incoming := map[string]any{"volcengine_5h_used_percent": 2.0, "volcengine_weekly_used_percent": 99.0, "note": "keep"}
	mergeAccountRuntimeExtra(incoming, current)
	require.Equal(t, 70.0, incoming["volcengine_5h_used_percent"])
	require.Equal(t, "current-reset", incoming["volcengine_monthly_reset_at"])
	require.NotContains(t, incoming, "volcengine_weekly_used_percent")
	require.Equal(t, "keep", incoming["note"])
}

func TestManagementQuerySecretAloneTriggersEncryption(t *testing.T) {
	for _, field := range []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token", "byteplus_access_key_id", "byteplus_secret_access_key"} {
		t.Run(field, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, database.Close())
			}()
			encryptor, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
			require.NoError(t, err)
			credentials := map[string]any{"api_key": "inference-secret", field: "management-secret", "usage_query_user_id": "user"}
			var reference, ciphertext string
			mock.ExpectExec("INSERT INTO credential_secrets").WithArgs(volcengineCredentialCapture{&reference}, int64(7), int64(17), volcengineCredentialCapture{&ciphertext}, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
			inline, err := prepareReferencedCredentials(context.Background(), database, encryptor, 17, 7, nil, credentials)
			require.NoError(t, err)
			require.NotContains(t, inline, field)
			require.NotContains(t, inline, "api_key")
			require.Equal(t, "user", inline["usage_query_user_id"])
			resolved, err := service.ResolveCredentialSecret(context.Background(), encryptor, service.CredentialSecret{Reference: reference, AccountID: 17, WorkspaceID: 7, Version: 1}, ciphertext)
			require.NoError(t, err)
			require.Equal(t, "management-secret", resolved[field])
			require.Equal(t, "inference-secret", resolved["api_key"])
			projected := dto.AccountFromService(&service.Account{ID: 17, Platform: "azure_openai", Type: "apikey", Credentials: resolved})
			for _, response := range []any{projected, dto.AccountListItemFromAccount(projected)} {
				encoded, encodeErr := json.Marshal(response)
				require.NoError(t, encodeErr)
				require.NotContains(t, string(encoded), "inference-secret")
				require.NotContains(t, string(encoded), "management-secret")
				require.NotContains(t, string(encoded), "management-access")
				require.NotContains(t, string(encoded), "rotated")
			}
			require.True(t, projected.CredentialsStatus["has_"+field])

			require.NoError(t, mock.ExpectationsWereMet())
			_, err = newAccountRepositoryWithSQL(nil, nil, nil).BulkUpdate(context.Background(), []int64{17}, service.AccountBulkUpdate{Credentials: credentials})
			require.ErrorIs(t, err, service.ErrCredentialSecretInvalid)
		})
	}
}

func TestCloudQuotaSnapshotConfigurationPreservation(t *testing.T) {
	current := map[string]any{"volcengine_daily_used_percent": 30.0, "hunyuan_token_plan": map[string]any{"unit": "tokens"}, "hunyuan_usage_updated_at": "old"}
	incoming := map[string]any{"volcengine_daily_used_percent": 0, "volcengine_daily_reset_at": "invented", "hunyuan_token_plan": map[string]any{"unit": "credits"}}
	mergeAccountRuntimeExtra(incoming, current)
	require.Equal(t, current, incoming)
	// Both the bulk identity path and direct secret-rotation path must erase
	// observations tied to the previous management identity.
	for _, expression := range []string{clearChangedAccountRuntimeObservationsSQL("credentials", "extra", "TRUE"), cloudQuotaInvalidationSQL("$1::jsonb")} {
		for _, key := range []string{"volcengine_daily_used_percent", "volcengine_daily_reset_at", "hunyuan_token_plan", "hunyuan_usage_updated_at", "byteplus_5h_used_percent", "byteplus_5h_reset_at", "byteplus_weekly_used_percent", "byteplus_weekly_reset_at", "byteplus_monthly_used_percent", "byteplus_monthly_reset_at", "byteplus_usage_updated_at"} {
			require.Contains(t, expression, "'"+key+"'")
		}
	}
}

func TestByteplusSnapshotConfigurationPreservation(test *testing.T) {
	current := map[string]any{"byteplus_5h_used_percent": 70.0, "byteplus_monthly_reset_at": "current-reset"}
	incoming := map[string]any{"byteplus_5h_used_percent": 2.0, "byteplus_weekly_used_percent": 99.0, "note": "keep"}
	mergeAccountRuntimeExtra(incoming, current)
	require.Equal(test, 70.0, incoming["byteplus_5h_used_percent"])
	require.Equal(test, "current-reset", incoming["byteplus_monthly_reset_at"])
	require.NotContains(test, incoming, "byteplus_weekly_used_percent")
	require.Equal(test, "keep", incoming["note"])
}

func TestByteplusBulkSeatChangeClearsOldWindows(test *testing.T) {
	executor := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, executor, nil)
	count, err := repo.BulkUpdate(context.Background(), []int64{42}, service.AccountBulkUpdate{Credentials: map[string]any{"byteplus_seat_id": "new-seat"}})
	require.NoError(test, err)
	require.Equal(test, int64(1), count)
	require.NotEmpty(test, executor.execQueries)
	query := normalizeSQLWhitespace(executor.execQueries[0])
	for _, suffix := range []string{"5h_used_percent", "5h_reset_at", "weekly_used_percent", "weekly_reset_at", "monthly_used_percent", "monthly_reset_at", "usage_updated_at"} {
		require.Contains(test, query, "'byteplus_"+suffix+"'")
	}
	require.Contains(test, query, "credentials IS DISTINCT FROM")
	require.Less(test, strings.Index(query, "extra ="), strings.Index(query, "credentials = COALESCE"))
}

type encryptedAccountDocument struct{}

func (encryptedAccountDocument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	var doc map[string]any
	if !ok || json.Unmarshal([]byte(raw), &doc) != nil {
		return false
	}
	ref, _ := service.CredentialReference(doc)
	return ref != "" && !hasCredentialSecrets(doc)
}
