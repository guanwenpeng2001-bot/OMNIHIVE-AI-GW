package config

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadWithoutPersistentEncryptionKeyStillStarts(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("TOTP_ENCRYPTION_KEY", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.Totp.EncryptionKeyConfigured)
	key, err := hex.DecodeString(cfg.Totp.EncryptionKey)
	require.NoError(t, err)
	require.Len(t, key, 32)
}
