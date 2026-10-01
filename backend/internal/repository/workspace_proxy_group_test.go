//go:build unit

package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"regexp"
	"testing"
)

func TestProxyGroupWorkspaceResourceAllowlist(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repository := NewWorkspaceRepository(db)
	query := regexp.QuoteMeta("SELECT count(DISTINCT id) FROM proxy_groups WHERE workspace_id=$1 AND id=ANY($2)")
	mock.ExpectQuery(query).WithArgs(int64(2), pq.Array([]int64{201, 202})).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	contained, err := repository.ContainsResources(context.Background(), 2, "proxy_groups", []int64{201, 202})
	require.NoError(t, err)
	require.True(t, contained)
	mock.ExpectQuery(query).WithArgs(int64(2), pq.Array([]int64{201, 101})).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	contained, err = repository.ContainsResources(context.Background(), 2, "proxy_groups", []int64{201, 101})
	require.NoError(t, err)
	require.False(t, contained)
	require.NoError(t, mock.ExpectationsWereMet())
}
