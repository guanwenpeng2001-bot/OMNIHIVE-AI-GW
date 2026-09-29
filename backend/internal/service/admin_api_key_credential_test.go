//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// apiKeyCredentialLifecycleStub satisfies both the auth cache invalidator contract and
// the credential lifecycle capability the admin service asserts on.
type apiKeyCredentialLifecycleStub struct {
	authCacheInvalidatorStub
	rotated    []int64
	rotatedTTL []time.Duration
	revoked    []bool
	statuses   map[int64]string
	deleted    []int64
	rotateErr  error
	statusErr  error
	deleteErr  error
	actor      ConfigurationAuditActor
}

func (s *apiKeyCredentialLifecycleStub) RotateAPIKey(_ context.Context, apiKeyID int64, graceTTL time.Duration, revokePrevious bool, actor ConfigurationAuditActor) (*APIKeyCredentialIssue, error) {
	if s.rotateErr != nil {
		return nil, s.rotateErr
	}
	s.rotated = append(s.rotated, apiKeyID)
	s.rotatedTTL = append(s.rotatedTTL, graceTTL)
	s.revoked = append(s.revoked, revokePrevious)
	s.actor = actor
	return &APIKeyCredentialIssue{
		APIKey: &APIKey{ID: apiKeyID, UserID: 7, Status: StatusAPIKeyActive},
		Secret: "sk-rotated-by-admin",
	}, nil
}

func (s *apiKeyCredentialLifecycleStub) SetAPIKeyStatus(_ context.Context, apiKeyID int64, status string) (*APIKey, error) {
	if s.statusErr != nil {
		return nil, s.statusErr
	}
	if s.statuses == nil {
		s.statuses = map[int64]string{}
	}
	s.statuses[apiKeyID] = status
	return &APIKey{ID: apiKeyID, UserID: 7, Status: status}, nil
}

func (s *apiKeyCredentialLifecycleStub) DeleteAPIKeyByID(_ context.Context, apiKeyID int64) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, apiKeyID)
	return nil
}

func newAdminAPIKeyLifecycleService(lifecycle *apiKeyCredentialLifecycleStub) *adminServiceImpl {
	return &adminServiceImpl{
		apiKeyRepo:           &apiKeyRepoStub{apiKey: &APIKey{ID: 10, UserID: 7}},
		authCacheInvalidator: lifecycle,
	}
}

// The admin surface needs the credential capability the APIKeyService provides; when it
// is missing the operation must fail loudly instead of silently doing nothing.
func TestAdminService_APIKeyLifecycle_UnavailableWithoutCredentialCapability(t *testing.T) {
	svc := &adminServiceImpl{apiKeyRepo: &apiKeyRepoStub{}, authCacheInvalidator: &authCacheInvalidatorStub{}}

	_, err := svc.AdminSetAPIKeyStatus(context.Background(), 10, StatusAPIKeyDisabled)
	require.ErrorIs(t, err, ErrAPIKeyCredentialUnavailable)

	_, err = svc.AdminRotateAPIKeyCredential(context.Background(), 10, 0, false)
	require.ErrorIs(t, err, ErrAPIKeyCredentialUnavailable)

	require.ErrorIs(t, svc.AdminDeleteAPIKey(context.Background(), 10), ErrAPIKeyCredentialUnavailable)
}

func TestAdminService_AdminSetAPIKeyStatus_TargetsAnyUsersKey(t *testing.T) {
	lifecycle := &apiKeyCredentialLifecycleStub{}
	svc := newAdminAPIKeyLifecycleService(lifecycle)

	key, err := svc.AdminSetAPIKeyStatus(context.Background(), 10, StatusAPIKeyDisabled)
	require.NoError(t, err)
	require.Equal(t, StatusAPIKeyDisabled, key.Status)
	require.Equal(t, map[int64]string{10: StatusAPIKeyDisabled}, lifecycle.statuses)
}

func TestAdminService_AdminDeleteAPIKey_RevokesTheCredentialLifecycle(t *testing.T) {
	lifecycle := &apiKeyCredentialLifecycleStub{}
	svc := newAdminAPIKeyLifecycleService(lifecycle)

	require.NoError(t, svc.AdminDeleteAPIKey(context.Background(), 10))
	require.Equal(t, []int64{10}, lifecycle.deleted)
}

func TestAdminService_AdminRotateAPIKeyCredential_ForwardsTheRequestAndActor(t *testing.T) {
	lifecycle := &apiKeyCredentialLifecycleStub{}
	svc := newAdminAPIKeyLifecycleService(lifecycle)

	ctx := WithConfigurationAuditActor(context.Background(), ConfigurationAuditActor{UserID: 1, Role: RoleAdmin, RequestID: "req-1"})
	issue, err := svc.AdminRotateAPIKeyCredential(ctx, 10, 3*time.Hour, true)
	require.NoError(t, err)
	require.Equal(t, int64(10), issue.APIKey.ID)
	require.Equal(t, "sk-rotated-by-admin", issue.Secret)
	require.Equal(t, []time.Duration{3 * time.Hour}, lifecycle.rotatedTTL)
	require.Equal(t, []bool{true}, lifecycle.revoked)
	require.Equal(t, ConfigurationAuditActor{UserID: 1, Role: RoleAdmin, RequestID: "req-1"}, lifecycle.actor)
}

func TestAdminService_AdminRotateAPIKeyCredential_PropagatesFailure(t *testing.T) {
	rotateErr := errors.New("rotate unavailable")
	lifecycle := &apiKeyCredentialLifecycleStub{rotateErr: rotateErr}
	svc := newAdminAPIKeyLifecycleService(lifecycle)

	_, err := svc.AdminRotateAPIKeyCredential(context.Background(), 10, 0, false)
	require.ErrorIs(t, err, rotateErr)
	require.Empty(t, lifecycle.rotated)
}
