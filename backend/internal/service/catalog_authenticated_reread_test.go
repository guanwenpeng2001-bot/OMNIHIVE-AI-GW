package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type catalogRereadKeyRepository struct {
	APIKeyRepository
	accounts *visiblePolicyAccountRepo
	key      *APIKey
	keyError error
	reads    []int64
	snapshot func(int) (ModelAccessSnapshot, error)
}

func (repository *catalogRereadKeyRepository) ReadModelAccessSnapshot(_ context.Context, keyID int64) (ModelAccessSnapshot, error) {
	repository.reads = append(repository.reads, keyID)
	if repository.snapshot != nil {
		return repository.snapshot(len(repository.reads))
	}
	return ModelAccessSnapshot{Generation: repository.accounts.generation, RevisionKnown: true}, nil
}

func (repository *catalogRereadKeyRepository) GetByID(context.Context, int64) (*APIKey, error) {
	return repository.key, repository.keyError
}

type catalogRereadUserRepository struct {
	UserRepository
	user *User
}

func (repository *catalogRereadUserRepository) GetByID(context.Context, int64) (*User, error) {
	return repository.user, nil
}

func catalogRereadFixture() (*GatewayService, *APIKeyService, *catalogRereadKeyRepository, *catalogRereadUserRepository) {
	accounts := &visiblePolicyAccountRepo{accounts: []Account{catalogEvidenceAccount(11, PlatformOpenAI, "public")}, generation: 7}
	keys := &catalogRereadKeyRepository{accounts: accounts, key: &APIKey{ID: 41, UserID: 21, Status: StatusActive}}
	users := &catalogRereadUserRepository{user: &User{ID: 21, Status: StatusActive}}
	return &GatewayService{accountRepo: accounts}, &APIKeyService{apiKeyRepo: keys, userRepo: users}, keys, users
}

func TestAuthenticatedCatalogRereadRefreshesEveryPolicyRestriction(t *testing.T) {
	for _, restriction := range []string{"inherit", "clear_old_restrictions", "deny_all", "key_group", "key_exclude", "account_models", "account_exclude"} {
		t.Run(restriction, func(t *testing.T) {
			gateway, keys, repository, _ := catalogRereadFixture()
			reads := 0
			repository.accounts.assertScope = func(*int64, []string, bool) {
				reads++
				if reads == 1 {
					repository.accounts.generation++
				}
			}
			repository.snapshot = func(call int) (ModelAccessSnapshot, error) {
				snapshot := ModelAccessSnapshot{Generation: repository.accounts.generation, RevisionKnown: true}
				if call == 1 && restriction == "clear_old_restrictions" {
					snapshot.Key = []string{}
					snapshot.KeyGroup = GroupModelAllowlist{Enabled: true, Models: []string{}}
					snapshot.KeyExclude = []string{"public"}
					snapshot.Accounts = map[int64][]string{11: {}}
					snapshot.AccountExcludes = map[int64][]string{11: {"public"}}
				}
				if call > 1 {
					switch restriction {
					case "deny_all":
						snapshot.Key = []string{}
					case "key_group":
						snapshot.KeyGroup = GroupModelAllowlist{Enabled: true, Models: []string{}}
					case "key_exclude":
						snapshot.KeyExclude = []string{"public"}
					case "account_models":
						snapshot.Accounts = map[int64][]string{11: {}}
					case "account_exclude":
						snapshot.AccountExcludes = map[int64][]string{11: {"public"}}
					}
				}
				return snapshot, nil
			}
			ctx, err := keys.AttachModelAccessPolicy(context.Background(), 41)
			require.NoError(t, err)
			view, metadata, err := gateway.VisibleModelsWithMetadata(ctx, nil, PlatformOpenAI)
			require.NoError(t, err)
			require.Equal(t, 2, reads)
			require.Equal(t, []int64{41, 41}, repository.reads)
			if restriction == "inherit" || restriction == "clear_old_restrictions" {
				require.Equal(t, []string{"public"}, view.Models)
			} else {
				require.Empty(t, view.Models)
				require.Empty(t, view.Bindings)
				require.Empty(t, view.ExecutableEndpoints)
				require.Empty(t, metadata)
			}
		})
	}
}

