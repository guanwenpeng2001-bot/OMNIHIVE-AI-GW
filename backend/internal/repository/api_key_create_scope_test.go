package repository

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/apikeycredential"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Exercise the real create service, workspace query hooks and transactional
// credential repository against a disposable in-memory database, with no DSN.
func TestAPIKeyCreateScopedPersistence_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	workspaceID, err := domain.ResolveDefaultWorkspaceID(t.Context(), client)
	require.NoError(t, err)
	ctx := domain.WithWorkspaceScope(t.Context(), workspaceID)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "cumora-create@example.test")
	group, err := client.Group.Create().SetName("provisioning-default").Save(ctx)
	require.NoError(t, err)
	foreignGroup, err := client.Group.Create().SetName("provisioning-foreign").Save(domain.WithWorkspaceScope(ctx, 2))
	require.NoError(t, err)
	svc := service.NewAPIKeyService(repo, NewUserRepository(client, nil), newGroupRepositoryWithSQL(client, repo.sql), nil, nil, nil, &config.Config{})

	key, err := svc.Create(ctx, user.ID, service.CreateAPIKeyRequest{Name: "foreign", GroupID: &foreignGroup.ID})
	require.ErrorIs(t, err, service.ErrGroupNotFound, "a global admin provisioning URL cannot bind a foreign workspace group")
	require.Nil(t, key)
	require.Zero(t, client.APIKey.Query().CountX(ctx))
	require.Zero(t, client.APIKeyCredential.Query().CountX(ctx))

	key, err = svc.Create(ctx, user.ID, service.CreateAPIKeyRequest{Name: "cumora · Alice", GroupID: &group.ID})
	require.NoError(t, err)
	require.NotEmpty(t, key.Key)
	stored := client.APIKey.GetX(ctx, key.ID)
	require.Equal(t, user.ID, stored.UserID)
	require.Equal(t, workspaceID, stored.WorkspaceID)
	require.Equal(t, group.ID, *stored.GroupID)
	require.Empty(t, stored.Key, "the key identity row must not persist plaintext")
	credential := client.APIKeyCredential.Query().Where(apikeycredential.APIKeyIDEQ(key.ID)).OnlyX(ctx)
	require.Equal(t, service.APIKeyCredentialDigest(key.Key), credential.CredentialDigest)
	require.Equal(t, service.APIKeyCredentialDisplaySuffix(key.Key), credential.DisplaySuffix)
	require.Equal(t, workspaceID, credential.WorkspaceID)
	require.Equal(t, domain.CredentialStatusCurrent, credential.Status)

	resolved, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, key.ID, resolved.ID)
	require.Equal(t, user.ID, resolved.User.ID)
	require.Empty(t, resolved.Key)
	listed, _, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].Key)
	detail, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Empty(t, detail.Key)

	// A credential-insert failure must roll back the identity insert as well.
	duplicate := &service.APIKey{UserID: user.ID, GroupID: &group.ID, Name: "duplicate", Status: service.StatusActive, Key: key.Key}
	require.ErrorIs(t, repo.Create(ctx, duplicate), service.ErrAPIKeyExists)
	require.Zero(t, duplicate.ID)
	require.Equal(t, 1, client.APIKey.Query().CountX(ctx))
	require.Equal(t, 1, client.APIKeyCredential.Query().CountX(ctx))
}
