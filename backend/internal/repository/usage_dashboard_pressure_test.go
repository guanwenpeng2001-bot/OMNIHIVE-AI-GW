//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

const usagePressureRows = 100000
const usagePressureReaders = 16
const usagePressureReadsPerReader = 64 // 1024 measured reads, same distribution in both phases.

type usagePressureFixture struct {
	database *sql.DB
	start    time.Time
	end      time.Time
}

// Only schema definitions are copied, never public data. The dashboard upgrade
// uses the exact embedded pre-coverage migrations and migration 258, rather than
// manufacturing an old watermark on an already-upgraded dashboard.
func newUsagePressureFixture(test *testing.T) usagePressureFixture {
	test.Helper()
	dsn := os.Getenv("USAGE_STATS_TEST_DSN")
	if dsn == "" {
		test.Skip("USAGE_STATS_TEST_DSN must name disposable stats_test")
	}
	database, err := testresources.OpenPostgres(context.Background(), dsn, "stats_test")
	require.NoError(test, err)
	test.Cleanup(func() { require.NoError(test, database.Close()) })
	parsed, err := url.Parse(dsn)
	require.NoError(test, err)
	var otherConnections int
	require.NoError(test, database.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname IN ('stats_test','job_test') AND pid <> pg_backend_pid() AND backend_type='client backend'`).Scan(&otherConnections))
	require.Zero(test, otherConnections, "exclusive stats_test/job_test window is required; never terminate other sessions")
	test.Logf("exclusive_preflight other_connections=%d", otherConnections)
	schema := fmt.Sprintf("usage_pressure_%d", time.Now().UnixNano())
	_, err = database.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(test, err)
	test.Cleanup(func() {
		_, cleanupErr := database.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		require.NoError(test, cleanupErr)
	})
	parameters := parsed.Query()
	parameters.Set("search_path", schema)
	parameters.Set("application_name", "usage-pressure-fixture")
	parsed.RawQuery = parameters.Encode()
	isolated, err := sql.Open("postgres", parsed.String())
	require.NoError(test, err)
	isolated.SetMaxOpenConns(usagePressureReaders + 2)
	isolated.SetMaxIdleConns(usagePressureReaders + 2)
	test.Cleanup(func() { require.NoError(test, isolated.Close()) })
	previousTimezone := timezone.Name()
	require.NoError(test, timezone.Init("UTC"))
	test.Cleanup(func() { require.NoError(test, timezone.Init(previousTimezone)) })
	// LIKE excludes foreign keys and triggers. IDs are supplied explicitly so
	// no copied sequence default can write outside this fixture's schema.
	for _, table := range []string{"usage_logs", "users", "api_keys", "accounts", "usage_group_rollup_state", "usage_settlement_receipts"} {
		_, err = isolated.Exec(`CREATE TABLE ` + table + ` (LIKE public.` + table + ` INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`)
		require.NoError(test, err)
	}
	fixture := usagePressureFixture{database: isolated, start: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	fixture.end = fixture.start.AddDate(0, 0, 7)
	for _, migration := range []string{"034_usage_dashboard_aggregation_tables.sql", "107_add_account_cost_to_dashboard_tables.sql"} {
		fixture.migrate(test, migration)
	}
	fixture.exec(test, `INSERT INTO usage_group_rollup_state(id) VALUES(1)`)
	fixture.exec(test, `INSERT INTO users(id,email,password_hash,balance) VALUES(1,'synthetic-pressure@example.test','synthetic-not-a-credential',100)`)
	fixture.exec(test, `INSERT INTO api_keys(id,user_id,key,name,quota_used) VALUES(1,1,'synthetic-pressure-not-a-key','pressure',7)`)
	fixture.exec(test, `INSERT INTO accounts(id,name,platform,type) VALUES(1,'synthetic-pressure','openai','apikey')`)
	fixture.exec(test, `INSERT INTO usage_logs(id,user_id,api_key_id,account_id,model,input_tokens,output_tokens,total_cost,actual_cost,account_stats_cost,account_rate_multiplier,created_at)
	 SELECT ordinal+1,1,1,1,'synthetic-pressure',10,5,1,0.125,2,0.5,
	 $1::timestamptz + (ordinal % 7)*INTERVAL '1 day' + (ordinal / 7)*INTERVAL '6 seconds'
	 FROM generate_series(0,99999) ordinal`, fixture.start)
	fixture.exec(test, `INSERT INTO usage_settlement_receipts(id,request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,usage_log_id,settled_at)
	 VALUES(1,'synthetic-pressure-receipt',1,1,1,'synthetic-fingerprint','{}','{}','priced','{}','resolved','complete','settled',1,$1)`, fixture.end)
	fixture.exec(test, `ANALYZE usage_logs`)
	return fixture
}

func (fixture usagePressureFixture) exec(test *testing.T, query string, arguments ...any) {
	test.Helper()
	_, err := fixture.database.Exec(query, arguments...)
	require.NoError(test, err)
}

func (fixture usagePressureFixture) migrate(test *testing.T, filename string) {
	test.Helper()
	body, err := migrations.FS.ReadFile(filename)
	require.NoError(test, err)
	fixture.exec(test, string(body))
}

func (fixture usagePressureFixture) financialState(test *testing.T) string {
	test.Helper()
	var result string
	require.NoError(test, fixture.database.QueryRow(`SELECT json_build_object('users',(SELECT json_agg(row_to_json(u)) FROM users u),'keys',(SELECT json_agg(row_to_json(k)) FROM api_keys k),'receipts',(SELECT json_agg(row_to_json(r)) FROM usage_settlement_receipts r))::text`).Scan(&result))
	return result
}

func (fixture usagePressureFixture) calibrate(test *testing.T) {
	test.Helper()
	// Synthetic fixture proves the complete raw boundary; production migration
	// intentionally cannot infer it from MIN(created_at).
	fixture.exec(test, `UPDATE usage_dashboard_aggregation_watermark SET raw_retained_from=$1 WHERE last_aggregated_at='1970-01-01' AND timezone_name='' AND hourly_retained_from IS NULL AND daily_retained_from IS NULL`, fixture.start)
}

func (fixture usagePressureFixture) verify(test *testing.T, financialState string) {
	test.Helper()
	for _, query := range []string{
		`SELECT count(*),sum(input_tokens),sum(output_tokens),sum(actual_cost),sum(COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)) FROM usage_logs`,
		`SELECT sum(total_requests),sum(input_tokens),sum(output_tokens),sum(actual_cost),sum(account_cost) FROM usage_dashboard_hourly`,
		`SELECT sum(total_requests),sum(input_tokens),sum(output_tokens),sum(actual_cost),sum(account_cost) FROM usage_dashboard_daily`,
	} {
		var requests, inputs, outputs int64
		var actual, account float64
		require.NoError(test, fixture.database.QueryRow(query).Scan(&requests, &inputs, &outputs, &actual, &account))
		require.EqualValues(test, usagePressureRows, requests)
		require.EqualValues(test, usagePressureRows*10, inputs)
		require.EqualValues(test, usagePressureRows*5, outputs)
		require.Equal(test, float64(usagePressureRows)*0.125, actual)
		require.Equal(test, float64(usagePressureRows), account)
	}
	coverage, err := readDashboardCoverage(context.Background(), fixture.database, false)
	require.NoError(test, err)
	require.True(test, coverage.raw.Equal(fixture.start))
	require.True(test, coverage.published.Equal(fixture.end))
	require.True(test, coverage.hourly.Valid && coverage.hourly.Time.Equal(fixture.start))
	require.True(test, coverage.daily.Valid && coverage.daily.Time.Equal(fixture.start))
	require.False(test, coverage.dirty.Valid)
	require.Equal(test, financialState, fixture.financialState(test), "rebuild/upgrade must not settle funds")
}

func TestUsageDashboardUpgradePreservesHistory(test *testing.T) {
	fixture := newUsagePressureFixture(test)
	before := fixture.financialState(test)
	// Seed old aggregates directly from the synthetic raw data using the old
	// table contract. Migration must preserve these rows but distrust coverage.
	fixture.exec(test, `INSERT INTO usage_dashboard_daily(bucket_date,total_requests,actual_cost) SELECT created_at::date,count(*),sum(actual_cost) FROM usage_logs GROUP BY 1`)
	fixture.exec(test, `UPDATE usage_dashboard_aggregation_watermark SET last_aggregated_at=$1`, fixture.end)
	fixture.migrate(test, "258_usage_dashboard_coverage.sql")
	fixture.migrate(test, "279_usage_history_origin.sql")
	var historicalRequests int64
	require.NoError(test, fixture.database.QueryRow(`SELECT sum(total_requests) FROM usage_dashboard_daily`).Scan(&historicalRequests))
	require.EqualValues(test, usagePressureRows, historicalRequests)
	coverage, err := readDashboardCoverage(context.Background(), fixture.database, false)
	require.NoError(test, err)
	require.True(test, coverage.published.Equal(time.Unix(0, 0)))
	require.True(test, coverage.raw.After(fixture.end), "upgrade must not fabricate historical raw coverage")
	require.False(test, coverage.daily.Valid)
	require.False(test, coverage.hourly.Valid)
	fixture.calibrate(test)
	aggregator := newDashboardAggregationRepositoryWithSQL(fixture.database)
	aggregator.clock = func() time.Time { return fixture.end }
	require.NoError(test, aggregator.AggregateRangeAndPublish(context.Background(), fixture.start, fixture.end))
	fixture.verify(test, before)
	test.Log("upgrade=034+107->258 raw_rows=100000 days=7 preserved_old_rows=true calibrated_rebuild=true financial_state_unchanged=true")
}

func TestUsageDashboardRebuildPressure(test *testing.T) {
	fixture := newUsagePressureFixture(test)
	fixture.migrate(test, "258_usage_dashboard_coverage.sql")
	fixture.migrate(test, "279_usage_history_origin.sql")
	fixture.calibrate(test)
	before := fixture.financialState(test)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	aggregator := newDashboardAggregationRepositoryWithSQL(fixture.database)
	aggregator.clock = func() time.Time { return fixture.end }
	require.NoError(test, aggregator.AggregateRangeAndPublish(ctx, fixture.start, fixture.end))
	fixture.verify(test, before)
	repository := &usageLogRepository{sql: fixture.database}
	read := func(sequence int) error {
		from, until, granularity := fixture.start, fixture.end, "day"
		if sequence%2 != 0 {
			granularity = "hour"
		}
		if sequence%4 >= 2 {
			from = from.AddDate(0, 0, 2)
			until = from.AddDate(0, 0, 1)
		}
		if sequence%4 == 3 {
			from = from.Add(30 * time.Second)
			until = until.Add(-30 * time.Second)
		}
		points, err := repository.getUsageTrendFromAggregates(ctx, from, until, granularity)
		if err != nil {
			return err
		}
		var requests int64
		var cost float64
		for _, point := range points {
			requests += point.Requests
			cost += point.ActualCost
		}
		wanted := int64(usagePressureRows)
		if sequence%4 >= 2 {
			wanted = 14286
		}
		if sequence%4 == 3 {
			wanted -= 5
		}
		if requests != wanted || cost != float64(wanted)*0.125 {
			return fmt.Errorf("sequence=%d requests=%d cost=%g want=%d", sequence, requests, cost, wanted)
		}
		return nil
	}
	measure := func(rebuild bool) ([]int64, int64, int64, uint64) {
		// Five full reader rounds before EACH phase, using the same distribution.
		for warmup := 0; warmup < 5; warmup++ {
			var warming sync.WaitGroup
			warmupErrors := make(chan error, usagePressureReaders)
			for reader := 0; reader < usagePressureReaders; reader++ {
				warming.Go(func() { warmupErrors <- read(reader + warmup) })
			}
			warming.Wait()
			close(warmupErrors)
			for warmupError := range warmupErrors {
				require.NoError(test, warmupError)
			}
		}
		var failures, rebuilds atomic.Int64
		var peak atomic.Uint64
		finished := make(chan struct{})
		var observer sync.WaitGroup
		observer.Go(func() {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				if memory.HeapAlloc > peak.Load() {
					peak.Store(memory.HeapAlloc)
				}
				select {
				case <-finished:
					return
				case <-ticker.C:
				}
			}
		})
		var readers sync.WaitGroup
		var writer sync.WaitGroup
		start := make(chan struct{})
		samples := make([]int64, usagePressureReaders*usagePressureReadsPerReader)
		if rebuild {
			writer.Go(func() {
				<-start
				for {
					if err := aggregator.AggregateRangeAndPublish(ctx, fixture.start, fixture.end); err != nil {
						failures.Add(1)
						return
					}
					rebuilds.Add(1)
					select {
					case <-finished:
						return
					default:
					}
				}
			})
		}
		for reader := 0; reader < usagePressureReaders; reader++ {
			readers.Go(func() {
				<-start
				for iteration := 0; iteration < usagePressureReadsPerReader; iteration++ {
					began := time.Now()
					if readErr := read(reader + iteration); readErr != nil {
						failures.Add(1)
					}
					samples[reader*usagePressureReadsPerReader+iteration] = time.Since(began).Nanoseconds()
				}
			})
		}
		close(start)
		readers.Wait()
		close(finished)
		writer.Wait()
		observer.Wait()
		return samples, failures.Load(), rebuilds.Load(), peak.Load()
	}
	baseline, baselineErrors, _, baselinePeak := measure(false)
	pressure, pressureErrors, rebuilds, pressurePeak := measure(true)
	quantile := func(samples []int64, percentile int) int64 {
		ordered := append([]int64(nil), samples...)
		sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
		return ordered[(len(ordered)*percentile+99)/100-1]
	}
	report := map[string]any{"rows": usagePressureRows, "days": 7, "readers": usagePressureReaders, "warmup_rounds": 5, "reads_per_phase": len(pressure), "db_end_to_end": true, "baseline_ns": baseline, "pressure_ns": pressure, "baseline_p50_ns": quantile(baseline, 50), "baseline_p95_ns": quantile(baseline, 95), "baseline_p99_ns": quantile(baseline, 99), "pressure_p50_ns": quantile(pressure, 50), "pressure_p95_ns": quantile(pressure, 95), "pressure_p99_ns": quantile(pressure, 99), "errors": baselineErrors + pressureErrors, "rebuilds": rebuilds, "baseline_peak_heap_bytes": baselinePeak, "pressure_peak_heap_bytes": pressurePeak}
	body, err := json.Marshal(report)
	require.NoError(test, err)
	test.Logf("usage_pressure_measurement=%s", body)
	fixture.verify(test, before)
	require.Zero(test, baselineErrors+pressureErrors)
	require.Positive(test, rebuilds)
	require.LessOrEqual(test, float64(quantile(pressure, 95)), float64(quantile(baseline, 95))*1.2, "fixed p95 budget; preserve failures and rerun only in a fresh evidence directory")
}
