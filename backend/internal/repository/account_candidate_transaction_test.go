package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepository_CandidatePageUsesContextTransaction(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	ctx := dbent.NewTxContext(context.Background(), tx)
	// A missing base executor must not prevent a caller-owned transaction read.
	repo := newAccountRepositoryWithSQL(client, nil, nil)
	mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	page, err := repo.ListOAuthRefreshCandidatePage(ctx, service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, Limit: 1})
	mock.ExpectRollback()
	rollbackErr := tx.Rollback()
	require.NoError(t, err)
	require.NoError(t, rollbackErr)
	require.Empty(t, page.Accounts)
	require.NoError(t, mock.ExpectationsWereMet())
}
