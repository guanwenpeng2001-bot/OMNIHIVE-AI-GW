//go:build integration

package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestManagedAccountGroupsRemainEditable(t *testing.T) {
	database, client, accounts := testutil.NewCapabilityPostgres(t)
	ctx := context.Background()
	var workspaceID int64
	require.NoError(t, database.QueryRowContext(ctx, "INSERT INTO workspaces(name) VALUES('managed group test') RETURNING id").Scan(&workspaceID))
	var groupID int64
	require.NoError(t, database.QueryRowContext(ctx, "INSERT INTO groups(workspace_id,name,platform) VALUES($1,'managed-route','openai') RETURNING id", workspaceID).Scan(&groupID))
	account, err := client.Account.Create().SetWorkspaceID(workspaceID).SetName("wizard-managed").SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeAPIKey).SetStatus(service.StatusActive).SetSchedulable(true).SetCredentials(map[string]any{"base_url": "https://managed.example"}).Save(ctx)
	require.NoError(t, err)
	connectionID := "00000000-0000-0000-0000-000000000001"
	_, err = database.ExecContext(ctx, "INSERT INTO provider_connections(account_id,workspace_id,provider,auth_type,name,endpoint,concurrency,state,verified_id) VALUES($1,$2,'openai','api_key','wizard-managed','https://managed.example',1,'active',$3)", account.ID, workspaceID, connectionID)
	require.NoError(t, err)
	groups := repository.NewAdminGroupRepository(client, database)
	admin := service.NewAdminService(&config.Config{}, nil, nil, groups, accounts.(service.AdminAccountRepository), nil, nil, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	for _, ids := range [][]int64{{groupID}, {}} {
		updated, err := admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{GroupIDs: &ids})
		require.NoError(t, err)
		require.ElementsMatch(t, ids, updated.GroupIDs)
		var version int
		var verified sql.NullString
		require.NoError(t, database.QueryRowContext(ctx, "SELECT version,verified_id FROM provider_connections WHERE account_id=$1", account.ID).Scan(&version, &verified))
		require.Equal(t, 1, version)
		require.Equal(t, connectionID, verified.String)
	}
	ids := []int64{groupID}
	_, err = admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{Name: "legacy-rename", GroupIDs: &ids})
	require.ErrorIs(t, err, service.ErrAccountConnectionManaged)
	require.Zero(t, countAccountGroups(t, ctx, database, account.ID))
	var name string
	require.NoError(t, database.QueryRowContext(ctx, "SELECT name FROM accounts WHERE id=$1", account.ID).Scan(&name))
	require.Equal(t, "wizard-managed", name)
}

func countAccountGroups(t *testing.T, ctx context.Context, database *sql.DB, accountID int64) int {
	t.Helper()
	var count int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM account_groups WHERE account_id=$1", accountID).Scan(&count))
	return count
}
