//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestDashboardPublicationRollsBackWithoutPublishingOnRebuildFailure(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(95 * time.Minute)
	repo.clock = func() time.Time { return end }
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_retained_from.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"raw", "hourly", "daily", "published", "dirty", "zone", "version", "origin"}).AddRow(time.Unix(0, 0), start, start, start, nil, timezone.Name(), 2, nil))
	mock.ExpectExec(`UPDATE usage_dashboard_aggregation_watermark SET timezone_name`).WillReturnResult(sqlmock.NewResult(0, 1))
	expectDashboardRebuildCoverage(mock)
	// The open hour must remain raw, and no watermark can escape a failed rebuild.
	mock.ExpectExec(`DELETE FROM usage_dashboard_hourly WHERE`).WithArgs(bucketFloor(start, "day").UTC(), start.Add(time.Hour).In(timezone.Location())).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.AggregateRangeAndPublish(context.Background(), start, end), sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}
