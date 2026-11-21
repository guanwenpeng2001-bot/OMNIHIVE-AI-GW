//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// apiKeyCredentialRepoStub records how the credential lifecycle service used the
// credential repository without touching a database.
type apiKeyCredentialRepoStub struct {
	digests        []string
	listErr        error
	rotations      []APIKeyCredentialRotation
	rotateErr      error
	revokeGrace    int64
	revokeGraceErr error
	revokeAll      int64
	revokeAllErr   error
}

func (s *apiKeyCredentialRepoStub) ListDigestsByAPIKeyID(context.Context, int64) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.digests, nil
}

func (s *apiKeyCredentialRepoStub) Rotate(_ context.Context, rotation APIKeyCredentialRotation) error {
	if s.rotateErr != nil {
		return s.rotateErr
	}
	s.rotations = append(s.rotations, rotation)
	return nil
}

func (s *apiKeyCredentialRepoStub) RevokeGrace(context.Context, int64, time.Time, ConfigurationAuditActor) (int64, error) {
	return s.revokeGrace, s.revokeGraceErr
}

func (s *apiKeyCredentialRepoStub) RevokeAll(context.Context, int64, time.Time, ConfigurationAuditActor) (int64, error) {
	return s.revokeAll, s.revokeAllErr
}

func newCredentialLifecycleService(repo *apiKeyCredentialRepoStub, cache *apiKeyCacheStub, keys *APIKey) *APIKeyService {
	svc := NewAPIKeyService(&apiKeyRepoStub{apiKey: keys}, nil, nil, nil, nil, cache, testConfig())
	svc.SetCredentialRepository(repo)
	return svc
}

// The digest is the auth cache key, so evicting by digest reaches exactly the cached
// entry of the presented credential.
func TestAPIKeyCredentialDigest_IsTheAuthCacheIdentifier(t *testing.T) {
	secret := "sk-0123456789abcdef"
	svc := NewAPIKeyService(&apiKeyRepoStub{}, nil, nil, nil, nil, nil, testConfig())

	require.Equal(t, svc.authCacheKey(secret), APIKeyCredentialDigest(secret))
	require.Len(t, APIKeyCredentialDigest(secret), 64)
}

func TestAPIKeyCredentialDisplaySuffix_KeepsOnlyTheTail(t *testing.T) {
	require.Equal(t, "cdef", APIKeyCredentialDisplaySuffix("sk-0123456789abcdef"))
	require.Equal(t, "abc", APIKeyCredentialDisplaySuffix("abc"))
}

func TestAPIKeyService_RotateAPIKey_KeepsIdentityAndOpensGraceWindow(t *testing.T) {
	presentedDigest := APIKeyCredentialDigest("sk-presented-old")
	repo := &apiKeyCredentialRepoStub{digests: []string{presentedDigest}}
	cache := &apiKeyCacheStub{}
	svc := newCredentialLifecycleService(repo, cache, &APIKey{ID: 42, UserID: 7, WorkspaceID: 3, Key: "sk-stored-old"})

	issue, err := svc.RotateAPIKey(context.Background(), 42, 0, false, ConfigurationAuditActor{UserID: 7, Role: RoleUser})
	require.NoError(t, err)

	require.NotNil(t, issue)
	require.Equal(t, int64(42), issue.APIKey.ID, "rotation must keep the key identity")
	require.NotEmpty(t, issue.Secret)
	require.NotEqual(t, "sk-stored-old", issue.Secret)
	require.NotNil(t, issue.GraceUntil, "a rotation without immediate revoke opens a grace window")

	require.Len(t, repo.rotations, 1)
	rotation := repo.rotations[0]
	require.Equal(t, int64(42), rotation.APIKeyID)
	require.Equal(t, int64(3), rotation.WorkspaceID)
	require.Equal(t, APIKeyCredentialDigest(issue.Secret), rotation.NewCredential.Digest)
	require.Equal(t, domain.CredentialStatusCurrent, rotation.NewCredential.Status)
	require.Equal(t, APIKeyCredentialDisplaySuffix(issue.Secret), rotation.NewCredential.DisplaySuffix)
	require.False(t, rotation.RevokePrevious)
	require.WithinDuration(t, time.Now().UTC().Add(DefaultAPIKeyCredentialGraceHours*time.Hour), rotation.GraceUntil, time.Minute)
	require.Equal(t, int64(7), rotation.Actor.UserID)

	// Both the credential that was presented and the new one must be evicted, otherwise
	// an instance could keep serving a stale snapshot.
	require.Equal(t, []string{presentedDigest, APIKeyCredentialDigest(issue.Secret)}, cache.deleteAuthKeys)
}

