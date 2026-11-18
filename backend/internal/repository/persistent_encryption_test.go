package repository

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func persistentTestAES(t *testing.T, configured bool) *AESEncryptor {
	t.Helper()
	encryptor, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: configured}})
	require.NoError(t, err)
	return encryptor
}

func TestPersistentAESCompatibilityAcrossInstances(t *testing.T) {
	for _, configured := range []bool{false, true} {
		first, second := persistentTestAES(t, configured), persistentTestAES(t, configured)
		require.Equal(t, configured, first.EncryptionKeyConfigured())
		ciphertext, err := first.Encrypt("synthetic token")
		require.NoError(t, err)
		plain, err := second.Decrypt(ciphertext)
		require.NoError(t, err)
		require.Equal(t, "synthetic token", plain)
	}
	metadata := service.CredentialSecret{Reference: "test", AccountID: 17, WorkspaceID: 7, Version: 1}
	secrets := map[string]any{"api_key": "synthetic"}
	ciphertext, err := service.EncryptCredentialSecret(persistentTestAES(t, true), metadata, secrets)
	require.NoError(t, err)
	resolved, err := service.ResolveCredentialSecret(context.Background(), persistentTestAES(t, true), metadata, ciphertext)
	require.NoError(t, err)
	require.Equal(t, secrets, resolved)
	// The flag only gates writes; old ciphertext remains readable.
	_, err = service.ResolveCredentialSecret(context.Background(), persistentTestAES(t, false), metadata, ciphertext)
	require.NoError(t, err)
}

func TestPersistentQueryCredentialCreationRejectedBeforeAnyInsert(t *testing.T) {
	for _, withGroups := range []bool{false, true} {
		database, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, database.Close()) })
		client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
		repo := newAccountRepositoryWithSQL(client, database, nil)
		repo.credentialEncryptor = persistentTestAES(t, false)
		credentials := quotaCredentialMap()
		original := maps.Clone(credentials)
		account := &service.Account{Name: "synthetic", Platform: service.PlatformVolcengine, Type: service.AccountTypeAPIKey, Credentials: credentials}
		mock.ExpectBegin()
		mock.ExpectRollback()
		if withGroups {
			err = repo.CreateWithAccountGroups(context.Background(), account, nil)
		} else {
			err = repo.Create(context.Background(), account)
		}
		require.ErrorIs(t, err, service.ErrEncryptionKeyRequired)
		require.Zero(t, account.ID)
		require.Equal(t, original, account.Credentials)
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
	}
}

func TestPersistentQueryCredentialUpdateRejectedBeforeCAS(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	metadata := service.CredentialSecret{Reference: "test", WorkspaceID: 7, AccountID: 17, Version: 3}
	ciphertext, err := service.EncryptCredentialSecret(persistentTestAES(t, true), metadata, map[string]any{"usage_query_token": "synthetic"})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT reference, workspace_id, account_id, version, updated_at, revoked_at, ciphertext").WithArgs(int64(17)).WillReturnRows(sqlmock.NewRows([]string{"reference", "workspace_id", "account_id", "version", "updated_at", "revoked_at", "ciphertext"}).AddRow("test", 7, 17, 3, time.Now(), nil, ciphertext))
	current := map[string]any{service.CredentialSecretReferenceKey: "test", service.CredentialSecretVersionKey: int64(3)}
	incoming := maps.Clone(current)
	incoming["usage_query_token"] = "replacement"
	original := maps.Clone(incoming)
	_, err = prepareReferencedCredentials(context.Background(), database, persistentTestAES(t, false), 17, 7, current, incoming)
	require.ErrorIs(t, err, service.ErrEncryptionKeyRequired)
	require.Equal(t, original, incoming)
	require.NoError(t, mock.ExpectationsWereMet())
	mock.ExpectClose()
}
