package repository

import (
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestKiloCredentialPersistencePostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	for _, mode := range []string{service.KiloAccountModeFree, service.KiloAccountModeAPIKey} {
		t.Run(mode, func(t *testing.T) {
			key := ""
			if mode == service.KiloAccountModeAPIKey {
				key = "kilo-private-fixture"
			}
			a := f.createAccount(t, f.ctx, uniqueName("kilo-secret"), service.PlatformKilo, service.AccountTypeAPIKey, map[string]any{"account_mode": mode, "api_key": key})
			stored := f.storedCredentials(t, a.ID)
			require.Equal(t, mode, stored["account_mode"])
			require.NotContains(t, stored, "api_key")
			resolved, err := f.accounts.GetByID(f.ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, key, resolved.GetCredential("api_key"))
			require.Equal(t, mode, resolved.GetKiloAccountMode())
			visible, err := json.Marshal(dto.AccountFromService(resolved))
			require.NoError(t, err)
			if key != "" {
				_, cipher, err := readCredentialSecret(f.ctx, f.db, a.ID)
				require.NoError(t, err)
				require.NotContains(t, cipher, key)
				require.NotContains(t, string(visible), key)
			}
		})
	}
	a := f.createAccount(t, f.ctx, uniqueName("zen-free-secret"), service.PlatformOpenCodeGo, service.AccountTypeAPIKey, map[string]any{"account_mode": service.AccountModeZenFree, "api_key": "public"})
	resolved, err := f.accounts.GetByID(f.ctx, a.ID)
	require.NoError(t, err)
	require.True(t, resolved.IsOpenCodeZenFree())
	require.Equal(t, "public", resolved.GetCredential("api_key"))
	visible, err := json.Marshal(dto.AccountFromService(resolved))
	require.NoError(t, err)
	require.NotContains(t, string(visible), `"api_key":"public"`)
}
