//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type persistentTotpUsers struct {
	UserRepository
	ciphertext string
	enabled    bool
}

func (users *persistentTotpUsers) UpdateTotpSecret(_ context.Context, _ int64, ciphertext *string) error {
	users.ciphertext = *ciphertext
	return nil
}
func (users *persistentTotpUsers) EnableTotp(context.Context, int64) error {
	users.enabled = true
	return nil
}

type persistentTotpCache struct {
	TotpCache
	session *TotpSetupSession
	deleted bool
}

func (cache *persistentTotpCache) GetSetupSession(context.Context, int64) (*TotpSetupSession, error) {
	return cache.session, nil
}
func (cache *persistentTotpCache) DeleteSetupSession(context.Context, int64) error {
	cache.deleted = true
	return nil
}

func TestTotpSetupRequiresPersistentKeyBeforeSaving(t *testing.T) {
	for _, configured := range []bool{false, true} {
		users := &persistentTotpUsers{}
		cache := &persistentTotpCache{session: &TotpSetupSession{Secret: "JBSWY3DPEHPK3PXP", SetupToken: "synthetic"}}
		encryptor := &persistentTestEncryptor{configured: configured}
		settings := NewSettingService(&totpVMSettingRepoStub{values: map[string]string{SettingKeyTotpEnabled: "true"}}, nil)
		totpService := NewTotpService(users, encryptor, cache, settings, nil, nil)
		code, err := totp.GenerateCode(cache.session.Secret, time.Now())
		require.NoError(t, err)
		err = totpService.CompleteSetup(context.Background(), 42, code, "synthetic")
		if configured {
			require.NoError(t, err)
			plain, err := encryptor.Decrypt(users.ciphertext)
			require.NoError(t, err)
			require.Equal(t, cache.session.Secret, plain)
			require.True(t, users.enabled)
			require.True(t, cache.deleted)
		} else {
			require.ErrorIs(t, err, ErrEncryptionKeyRequired)
			require.Empty(t, users.ciphertext)
			require.False(t, users.enabled)
			require.False(t, cache.deleted)
		}
	}
}
