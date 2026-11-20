package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type unknownPersistentEncryptor struct{ SecretEncryptor }

type persistentTestEncryptor struct {
	pluginTokenEncryptor
	configured   bool
	encryptCalls int
}

func (encryptor *persistentTestEncryptor) EncryptionKeyConfigured() bool { return encryptor.configured }
func (encryptor *persistentTestEncryptor) Encrypt(plain string) (string, error) {
	encryptor.encryptCalls++
	return encryptor.pluginTokenEncryptor.Encrypt(plain)
}

func TestPersistentEncryptionCapabilityFailsClosed(t *testing.T) {
	for _, encryptor := range []SecretEncryptor{nil, unknownPersistentEncryptor{}, &persistentTestEncryptor{}} {
		err := RequirePersistentEncryption(encryptor)
		require.ErrorIs(t, err, ErrEncryptionKeyRequired)
		wrapped := fmt.Errorf("account operation: %w", err)
		require.Equal(t, http.StatusBadRequest, infraerrors.Code(wrapped))
		require.Equal(t, "encryption_key_required", infraerrors.Reason(wrapped))
		require.Contains(t, infraerrors.Message(wrapped), "TOTP_ENCRYPTION_KEY")
	}
	require.NoError(t, RequirePersistentEncryption(&persistentTestEncryptor{configured: true}))
}

func TestPersistentSecretServicesRejectBeforeSideEffects(t *testing.T) {
	encryptor := &persistentTestEncryptor{}
	_, err := EncryptCredentialSecret(encryptor, credentialTestMetadata(), map[string]any{"api_key": "synthetic"})
	require.ErrorIs(t, err, ErrEncryptionKeyRequired)
	// Nil repositories/cache/runtime panic on access: rejection must precede all
	// persistence and plugin runtime changes, including an empty config object.
	manager := &PluginManager{encryptor: encryptor}
	for _, raw := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"token":"synthetic"}`)} {
		_, err = manager.SaveConfig(context.Background(), 42, raw)
		require.ErrorIs(t, err, ErrEncryptionKeyRequired)
	}
	totpService := &TotpService{encryptor: encryptor}
	require.ErrorIs(t, totpService.CompleteSetup(context.Background(), 42, "123456", "synthetic"), ErrEncryptionKeyRequired)
	require.Zero(t, encryptor.encryptCalls)
}

func TestPersistentEncryptionDoesNotBlockEphemeralPluginToken(t *testing.T) {
	encryptor := &persistentTestEncryptor{}
	manager := &PluginManager{encryptor: encryptor, repo: &pluginTokenRepository{installation: &PluginInstallation{ID: 42}}}
	token, _, err := manager.CreateUIAssetToken(context.Background(), 42, time.Minute)
	require.NoError(t, err)
	pluginID, err := manager.ResolveUIAssetToken(token)
	require.NoError(t, err)
	require.Equal(t, int64(42), pluginID)
	require.Equal(t, 1, encryptor.encryptCalls)
}
