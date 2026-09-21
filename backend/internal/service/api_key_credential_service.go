package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ErrAPIKeyCredentialUnavailable reports that credential rotation is not wired.
var ErrAPIKeyCredentialUnavailable = infraerrors.ServiceUnavailable("API_KEY_CREDENTIAL_UNAVAILABLE", "api key credential lifecycle is not available")

// RotateAPIKey rotates a key's credential in place: a fresh secret is generated but
// the key keeps its identity id, so usage, quota and billing history stay attached.
// The previous secret keeps working until the grace deadline unless revokePrevious
// is set, in which case it stops immediately.
func (s *APIKeyService) RotateAPIKey(ctx context.Context, apiKeyID int64, graceTTL time.Duration, revokePrevious bool, actor ConfigurationAuditActor) (*APIKeyCredentialIssue, error) {
	if s == nil || s.credentialRepo == nil {
		return nil, ErrAPIKeyCredentialUnavailable
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}

	secret, err := s.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if graceTTL <= 0 {
		graceTTL = s.apiKeyCredentialGracePeriod()
	}

	rotation := APIKeyCredentialRotation{
		APIKeyID:    apiKey.ID,
		WorkspaceID: apiKey.WorkspaceID,
		NewCredential: APIKeyCredential{
			Digest:        APIKeyCredentialDigest(secret),
			DisplaySuffix: APIKeyCredentialDisplaySuffix(secret),
			Status:        domain.CredentialStatusCurrent,
		},
		GraceUntil:     time.Now().UTC().Add(graceTTL),
		RevokePrevious: revokePrevious,
		Actor:          actor,
	}
	if err := s.credentialRepo.Rotate(ctx, rotation); err != nil {
		return nil, fmt.Errorf("rotate api key credential: %w", err)
	}

	// Drop every cached entry of the key, then the new credential's own entry so the
	// new secret is usable immediately on every instance.
	s.InvalidateAuthCacheByAPIKeyID(ctx, apiKey.ID)
	s.InvalidateAuthCacheByDigest(ctx, rotation.NewCredential.Digest)

	apiKey.Key = secret
	apiKey.DisplaySuffix = rotation.NewCredential.DisplaySuffix
	issue := &APIKeyCredentialIssue{APIKey: apiKey, Secret: secret}
	if !revokePrevious {
		graceUntil := rotation.GraceUntil
		issue.GraceUntil = &graceUntil
	}
	return issue, nil
}

// RevokeAPIKeyCredentials immediately revokes the graceful credentials of a key
// (the "leaked secret" response), keeping the current secret valid.
func (s *APIKeyService) RevokeAPIKeyCredentials(ctx context.Context, apiKeyID int64, actor ConfigurationAuditActor) (int64, error) {
	if s == nil || s.credentialRepo == nil {
		return 0, ErrAPIKeyCredentialUnavailable
	}
	revoked, err := s.credentialRepo.RevokeGrace(ctx, apiKeyID, time.Now().UTC(), actor)
	if err != nil {
		return 0, fmt.Errorf("revoke api key credentials: %w", err)
	}
	s.InvalidateAuthCacheByAPIKeyID(ctx, apiKeyID)
	return revoked, nil
}

// RevokeAllAPIKeyCredentials revokes every credential of a key; used when an admin
// retires a key so no presented secret remains valid.
func (s *APIKeyService) RevokeAllAPIKeyCredentials(ctx context.Context, apiKeyID int64, actor ConfigurationAuditActor) (int64, error) {
	if s == nil || s.credentialRepo == nil {
		return 0, ErrAPIKeyCredentialUnavailable
	}
	revoked, err := s.credentialRepo.RevokeAll(ctx, apiKeyID, time.Now().UTC(), actor)
	if err != nil {
		return 0, fmt.Errorf("revoke api key credentials: %w", err)
	}
	s.InvalidateAuthCacheByAPIKeyID(ctx, apiKeyID)
	return revoked, nil
}

// apiKeyCredentialGracePeriod resolves the dual-read window, defaulting to 24h.
func (s *APIKeyService) apiKeyCredentialGracePeriod() time.Duration {
	hours := DefaultAPIKeyCredentialGraceHours
	if s != nil && s.cfg != nil && s.cfg.APIKeyCredential.GraceTTLHours > 0 {
		hours = s.cfg.APIKeyCredential.GraceTTLHours
	}
	return time.Duration(hours) * time.Hour
}

// SetAPIKeyStatus sets a key's status without an ownership check; callers must
// authorize the actor. Auth cache entries for the key are invalidated immediately.
func (s *APIKeyService) SetAPIKeyStatus(ctx context.Context, apiKeyID int64, status string) (*APIKey, error) {
	if status != StatusAPIKeyActive && status != StatusAPIKeyDisabled {
		return nil, infraerrors.BadRequest("API_KEY_STATUS_INVALID", "api key status must be active or disabled")
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	if apiKey.Status != status {
		apiKey.Status = status
		if err := s.apiKeyRepo.Update(ctx, apiKey, APIKeyUpdateFields{Status: true}); err != nil {
			return nil, fmt.Errorf("update api key: %w", err)
		}
		s.InvalidateAuthCacheByAPIKeyID(ctx, apiKeyID)
	}
	s.compileAPIKeyIPRules(apiKey)
	return apiKey, nil
}

// DeleteAPIKeyByID removes a key and revokes its credentials without an ownership
// check; callers must authorize the actor.
func (s *APIKeyService) DeleteAPIKeyByID(ctx context.Context, apiKeyID int64) error {
	if err := s.apiKeyRepo.DeleteWithAudit(ctx, apiKeyID); err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if s.credentialRepo != nil {
		_, _ = s.credentialRepo.RevokeAll(ctx, apiKeyID, time.Now().UTC(), ConfigurationActor(ctx))
	}
	s.InvalidateAuthCacheByAPIKeyID(ctx, apiKeyID)
	s.lastUsedTouchL1.Delete(apiKeyID)
	return nil
}

// apiKeyStatusSetter is the narrow status-mutation surface the content moderation
// path needs. *APIKeyService satisfies it and is wired as the auth cache invalidator.
type apiKeyStatusSetter interface {
	SetAPIKeyStatus(ctx context.Context, apiKeyID int64, status string) (*APIKey, error)
}

// apiKeyCredentialLifecycle is the credential surface the admin path needs. It is
// satisfied by *APIKeyService, which wire provides as the auth cache invalidator.
type apiKeyCredentialLifecycle interface {
	RotateAPIKey(ctx context.Context, apiKeyID int64, graceTTL time.Duration, revokePrevious bool, actor ConfigurationAuditActor) (*APIKeyCredentialIssue, error)
	SetAPIKeyStatus(ctx context.Context, apiKeyID int64, status string) (*APIKey, error)
	DeleteAPIKeyByID(ctx context.Context, apiKeyID int64) error
}