func TestAPIKeyService_RotateAPIKey_RevokePreviousSkipsTheGraceWindow(t *testing.T) {
	repo := &apiKeyCredentialRepoStub{}
	svc := newCredentialLifecycleService(repo, &apiKeyCacheStub{}, &APIKey{ID: 42, UserID: 7})

	issue, err := svc.RotateAPIKey(context.Background(), 42, 0, true, ConfigurationAuditActor{UserID: 7})
	require.NoError(t, err)
	require.Nil(t, issue.GraceUntil, "immediate revocation must not advertise a grace deadline")
	require.Len(t, repo.rotations, 1)
	require.True(t, repo.rotations[0].RevokePrevious)
}

func TestAPIKeyService_RotateAPIKey_HonoursConfiguredAndExplicitGraceWindow(t *testing.T) {
	cfg := testConfig()
	cfg.APIKeyCredential.GraceTTLHours = 2

	repo := &apiKeyCredentialRepoStub{}
	svc := NewAPIKeyService(&apiKeyRepoStub{apiKey: &APIKey{ID: 42, UserID: 7}}, nil, nil, nil, nil, nil, cfg)
	svc.SetCredentialRepository(repo)

	_, err := svc.RotateAPIKey(context.Background(), 42, 0, false, ConfigurationAuditActor{UserID: 7})
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC().Add(2*time.Hour), repo.rotations[0].GraceUntil, time.Minute)

	repo.rotations = nil
	_, err = svc.RotateAPIKey(context.Background(), 42, 30*time.Minute, false, ConfigurationAuditActor{UserID: 7})
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC().Add(30*time.Minute), repo.rotations[0].GraceUntil, time.Minute)
}

func TestAPIKeyService_RotateAPIKey_RequiresTheCredentialRepository(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{apiKey: &APIKey{ID: 42, UserID: 7}}, nil, nil, nil, nil, nil, testConfig())

	issue, err := svc.RotateAPIKey(context.Background(), 42, 0, false, ConfigurationAuditActor{UserID: 7})
	require.ErrorIs(t, err, ErrAPIKeyCredentialUnavailable)
	require.Nil(t, issue)
}

func TestAPIKeyService_RotateAPIKey_PropagatesRepositoryFailure(t *testing.T) {
	rotateErr := errors.New("rotate failed")
	repo := &apiKeyCredentialRepoStub{rotateErr: rotateErr}
	svc := newCredentialLifecycleService(repo, &apiKeyCacheStub{}, &APIKey{ID: 42, UserID: 7})

	issue, err := svc.RotateAPIKey(context.Background(), 42, 0, false, ConfigurationAuditActor{UserID: 7})
	require.ErrorIs(t, err, rotateErr)
	require.Nil(t, issue)
}

func TestAPIKeyService_RevokeAPIKeyCredentials_RevokesGraceAndEvictsTheKey(t *testing.T) {
	digest := APIKeyCredentialDigest("sk-presented")
	repo := &apiKeyCredentialRepoStub{digests: []string{digest}, revokeGrace: 1}
	cache := &apiKeyCacheStub{}
	svc := newCredentialLifecycleService(repo, cache, &APIKey{ID: 42, UserID: 7})

	revoked, err := svc.RevokeAPIKeyCredentials(context.Background(), 42, ConfigurationAuditActor{UserID: 7})
	require.NoError(t, err)
	require.Equal(t, int64(1), revoked)
	require.Equal(t, []string{digest}, cache.deleteAuthKeys)
}

func TestAPIKeyService_RevokeAPIKeyCredentials_RequiresTheCredentialRepository(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{}, nil, nil, nil, nil, nil, testConfig())

	revoked, err := svc.RevokeAPIKeyCredentials(context.Background(), 42, ConfigurationAuditActor{UserID: 7})
	require.ErrorIs(t, err, ErrAPIKeyCredentialUnavailable)
	require.Zero(t, revoked)
}

func TestAPIKeyService_DeleteAPIKeyByID_RevokesEveryCredential(t *testing.T) {
	digest := APIKeyCredentialDigest("sk-presented")
	repo := &apiKeyCredentialRepoStub{digests: []string{digest}, revokeAll: 2}
	cache := &apiKeyCacheStub{}
	apiKeyRepo := &apiKeyRepoStub{apiKey: &APIKey{ID: 42, UserID: 7, Key: "sk-stored"}}
	svc := NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, cache, testConfig())
	svc.SetCredentialRepository(repo)

	require.NoError(t, svc.DeleteAPIKeyByID(context.Background(), 42))
	require.Equal(t, []int64{42}, apiKeyRepo.deletedIDs)
	require.Equal(t, []string{digest}, cache.deleteAuthKeys)
}

