package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"golang.org/x/sync/singleflight"
)

const (
	credentialDecryptionCacheTTL     = 30 * time.Second
	credentialDecryptionCacheEntries = 512
)

// credentialDecryptionCacheKey deliberately includes every persisted value that
// can change the decrypted document. A credential rotation or a same-version
// ciphertext rewrite therefore misses immediately; expiry only bounds how long
// unchanged plaintext remains resident in this process.
type credentialDecryptionCacheKey struct {
	accountID      int64
	version        int64
	updatedAtNanos int64
	reference      string
	ciphertextHash [sha256.Size]byte
}

func (k credentialDecryptionCacheKey) flightKey() string {
	return fmt.Sprintf("%d/%d/%d/%s/%x", k.accountID, k.version, k.updatedAtNanos, k.reference, k.ciphertextHash)
}

type credentialDecryptionCacheEntry struct {
	credentials map[string]any
	expiresAt   time.Time
}

// credentialDecryptionCache is intentionally small and process-local. It
// never returns the stored map: callers may enrich/mutate Credentials, and
// sharing that map would permit a concurrent-map-write panic or secret bleed.
type credentialDecryptionCache struct {
	mu      sync.Mutex
	entries map[credentialDecryptionCacheKey]credentialDecryptionCacheEntry
	flights singleflight.Group
	now     func() time.Time
}

func newCredentialDecryptionCache() *credentialDecryptionCache {
	return &credentialDecryptionCache{
		entries: make(map[credentialDecryptionCacheKey]credentialDecryptionCacheEntry),
		now:     time.Now,
	}
}

func credentialDecryptionKey(metadata service.CredentialSecret, ciphertext string) credentialDecryptionCacheKey {
	return credentialDecryptionCacheKey{
		accountID:      metadata.AccountID,
		version:        metadata.Version,
		updatedAtNanos: metadata.UpdatedAt.UTC().UnixNano(),
		reference:      metadata.Reference,
		ciphertextHash: sha256.Sum256([]byte(ciphertext)),
	}
}

func cloneCredentialMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneCredentialValue(value)
	}
	return out
}

func cloneCredentialValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneCredentialMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneCredentialValue(v[i])
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}

