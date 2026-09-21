package service

import "context"

// apiKeyAuthCacheInvalidatorByAPIKeyID is the optional capability of an auth cache
// invalidator that evicts every credential of a key from the key identity alone. It
// stays optional so the narrow APIKeyAuthCacheInvalidator contract used by other
// services does not grow; *APIKeyService always satisfies it.
type apiKeyAuthCacheInvalidatorByAPIKeyID interface {
	InvalidateAuthCacheByAPIKeyID(ctx context.Context, apiKeyID int64)
}

// InvalidateAuthCacheByKey clears the auth cache entry for a credential digest.
// The digest is the auth cache identifier, so callers pass one directly; use
// InvalidateAuthCacheByAPIKeyID to clear every credential of a key.
func (s *APIKeyService) InvalidateAuthCacheByKey(ctx context.Context, credentialDigest string) {
	if credentialDigest == "" {
		return
	}
	s.deleteAuthCache(ctx, credentialDigest)
}

// InvalidateAuthCacheByAPIKeyID clears every credential's auth cache entry for a
// key, without needing any plaintext secret.
func (s *APIKeyService) InvalidateAuthCacheByAPIKeyID(ctx context.Context, apiKeyID int64) {
	if apiKeyID <= 0 || s.credentialRepo == nil {
		return
	}
	digests, err := s.credentialRepo.ListDigestsByAPIKeyID(ctx, apiKeyID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByKeys(ctx, digests)
}

// InvalidateAuthCacheByDigest clears the auth cache entry for a credential digest.
func (s *APIKeyService) InvalidateAuthCacheByDigest(ctx context.Context, credentialDigest string) {
	s.deleteAuthCache(ctx, credentialDigest)
}

// InvalidateAuthCacheByUserID 清除用户相关的 API Key 认证缓存
func (s *APIKeyService) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	if userID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeysByUserID(ctx, userID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByKeys(ctx, keys)
}

// InvalidateAuthCacheByGroupID 清除分组相关的 API Key 认证缓存
func (s *APIKeyService) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	if groupID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeysByGroupID(ctx, groupID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByKeys(ctx, keys)
}

func (s *APIKeyService) deleteAuthCacheByKeys(ctx context.Context, credentialDigests []string) {
	if len(credentialDigests) == 0 {
		return
	}
	for _, digest := range credentialDigests {
		if digest == "" {
			continue
		}
		s.deleteAuthCache(ctx, digest)
	}
}
