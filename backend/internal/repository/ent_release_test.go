package repository

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestInitEntReleaseRequiresPersistentCredentialKey(t *testing.T) {
	client, db, err := InitEnt(&config.Config{Server: config.ServerConfig{Mode: "release"}})
	require.Nil(t, client)
	require.Nil(t, db)
	require.ErrorContains(t, err, "TOTP_ENCRYPTION_KEY")
}