func TestAuthenticatedCatalogAttachSingleBumpAndAuthorization(t *testing.T) {
	for _, scenario := range []string{"success", "revoked", "changed_request_group"} {
		t.Run(scenario, func(t *testing.T) {
			gateway, keys, repository, _ := catalogRereadFixture()
			repository.snapshot = func(call int) (ModelAccessSnapshot, error) {
				if call == 1 {
					return ModelAccessSnapshot{}, ErrVisibleModelRevisionChanged
				}
				if scenario == "revoked" {
					repository.keyError = ErrAPIKeyNotFound
				}
				if scenario == "changed_request_group" {
					group := Group{ID: 9, Status: StatusActive}
					repository.key.GroupID = &group.ID
					keys.groupRepo = &visiblePolicyGroupRepo{groups: []Group{group}}
				}
				return ModelAccessSnapshot{Generation: repository.accounts.generation, RevisionKnown: true}, nil
			}
			ctx, err := keys.AttachModelAccessPolicy(context.Background(), 41)
			require.Equal(t, []int64{41, 41}, repository.reads)
			if scenario == "revoked" {
				require.ErrorIs(t, err, ErrAPIKeyNotFound)
				return
			}
			require.NoError(t, err)
			view, _, err := gateway.VisibleModelsWithMetadata(ctx, nil, PlatformOpenAI)
			if scenario == "changed_request_group" {
				require.ErrorIs(t, err, ErrGroupNotAllowed)
				require.Empty(t, view.Models)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"public"}, view.Models)
			}
		})
	}
}

func TestAuthenticatedCatalogCanceledAndUnidentifiedAttachDoNotRetry(t *testing.T) {
	for _, mode := range []string{"already_canceled", "canceled_during_read", "no_key_id", "pre_frozen"} {
		t.Run(mode, func(t *testing.T) {
			_, keys, repository, _ := catalogRereadFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			keyID := int64(41)
			if mode == "already_canceled" {
				cancel()
			}
			if mode == "no_key_id" {
				keyID = 0
			}
			if mode == "pre_frozen" {
				ctx = context.WithValue(ctx, visibleModelRevisionKey{}, visibleModelRevision{generation: 7, authoritative: true})
			}
			repository.snapshot = func(int) (ModelAccessSnapshot, error) {
				if mode == "canceled_during_read" {
					cancel()
				}
				return ModelAccessSnapshot{}, ErrVisibleModelRevisionChanged
			}
			_, err := keys.AttachModelAccessPolicy(ctx, keyID)
			if mode == "already_canceled" {
				require.ErrorIs(t, err, context.Canceled)
				require.Empty(t, repository.reads)
			} else {
				require.Error(t, err)
				require.Len(t, repository.reads, 1)
			}
		})
	}
}

func TestAuthenticatedCatalogRereadRefreshesAccountMembershipAndAdmissionGroup(t *testing.T) {
	for _, change := range []string{"membership_removed", "group_deny_all"} {
		t.Run(change, func(t *testing.T) {
			gateway, keys, repository, _ := catalogRereadFixture()
			group := Group{ID: 9, Platform: PlatformOpenAI, Status: StatusActive}
			groups := &visiblePolicyGroupRepo{groups: []Group{group}}
			keys.groupRepo, gateway.groupRepo = groups, groups
			repository.key.GroupID = &group.ID
			repository.accounts.accounts[0].GroupIDs = []int64{9}
			reads := 0
			repository.accounts.assertScope = func(*int64, []string, bool) {
				reads++
				if reads == 1 {
					repository.accounts.generation++
					if change == "membership_removed" {
						repository.accounts.accounts[0].GroupIDs = []int64{10}
					} else {
						groups.groups[0].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{}}
					}
				}
			}
			ctx, err := keys.AttachModelAccessPolicy(context.WithValue(context.Background(), admissionGroupKey{}, &group), 41)
			require.NoError(t, err)
			view, metadata, err := gateway.VisibleModelsWithMetadata(ctx, &group.ID, PlatformOpenAI)
			require.NoError(t, err)
			require.Empty(t, view.Models)
			require.Empty(t, metadata)
			require.Equal(t, 2, reads)
		})
	}
}

