package service

import (
	"context"
	"time"
)

// adminAPIKeyLifecycle resolves the credential operations behind the admin key
// endpoints. Wire passes *APIKeyService as the auth cache invalidator, so the
// assertion succeeds in production and degrades to a clear error in tests.
func (s *adminServiceImpl) adminAPIKeyLifecycle() (apiKeyCredentialLifecycle, error) {
	if s == nil || s.authCacheInvalidator == nil {
		return nil, ErrAPIKeyCredentialUnavailable
	}
	lifecycle, ok := s.authCacheInvalidator.(apiKeyCredentialLifecycle)
	if !ok {
		return nil, ErrAPIKeyCredentialUnavailable
	}
	return lifecycle, nil
}

// AdminSetAPIKeyStatus enables or disables any user's key.
func (s *adminServiceImpl) AdminSetAPIKeyStatus(ctx context.Context, keyID int64, status string) (*APIKey, error) {
	lifecycle, err := s.adminAPIKeyLifecycle()
	if err != nil {
		return nil, err
	}
	return lifecycle.SetAPIKeyStatus(ctx, keyID, status)
}

// AdminDeleteAPIKey removes any user's key and revokes its credentials.
func (s *adminServiceImpl) AdminDeleteAPIKey(ctx context.Context, keyID int64) error {
	lifecycle, err := s.adminAPIKeyLifecycle()
	if err != nil {
		return err
	}
	return lifecycle.DeleteAPIKeyByID(ctx, keyID)
}

// AdminRotateAPIKeyCredential rotates any user's key secret, keeping its identity.
func (s *adminServiceImpl) AdminRotateAPIKeyCredential(ctx context.Context, keyID int64, graceTTL time.Duration, revokePrevious bool) (*APIKeyCredentialIssue, error) {
	lifecycle, err := s.adminAPIKeyLifecycle()
	if err != nil {
		return nil, err
	}
	return lifecycle.RotateAPIKey(ctx, keyID, graceTTL, revokePrevious, ConfigurationActor(ctx))
}

// invalidateAPIKeyAuthCache drops every cached credential of a key. It prefers the
// digest-based invalidation when the invalidator exposes it, so no plaintext secret
// is ever needed to evict a key's cache entries.
func invalidateAPIKeyAuthCache(ctx context.Context, invalidator APIKeyAuthCacheInvalidator, apiKeyID int64) {
	if invalidator == nil || apiKeyID <= 0 {
		return
	}
	if byID, ok := invalidator.(apiKeyAuthCacheInvalidatorByAPIKeyID); ok {
		byID.InvalidateAuthCacheByAPIKeyID(ctx, apiKeyID)
	}
}
