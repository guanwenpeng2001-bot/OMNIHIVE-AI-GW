package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

var ErrEncryptionKeyRequired = infraerrors.BadRequest("encryption_key_required", "Set a persistent TOTP_ENCRYPTION_KEY environment variable and restart the service before saving encrypted secrets.")

// RequirePersistentEncryption must run before persistent secret writes or their
// side effects. Encrypt itself also serves ephemeral tokens and stays unchanged.
// Encryptors without a declared persistent key fail closed.
func RequirePersistentEncryption(encryptor SecretEncryptor) error {
	configured, ok := encryptor.(interface{ EncryptionKeyConfigured() bool })
	if !ok || !configured.EncryptionKeyConfigured() {
		return ErrEncryptionKeyRequired
	}
	return nil
}