func TestAuthenticatedCatalogRereadBoundsAndErrors(t *testing.T) {
	for _, scenario := range []string{"continuous", "snapshot_race", "repository_error", "canceled", "deadline", "pre_frozen"} {
		t.Run(scenario, func(t *testing.T) {
			gateway, keys, repository, _ := catalogRereadFixture()
			reads := 0
			repository.accounts.assertScope = func(*int64, []string, bool) { reads++; repository.accounts.generation++ }
			ctx, err := keys.AttachModelAccessPolicy(context.Background(), 41)
			require.NoError(t, err)
			wantError := ErrVisibleModelRevisionChanged
			wantSnapshots, wantReads := 3, 3
			if scenario == "pre_frozen" {
				ctx, err = (&VisibleModelResolver{gateway: gateway}).freeze(ctx)
				require.NoError(t, err)
				wantSnapshots, wantReads = 1, 1
			} else if scenario != "continuous" {
				wantReads = 1
				if scenario != "snapshot_race" {
					wantSnapshots = 2
				}
				switch scenario {
				case "repository_error":
					wantError = errors.New("policy read failed")
				case "canceled":
					wantError = context.Canceled
				case "deadline":
					wantError = context.DeadlineExceeded
				}
				repository.snapshot = func(int) (ModelAccessSnapshot, error) { return ModelAccessSnapshot{}, wantError }
			}
			view, metadata, err := gateway.VisibleModelsWithMetadata(ctx, nil, PlatformOpenAI)
			require.ErrorIs(t, err, wantError)
			require.Empty(t, view.Models)
			require.Nil(t, metadata)
			require.Len(t, repository.reads, wantSnapshots)
			require.Equal(t, wantReads, reads)
			for _, keyID := range repository.reads {
				require.EqualValues(t, 41, keyID)
			}
		})
	}
}

func TestAuthenticatedCatalogAttachRetriesOnlyRevisionChanges(t *testing.T) {
	for _, failure := range []error{ErrVisibleModelRevisionChanged, errors.New("read failed"), context.Canceled, context.DeadlineExceeded} {
		_, keys, repository, _ := catalogRereadFixture()
		repository.snapshot = func(int) (ModelAccessSnapshot, error) { return ModelAccessSnapshot{}, failure }
		_, err := keys.AttachModelAccessPolicy(context.Background(), 41)
		require.ErrorIs(t, err, failure)
		attempts := 1
		if errors.Is(failure, ErrVisibleModelRevisionChanged) {
			attempts = 3
		}
		require.Len(t, repository.reads, attempts)
	}
}

func TestAuthenticatedCatalogRereadRejectsChangedAuthorization(t *testing.T) {
	for _, scenario := range []string{"revoked", "disabled", "user_disabled", "owner_changed", "group_changed", "group_disabled", "group_denied", "revoked_during_retry"} {
		t.Run(scenario, func(t *testing.T) {
			gateway, keys, repository, users := catalogRereadFixture()
			group := Group{ID: 9, Status: StatusActive, Platform: PlatformOpenAI}
			groups := &visiblePolicyGroupRepo{groups: []Group{group}}
			keys.groupRepo, gateway.groupRepo = groups, groups
			repository.key.GroupID = &group.ID
			repository.accounts.accounts[0].GroupIDs = []int64{group.ID}
			reads := 0
			repository.accounts.assertScope = func(*int64, []string, bool) {
				reads++
				if reads == 1 {
					repository.accounts.generation++
				}
				if reads == 2 && scenario == "revoked_during_retry" {
					repository.keyError = ErrAPIKeyNotFound
				}
			}
			ctx, err := keys.AttachModelAccessPolicy(context.Background(), 41)
			require.NoError(t, err)
			ctx = context.WithValue(ctx, ctxkey.UserID, int64(21))
			repository.snapshot = func(int) (ModelAccessSnapshot, error) {
				switch scenario {
				case "revoked":
					repository.keyError = ErrAPIKeyNotFound
				case "disabled":
					repository.key.Status = StatusAPIKeyDisabled
				case "user_disabled":
					users.user.Status = StatusDisabled
				case "owner_changed":
					repository.key.UserID = 22
				case "group_changed":
					other := int64(10)
					repository.key.GroupID = &other
				case "group_disabled":
					groups.groups[0].Status = StatusDisabled
				case "group_denied":
					users.user.RestrictPublicGroups = true
				}
				return ModelAccessSnapshot{Generation: repository.accounts.generation, RevisionKnown: true}, nil
			}
			view, metadata, err := gateway.VisibleModelsWithMetadata(ctx, &group.ID, PlatformOpenAI)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrVisibleModelRevisionChanged, "authorization failures are not retried")
			require.Empty(t, view.Models)
			require.Nil(t, metadata)
			require.Equal(t, []int64{41, 41}, repository.reads)
		})
	}
}

