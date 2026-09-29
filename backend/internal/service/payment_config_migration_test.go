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

func TestPaymentConfigMigrationPreservesBadRecordsAndIsIdempotent(test *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(test)
	key := []byte("0123456789abcdef0123456789abcdef")
	user, err := client.User.Create().SetEmail("migration@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(test, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).
		SetUserEmail(user.Email).SetUserName("migration").SetAmount(100).SetPayAmount(103).
		SetRechargeCode("migration-order").SetOutTradeNo("original-external-id").SetPaymentTradeNo("original-trade").
		SetPaymentType(payment.TypeAlipay).SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("localhost").Save(ctx)
	require.NoError(test, err)
	plaintext := `{"secret":"original-secret","appId":"merchant"}`
	// This fixture must be written with the historical encoder on purpose: the test
	// migrates pre-existing legacy ciphertext, so the deprecated API is the point.
	//nolint:staticcheck // SA1019: deliberate use of the retained historical encoder for a migration fixture.
	legacy, err := payment.Encrypt(plaintext, key)
	require.NoError(test, err)
	legacyInstance, err := client.PaymentProviderInstance.Create().SetName("legacy").SetProviderKey(payment.TypeAlipay).SetConfig(legacy).Save(ctx)
	require.NoError(test, err)
	plainInstance, err := client.PaymentProviderInstance.Create().SetName("plain").SetProviderKey(payment.TypeAlipay).SetConfig(plaintext).Save(ctx)
	require.NoError(test, err)
	badInstance, err := client.PaymentProviderInstance.Create().SetName("bad").SetProviderKey(payment.TypeAlipay).SetConfig("broken-ciphertext").Save(ctx)
	require.NoError(test, err)
	err = payment.MigrateProviderConfigs(ctx, client, key)
	require.ErrorContains(test, err, "payment config")
	firstPass, err := client.PaymentProviderInstance.Get(ctx, legacyInstance.ID)
	require.NoError(test, err)
	require.True(test, strings.HasPrefix(firstPass.Config, "payment-aead:"))
	config, err := payment.DecryptProviderConfig(firstPass.Config, key)
	require.NoError(test, err)
	require.Equal(test, "original-secret", config["secret"])
	plainMigrated, err := client.PaymentProviderInstance.Get(ctx, plainInstance.ID)
	require.NoError(test, err)
	require.True(test, strings.HasPrefix(plainMigrated.Config, "payment-aead:"))
	require.Error(test, payment.MigrateProviderConfigs(ctx, client, key))
	secondPass, err := client.PaymentProviderInstance.Get(ctx, legacyInstance.ID)
	require.NoError(test, err)
	require.Equal(test, firstPass.Config, secondPass.Config)
	bad, err := client.PaymentProviderInstance.Get(ctx, badInstance.ID)
	require.NoError(test, err)
	require.Equal(test, "broken-ciphertext", bad.Config)
	require.NoError(test, client.PaymentProviderInstance.DeleteOneID(bad.ID).Exec(ctx))
	require.NoError(test, payment.MigrateProviderConfigs(ctx, client, key))
	preservedOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(test, err)
	require.Equal(test, order.ID, preservedOrder.ID)
	require.Equal(test, order.OutTradeNo, preservedOrder.OutTradeNo)
	require.Equal(test, order.Amount, preservedOrder.Amount)
	require.Equal(test, order.PayAmount, preservedOrder.PayAmount)
	require.Equal(test, order.Status, preservedOrder.Status)
}

func TestPaymentConfigConstructorMigratesBeforeStrictReads(test *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(test)
	key := []byte("0123456789abcdef0123456789abcdef")
	instance, err := client.PaymentProviderInstance.Create().SetName("plain").SetProviderKey(payment.TypeAlipay).SetConfig(`{"secret":"original"}`).Save(ctx)
	require.NoError(test, err)
	service := NewPaymentConfigService(client, nil, key)
	migrated, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(test, err)
	config, err := service.decryptConfig(migrated.Config)
	require.NoError(test, err)
	require.Equal(test, "original", config["secret"])
	NewPaymentConfigService(client, nil, key)
	rerun, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(test, err)
	require.Equal(test, migrated.Config, rerun.Config)
	_, err = service.decryptConfig(instance.Config)
	require.ErrorContains(test, err, "requires migration")
}

func TestPaymentConfigNewWritesRequireStableKeyAndAuthenticate(test *testing.T) {
	config := map[string]string{"secret": "provider-secret"}
	_, err := (&PaymentConfigService{}).encryptConfig(config)
	require.Error(test, err)
	service := &PaymentConfigService{encryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	ciphertext, err := service.encryptConfig(config)
	require.NoError(test, err)
	require.NotContains(test, ciphertext, "provider-secret")
	got, err := service.decryptConfig(ciphertext)
	require.NoError(test, err)
	require.Equal(test, config, got)
	_, err = payment.DecryptProviderConfig(ciphertext, []byte(strings.Repeat("a", 32)))
	require.Error(test, err)
	_, err = service.decryptConfig(ciphertext[:len(ciphertext)-4])
	require.Error(test, err)
}

func TestPaymentConfigMigrationDoesNotOverwriteConcurrentEdit(test *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(test)
	key := []byte("0123456789abcdef0123456789abcdef")
	instance, err := client.PaymentProviderInstance.Create().SetName("concurrent").SetProviderKey(payment.TypeAlipay).SetConfig(`{"secret":"old"}`).Save(ctx)
	require.NoError(test, err)
	newConfig, err := payment.EncryptProviderConfig(map[string]string{"secret": "edited"}, key)
	require.NoError(test, err)
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
	require.NoError(test, payment.MigrateProviderConfigs(ctx, client, key))
	stored, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(test, err)
	require.True(test, edited)
	require.Equal(test, newConfig, stored.Config)
	require.NoError(test, payment.MigrateProviderConfigs(ctx, client, key))
}
