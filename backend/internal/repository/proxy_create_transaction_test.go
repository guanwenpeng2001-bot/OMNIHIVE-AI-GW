package repository

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/enttest"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestProxyCreateTransactionOwnership(t *testing.T) {
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Workspace.Create().SetName("Default").SetIsDefault(true).Exec(t.Context()))
	repository := newProxyRepositoryWithSQL(client, database)
	ctx := context.Background()
	transaction, err := client.Tx(ctx)
	require.NoError(t, err)
	candidate := &service.Proxy{Name: "batch-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8081, Status: service.StatusActive, FallbackMode: service.FallbackModeNone}
	require.NoError(t, repository.Create(dbent.NewTxContext(ctx, transaction), candidate))
	visible, err := repository.GetByID(dbent.NewTxContext(ctx, transaction), candidate.ID)
	require.NoError(t, err)
	require.Equal(t, candidate.Name, visible.Name, "import status updates must see their newly created proxy")
	require.Equal(t, 1, transaction.Proxy.Query().CountX(ctx))
	require.NoError(t, transaction.Rollback())
	require.Zero(t, client.Proxy.Query().CountX(ctx), "a failed account import must not leave an orphan proxy")
	candidate.ID = 0
	require.NoError(t, repository.Create(ctx, candidate))
	require.Positive(t, candidate.ID)
	visible, err = repository.GetByID(ctx, candidate.ID)
	require.NoError(t, err)
	require.Equal(t, candidate.Name, visible.Name)
	require.Equal(t, 1, client.Proxy.Query().CountX(ctx), "ordinary Create retains its existing persistence behavior")
}
