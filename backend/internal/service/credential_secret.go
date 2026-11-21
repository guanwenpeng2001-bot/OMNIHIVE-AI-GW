package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const CredentialSecretReferenceKey = "credential_secret_reference"
const CredentialSecretVersionKey = "credential_secret_version"

type credentialRevisionKey struct{}

func WithCredentialRevision(ctx context.Context, revision string) context.Context {
	return context.WithValue(ctx, credentialRevisionKey{}, revision)
}

func ExpectedCredentialRevision(ctx context.Context) string {
	revision, _ := ctx.Value(credentialRevisionKey{}).(string)
	return revision
}

var (
	ErrCredentialSecretNotFound = infraerrors.NotFound("CREDENTIAL_SECRET_NOT_FOUND", "Credential reference not found")
	ErrCredentialSecretRevoked  = infraerrors.Conflict("CREDENTIAL_SECRET_REVOKED", "Credential reference has been revoked")
	ErrCredentialSecretConflict = infraerrors.Conflict("CREDENTIAL_SECRET_CONFLICT", "Credential version changed; reload before retrying")
	ErrCredentialSecretInvalid  = infraerrors.BadRequest("CREDENTIAL_SECRET_INVALID", "Invalid credential reference")
)

// CredentialSecret is the only public projection. Ciphertext and plaintext are
// deliberately absent; references are bound to both an account and a workspace.
type CredentialSecret struct {
	Reference   string     `json:"reference"`
	WorkspaceID int64      `json:"workspace_id"`
	AccountID   int64      `json:"account_id"`
	Version     int64      `json:"version"`
	UpdatedAt   time.Time  `json:"updated_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type CredentialSecretRepository interface {
	Metadata(context.Context, int64) (*CredentialSecret, error)
	Convert(context.Context, int64, string) (*CredentialSecret, error)
	Rotate(context.Context, int64, int64, map[string]any) (*CredentialSecret, error)
	Revoke(context.Context, int64, int64) (*CredentialSecret, error)
}

type CredentialSecretService struct{ repository CredentialSecretRepository }

func NewCredentialSecretService(repository CredentialSecretRepository) *CredentialSecretService {
	return &CredentialSecretService{repository: repository}
}

func (s *CredentialSecretService) Metadata(ctx context.Context, accountID int64) (*CredentialSecret, error) {
	return s.repository.Metadata(ctx, accountID)
}

// Convert requires an explicit confirmation and the reviewed account revision.
// A stale review cannot encrypt a concurrently replaced inline credential.
func (s *CredentialSecretService) Convert(ctx context.Context, accountID int64, revision string, confirmed bool) (*CredentialSecret, error) {
	if !confirmed || revision == "" {
		return nil, ErrCredentialSecretInvalid
	}
	return s.repository.Convert(ctx, accountID, revision)
}

func (s *CredentialSecretService) Rotate(ctx context.Context, accountID, expectedVersion int64, credentials map[string]any) (*CredentialSecret, error) {
	if expectedVersion < 1 || len(credentials) == 0 {
		return nil, ErrCredentialSecretInvalid
	}
	for field := range credentials {
		if !IsSensitiveCredentialKey(field) {
			return nil, ErrCredentialSecretInvalid
		}
	}
	return s.repository.Rotate(ctx, accountID, expectedVersion, credentials)
}

func (s *CredentialSecretService) Revoke(ctx context.Context, accountID, expectedVersion int64) (*CredentialSecret, error) {
	if expectedVersion < 1 {
		return nil, ErrCredentialSecretInvalid
	}
	return s.repository.Revoke(ctx, accountID, expectedVersion)
}

func CredentialReference(credentials map[string]any) (string, bool) {
	value, exists := credentials[CredentialSecretReferenceKey]
	if !exists {
		return "", false
	}
	reference, _ := value.(string)
	return reference, true
}

func CredentialVersion(credentials map[string]any) int64 {
	switch value := credentials[CredentialSecretVersionKey].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		if value == float64(int64(value)) {
			return int64(value)
		}
	case json.Number:
		version, _ := value.Int64()
		return version
	}
	return 0
}

// StripCredentialReferenceKeys drops the deployment-local reference markers from
// a credentials copy so the copy starts inline. A reference is bound to one
// account and one workspace and must never travel with duplicated, exported or
// imported credential documents; the resolved plaintext stays, so explicit admin
// copies keep working while new writes decide their own storage form.
func StripCredentialReferenceKeys(credentials map[string]any) {
	delete(credentials, CredentialSecretReferenceKey)
	delete(credentials, CredentialSecretVersionKey)
}

// SplitCredentialSecrets never mutates the supplied map. Only the established
// credential allowlist enters the encrypted payload; configuration stays inline.
func SplitCredentialSecrets(credentials map[string]any) (map[string]any, map[string]any) {
	inline := make(map[string]any, len(credentials))
	secrets := make(map[string]any)
	for field, value := range credentials {
		if IsSensitiveCredentialKey(field) {
			secrets[field] = value
		} else {
			inline[field] = value
		}
	}
	delete(inline, CredentialSecretVersionKey)
	return inline, secrets
}

// The authenticated payload binds AES-GCM ciphertext to its owning resource,
// preventing a database ciphertext swap from becoming a cross-domain read.
type credentialSecretEnvelope struct {
	WorkspaceID int64          `json:"workspace_id"`
	AccountID   int64          `json:"account_id"`
	Reference   string         `json:"reference"`
	Version     int64          `json:"version"`
	Credentials map[string]any `json:"credentials"`
}

func EncryptCredentialSecret(encryptor SecretEncryptor, metadata CredentialSecret, credentials map[string]any) (string, error) {
	if encryptor == nil {
		return "", ErrCredentialSecretInvalid
	}
	if err := RequirePersistentEncryption(encryptor); err != nil {
		return "", err
	}
	payload, err := json.Marshal(credentialSecretEnvelope{metadata.WorkspaceID, metadata.AccountID, metadata.Reference, metadata.Version, credentials})
	if err != nil {
		return "", ErrCredentialSecretInvalid
	}
	return encryptor.Encrypt(string(payload))
}

func ResolveCredentialSecret(ctx context.Context, encryptor SecretEncryptor, metadata CredentialSecret, ciphertext string) (map[string]any, error) {
	if workspaceID, scoped := domain.WorkspaceScope(ctx); scoped && workspaceID != metadata.WorkspaceID {
		return nil, ErrCredentialSecretNotFound
	}
	if metadata.RevokedAt != nil {
		return nil, ErrCredentialSecretRevoked
	}
	if encryptor == nil {
		return nil, ErrCredentialSecretInvalid
	}
	plaintext, err := encryptor.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential reference: %w", ErrCredentialSecretInvalid)
	}
	var payload credentialSecretEnvelope
	if json.Unmarshal([]byte(plaintext), &payload) != nil || payload.WorkspaceID != metadata.WorkspaceID || payload.AccountID != metadata.AccountID || payload.Reference != metadata.Reference || payload.Version != metadata.Version {
		return nil, ErrCredentialSecretInvalid
	}
	return payload.Credentials, nil
}

func EncryptVerifiedCredential(ctx context.Context, enc SecretEncryptor, m CredentialSecret, values map[string]any) (string, error) {
	cipher, err := EncryptCredentialSecret(enc, m, values)
	if err != nil {
		return "", err
	}
	decoded, err := ResolveCredentialSecret(ctx, enc, m, cipher)
	if err != nil {
		return "", err
	}
	before, e1 := json.Marshal(values)
	after, e2 := json.Marshal(decoded)
	if e1 != nil || e2 != nil || !bytes.Equal(before, after) {
		return "", ErrCredentialSecretInvalid
	}
	return cipher, nil
}

func AccountAPIKeyDigest(credentials map[string]any) string {
	key, ok := credentials["api_key"].(string)
	if !ok || key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// StoredCredentialDocument returns the JSON used by row-level compare-and-swap.
func StoredCredentialDocument(credentials map[string]any) map[string]any {
	if _, ok := CredentialReference(credentials); !ok {
		return credentials
	}
	result := make(map[string]any, len(credentials))
	for k, v := range credentials {
		if !IsSensitiveCredentialKey(k) {
			result[k] = v
		}
	}
	return result
}

type unresolvedCredentialRowsKey struct{}

// WithUnresolvedCredentialRows lets an operator list show accounts whose
// stored credential cannot be resolved, with secret fields removed, so the
// operator can find and repair them. Other reads omit such rows.
func WithUnresolvedCredentialRows(ctx context.Context) context.Context {
	return context.WithValue(ctx, unresolvedCredentialRowsKey{}, true)
}

func UnresolvedCredentialRowsVisible(ctx context.Context) bool {
	visible, _ := ctx.Value(unresolvedCredentialRowsKey{}).(bool)
	return visible
}

type unusableCredentialRowsOmittedKey struct{}

// WithUnusableCredentialRowsOmitted lets a batch read treat accounts whose
// stored credential cannot be resolved as absent instead of failing the batch.
func WithUnusableCredentialRowsOmitted(ctx context.Context) context.Context {
	return context.WithValue(ctx, unusableCredentialRowsOmittedKey{}, true)
}

func UnusableCredentialRowsOmitted(ctx context.Context) bool {
	omitted, _ := ctx.Value(unusableCredentialRowsOmittedKey{}).(bool)
	return omitted
}

// IsCredentialUnusable reports a stored credential that cannot be used until an
// operator repairs it. Transient version races are not included.
func IsCredentialUnusable(err error) bool {
	return errors.Is(err, ErrCredentialSecretNotFound) || errors.Is(err, ErrCredentialSecretInvalid) || errors.Is(err, ErrCredentialSecretRevoked)
}
