package service

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func (credentialTestEncryptor) EncryptionKeyConfigured() bool { return true }

type credentialTestEncryptor struct{}

func (credentialTestEncryptor) Encrypt(plaintext string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (credentialTestEncryptor) Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func credentialTestMetadata() CredentialSecret {
	return CredentialSecret{Reference: "ref-1", WorkspaceID: 7, AccountID: 42, Version: 3}
}

func TestCredentialSecretEncryptResolveRoundtrip(t *testing.T) {
	metadata := credentialTestMetadata()
	secrets := map[string]any{"refresh_token": "token-value", "api_key": "key-value"}
	ciphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, metadata, secrets)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "token-value", "ciphertext must not embed plaintext")

	resolved, err := ResolveCredentialSecret(context.Background(), credentialTestEncryptor{}, metadata, ciphertext)
	require.NoError(t, err)
	require.Equal(t, secrets, resolved)
}

func TestCredentialSecretResolveBindsEnvelopeToOwner(t *testing.T) {
	metadata := credentialTestMetadata()
	ciphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, metadata, map[string]any{"api_key": "key-value"})
	require.NoError(t, err)

	cases := map[string]func(CredentialSecret) CredentialSecret{
		"workspace": func(m CredentialSecret) CredentialSecret { m.WorkspaceID = 8; return m },
		"account":   func(m CredentialSecret) CredentialSecret { m.AccountID = 43; return m },
		"reference": func(m CredentialSecret) CredentialSecret { m.Reference = "ref-2"; return m },
		"version":   func(m CredentialSecret) CredentialSecret { m.Version = 4; return m },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			foreign := mutate(credentialTestMetadata())
			foreignCiphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, foreign, map[string]any{"api_key": "key-value"})
			require.NoError(t, err)
			// A ciphertext minted for another owner must not resolve under this metadata.
			_, err = ResolveCredentialSecret(context.Background(), credentialTestEncryptor{}, metadata, foreignCiphertext)
			require.ErrorIs(t, err, ErrCredentialSecretInvalid)
			_ = ciphertext
		})
	}
}

func TestCredentialSecretResolveRejectsCrossWorkspaceScope(t *testing.T) {
	metadata := credentialTestMetadata()
	ciphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, metadata, map[string]any{"api_key": "key-value"})
	require.NoError(t, err)

	scoped := domain.WithWorkspaceScope(context.Background(), metadata.WorkspaceID+1)
	_, err = ResolveCredentialSecret(scoped, credentialTestEncryptor{}, metadata, ciphertext)
	require.ErrorIs(t, err, ErrCredentialSecretNotFound)

	sameScope := domain.WithWorkspaceScope(context.Background(), metadata.WorkspaceID)
	resolved, err := ResolveCredentialSecret(sameScope, credentialTestEncryptor{}, metadata, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "key-value", resolved["api_key"])
}

func TestCredentialSecretResolveRevokedIsExplicitError(t *testing.T) {
	metadata := credentialTestMetadata()
	now := mustParseTime(t, "2026-09-21T00:00:00Z")
	metadata.RevokedAt = &now
	ciphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, metadata, map[string]any{"api_key": "key-value"})
	require.NoError(t, err)
	_, err = ResolveCredentialSecret(context.Background(), credentialTestEncryptor{}, metadata, ciphertext)
	require.ErrorIs(t, err, ErrCredentialSecretRevoked)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsedTime, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsedTime
}

func TestSplitCredentialSecretsSeparatesWithoutMutating(t *testing.T) {
	input := map[string]any{
		"refresh_token":            "token-value",
		"api_key":                  "key-value",
		"base_url":                 "https://example.invalid",
		CredentialSecretVersionKey: int64(2),
	}
	inline, secrets := SplitCredentialSecrets(input)
	require.Equal(t, "token-value", input["refresh_token"], "input must stay intact")
	require.Equal(t, map[string]any{"base_url": "https://example.invalid"}, inline)
	require.Equal(t, map[string]any{"refresh_token": "token-value", "api_key": "key-value"}, secrets)
}

