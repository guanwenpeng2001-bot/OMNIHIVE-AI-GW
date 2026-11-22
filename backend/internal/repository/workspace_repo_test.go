package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceContainsResourcesDeduplicatesAndBoundsIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewWorkspaceRepository(db)

	ok, err := repo.ContainsResources(t.Context(), 3, "accounts", make([]int64, 1001))
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.ContainsResources(t.Context(), 3, "accounts", nil)
	require.NoError(t, err)
	require.True(t, ok)

	mock.ExpectQuery("FROM accounts").WithArgs(int64(3), "{2,5}").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	ok, err = repo.ContainsResources(t.Context(), 3, "accounts", []int64{2, 5, 2, 5})
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}
