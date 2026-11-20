//go:build unit

package service

import (
	"context"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

type createGroupUserRepo struct {
	UserRepository
	role string
}

func (r *createGroupUserRepo) GetByID(context.Context, int64) (*User, error) {
	return &User{ID: 1, Role: r.role, Status: StatusActive}, nil
}

type createGroupRepo struct{ GroupRepository }

func (r *createGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	return &Group{ID: 7, Status: StatusActive}, nil
}

type createGroupKeyRepo struct {
	APIKeyRepository
	saved *APIKey
}

func (r *createGroupKeyRepo) Create(_ context.Context, key *APIKey) error {
	key.ID = 1
	r.saved = key
	return nil
}

func TestAPIKeyCreateRequiresGroupForUsersAndAdmins(t *testing.T) {
	for _, role := range []string{RoleUser, RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			repo := &createGroupKeyRepo{}
			svc := &APIKeyService{apiKeyRepo: repo, userRepo: &createGroupUserRepo{role: role}, groupRepo: &createGroupRepo{}, cfg: &config.Config{}}
			for _, groupID := range []*int64{nil, int64Ptr(0), int64Ptr(-1)} {
				key, err := svc.Create(context.Background(), 1, CreateAPIKeyRequest{Name: "test", GroupID: groupID})
				require.ErrorIs(t, err, ErrAPIKeyGroupRequired)
				require.EqualValues(t, 400, ErrAPIKeyGroupRequired.Code)
				require.Nil(t, key)
				require.Nil(t, repo.saved)
			}
			key, err := svc.Create(context.Background(), 1, CreateAPIKeyRequest{Name: "test", GroupID: int64Ptr(7)})
			require.NoError(t, err)
			require.Same(t, repo.saved, key)
			require.Equal(t, int64(7), *key.GroupID)
		})
	}
}
