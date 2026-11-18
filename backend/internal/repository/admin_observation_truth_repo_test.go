//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// 分组统计按传入 group_id 聚合，并回传原始账本保留窗口起点。
func TestUsageLogRepositoryGetGroupUsageTotalsScopesGroupAndWindow(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	windowStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`LEFT JOIN usage_logs`).
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "total_cost", "actual_cost", "window_start"}).
			AddRow(int64(11), 1.25, 1.0, windowStart))

	totals, err := repo.GetGroupUsageTotals(context.Background(), 2)

	require.NoError(t, err)
	require.Equal(t, int64(11), totals.Requests)
	require.InDelta(t, 1.25, totals.TotalCost, 0.000001)
	require.InDelta(t, 1.0, totals.ActualCost, 0.000001)
	require.Equal(t, windowStart, totals.WindowStart.UTC())
	require.False(t, totals.WindowEnd.IsZero())
	require.NoError(t, mock.ExpectationsWereMet())
}

// 窗口覆盖不住账本时两个指标为 NULL（未知），而不是 0。
func TestUsageLogRepositoryGetRealtimeWindowStatsKeepsUnknownNull(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)

	mock.ExpectQuery(`AVG\(ul.duration_ms\)`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "avg_duration_ms", "window_start"}).
			AddRow(nil, nil, end))

	stats, err := repo.GetRealtimeWindowStats(context.Background(), start, end)

	require.NoError(t, err)
	require.Nil(t, stats.Requests)
	require.Nil(t, stats.AverageResponseTimeMs)
	require.False(t, stats.CoverageComplete)
	require.Equal(t, start, stats.WindowStart)
	require.Equal(t, end, stats.WindowEnd)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 窗口被账本覆盖时返回真实观测（0 请求是真实观测，不是未知）。
func TestUsageLogRepositoryGetRealtimeWindowStatsReturnsObservation(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	retainedFrom := start.Add(-48 * time.Hour)

	mock.ExpectQuery(`AVG\(ul.duration_ms\)`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "avg_duration_ms", "window_start"}).
			AddRow(int64(0), nil, retainedFrom))

	stats, err := repo.GetRealtimeWindowStats(context.Background(), start, end)

	require.NoError(t, err)
	require.True(t, stats.CoverageComplete)
	require.NotNil(t, stats.Requests)
	require.Equal(t, int64(0), *stats.Requests)
	// 窗口内没有耗时样本：保持未知，不用 0 冒充
	require.Nil(t, stats.AverageResponseTimeMs)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 代理统计按代理归属账号聚合，并区分启用中的账号。
func TestProxyRepositoryCountsAccountsByStatusAndUsage(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newProxyRepositoryWithSQL(nil, db)
	windowStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	lastRequest := time.Date(2026, 9, 21, 11, 59, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM accounts WHERE proxy_id = \$1 AND deleted_at IS NULL AND status = \$2`).
		WithArgs(int64(4), service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))

	active, err := repo.CountAccountsByProxyIDAndStatus(context.Background(), 4, service.StatusActive)

	require.NoError(t, err)
	require.Equal(t, int64(3), active)

	mock.ExpectQuery(`MAX\(ul.created_at\)`).
		WithArgs(int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "last_request_at", "window_start"}).
			AddRow(int64(128), lastRequest, windowStart))

	totals, err := repo.GetProxyUsageTotals(context.Background(), 4)

	require.NoError(t, err)
	require.Equal(t, int64(128), totals.Requests)
	require.NotNil(t, totals.LastRequestAt)
	require.Equal(t, lastRequest.UTC(), totals.LastRequestAt.UTC())
	require.Equal(t, windowStart, totals.WindowStart.UTC())
	require.False(t, totals.WindowEnd.IsZero())
	require.NoError(t, mock.ExpectationsWereMet())
}

// 代理从未服务过请求时 last_request_at 为 NULL。
func TestProxyRepositoryUsageTotalsKeepsMissingLastRequestNull(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newProxyRepositoryWithSQL(nil, db)
	windowStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`MAX\(ul.created_at\)`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "last_request_at", "window_start"}).
			AddRow(int64(0), nil, windowStart))

	totals, err := repo.GetProxyUsageTotals(context.Background(), 9)

	require.NoError(t, err)
	require.Equal(t, int64(0), totals.Requests)
	require.Nil(t, totals.LastRequestAt)
	require.NoError(t, mock.ExpectationsWereMet())
}
