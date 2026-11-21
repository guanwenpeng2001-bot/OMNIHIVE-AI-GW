package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/wire"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
)

// EncryptionKey is a named type for the payment encryption key (AES-256, 32 bytes).
// Using a named type avoids Wire ambiguity with other []byte parameters.
type EncryptionKey []byte

// ProvideEncryptionKey preserves an explicit TOTP key for existing ciphertext,
// or derives a payment-specific key from the repository's persisted JWT secret.
// A missing persistent source disables signing; a malformed configured key fails startup.
func ProvideEncryptionKey(cfg *config.Config) (EncryptionKey, error) {
	if cfg == nil {
		slog.Warn("payment encryption key not configured — encrypted payment config and resume signing will be unavailable")
		return nil, nil
	}
	keyHex := strings.TrimSpace(cfg.Totp.EncryptionKey)
	// Reject auto-generated TOTP keys for payment signing.
	// They change across restarts/instances and can silently break resume-token flows.
	if keyHex == "" || !cfg.Totp.EncryptionKeyConfigured {
		// Repository bootstrap persists this secret before payment wiring runs.
		secret := strings.TrimSpace(cfg.JWT.Secret)
		if len(secret) < 32 {
			slog.Warn("payment encryption/signing key unavailable: persistent secret is not ready")
			return nil, nil
		}
		derivation := hmac.New(sha256.New, []byte(secret))
		_, _ = derivation.Write([]byte("sub2api/payment/encryption"))
		return EncryptionKey(derivation.Sum(nil)), nil
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid payment encryption key (hex decode): %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("payment encryption key must be 32 bytes, got %d", len(key))
	}
	return EncryptionKey(key), nil
}

// ProvideRegistry creates an empty payment provider registry.
// Providers are registered at runtime after application startup.
func ProvideRegistry() *Registry {
	return NewRegistry()
}

// ProvideDefaultLoadBalancer creates a DefaultLoadBalancer backed by the ent client.
func ProvideDefaultLoadBalancer(client *dbent.Client, key EncryptionKey) *DefaultLoadBalancer {
	return NewDefaultLoadBalancer(client, []byte(key))
}

// ProviderSet is the Wire provider set for the payment package.
var ProviderSet = wire.NewSet(
	ProvideEncryptionKey,
	ProvideRegistry,
	ProvideDefaultLoadBalancer,
	wire.Bind(new(LoadBalancer), new(*DefaultLoadBalancer)),
)