type catalogMetadataRaceRepository struct {
	*visiblePolicyAccountRepo
	metadata map[string][]UpstreamModelMetadata
	bumped   bool
}

func (repository *catalogMetadataRaceRepository) ModelCatalogGeneration(context.Context, string) (int64, error) {
	// Commit after a complete first projection, rather than during account load.
	if !repository.bumped && len(repository.metadata["public"]) > 0 {
		repository.generation++
		repository.bumped = true
	}
	return repository.generation, nil
}

func TestAuthenticatedCatalogRereadDiscardsCompletedOldProjection(t *testing.T) {
	gateway, keys, repository, _ := catalogRereadFixture()
	metadata := make(map[string][]UpstreamModelMetadata)
	accounts := &catalogMetadataRaceRepository{visiblePolicyAccountRepo: repository.accounts, metadata: metadata}
	gateway.accountRepo = accounts
	repository.snapshot = func(call int) (ModelAccessSnapshot, error) {
		snapshot := ModelAccessSnapshot{Generation: accounts.generation, RevisionKnown: true}
		if call > 1 {
			snapshot.Key = []string{}
		}
		return snapshot, nil
	}
	ctx, err := keys.AttachModelAccessPolicy(context.Background(), 41)
	require.NoError(t, err)
	view, err := (&VisibleModelResolver{gateway: gateway}).resolve(ctx, nil, PlatformOpenAI, metadata)
	require.NoError(t, err)
	require.True(t, accounts.bumped, "first attempt must have produced metadata before the commit")
	require.Equal(t, []int64{41, 41}, repository.reads)
	require.Empty(t, view.Models)
	require.Empty(t, view.Bindings)
	require.Empty(t, view.ExecutableEndpoints)
	require.Empty(t, metadata)
}

func TestAuthenticatedCatalogAttachAndResolutionShareAttemptBound(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		gateway, keys, repository, _ := catalogRereadFixture()
		repository.snapshot = func(call int) (ModelAccessSnapshot, error) {
			if call == 1 {
				return ModelAccessSnapshot{}, ErrVisibleModelRevisionChanged
			}
			return ModelAccessSnapshot{Generation: repository.accounts.generation, RevisionKnown: true}, nil
		}
		reads := 0
		repository.accounts.assertScope = func(*int64, []string, bool) {
			reads++
			if reads == 1 || continuous {
				repository.accounts.generation++
			}
		}
		ctx, err := keys.AttachModelAccessPolicy(context.Background(), 41)
		require.NoError(t, err)
		view, metadata, err := gateway.VisibleModelsWithMetadata(ctx, nil, PlatformOpenAI)
		require.Equal(t, []int64{41, 41, 41}, repository.reads)
		require.Equal(t, 2, reads)
		if continuous {
			require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
			require.Empty(t, view.Models)
			require.Nil(t, metadata)
		} else {
			require.NoError(t, err)
			require.Equal(t, []string{"public"}, view.Models)
		}
	}
}
