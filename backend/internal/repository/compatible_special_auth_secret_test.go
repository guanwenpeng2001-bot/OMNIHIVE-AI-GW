package repository

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCopilotNousRepositoryEncryptedReferenceOwnership(t *testing.T) {
	for _, platform := range []string{"github_copilot", "nous_research"} {
		t.Run(platform, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, database.Close())
			}()
			encryptor, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32), EncryptionKeyConfigured: true}})
			require.NoError(t, err)
			metadata := service.CredentialSecret{Reference: "fixture-reference", WorkspaceID: 9, AccountID: 731, Version: 3}
			ciphertext, err := service.EncryptCredentialSecret(encryptor, metadata, map[string]any{"api_key": "synthetic-secret"})
			require.NoError(t, err)
			require.NotContains(t, ciphertext, "synthetic-secret")
			read := func() {
				mock.ExpectQuery("SELECT reference, workspace_id, account_id, version, updated_at, revoked_at, ciphertext").WithArgs(metadata.AccountID).WillReturnRows(sqlmock.NewRows([]string{"reference", "workspace_id", "account_id", "version", "updated_at", "revoked_at", "ciphertext"}).AddRow(metadata.Reference, metadata.WorkspaceID, metadata.AccountID, metadata.Version, time.Now(), nil, ciphertext))
			}
			inline := map[string]any{service.CredentialSecretReferenceKey: metadata.Reference, service.CredentialSecretVersionKey: metadata.Version, "auth_mode": "official_proxy", "account_mode": "payg", "base_url": "https://operator.test/v1"}
			if platform == "github_copilot" {
				inline["auth_mode"] = "fine_grained_pat"
				inline["base_url"] = ""
			}
			read()
			retained, err := prepareReferencedCredentials(context.Background(), database, encryptor, metadata.AccountID, metadata.WorkspaceID, inline, maps.Clone(inline))
			require.NoError(t, err)
			require.NotContains(t, retained, "api_key")
			require.Equal(t, metadata.Reference, retained[service.CredentialSecretReferenceKey])
			repository := &accountRepository{credentialEncryptor: encryptor}
			account := &service.Account{ID: metadata.AccountID, WorkspaceID: metadata.WorkspaceID, Platform: platform, Type: service.AccountTypeAPIKey, Credentials: retained}
			read()
			require.NoError(t, repository.resolveAccountCredentials(context.Background(), database, account))
			require.Equal(t, "synthetic-secret", account.GetCredential("api_key"))
			require.Equal(t, metadata.Reference, account.Credentials[service.CredentialSecretReferenceKey])
			require.Equal(t, "payg", account.GetCredential("account_mode"))
			account.WorkspaceID++
			read()
			require.ErrorIs(t, repository.resolveAccountCredentials(context.Background(), database, account), service.ErrCredentialSecretNotFound)
			account.WorkspaceID = metadata.WorkspaceID
			account.Credentials[service.CredentialSecretReferenceKey] = "foreign-reference"
			read()
			require.ErrorIs(t, repository.resolveAccountCredentials(context.Background(), database, account), service.ErrCredentialSecretNotFound)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
