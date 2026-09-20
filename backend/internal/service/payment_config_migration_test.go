package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentConfigMigrationPreservesBadRecordsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	plaintext := `{"secret":"original-secret","appId":"merchant"}`
	legacy, err := payment.Encrypt(plaintext, key)
	require.NoError(t, err)
	legacyInstance, err := client.PaymentProviderInstance.Create().SetName("legacy").SetProviderKey(payment.TypeAlipay).SetConfig(legacy).Save(ctx)
	require.NoError(t, err)
	plainInstance, err := client.PaymentProviderInstance.Create().SetName("plain").SetProviderKey(payment.TypeAlipay).SetConfig(plaintext).Save(ctx)
	require.NoError(t, err)
	badInstance, err := client.PaymentProviderInstance.Create().SetName("bad").SetProviderKey(payment.TypeAlipay).SetConfig("broken-ciphertext").Save(ctx)
	require.NoError(t, err)
	err = payment.MigrateProviderConfigs(ctx, client, key)
	require.ErrorContains(t, err, "payment config")
	firstPass, err := client.PaymentProviderInstance.Get(ctx, legacyInstance.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(firstPass.Config, "payment-aead:"))
	config, err := payment.DecryptProviderConfig(firstPass.Config, key)
	require.NoError(t, err)
	require.Equal(t, "original-secret", config["secret"])
	plainMigrated, err := client.PaymentProviderInstance.Get(ctx, plainInstance.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(plainMigrated.Config, "payment-aead:"))
	require.Error(t, payment.MigrateProviderConfigs(ctx, client, key))
	secondPass, err := client.PaymentProviderInstance.Get(ctx, legacyInstance.ID)
	require.NoError(t, err)
	require.Equal(t, firstPass.Config, secondPass.Config)
	bad, err := client.PaymentProviderInstance.Get(ctx, badInstance.ID)
	require.NoError(t, err)
	require.Equal(t, "broken-ciphertext", bad.Config)
	require.NoError(t, client.PaymentProviderInstance.DeleteOneID(bad.ID).Exec(ctx))
	require.NoError(t, payment.MigrateProviderConfigs(ctx, client, key))
}

func TestPaymentConfigNewWritesRequireStableKeyAndAuthenticate(t *testing.T) {
	config := map[string]string{"secret": "provider-secret"}
	_, err := (&PaymentConfigService{}).encryptConfig(config)
	require.Error(t, err)
	service := &PaymentConfigService{encryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	ciphertext, err := service.encryptConfig(config)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "provider-secret")
	got, err := service.decryptConfig(ciphertext)
	require.NoError(t, err)
	require.Equal(t, config, got)
	_, err = payment.DecryptProviderConfig(ciphertext, []byte("abcdef0123456789abcdef0123456789"))
	require.Error(t, err)
	_, err = service.decryptConfig(ciphertext[:len(ciphertext)-4])
	require.Error(t, err)
}
