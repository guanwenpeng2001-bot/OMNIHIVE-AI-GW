package service

import (
	"context"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentConfigMigrationPreservesBadRecordsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	user, err := client.User.Create().SetEmail("migration@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).
		SetUserEmail(user.Email).SetUserName("migration").SetAmount(100).SetPayAmount(103).
		SetRechargeCode("migration-order").SetOutTradeNo("original-external-id").SetPaymentTradeNo("original-trade").
		SetPaymentType(payment.TypeAlipay).SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("localhost").Save(ctx)
	require.NoError(t, err)
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
	preservedOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, order.ID, preservedOrder.ID)
	require.Equal(t, order.OutTradeNo, preservedOrder.OutTradeNo)
	require.Equal(t, order.Amount, preservedOrder.Amount)
	require.Equal(t, order.PayAmount, preservedOrder.PayAmount)
	require.Equal(t, order.Status, preservedOrder.Status)
}

func TestPaymentConfigConstructorMigratesBeforeStrictReads(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	instance, err := client.PaymentProviderInstance.Create().SetName("plain").SetProviderKey(payment.TypeAlipay).SetConfig(`{"secret":"original"}`).Save(ctx)
	require.NoError(t, err)
	service := NewPaymentConfigService(client, nil, key)
	migrated, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	config, err := service.decryptConfig(migrated.Config)
	require.NoError(t, err)
	require.Equal(t, "original", config["secret"])
	NewPaymentConfigService(client, nil, key)
	rerun, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, migrated.Config, rerun.Config)
	_, err = service.decryptConfig(instance.Config)
	require.ErrorContains(t, err, "requires migration")
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
	_, err = payment.DecryptProviderConfig(ciphertext, []byte(strings.Repeat("a", 32)))
	require.Error(t, err)
	_, err = service.decryptConfig(ciphertext[:len(ciphertext)-4])
	require.Error(t, err)
}

func TestPaymentConfigMigrationDoesNotOverwriteConcurrentEdit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	instance, err := client.PaymentProviderInstance.Create().SetName("concurrent").SetProviderKey(payment.TypeAlipay).SetConfig(`{"secret":"old"}`).Save(ctx)
	require.NoError(t, err)
	newConfig, err := payment.EncryptProviderConfig(map[string]string{"secret": "edited"}, key)
	require.NoError(t, err)
	edited := false
	client.PaymentProviderInstance.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if mutation.Op() == dbent.OpUpdate && !edited {
				edited = true
				if err := client.PaymentProviderInstance.UpdateOneID(instance.ID).SetConfig(newConfig).Exec(ctx); err != nil {
					return nil, err
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	require.NoError(t, payment.MigrateProviderConfigs(ctx, client, key))
	stored, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.True(t, edited)
	require.Equal(t, newConfig, stored.Config)
	require.NoError(t, payment.MigrateProviderConfigs(ctx, client, key))
}