// An admin or a moderator may only move a key between the two real states.
func TestAPIKeyService_SetAPIKeyStatus_RejectsUnknownStatus(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{}, nil, nil, nil, nil, nil, testConfig())

	_, err := svc.SetAPIKeyStatus(context.Background(), 42, StatusAPIKeyQuotaExhausted)
	require.Error(t, err)
}

func TestAPIKeyService_SetAPIKeyStatus_DisablesAndEvictsTheKey(t *testing.T) {
	digest := APIKeyCredentialDigest("sk-presented")
	repo := &apiKeyCredentialRepoStub{digests: []string{digest}}
	cache := &apiKeyCacheStub{}
	apiKeyRepo := &apiKeyRepoStub{apiKey: &APIKey{ID: 42, UserID: 7, Key: "sk-stored", Status: StatusAPIKeyActive}}
	svc := NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, cache, testConfig())
	svc.SetCredentialRepository(repo)

	updated, err := svc.SetAPIKeyStatus(context.Background(), 42, StatusAPIKeyDisabled)
	require.NoError(t, err)
	require.Equal(t, StatusAPIKeyDisabled, updated.Status)
	require.Len(t, apiKeyRepo.updatedKeys, 1)
	require.Equal(t, StatusAPIKeyDisabled, apiKeyRepo.updatedKeys[0].Status)
	require.Equal(t, []string{digest}, cache.deleteAuthKeys)
}

// Setting the state a key already has must not write or evict anything.
func TestAPIKeyService_SetAPIKeyStatus_SameStatusIsANoOp(t *testing.T) {
	repo := &apiKeyCredentialRepoStub{digests: []string{APIKeyCredentialDigest("sk-presented")}}
	cache := &apiKeyCacheStub{}
	apiKeyRepo := &apiKeyRepoStub{apiKey: &APIKey{ID: 42, UserID: 7, Status: StatusAPIKeyActive}}
	svc := NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, cache, testConfig())
	svc.SetCredentialRepository(repo)

	updated, err := svc.SetAPIKeyStatus(context.Background(), 42, StatusAPIKeyActive)
	require.NoError(t, err)
	require.Equal(t, StatusAPIKeyActive, updated.Status)
	require.Empty(t, apiKeyRepo.updatedKeys)
	require.Empty(t, cache.deleteAuthKeys)
}

// ---------------------------------------------------------------------------
// CC-35: update省略=不变、0=解绑，用户与管理员一致
// ---------------------------------------------------------------------------

func TestAPIKeyService_Update_ZeroGroupIDUnbinds(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 7, Key: "sk-stored", GroupID: int64Ptr(5), Group: &Group{ID: 5, Name: "Old"}}
	repo := &apiKeyRepoStubForGroupUpdate{key: existing}
	credentialRepo := &apiKeyCredentialRepoStub{}
	// Both stubs panic on any unexpected call, so reaching the user or group repository
	// while unbinding would fail the test loudly.
	svc := NewAPIKeyService(repo, &userRepoStubForGroupUpdate{}, &groupRepoStubForGroupUpdate{}, nil, nil, nil, testConfig())
	svc.SetCredentialRepository(credentialRepo)

	updated, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: int64Ptr(0)})
	require.NoError(t, err)
	require.Nil(t, updated.GroupID, "group_id 0 must unbind")
	require.Nil(t, updated.Group)
	require.NotNil(t, repo.updated)
	require.Nil(t, repo.updated.GroupID)
}

func TestAPIKeyService_Update_NegativeGroupIDIsRejected(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 7, GroupID: int64Ptr(5)}
	repo := &apiKeyRepoStubForGroupUpdate{key: existing}
	svc := NewAPIKeyService(repo, &userRepoStubForGroupUpdate{}, &groupRepoStubForGroupUpdate{}, nil, nil, nil, testConfig())

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: int64Ptr(-1)})
	require.Error(t, err)
	require.Nil(t, repo.updated)
}

// A nil group_id (omitted field or JSON null) must leave the binding untouched and
// must never be turned into a lookup of group id 0.
func TestAPIKeyService_Update_OmittedGroupIDKeepsTheBinding(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 7, GroupID: int64Ptr(5)}
	repo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 5, Name: "Old", Status: StatusActive}}
	svc := NewAPIKeyService(repo, &userRepoStubForGroupUpdate{}, groupRepo, nil, nil, nil, testConfig())

	updated, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: nil})
	require.NoError(t, err)
	require.NotNil(t, updated.GroupID)
	require.Equal(t, int64(5), *updated.GroupID)
	require.Zero(t, groupRepo.lastGetByIDArg, "an omitted group_id must not query a group")
}