func (c *credentialDecryptionCache) load(key credentialDecryptionCacheKey) (map[string]any, bool) {
	if c == nil {
		return nil, false
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return cloneCredentialMap(entry.credentials), true
}

func (c *credentialDecryptionCache) store(key credentialDecryptionCacheKey, credentials map[string]any) {
	if c == nil {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for cacheKey, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, cacheKey)
		}
	}
	if len(c.entries) >= credentialDecryptionCacheEntries {
		var oldestKey credentialDecryptionCacheKey
		var oldestExpiry time.Time
		for cacheKey, entry := range c.entries {
			if oldestExpiry.IsZero() || entry.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = cacheKey, entry.expiresAt
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = credentialDecryptionCacheEntry{
		credentials: cloneCredentialMap(credentials),
		expiresAt:   now.Add(credentialDecryptionCacheTTL),
	}
}

func (r *accountRepository) resolveStoredCredentialSecret(ctx context.Context, metadata service.CredentialSecret, ciphertext string) (map[string]any, error) {
	cache := r.credentialDecryptionCache
	if cache == nil {
		return service.ResolveCredentialSecret(ctx, r.credentialEncryptor, metadata, ciphertext)
	}
	key := credentialDecryptionKey(metadata, ciphertext)
	if credentials, ok := cache.load(key); ok {
		return credentials, nil
	}

	value, err, _ := cache.flights.Do(key.flightKey(), func() (any, error) {
		if credentials, ok := cache.load(key); ok {
			return credentials, nil
		}
		credentials, err := service.ResolveCredentialSecret(ctx, r.credentialEncryptor, metadata, ciphertext)
		if err != nil {
			return nil, err
		}
		cache.store(key, credentials)
		return cloneCredentialMap(credentials), nil
	})
	if err != nil {
		return nil, err
	}
	creds, _ := value.(map[string]any)
	return cloneCredentialMap(creds), nil
}

type storedCredentialSecret struct {
	metadata   service.CredentialSecret
	ciphertext string
}

// One query for the whole ID set, on the caller's transaction/client. Neither
// query arguments nor returned ciphertext/credentials are logged.
func readCredentialSecretsBatch(ctx context.Context, database credentialSQL, accountIDs []int64) (map[int64]storedCredentialSecret, error) {
	secrets := make(map[int64]storedCredentialSecret, len(accountIDs))
	if len(accountIDs) == 0 {
		return secrets, nil
	}
	rows, err := database.QueryContext(ctx, `SELECT reference, workspace_id, account_id, version, updated_at, revoked_at, ciphertext
		FROM credential_secrets WHERE account_id = ANY($1)`+workspaceClause(ctx, "workspace_id"), pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var record storedCredentialSecret
		m := &record.metadata
		if err := rows.Scan(&m.Reference, &m.WorkspaceID, &m.AccountID, &m.Version, &m.UpdatedAt, &m.RevokedAt, &record.ciphertext); err != nil {
			return nil, err
		}
		if _, duplicate := secrets[m.AccountID]; duplicate {
			return nil, service.ErrCredentialSecretInvalid
		}
		secrets[m.AccountID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return secrets, nil
}

// Validate the persisted pointer before decrypting. A row/secret race is a
// conflict, never permission to combine a stale revision with newer material.
func (r *accountRepository) applyStoredCredentialSecret(ctx context.Context, account *service.Account, metadata *service.CredentialSecret, ciphertext string) error {
	reference, referenced := service.CredentialReference(account.Credentials)
	version := service.CredentialVersion(account.Credentials)
	if !referenced || reference == "" || version < 1 {
		return service.ErrCredentialSecretInvalid
	}
	if metadata == nil || metadata.AccountID != account.ID || metadata.WorkspaceID != account.WorkspaceID || metadata.Reference != reference {
		return service.ErrCredentialSecretNotFound
	}
	if metadata.Version != version {
		return service.ErrCredentialSecretConflict
	}
	for field := range account.Credentials {
		if service.IsSensitiveCredentialKey(field) {
			return service.ErrCredentialSecretInvalid
		}
	}
	secrets, err := r.resolveStoredCredentialSecret(ctx, *metadata, ciphertext)
	if err != nil {
		return err
	}
	resolved := make(map[string]any, len(account.Credentials)+len(secrets))
	for field, value := range account.Credentials {
		resolved[field] = value
	}
	for field, value := range secrets {
		// Encrypted configuration/reference injection must not overwrite the
		// identity that was just checked or bypass Account.MarshalJSON redaction.
		if !service.IsSensitiveCredentialKey(field) {
			return service.ErrCredentialSecretInvalid
		}
		resolved[field] = value
	}
	resolved[service.CredentialSecretVersionKey] = metadata.Version
	account.Credentials = resolved
	return nil
}

func credentialReadUnavailable(err error) bool {
	return errors.Is(err, service.ErrCredentialSecretNotFound) || errors.Is(err, service.ErrCredentialSecretInvalid) ||
		errors.Is(err, service.ErrCredentialSecretRevoked) || errors.Is(err, service.ErrCredentialSecretConflict)
}

// General reads are all-or-nothing. Candidate scans may omit an unusable row,
// but never return its unresolved credentials or fall back to inline secrets.
// Database/transport errors always fail the whole read, including scans.
func (r *accountRepository) resolveAccountCredentialsBatch(ctx context.Context, database credentialSQL, accounts []*service.Account, skipUnavailable bool) ([]*service.Account, error) {
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		account.AttachEgress(r.egressRuntime)
		account.CredentialRevision = credentialRevision(account.Credentials)
		if _, referenced := service.CredentialReference(account.Credentials); referenced {
			ids = append(ids, account.ID)
		}
	}
	records, err := readCredentialSecretsBatch(ctx, database, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*service.Account, 0, len(accounts))
	for _, account := range accounts {
		if _, referenced := service.CredentialReference(account.Credentials); referenced {
			record, found := records[account.ID]
			var metadata *service.CredentialSecret
			if found {
				metadata = &record.metadata
			}
			if err := r.applyStoredCredentialSecret(ctx, account, metadata, record.ciphertext); err != nil {
				if skipUnavailable && credentialReadUnavailable(err) {
					continue
				}
				return nil, err
			}
		}
		out = append(out, account)
	}
	return out, nil
}