func TestCredentialReferenceAndVersionParsing(t *testing.T) {
	_, referenced := CredentialReference(map[string]any{})
	require.False(t, referenced)
	reference, referenced := CredentialReference(map[string]any{CredentialSecretReferenceKey: "ref-9"})
	require.True(t, referenced)
	require.Equal(t, "ref-9", reference)

	require.Equal(t, int64(3), CredentialVersion(map[string]any{CredentialSecretVersionKey: int64(3)}))
	require.Equal(t, int64(3), CredentialVersion(map[string]any{CredentialSecretVersionKey: float64(3)}))
	require.Equal(t, int64(3), CredentialVersion(map[string]any{CredentialSecretVersionKey: 3}))
	require.Equal(t, int64(0), CredentialVersion(map[string]any{CredentialSecretVersionKey: 1.5}))
	require.Equal(t, int64(0), CredentialVersion(map[string]any{}))
}

type credentialSecretRepositoryStub struct {
	convert func(ctx context.Context, accountID int64, revision string) (*CredentialSecret, error)
	rotate  func(ctx context.Context, accountID, expectedVersion int64, credentials map[string]any) (*CredentialSecret, error)
	revoke  func(ctx context.Context, accountID, expectedVersion int64) (*CredentialSecret, error)
}

func (s credentialSecretRepositoryStub) Metadata(ctx context.Context, accountID int64) (*CredentialSecret, error) {
	return nil, ErrCredentialSecretNotFound
}

func (s credentialSecretRepositoryStub) Convert(ctx context.Context, accountID int64, revision string) (*CredentialSecret, error) {
	return s.convert(ctx, accountID, revision)
}

func (s credentialSecretRepositoryStub) Rotate(ctx context.Context, accountID, expectedVersion int64, credentials map[string]any) (*CredentialSecret, error) {
	return s.rotate(ctx, accountID, expectedVersion, credentials)
}

func (s credentialSecretRepositoryStub) Revoke(ctx context.Context, accountID, expectedVersion int64) (*CredentialSecret, error) {
	return s.revoke(ctx, accountID, expectedVersion)
}

func TestCredentialSecretServiceConvertRequiresExplicitConfirmationAndRevision(t *testing.T) {
	called := false
	service := NewCredentialSecretService(credentialSecretRepositoryStub{
		convert: func(context.Context, int64, string) (*CredentialSecret, error) {
			called = true
			return &CredentialSecret{}, nil
		},
	})
	_, err := service.Convert(context.Background(), 1, "rev", false)
	require.ErrorIs(t, err, ErrCredentialSecretInvalid)
	_, err = service.Convert(context.Background(), 1, "", true)
	require.ErrorIs(t, err, ErrCredentialSecretInvalid)
	require.False(t, called, "repository must not run without explicit confirmation")
	_, err = service.Convert(context.Background(), 1, "rev", true)
	require.NoError(t, err)
	require.True(t, called)
}

func TestCredentialSecretServiceRotateOnlyAcceptsSensitiveFields(t *testing.T) {
	service := NewCredentialSecretService(credentialSecretRepositoryStub{
		rotate: func(context.Context, int64, int64, map[string]any) (*CredentialSecret, error) {
			return &CredentialSecret{}, nil
		},
	})
	_, err := service.Rotate(context.Background(), 1, 1, map[string]any{"base_url": "https://example.invalid"})
	require.ErrorIs(t, err, ErrCredentialSecretInvalid, "non-secret fields must not enter the encrypted payload")
	_, err = service.Rotate(context.Background(), 1, 0, map[string]any{"api_key": "key"})
	require.ErrorIs(t, err, ErrCredentialSecretInvalid)
	_, err = service.Rotate(context.Background(), 1, 1, map[string]any{"api_key": "key"})
	require.NoError(t, err)
}

func TestAccountJSONRedactsReferencedSecrets(t *testing.T) {
	account := Account{ID: 1, Credentials: map[string]any{
		CredentialSecretReferenceKey: "ref-1",
		CredentialSecretVersionKey:   int64(2),
		"refresh_token":              "token-value",
		"base_url":                   "https://example.invalid",
	}}
	encoded, err := account.MarshalJSON()
	require.NoError(t, err)
	text := string(encoded)
	require.NotContains(t, text, "token-value", "serialized account must not carry referenced plaintext")
	require.Contains(t, text, "ref-1")
	require.Contains(t, text, "credential_secret_version")
}

func TestAccountJSONKeepsInlineAccountsUntouched(t *testing.T) {
	account := Account{ID: 1, Credentials: map[string]any{"api_key": "key-value"}}
	encoded, err := account.MarshalJSON()
	require.NoError(t, err)
	require.True(t, strings.Contains(string(encoded), "key-value"), "inline serialization stays as before")
}
