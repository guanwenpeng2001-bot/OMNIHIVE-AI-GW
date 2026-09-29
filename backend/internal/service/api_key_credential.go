package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// DefaultAPIKeyCredentialGraceHours is the default dual-read window (24h) opened
// for a rotated credential. It stays until the operator changes the configuration.
const DefaultAPIKeyCredentialGraceHours = 24

// APIKeyCredentialDigest returns the hex-encoded SHA-256 of a secret. It is the
// credential lookup key in the database and the API key auth cache identifier, so
// the same function must hash both the presented secret and the stored credential.
func APIKeyCredentialDigest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// APIKeyCredentialDisplaySuffix returns a short, non-secret tail used only for
// display; it never lets a secret be reconstructed.
func APIKeyCredentialDisplaySuffix(secret string) string {
	const suffixLength = 4
	if len(secret) <= suffixLength {
		return secret
	}
	return secret[len(secret)-suffixLength:]
}

// APIKeyCredential is one versioned credential of an API key. The identity stays on
// the API key, so usage, quota and billing history never move on rotation.
type APIKeyCredential struct {
	ID            int64
	WorkspaceID   int64
	APIKeyID      int64
	Digest        string
	DisplaySuffix string
	Status        string
	ValidUntil    *time.Time
	RevokedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// APIKeyCredentialRotation describes one rotation: the previous credential either
// becomes a bounded grace credential or is revoked immediately, then the new
// current credential is inserted.
type APIKeyCredentialRotation struct {
	APIKeyID       int64
	WorkspaceID    int64
	NewCredential  APIKeyCredential
	GraceUntil     time.Time
	RevokePrevious bool
	Actor          ConfigurationAuditActor
}

// APIKeyCredentialRepository persists key credentials. Authentication resolves a
// presented secret through apiKeyRepository, which owns the api_keys projection.
type APIKeyCredentialRepository interface {
	// ListDigestsByAPIKeyID returns the digests that identify a key's auth cache
	// entries, so a key can be evicted without knowing any plaintext secret.
	ListDigestsByAPIKeyID(ctx context.Context, apiKeyID int64) ([]string, error)
	// Rotate atomically demotes or revokes the previous credential, revokes any older
	// graceful credential, inserts the new current credential and records audit.
	Rotate(ctx context.Context, rotation APIKeyCredentialRotation) error
	// RevokeGrace revokes the graceful credentials of a key immediately.
	RevokeGrace(ctx context.Context, apiKeyID int64, now time.Time, actor ConfigurationAuditActor) (int64, error)
	// RevokeAll revokes every credential of a key (used when the key is deleted).
	RevokeAll(ctx context.Context, apiKeyID int64, now time.Time, actor ConfigurationAuditActor) (int64, error)
}

// APIKeyCredentialIssue is the one-time result of creating or rotating a key: the
// secret is returned exactly once and never again from any read path.
type APIKeyCredentialIssue struct {
	APIKey     *APIKey
	Secret     string
	GraceUntil *time.Time
}
