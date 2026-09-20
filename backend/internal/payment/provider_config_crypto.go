package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
)

const providerConfigEnvelope = "payment-aead:"

// EncryptProviderConfig writes authenticated, explicitly identified ciphertext.
func EncryptProviderConfig(config map[string]string, key []byte) (string, error) {
	plaintext, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	sealed, err := providerConfigCipher(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, sealed.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := sealed.Seal(nonce, nonce, plaintext, []byte(providerConfigEnvelope))
	return providerConfigEnvelope + base64.StdEncoding.EncodeToString(payload), nil
}

func providerConfigCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != AES256KeySize {
		return nil, fmt.Errorf("payment encryption key is not configured")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// DecryptProviderConfig supports both formats during the deployment transition.
// Unreadable records are errors, never an empty config that can be overwritten.
func DecryptProviderConfig(stored string, key []byte) (map[string]string, error) {
	if !strings.HasPrefix(stored, providerConfigEnvelope) {
		return readLegacyProviderConfig(stored, key)
	}
	sealed, err := providerConfigCipher(key)
	if err != nil {
		return nil, err
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, providerConfigEnvelope))
	if err != nil {
		return nil, fmt.Errorf("decode payment config: %w", err)
	}
	if len(payload) < sealed.NonceSize()+sealed.Overhead() {
		return nil, fmt.Errorf("payment config ciphertext is truncated")
	}
	plaintext, err := sealed.Open(nil, payload[:sealed.NonceSize()], payload[sealed.NonceSize():], []byte(providerConfigEnvelope))
	if err != nil {
		return nil, fmt.Errorf("authenticate payment config: %w", err)
	}
	var config map[string]string
	if err := json.Unmarshal(plaintext, &config); err != nil {
		return nil, fmt.Errorf("decode payment config JSON: %w", err)
	}
	return config, nil
}

func readLegacyProviderConfig(stored string, key []byte) (map[string]string, error) {
	if stored == "" {
		return nil, nil
	}
	var config map[string]string
	if err := json.Unmarshal([]byte(stored), &config); err == nil {
		return config, nil
	}
	plaintext, err := Decrypt(stored, key)
	if err != nil {
		return nil, fmt.Errorf("decrypt legacy payment config: %w", err)
	}
	if err := json.Unmarshal([]byte(plaintext), &config); err != nil {
		return nil, fmt.Errorf("decode legacy payment config: %w", err)
	}
	return config, nil
}

// MigrateProviderConfigs re-encrypts only verified records with a compare-and-swap
// update. It neither touches orders nor overwrites concurrent configuration edits.
// Bad records remain intact and are returned as errors; reruns skip current records.
func MigrateProviderConfigs(ctx context.Context, client *dbent.Client, key []byte) error {
	instances, err := client.PaymentProviderInstance.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("query payment configs for migration: %w", err)
	}
	var failures []error
	for _, instance := range instances {
		if strings.HasPrefix(instance.Config, providerConfigEnvelope) {
			continue
		}
		config, err := readLegacyProviderConfig(instance.Config, key)
		if err != nil {
			failures = append(failures, fmt.Errorf("payment config %d: %w", instance.ID, err))
			continue
		}
		ciphertext, err := EncryptProviderConfig(config, key)
		if err != nil {
			failures = append(failures, fmt.Errorf("payment config %d: %w", instance.ID, err))
			continue
		}
		if _, err := DecryptProviderConfig(ciphertext, key); err != nil {
			failures = append(failures, err)
			continue
		}
		_, err = client.PaymentProviderInstance.Update().Where(
			paymentproviderinstance.IDEQ(instance.ID), paymentproviderinstance.ConfigEQ(instance.Config),
		).SetConfig(ciphertext).Save(ctx)
		if err != nil {
			failures = append(failures, fmt.Errorf("migrate payment config %d: %w", instance.ID, err))
		}
	}
	return errors.Join(failures...)
}
