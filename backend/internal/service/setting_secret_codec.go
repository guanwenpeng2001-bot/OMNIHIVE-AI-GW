package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
)

const settingSecretPrefix = "omnihive-secret:v1:"

// settingSecretEncryptor changes only integration storage credentials. TOTP and
// existing unversioned ciphertext continue to use the injected legacy decoder.
// Failed authentication is never interpreted as a plaintext credential.
type settingSecretEncryptor struct {
	legacy     SecretEncryptor
	key        []byte
	keyID      string
	configured bool
	initErr    error
}

func newSettingSecretEncryptor(cfg *config.Config, legacy SecretEncryptor) *settingSecretEncryptor {
	e := &settingSecretEncryptor{legacy: legacy}
	if cfg == nil {
		return e
	}
	e.configured = cfg.Totp.EncryptionKeyConfigured
	raw := strings.TrimSpace(cfg.Secrets.EncryptionKey)
	if raw == "" {
		return e
	}
	e.key, e.initErr = hex.DecodeString(raw)
	if e.initErr != nil || len(e.key) != 32 {
		e.initErr = errors.New("integration secret encryption key is invalid")
		return e
	}
	digest := sha256.Sum256(e.key)
	e.keyID = hex.EncodeToString(digest[:8])
	e.configured = true
	return e
}

func (e *settingSecretEncryptor) EncryptionKeyConfigured() bool {
	return e != nil && e.initErr == nil && e.configured
}

func (e *settingSecretEncryptor) Encrypt(plaintext string) (string, error) {
	if e == nil || e.initErr != nil {
		return "", errors.New("integration secret encryption is unavailable")
	}
	if len(e.key) == 0 {
		if e.legacy == nil {
			return "", errors.New("legacy secret encryption is unavailable")
		}
		return e.legacy.Encrypt(plaintext)
	}
	gcm, err := e.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create integration secret nonce: %w", err)
	}
	header := settingSecretPrefix + e.keyID + ":"
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(header))
	return header + base64.StdEncoding.EncodeToString(sealed), nil
}

func (e *settingSecretEncryptor) Decrypt(ciphertext string) (string, error) {
	if e == nil {
		return "", errors.New("integration secret decryption is unavailable")
	}
	if !strings.HasPrefix(ciphertext, "omnihive-secret:") {
		if e.legacy == nil {
			return "", errors.New("legacy secret decryption is unavailable")
		}
		return e.legacy.Decrypt(ciphertext)
	}
	if !strings.HasPrefix(ciphertext, settingSecretPrefix) {
		return "", errors.New("unsupported integration secret format")
	}
	keyID, encoded, ok := strings.Cut(strings.TrimPrefix(ciphertext, settingSecretPrefix), ":")
	if !ok || e.initErr != nil || len(e.key) != 32 || keyID != e.keyID {
		return "", errors.New("integration secret encryption key is unavailable or does not match")
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("integration secret encoding is invalid")
	}
	gcm, err := e.gcm()
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("integration secret ciphertext is truncated")
	}
	header := settingSecretPrefix + keyID + ":"
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(header))
	if err != nil {
		return "", errors.New("integration secret authentication failed")
	}
	return string(plaintext), nil
}

func (e *settingSecretEncryptor) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, errors.New("integration secret encryption key is invalid")
	}
	return cipher.NewGCM(block)
}

func settingSecretKeyID(value string) string {
	if rest, ok := strings.CutPrefix(value, settingSecretPrefix); ok {
		keyID, _, _ := strings.Cut(rest, ":")
		return keyID
	}
	if value != "" {
		return "legacy_totp"
	}
	return ""
}
