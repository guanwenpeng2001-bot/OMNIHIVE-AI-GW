package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWorkBuddyCredentialSecretEncryption(t *testing.T) {
	enc := persistentTestAES(t, true)
	metadata := service.CredentialSecret{Reference: "workbuddy-test", AccountID: 13, WorkspaceID: 7, Version: 1}
	secrets := map[string]any{"access_token": "workbuddy-access-secret", "refresh_token": "workbuddy-refresh-secret", "api_key": "workbuddy-key-secret"}
	encrypted, err := service.EncryptCredentialSecret(enc, metadata, secrets)
	require.NoError(t, err)
	for key, value := range secrets {
		require.True(t, service.IsSensitiveCredentialKey(key))
		require.NotContains(t, encrypted, value)
	}
	resolved, err := service.ResolveCredentialSecret(context.Background(), enc, metadata, encrypted)
	require.NoError(t, err)
	require.Equal(t, secrets, resolved)
	raw, _ := json.Marshal(map[string]any{service.CredentialSecretReferenceKey: metadata.Reference, "site": "cn", "uid": "13"})
	for _, value := range secrets {
		require.NotContains(t, string(raw), value)
	}
}

func TestWorkBuddyCredentialPersistencePostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			credentials := map[string]any{"site": "cn", "uid": "workbuddy-fixture-user"}
			secrets := map[string]string{"api_key": "workbuddy-db-key-fixture"}
			if kind == service.AccountTypeOAuth {
				secrets = map[string]string{"access_token": "workbuddy-db-access-fixture", "refresh_token": "workbuddy-db-refresh-fixture"}
			}
			for key, value := range secrets {
				credentials[key] = value
			}
			a := f.createAccount(t, f.ctx, uniqueName("workbuddy-secret"), service.PlatformWorkBuddy, kind, credentials)
			stored := f.storedCredentials(t, a.ID)
			require.NotEmpty(t, stored[service.CredentialSecretReferenceKey])
			metadata, ciphertext, err := readCredentialSecret(f.ctx, f.db, a.ID)
			require.NoError(t, err)
			require.NotNil(t, metadata)
			resolved, err := f.accounts.GetByID(f.ctx, a.ID)
			require.NoError(t, err)
			visible, err := json.Marshal(dto.AccountFromService(resolved))
			require.NoError(t, err)
			for key, value := range secrets {
				require.NotContains(t, stored, key)
				require.NotContains(t, ciphertext, value)
				require.Equal(t, value, resolved.GetCredential(key))
				require.NotContains(t, string(visible), value)
			}
		})
	}
}
