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

func TestRetainedSummaryCoverage(test *testing.T) {
	previous := timezone.Name()
	require.NoError(test, timezone.Init("Asia/Shanghai"))
	test.Cleanup(func() { require.NoError(test, timezone.Init(previous)) })
	floor := time.Date(2026, 9, 12, 0, 0, 0, 0, timezone.Location())
	now := floor.AddDate(0, 0, 20).Add(13 * time.Hour)
	base := dashboardCoverage{raw: floor, daily: sql.NullTime{Time: floor, Valid: true}, published: now.Add(-time.Hour), zone: timezone.Name(), version: 2}
	for _, scenario := range []struct {
		name   string
		change func(*dashboardCoverage)
		from   time.Time
		valid  bool
	}{
		{"unknown origin", func(*dashboardCoverage) {}, floor, true},
		{"archive earlier", func(coverage *dashboardCoverage) { coverage.raw = floor.AddDate(0, 0, 2) }, floor, true},
		{"raw earlier", func(coverage *dashboardCoverage) { coverage.daily.Time = floor.AddDate(0, 0, 2) }, floor, true},
		{"dirty gap", func(coverage *dashboardCoverage) {
			coverage.raw = floor.AddDate(0, 0, 2)
			coverage.dirty = sql.NullTime{Time: floor.AddDate(0, 0, 1), Valid: true}
		}, time.Time{}, false},
		{"timezone mismatch raw only", func(coverage *dashboardCoverage) { coverage.zone = "UTC"; coverage.raw = floor.AddDate(0, 0, 2) }, floor.AddDate(0, 0, 2), true},
		{"projection mismatch raw only", func(coverage *dashboardCoverage) { coverage.version = 1; coverage.raw = floor.AddDate(0, 0, 2) }, floor.AddDate(0, 0, 2), true},
		{"no archive", func(coverage *dashboardCoverage) { coverage.daily = sql.NullTime{} }, floor, true},
		{"future floor", func(coverage *dashboardCoverage) { coverage.daily = sql.NullTime{}; coverage.raw = now.Add(time.Hour) }, time.Time{}, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			coverage := base
			scenario.change(&coverage)
			metadata := coverage.summaryMetadata(bucketFloor(now, "day"), now)
			require.False(test, metadata.Complete)
			require.Equal(test, scenario.valid, metadata.RetainedComplete)
			if scenario.valid {
				require.Equal(test, scenario.from, *metadata.WindowFrom)
				require.Equal(test, now, *metadata.WindowUntil)
				require.Empty(test, metadata.Reason)
			} else {
				require.Nil(test, metadata.WindowFrom)
				require.Nil(test, metadata.WindowUntil)
				require.NotEmpty(test, metadata.Reason)
			}
		})
	}
}

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

func TestUsageCoverageKnownEmptyPrefix(test *testing.T) {
	origin := time.Date(2026, 3, 1, 10, 15, 0, 0, time.UTC)
	coverage := dashboardCoverage{raw: origin, historyKnownEmptyBefore: sql.NullTime{Time: origin, Valid: true}}
	for _, scenario := range []struct {
		name       string
		start, end time.Time
	}{
		{"before", origin.Add(-time.Hour), origin.Add(-time.Minute)},
		{"ends_at_origin", origin.Add(-time.Hour), origin},
		{"crosses_origin", origin.Add(-time.Hour), origin.Add(time.Minute)},
		{"starts_at_origin", origin, origin.Add(time.Minute)},
	} {
		test.Run(scenario.name, func(test *testing.T) { require.NoError(test, coverage.requireRaw(scenario.start, scenario.end)) })
	}
	require.True(test, coverage.rawHistoryComplete())
	require.Equal(test, origin, *coverage.metadata().HistoryKnownEmptyBefore)
	coverage.raw = origin.Add(time.Hour)
	require.NoError(test, coverage.requireRaw(origin.Add(-time.Hour), origin))
	require.ErrorIs(test, coverage.requireRaw(origin.Add(-time.Hour), origin.Add(time.Minute)), ErrUsageHistoryUnavailable)
	require.False(test, coverage.rawHistoryComplete())
	require.Equal(test, "history_after_origin_unavailable", coverage.metadata().Reason)
	coverage.historyKnownEmptyBefore = sql.NullTime{}
	require.ErrorIs(test, coverage.requireRaw(origin.Add(-time.Hour), origin), ErrUsageHistoryUnavailable)
	require.Nil(test, coverage.metadata().HistoryKnownEmptyBefore)
	require.Equal(test, "history_before_coverage_floor_unverified", coverage.metadata().Reason)
}

func TestUsageCoverageOriginBucketsAndPublication(test *testing.T) {
	previous := timezone.Name()
	test.Cleanup(func() { require.NoError(test, timezone.Init(previous)) })
	for _, zone := range []string{"UTC", "Asia/Shanghai", "Asia/Kolkata", "Asia/Kathmandu", "America/New_York"} {
		test.Run(zone, func(test *testing.T) {
			require.NoError(test, timezone.Init(zone))
			day := time.Date(2026, 11, 1, 0, 0, 0, 0, timezone.Location())
			origin := day.Add(10*time.Hour + 15*time.Minute)
			end := day.AddDate(0, 0, 1)
			coverage := dashboardCoverage{raw: origin, historyKnownEmptyBefore: sql.NullTime{Time: origin, Valid: true}, zone: zone, version: dashboardProjectionVersion}
			first, _ := dashboardPublicationWindow(coverage, time.Unix(0, 0), end)
			require.True(test, first.Equal(day))
			emptyStart, emptyEnd := dashboardPublicationWindow(coverage, day.AddDate(0, 0, -20), origin.Add(-time.Hour))
			require.Equal(test, emptyEnd, emptyStart, "an entirely empty prefix does not publish facts or advance coverage")
			coverage.raw = origin.Add(time.Minute)
			first, _ = dashboardPublicationWindow(coverage, origin, end.AddDate(0, 0, 1))
			require.True(test, first.Equal(end), "a real gap after origin cannot publish the first day")
			coverage.raw = end
			coverage.daily = sql.NullTime{Time: day, Valid: true}
			coverage.hourly = sql.NullTime{Time: bucketFloor(origin, "hour"), Valid: true}
			coverage.published = end
			for _, granularity := range []string{"day", "hour"} {
				require.NoError(test, coverage.requireTrend(origin, bucketFloor(end, granularity), granularity))
				require.NoError(test, coverage.requireTrend(day.AddDate(0, 0, -10), bucketFloor(end, granularity), granularity))
				require.ErrorIs(test, coverage.requireTrend(origin.Add(time.Minute), end, granularity), ErrUsageHistoryUnavailable)
				require.ErrorIs(test, coverage.requireTrend(origin, origin.Add(time.Minute), granularity), ErrUsageHistoryUnavailable)
			}
			require.True(test, coverage.summaryComplete(end, end.Add(time.Hour)))
			require.False(test, coverage.summaryComplete(day, end.Add(time.Hour)), "today's distinct users require raw")
			coverage.dirty = sql.NullTime{Time: origin, Valid: true}
			require.False(test, coverage.summaryComplete(end, end.Add(time.Hour)))
			coverage.raw = origin
			require.True(test, coverage.summaryComplete(end, end.Add(time.Hour)), "dirty archive may fall back to complete raw")
		})
	}
}
