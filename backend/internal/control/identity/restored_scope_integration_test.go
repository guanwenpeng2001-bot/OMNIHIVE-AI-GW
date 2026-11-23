//go:build integration

package identity_test

import (
	"testing"

	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestRestoredDefaultResourceOwnership(t *testing.T) {
	db, client, _ := testutil.NewCapabilityPostgres(t)
	tx, err := client.Tx(t.Context())
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := dbent.NewTxContext(t.Context(), tx)
	c := tx.Client()
	_, err = c.ExecContext(ctx, "UPDATE workspaces SET is_default=false WHERE is_default")
	require.NoError(t, err)
	const restoredID int64 = 17
	_, err = c.ExecContext(ctx, "INSERT INTO workspaces(id,name,is_default) VALUES($1,'Restored',true) ON CONFLICT(id) DO UPDATE SET is_default=true", restoredID)
	require.NoError(t, err)
	found, err := domain.ResolveDefaultWorkspaceID(ctx, c)
	require.NoError(t, err)
	require.Equal(t, restoredID, found)
	user := &service.User{Email: uuid.NewString() + "@restored.test", PasswordHash: "fixture", Role: "user", Status: "active"}
	require.NoError(t, repository.NewUserRepository(client, db).Create(ctx, user))
	id := func(query string, args ...any) int64 {
		t.Helper()
		rows, err := c.QueryContext(ctx, query, args...)
		require.NoError(t, err)
		defer rows.Close()
		require.True(t, rows.Next())
		var v int64
		require.NoError(t, rows.Scan(&v))
		return v
	}
	require.Equal(t, restoredID, id("SELECT workspace_id FROM workspace_members WHERE user_id=$1", user.ID))
	require.Zero(t, id("SELECT count(*) FROM project_grants WHERE user_id=$1", user.ID))
	proxy, err := c.Proxy.Create().SetName("ent").SetProtocol("http").SetHost("localhost").SetPort(8080).Save(ctx)
	require.NoError(t, err)
	require.Equal(t, restoredID, proxy.WorkspaceID)
	require.Equal(t, restoredID, id("INSERT INTO proxies(name,protocol,host,port) VALUES('raw','http','localhost',8080) RETURNING workspace_id"))
	proxy, err = c.Proxy.Create().SetName("explicit").SetProtocol("http").SetHost("localhost").SetPort(8080).Save(domain.WithWorkspaceScope(ctx, 1))
	require.NoError(t, err)
	require.EqualValues(t, 1, proxy.WorkspaceID)
}
