package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type workspaceSchedulerCache struct {
	SchedulerCache
	accounts []*Account
}

func (s *workspaceSchedulerCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return s.accounts, true, nil
}

func (s *workspaceSchedulerCache) GetAccount(_ context.Context, identifier int64) (*Account, error) {
	for _, account := range s.accounts {
		if account.ID == identifier {
			return account, nil
		}
	}
	return nil, nil
}

func TestWorkspaceSchedulerCacheCannotCrossUngroupedDomains(t *testing.T) {
	cache := &workspaceSchedulerCache{accounts: []*Account{{ID: 101, WorkspaceID: 1}, {ID: 201, WorkspaceID: 2}, {ID: 102}}}
	scheduler := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	for _, workspaceID := range []int64{1, 2} {
		ctx := domain.WithWorkspaceScope(context.Background(), workspaceID)
		accounts, _, err := scheduler.ListSchedulableAccounts(ctx, nil, PlatformOpenAI, false)
		require.NoError(t, err)
		if workspaceID == 1 {
			require.Len(t, accounts, 2)
		} else {
			require.Len(t, accounts, 1)
			require.Equal(t, int64(201), accounts[0].ID)
		}
		foreignID := int64(201)
		if workspaceID == 2 {
			foreignID = 101
		}
		_, err = scheduler.GetAccount(ctx, foreignID)
		require.ErrorIs(t, err, ErrAccountNotFound)
	}
	require.Len(t, cache.accounts, 3, "scope filtering cannot mutate the shared snapshot")
}

func TestWorkspaceCatalogCacheSeparatesSameUngroupedPlatform(t *testing.T) {
	repository := &catalogGenerationFixture{generation: 1, accounts: []Account{catalogFixtureAccount("default-model")}}
	gateway := catalogFixtureGateway(repository)
	defaultScope := domain.WithWorkspaceScope(context.Background(), 1)
	delegatedScope := domain.WithWorkspaceScope(context.Background(), 2)
	models, err := gateway.GetAvailableModelsChecked(defaultScope, nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, []string{"default-model"}, models)
	repository.accounts = []Account{catalogFixtureAccount("delegated-model")}
	models, err = gateway.GetAvailableModelsChecked(delegatedScope, nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, []string{"delegated-model"}, models)
	models, err = gateway.GetAvailableModelsChecked(defaultScope, nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, []string{"default-model"}, models)
}

func TestWorkspaceOwnershipSurvivesAPIKeyAuthSnapshot(t *testing.T) {
	keys := &APIKeyService{}
	key := &APIKey{ID: 7, UserID: 42, WorkspaceID: 2, User: &User{ID: 42}}
	snapshot := keys.snapshotFromAPIKey(context.Background(), key)
	require.NotNil(t, snapshot)
	restored := keys.snapshotToAPIKey("credential", snapshot)
	require.Equal(t, int64(2), restored.WorkspaceID)
}

func TestWorkspaceIdempotencyReplayNeverReturnsAnotherDomain(t *testing.T) {
	coordinator := NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), DefaultIdempotencyConfig())
	created := 0
	for _, workspaceID := range []int64{2, 3, 2} {
		ctx := domain.WithWorkspaceScope(context.Background(), workspaceID)
		result, err := coordinator.Execute(ctx, IdempotencyExecuteOptions{
			Scope:      domain.WorkspaceActorScope(ctx, "admin.accounts.create"),
			ActorScope: domain.WorkspaceActorScope(ctx, "admin:42"),
			Method:     "POST", Route: "/workspaces/:workspace_id/accounts", IdempotencyKey: "same-request", Payload: map[string]any{"name": "account"},
		}, func(context.Context) (any, error) { created++; return map[string]any{"workspace_id": workspaceID}, nil })
		require.NoError(t, err)
		if created == 2 && workspaceID == 2 {
			require.True(t, result.Replayed)
		} else {
			require.False(t, result.Replayed)
		}
	}
	require.Equal(t, 2, created)
}
