package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestUsageSnapshotRequiresDatabasePool(t *testing.T) {
	for _, repo := range []*usageLogRepository{{}, {sql: (*sql.DB)(nil)}} {
		_, err := usageReadSnapshot(context.Background(), repo, func(*usageLogRepository, dashboardCoverage) (int, error) {
			t.Fatal("snapshot callback must not run without a database pool")
			return 0, nil
		})
		require.ErrorContains(t, err, "requires a database connection pool")
	}
}

func TestUsageCoverageDoesNotTreatRetainedHourlyGapsAsZero(t *testing.T) {
	previous := timezone.Name()
	require.NoError(t, timezone.Init("UTC"))
	t.Cleanup(func() { require.NoError(t, timezone.Init(previous)) })
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	next := day.AddDate(0, 0, 1)
	c := dashboardCoverage{raw: next, published: next, hourly: sql.NullTime{Time: next, Valid: true}, daily: sql.NullTime{Time: day, Valid: true}, zone: "UTC", version: 2}
	require.NoError(t, c.requireTrend(day, next, "day"), "verified days survive hourly and raw retention")
	err := c.requireTrend(day, next, "hour")
	require.ErrorIs(t, err, ErrUsageHistoryUnavailable)
	code, status := infraerrors.ToHTTP(err)
	require.Equal(t, 409, code)
	require.Equal(t, "USAGE_HISTORY_INCOMPLETE", status.Reason)
	require.Equal(t, next.Format(time.RFC3339Nano), status.Metadata["raw_retained_from"])
	require.ErrorIs(t, c.requireTrend(day.Add(time.Minute), next, "day"), ErrUsageHistoryUnavailable, "partial boundaries require raw records")
	c.dirty = sql.NullTime{Time: day.Add(time.Hour), Valid: true}
	require.ErrorIs(t, c.requireTrend(day, next, "day"), ErrUsageHistoryUnavailable, "a receipt older than raw retention cannot repair the rest of its missing day")
	c.dirty = sql.NullTime{}
	c.zone = "America/New_York"
	require.ErrorIs(t, c.requireTrend(day, next, "day"), ErrUsageHistoryUnavailable, "old calendar buckets cannot be relabelled")
	c.zone = "UTC"
	c.version = 1
	require.ErrorIs(t, c.requireTrend(day, next, "day"), ErrUsageHistoryUnavailable, "old projection definitions cannot be trusted")
}

func TestUsageCoverageDirtyBoundaryUsesCalendarBeforeHourRounding(t *testing.T) {
	previous := timezone.Name()
	require.NoError(t, timezone.Init("Asia/Kolkata"))
	t.Cleanup(func() { require.NoError(t, timezone.Init(previous)) })
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, timezone.Location())
	next := day.AddDate(0, 0, 1)
	c := dashboardCoverage{raw: next, published: next.AddDate(0, 0, 1), daily: sql.NullTime{Time: day, Valid: true}, dirty: sql.NullTime{Time: next.Add(10 * time.Minute), Valid: true}, zone: "Asia/Kolkata", version: 2}
	require.NoError(t, c.requireTrend(day, next, "day"), "a dirty event after local midnight must not invalidate the clean preceding day in a half-hour timezone")
}
