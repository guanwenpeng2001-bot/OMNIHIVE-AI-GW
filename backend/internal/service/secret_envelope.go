package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const (
	backupSecretEncryptionKeyEnv         = "BACKUP_SECRET_ENCRYPTION_KEY"
	backupSecretEncryptionKeyPreviousEnv = "BACKUP_SECRET_ENCRYPTION_KEY_PREVIOUS"
	backupS3SecretAAD                    = "omnihive/backup-s3/v1"
)

var (
	ErrBackupSecretKeyNotConfigured = infraerrors.BadRequest(
		"BACKUP_SECRET_ENCRYPTION_KEY_NOT_CONFIGURED",
		"cannot store backup S3 credentials without BACKUP_SECRET_ENCRYPTION_KEY; configure a fixed 64-character hex key and retry",
	)
	ErrBackupSecretUnreadable = infraerrors.InternalServer(
		"BACKUP_SECRET_UNREADABLE",
		"the backup S3 secret cannot be decrypted with the configured backup secret encryption key",
	)
	// ErrSecretEncryptionKeyNotConfigured remains source-compatible for image
	// storage and older callers; its semantic owner is now the backup key.
	ErrSecretEncryptionKeyNotConfigured = ErrBackupSecretKeyNotConfigured
)

// BackupSecretEnvelope is deliberately a small, versioned wire format.  The
// ciphertext includes a random GCM nonce and is never logged or returned by an
// API response.
type BackupSecretEnvelope struct {
	Version    int    `json:"v"`
	KeyID      string `json:"kid"`
	Ciphertext string `json:"ciphertext"`
}

// BackupSecretPayloadRepository keeps the new envelope separate from the
// legacy settings JSON. Save atomically updates both copies so a v0.3.3
// rollback still has the TOTP-key ciphertext it understands.
type BackupSecretPayloadRepository interface {
	Get(ctx context.Context, settingKey string) (*BackupSecretEnvelope, error)
	Save(ctx context.Context, settingKey, legacySettingValue string, envelope BackupSecretEnvelope) error
	Delete(ctx context.Context, settingKey string) error
	SetMigrationState(ctx context.Context, settingKey, state, keyID, message string) error
}

// BackupSecretKeyStatus is safe for diagnostics/UI: it describes the source,
// never the key or plaintext.
type BackupSecretKeyStatus struct {
	Source            string `json:"source"`
	Configured        bool   `json:"configured"`
	MigrationRequired bool   `json:"migration_required"`
	KeyID             string `json:"key_id,omitempty"`
}

type backupSecretCryptor struct {
	current  []byte
	previous []byte
}

func newBackupSecretCryptorFromEnv() (*backupSecretCryptor, error) {
	current, err := parseBackupSecretKey(os.Getenv(backupSecretEncryptionKeyEnv))
	if err != nil {
		return &backupSecretCryptor{}, err
	}
	previous, err := parseBackupSecretKey(os.Getenv(backupSecretEncryptionKeyPreviousEnv))
	if err != nil {
		return &backupSecretCryptor{}, fmt.Errorf("invalid %s: %w", backupSecretEncryptionKeyPreviousEnv, err)
	}
	return &backupSecretCryptor{current: current, previous: previous}, nil
}

func parseBackupSecretKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, errors.New("must be exactly 32 bytes encoded as 64 hex characters")
	}
	return key, nil
}

func (c *backupSecretCryptor) configured() bool { return c != nil && len(c.current) == 32 }

func backupSecretKeyID(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])[:12]
}

func (c *backupSecretCryptor) keyID() string {
	if c == nil {
		return ""
	}
	return backupSecretKeyID(c.current)
}

func (c *backupSecretCryptor) seal(plaintext, aad string) (BackupSecretEnvelope, error) {
	if !c.configured() {
		return BackupSecretEnvelope{}, ErrBackupSecretKeyNotConfigured
	}
	block, err := aes.NewCipher(c.current)
	if err != nil {
		return BackupSecretEnvelope{}, fmt.Errorf("create backup secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return BackupSecretEnvelope{}, fmt.Errorf("create backup secret gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return BackupSecretEnvelope{}, fmt.Errorf("generate backup secret nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(aad))
	return BackupSecretEnvelope{Version: 1, KeyID: c.keyID(), Ciphertext: base64.StdEncoding.EncodeToString(ciphertext)}, nil
}

func (c *backupSecretCryptor) open(envelope BackupSecretEnvelope, aad string) (string, error) {
	if c == nil || envelope.Version != 1 || envelope.Ciphertext == "" {
		return "", ErrBackupSecretUnreadable
	}
	data, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return "", ErrBackupSecretUnreadable
	}
	keys := [][]byte{c.current, c.previous}
	for _, key := range keys {
		if len(key) != 32 || (envelope.KeyID != "" && envelope.KeyID != backupSecretKeyID(key)) {
			continue
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			continue
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil || len(data) < gcm.NonceSize() {
			continue
		}
		plaintext, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(aad))
		if err == nil {
			return string(plaintext), nil
		}
	}
	return "", ErrBackupSecretUnreadable
}

func backupSecretMigrationMessage(err error) string {
	if err == nil {
		return ""
	}
	// The persisted status is operational metadata, never a cryptographic or
	// plaintext error. Keep it bounded as it is rendered in admin diagnostics.
	return "backup secret migration failed"
}
