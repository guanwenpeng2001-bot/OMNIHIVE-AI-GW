package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type keyCreateUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *keyCreateUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	if r.err != nil {
		return nil, r.err
	}
	if id != r.user.ID {
		return nil, ErrUserNotFound
	}
	return r.user, nil
}

type keyCreateGroupRepo struct {
	GroupRepository
	group *Group
	err   error
}

func (r *keyCreateGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.err != nil {
		return nil, r.err
	}
	if id != r.group.ID {
		return nil, ErrGroupNotFound
	}
	return r.group, nil
}

type keyCreateSubRepo struct {
	UserSubscriptionRepository
	sub   *UserSubscription
	calls int
}

func (r *keyCreateSubRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	r.calls++
	if r.sub == nil || r.sub.UserID != userID || r.sub.GroupID != groupID || !r.sub.IsActive() {
		return nil, ErrSubscriptionNotFound
	}
	return r.sub, nil
}

type keyCreateRepo struct {
	APIKeyRepository
	saved       *APIKey
	workspaceID int64
	err         error
}

func (r *keyCreateRepo) Create(ctx context.Context, key *APIKey) error {
	if r.err != nil {
		return r.err
	}
	r.workspaceID = domain.ResourceWorkspaceID(ctx)
	key.ID = 123
	r.saved = key
	return nil
}

type projectKeyCreateRepo struct {
	keyCreateRepo
	project, group, user int64
	projectWorkspace     int64
}

func (r *projectKeyCreateRepo) ResolveKeyWorkspace(_ context.Context, user, project, group int64) (int64, error) {
	if project != r.project || group != r.group || user != r.user {
		return 0, ErrWorkspaceNotFound
	}
	return r.projectWorkspace, nil
}

func (r *projectKeyCreateRepo) CreateInProject(ctx context.Context, key *APIKey, project int64) error {
	if project != r.project || domain.ResourceWorkspaceID(ctx) != r.projectWorkspace {
		return ErrWorkspaceNotFound
	}
	return r.Create(ctx, key)
}

func TestAPIKeyCreateInSelectedProject(t *testing.T) {
	user := &User{ID: 7, Status: StatusActive}
	group := &Group{ID: 3, Status: StatusActive}
	repo := &projectKeyCreateRepo{project: 19, user: user.ID, group: group.ID, projectWorkspace: 9}
	svc := NewAPIKeyService(repo, &keyCreateUserRepo{user: user}, &keyCreateGroupRepo{group: group},
		nil, nil, nil, &config.Config{})
	ctx := domain.WithWorkspaceScope(t.Context(), domain.DefaultWorkspaceID)
	req := CreateAPIKeyRequest{Name: "scoped", GroupID: &group.ID, ProjectID: &repo.project}
	req.WorkspaceID = 4
	_, err := svc.Create(ctx, user.ID, req)
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
	require.Nil(t, repo.saved)
	req.WorkspaceID = 0
	unknown := int64(300)
	req.ProjectID = &unknown
	_, err = svc.Create(ctx, user.ID, req)
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
	require.Nil(t, repo.saved)
	req.ProjectID = &repo.project
	issued, err := svc.Create(ctx, user.ID, req)
	require.NoError(t, err)
	require.Equal(t, repo.projectWorkspace, repo.workspaceID)
	require.Equal(t, group.ID, *issued.GroupID)
}

func TestAPIKeyCreateAuthorization(t *testing.T) {
	storageErr := errors.New("credential transaction failed")
	for _, tc := range []struct {
		name                                         string
		userErr                                      error
		groupErr                                     error
		status                                       string
		groupStatus                                  string
		exclusive, restricted, granted, subscription bool
		subStatus                                    string
		expired                                      bool
		createErr, wantErr                           error
	}{
		{name: "public group, zero balance"},
		{name: "target missing", userErr: ErrUserNotFound, wantErr: ErrUserNotFound},
		{name: "target disabled", status: StatusDisabled, wantErr: ErrUserNotActive},
		{name: "target inactive", status: "inactive", wantErr: ErrUserNotActive},
		{name: "group missing", groupErr: ErrGroupNotFound, wantErr: ErrGroupNotFound},
		{name: "group disabled", groupStatus: StatusDisabled, wantErr: ErrAPIKeyGroupNotActive},
		{name: "group inactive", groupStatus: "inactive", wantErr: ErrAPIKeyGroupNotActive},
		{name: "exclusive not granted", exclusive: true, wantErr: ErrGroupNotAllowed},
		{name: "exclusive granted", exclusive: true, granted: true},
		{name: "public restricted", restricted: true, wantErr: ErrGroupNotAllowed},
		{name: "public restricted granted", restricted: true, granted: true},
		{name: "subscription missing despite grant", subscription: true, granted: true, wantErr: ErrGroupNotAllowed},
		{name: "subscription expired", subscription: true, subStatus: SubscriptionStatusActive, expired: true, wantErr: ErrGroupNotAllowed},
		{name: "subscription inactive", subscription: true, subStatus: "inactive", wantErr: ErrGroupNotAllowed},
		{name: "subscription active", subscription: true, subStatus: SubscriptionStatusActive},
		{name: "transaction failure", createErr: storageErr, wantErr: storageErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &User{ID: 42, Role: RoleUser, Status: StatusActive, RestrictPublicGroups: tc.restricted}
			if tc.status != "" {
				user.Status = tc.status
			}
			if tc.granted {
				user.AllowedGroups = []int64{7}
			}
			group := &Group{ID: 7, Status: StatusActive, IsExclusive: tc.exclusive}
			if tc.groupStatus != "" {
				group.Status = tc.groupStatus
			}
			if tc.subscription {
				group.SubscriptionType = "subscription"
			}
			subs := &keyCreateSubRepo{}
			if tc.subStatus != "" {
				expires := time.Now().Add(time.Hour)
				if tc.expired {
					expires = time.Now().Add(-time.Hour)
				}
				subs.sub = &UserSubscription{UserID: user.ID, GroupID: group.ID, Status: tc.subStatus, ExpiresAt: expires}
			}
			repo := &keyCreateRepo{err: tc.createErr}
			svc := NewAPIKeyService(repo, &keyCreateUserRepo{user: user, err: tc.userErr},
				&keyCreateGroupRepo{group: group, err: tc.groupErr}, subs, nil, nil, &config.Config{})
			ctx := domain.WithWorkspaceScope(context.Background(), 2)
			key, err := svc.Create(ctx, user.ID, CreateAPIKeyRequest{Name: "cumora · Test", GroupID: &group.ID})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, key, "failed creation must not expose a secret")
				require.Nil(t, repo.saved)
				return
			}
			require.NoError(t, err)
			require.Same(t, repo.saved, key)
			require.Equal(t, user.ID, key.UserID)
			require.Equal(t, group.ID, *key.GroupID)
			require.Equal(t, int64(2), repo.workspaceID, "creation must preserve the authorized workspace context")
			require.Equal(t, StatusActive, key.Status)
			require.Regexp(t, `^sk-[0-9a-f]{64}$`, key.Key)
			require.Equal(t, APIKeyCredentialDisplaySuffix(key.Key), key.DisplaySuffix)
			require.Zero(t, user.Balance, "minting a key must not grant spending credit")
			require.Zero(t, key.QuotaUsed)
			if tc.subscription {
				require.Equal(t, 1, subs.calls)
			}
		})
	}
}
