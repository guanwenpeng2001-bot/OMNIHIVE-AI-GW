package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHostedOverviewReadsOriginalRowsInsteadOfOverlappingHourlyDimensions(t *testing.T) {
	groupID := int64(8)
	for _, filter := range []service.OpsDashboardFilter{
		{Platform: "anthropic"}, {Platform: "bedrock"}, {Platform: "gemini"},
		{Platform: "openai"}, {GroupID: &groupID},
	} {
		for _, mode := range []service.OpsQueryMode{service.OpsQueryModeAuto, service.OpsQueryModePreagg} {
			database, mock := newSQLMock(t)
			repository := &opsRepository{db: database}
			filter.StartTime = time.Now().Add(-6 * time.Hour)
			filter.EndTime = time.Now()
			filter.QueryMode = mode
			rawRead := errors.New("original usage rows requested")
			// The hourly table can contain both legacy and recomputed buckets. The first
			// query must instead count original usage rows, never add those dimensions.
			mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*FROM usage_logs ul`).WillReturnError(rawRead)
			_, err := repository.GetDashboardOverview(context.Background(), &filter)
			require.ErrorIs(t, err, rawRead)
			require.NoError(t, mock.ExpectationsWereMet())
		}
	}
}

func TestHostedPreaggregatedOverviewNilFilterRemainsAnError(t *testing.T) {
	repository := &opsRepository{}
	_, err := repository.getDashboardOverviewPreaggregated(context.Background(), nil)
	require.ErrorContains(t, err, "nil filter")
}

func TestHostedOverviewCountsStayEqualAfterHourlyRecomputation(t *testing.T) {
	dsn := os.Getenv("HOSTED_IDENTITY_TEST_DSN")
	if dsn == "" {
		t.Skip("HOSTED_IDENTITY_TEST_DSN is not configured")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer database.Close()
	require.NoError(t, ApplyMigrations(context.Background(), database))
	// Temporary tables must remain on the same isolated connection used by repo queries.
	database.SetMaxOpenConns(1)
	_, err = database.Exec(`
  CREATE TEMP TABLE groups (id bigint, platform text);
  CREATE TEMP TABLE accounts (id bigint, platform text);
  CREATE TEMP TABLE usage_logs (id bigint, group_id bigint, account_id bigint, created_at timestamptz, duration_ms integer, first_token_ms integer, input_tokens bigint, output_tokens bigint, cache_creation_tokens bigint, cache_read_tokens bigint, actual_cost numeric);
  CREATE TEMP TABLE ops_error_logs (created_at timestamptz, platform text, group_id bigint, is_business_limited boolean, error_owner text, status_code integer, upstream_status_code integer, is_count_tokens boolean);
  CREATE TEMP TABLE ops_metrics_hourly (LIKE public.ops_metrics_hourly INCLUDING DEFAULTS INCLUDING INDEXES);
  INSERT INTO groups VALUES (1,'bedrock'),(2,'anthropic'),(3,'composite');
  INSERT INTO accounts VALUES (1,'bedrock'),(2,'anthropic');
  INSERT INTO usage_logs VALUES
   (1,1,1,'2025-01-01 01:10Z',10,5,10,5,0,0,1.25),
   (2,2,2,'2025-01-01 01:20Z',10,5,20,5,0,0,2.50),
   (3,3,1,'2025-01-01 01:30Z',10,5,30,5,0,0,3.75);
 `)
	require.NoError(t, err)
	repository := &opsRepository{db: database}
	start := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	filter := &service.OpsDashboardFilter{Platform: service.PlatformAnthropic}
	assertOriginalRows := func() {
		count, tokens, readErr := repository.queryUsageCounts(context.Background(), filter, start, end)
		require.NoError(t, readErr)
		require.Equal(t, int64(3), count)
		require.Equal(t, int64(75), tokens)
		hostedGroupID := int64(1)
		groupFilter := &service.OpsDashboardFilter{Platform: service.PlatformAnthropic, GroupID: &hostedGroupID}
		groupCount, groupTokens, groupErr := repository.queryUsageCounts(context.Background(), groupFilter, start, end)
		require.NoError(t, groupErr)
		require.Equal(t, int64(1), groupCount)
		require.Equal(t, int64(15), groupTokens)
		var amount string
		require.NoError(t, database.QueryRow(`SELECT SUM(actual_cost)::text FROM usage_logs`).Scan(&amount))
		require.Equal(t, "7.50", amount)
	}
	require.NoError(t, repository.UpsertHourlyMetrics(context.Background(), start, end))
	assertOriginalRows()
	_, err = database.Exec(`UPDATE groups SET platform='anthropic' WHERE platform='bedrock'; UPDATE accounts SET platform='anthropic' WHERE platform='bedrock'`)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, repository.UpsertHourlyMetrics(context.Background(), start, end))
		assertOriginalRows()
	}
	var legacyCount, overlapCount int64
	require.NoError(t, database.QueryRow(`SELECT success_count FROM ops_metrics_hourly WHERE platform='bedrock' AND group_id IS NULL`).Scan(&legacyCount))
	require.Equal(t, int64(1), legacyCount, "existing derived rows remain untouched")
	require.NoError(t, database.QueryRow(`SELECT SUM(success_count) FROM ops_metrics_hourly WHERE platform IN ('bedrock','anthropic') AND group_id IS NULL`).Scan(&overlapCount))
	require.Equal(t, int64(3), overlapCount, "legacy plus canonical buckets overlap while Composite remains separate")
	require.NoError(t, database.QueryRow(`SELECT SUM(success_count) FROM ops_metrics_hourly WHERE platform IN ('bedrock','anthropic') AND group_id=1`).Scan(&overlapCount))
	require.Equal(t, int64(2), overlapCount, "derived group dimensions double-count the original request, which the raw reader counts once")
}
