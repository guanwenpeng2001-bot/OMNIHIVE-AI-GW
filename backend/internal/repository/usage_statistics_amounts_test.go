//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// Coverage must preserve the baseline account-filter monetary contract.
func TestUsageStatisticsAccountFilterPreservesBaselineAmounts(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	mock.ExpectQuery(`SUM\(COALESCE\(account_stats_cost, total_cost\).*as actual_cost`).
		WithArgs(start, end, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input", "output", "creation", "read", "tokens", "cost", "actual_cost", "account_cost"}).
			AddRow("model", 1, 100, 10, 0, 0, 110, 10.0, 3.0, 3.0))
	models, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 7, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, 3.0, models[0].ActualCost)
	require.Equal(t, 3.0, models[0].AccountCost)
	mock.ExpectQuery(`SUM\(COALESCE\(account_stats_cost, total_cost\).*as actual_cost`).
		WithArgs(start, end, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "tokens", "cost", "actual_cost"}).AddRow("/v1/messages", 1, 110, 10.0, 3.0))
	endpoints, err := repo.getEndpointStatsByColumnWithFilters(context.Background(), "inbound_endpoint", start, end, 0, 0, 7, 0, "", "", nil, nil, nil, "")
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, 3.0, endpoints[0].ActualCost)
	require.NoError(t, mock.ExpectationsWereMet())
}
