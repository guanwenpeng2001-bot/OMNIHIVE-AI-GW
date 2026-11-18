package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func captureBillingBacklogLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func readBillingBacklogLog(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()
	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record), "one complete snapshot")
	require.Equal(t, "billing backlog", record["msg"])
	require.Equal(t, "INFO", record["level"])
	require.Equal(t, "billing", record["component"])
	return record
}

func TestBillingBacklogIndependentGauges(t *testing.T) {
	output := captureBillingBacklogLog(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{
		"pending", "review", "oldest_pending", "jobs", "unknown", "retries", "oldest_due", "terminal_review", "terminal_waiver",
	}).AddRow(3, 5, 11.5, 7, 13, 2, 17.5, 19, 23))
	require.NoError(t, (&OpsMetricsCollector{db: db}).collectBillingBacklog(context.Background()))
	record := readBillingBacklogLog(t, output)
	for key, want := range map[string]float64{
		"pending_count": 3, "needs_review_count": 5, "oldest_pending_age_seconds": 11.5,
		"pending_jobs": 7, "submission_unknown_count": 13, "retry_jobs": 2,
		"oldest_due_age_seconds": 17.5, "terminal_jobs_needs_review_count": 19,
		"terminal_jobs_manual_waiver_count": 23,
	} {
		require.Equal(t, want, record[key], key)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBillingBacklogFailureDoesNotLogZeroSnapshot(t *testing.T) {
	for _, scenario := range []string{"nil collector", "nil database", "query failure", "scan failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			output := captureBillingBacklogLog(t)
			var collector *OpsMetricsCollector
			ctx := context.Background()
			switch scenario {
			case "nil collector":
			case "nil database":
				collector = &OpsMetricsCollector{}
			default:
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })
				collector = &OpsMetricsCollector{db: db}
				switch scenario {
				case "canceled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "scan failure":
					mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{
						"a", "b", "c", "d", "e", "f", "g", "h", "i",
					}).AddRow(0, 0, 0, 0, 0, 0, 0, 1, "invalid count"))
				default:
					mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("private database error"))
				}
			}
			require.Error(t, collector.collectBillingBacklog(ctx))
			require.Empty(t, output.String(), "no partial, zero, or raw-error success log")
		})
	}
}

// Uses only temporary tables on a single connection, never application tables.
// Opt in with a loopback PostgreSQL URL for the dedicated test database.
func TestBillingBacklogPostgres(t *testing.T) {
	dsn := os.Getenv("OPS_BILLING_BACKLOG_TEST_DSN")
	if dsn == "" {
		t.Skip("set OPS_BILLING_BACKLOG_TEST_DSN to local ops_billing_backlog_test")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.Empty(t, u.RawQuery, "connection options must not override the local test target")
	require.Equal(t, "/ops_billing_backlog_test", u.Path)
	u.RawQuery = "sslmode=disable"
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var database string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database))
	require.Equal(t, "ops_billing_backlog_test", database)
	// Only columns read by this collector; billing invariants are tested by the
	// repository suite. Extra evidence columns let us verify collection is read-only.
	_, err = db.ExecContext(ctx, `CREATE TEMP TABLE upstream_jobs (
	 id bigserial PRIMARY KEY, public_request_id text, status text, error_code text, error_message text DEFAULT '',
	 charged_usage_log_id bigint, family text, attempt_count integer DEFAULT 0,
	 next_poll_at timestamptz DEFAULT NOW(), billing_snapshot jsonb DEFAULT '{}');
	 CREATE TEMP TABLE usage_settlement_receipts (
	 request_id text, state text, created_at timestamptz DEFAULT NOW());`)
	require.NoError(t, err)
	output := captureBillingBacklogLog(t)
	collector := &OpsMetricsCollector{db: db}
	const evidenceSQL = `SELECT (SELECT jsonb_agg(to_jsonb(j) ORDER BY j.id) FROM upstream_jobs j)::text,
	 (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.request_id), '[]') FROM usage_settlement_receipts r)::text`
	t.Run("empty", func(t *testing.T) {
		require.NoError(t, collector.collectBillingBacklog(ctx))
		record := readBillingBacklogLog(t, output)
		require.Equal(t, float64(0), record["terminal_jobs_needs_review_count"])
		require.Equal(t, float64(0), record["terminal_jobs_manual_waiver_count"])
	})
	for _, tc := range []struct {
		name, status, code, family, pricing string
		anchor                              any
		receipt                             bool
		want, pending                       float64
		message                             any
		waiver                              float64
	}{
		{"unpriced success", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, "", 0},
		{"already charged with stale marker", "succeeded", "settlement_needs_review", "video", "priced", int64(123), false, 0, 0, "", 0},
		{"free success without anchor", "succeeded", "", "files", "free", nil, false, 0, 0, "", 0},
		{"free success with anchor", "succeeded", "", "video", "free", int64(124), false, 0, 0, "", 0},
		{"success without review marker", "succeeded", "", "video", "unpriced", nil, false, 0, 0, "", 0},
		{"submitted review", "submitted", "settlement_needs_review", "video", "unpriced", nil, false, 0, 1, "", 0},
		{"running review", "running", "settlement_needs_review", "video", "unpriced", nil, false, 0, 1, "", 0},
		{"exhausted settlement", "failed", "settlement_needs_review", "realtime_turn", "unpriced", nil, false, 1, 0, "", 0},
		{"expired review", "expired", "settlement_needs_review", "batch", "unpriced", nil, false, 1, 0, "", 0},
		{"ordinary failure", "failed", "upstream_error", "video", "unpriced", nil, false, 0, 0, "", 0},
		{"session lease", "failed", "settlement_needs_review", "realtime_session", "unpriced", nil, false, 0, 0, "", 0},
		{"same usage has review receipt", "succeeded", "settlement_needs_review", "video", "unpriced", nil, true, 1, 0, "", 0},
		{"explicit manual waiver", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 1},
		{"appended waiver preserves evidence", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 0, 0, "Historical tariff missing\n[MANUAL_WAIVER_NO_DEBIT] authorized by user", 1},
		{"repeated marker counts one job", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 0, 0, "MANUAL_WAIVER_NO_DEBIT; MANUAL_WAIVER_NO_DEBIT", 1},
		{"waiver with review receipt", "succeeded", "settlement_needs_review", "video", "unpriced", nil, true, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 1},
		{"failed waived job", "failed", "settlement_needs_review", "realtime_turn", "unpriced", nil, false, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 1},
		{"expired waived job", "expired", "settlement_needs_review", "batch", "unpriced", nil, false, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 1},
		{"charged job with stale waiver", "succeeded", "settlement_needs_review", "video", "priced", int64(123), false, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 0},
		{"free job is not review waiver", "succeeded", "", "files", "free", nil, false, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 0},
		{"submitted waiver excluded", "submitted", "settlement_needs_review", "video", "unpriced", nil, false, 0, 1, "MANUAL_WAIVER_NO_DEBIT", 0},
		{"running waiver excluded", "running", "settlement_needs_review", "video", "unpriced", nil, false, 0, 1, "MANUAL_WAIVER_NO_DEBIT", 0},
		{"session waiver excluded", "failed", "settlement_needs_review", "realtime_session", "unpriced", nil, false, 0, 0, "MANUAL_WAIVER_NO_DEBIT", 0},
		{"null message is not waiver", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, nil, 0},
		{"no debit prose is not waiver", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, "Generation completed; no charge was applied", 0},
		{"case mismatch is not ops token", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, "manual_waiver_no_debit", 0},
		{"token prefix is not ops token", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, "NOT_MANUAL_WAIVER_NO_DEBIT", 0},
		{"token suffix is not ops token", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, "MANUAL_WAIVER_NO_DEBIT_REQUESTED", 0},
		{"underscores are literal", "succeeded", "settlement_needs_review", "video", "unpriced", nil, false, 1, 0, "MANUALXWAIVERYNOZDEBIT", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, `TRUNCATE pg_temp.upstream_jobs, pg_temp.usage_settlement_receipts`)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `INSERT INTO upstream_jobs (public_request_id,status,error_code,family,charged_usage_log_id,billing_snapshot,error_message)
			 VALUES ('same-request',$1,$2,$3,$4,jsonb_build_object('pricing_state',$5::text),$6)`, tc.status, tc.code, tc.family, tc.anchor, tc.pricing, tc.message)
			require.NoError(t, err)
			var receiptCount float64
			if tc.receipt {
				_, err = db.ExecContext(ctx, `INSERT INTO usage_settlement_receipts (request_id,state) VALUES ('same-request','needs_review')`)
				require.NoError(t, err)
				receiptCount = 1
			}
			var jobsBefore, receiptsBefore, jobsAfter, receiptsAfter string
			require.NoError(t, db.QueryRowContext(ctx, evidenceSQL).Scan(&jobsBefore, &receiptsBefore))
			output.Reset()
			require.NoError(t, collector.collectBillingBacklog(ctx))
			record := readBillingBacklogLog(t, output)
			require.Equal(t, tc.want, record["terminal_jobs_needs_review_count"])
			require.Equal(t, tc.waiver, record["terminal_jobs_manual_waiver_count"])
			require.Equal(t, receiptCount, record["needs_review_count"], "receipt gauge is never increased by job review")
			require.Equal(t, tc.pending, record["pending_jobs"])
			require.Equal(t, float64(0), record["retry_jobs"])
			require.NoError(t, db.QueryRowContext(ctx, evidenceSQL).Scan(&jobsAfter, &receiptsAfter))
			require.Equal(t, jobsBefore, jobsAfter)
			require.Equal(t, receiptsBefore, receiptsAfter)
		})
	}
	t.Run("two waived videos stay separate from unresolved jobs and receipts", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `TRUNCATE pg_temp.upstream_jobs, pg_temp.usage_settlement_receipts;
		 INSERT INTO upstream_jobs (public_request_id,status,error_code,family,error_message,billing_snapshot)
		 SELECT 'waived-video-' || n, 'succeeded', 'settlement_needs_review', 'video',
		 'Historical price unavailable; [MANUAL_WAIVER_NO_DEBIT] user approved', '{"pricing_state":"unpriced"}'
		 FROM generate_series(1,2) n;
		 INSERT INTO usage_settlement_receipts (request_id,state) VALUES ('waived-video-1','needs_review');`)
		require.NoError(t, err)
		for wantReview := 0; wantReview <= 1; wantReview++ {
			if wantReview == 1 {
				_, err = db.ExecContext(ctx, `INSERT INTO upstream_jobs (public_request_id,status,error_code,family,error_message,billing_snapshot)
				 VALUES ('unresolved-video','succeeded','settlement_needs_review','video','Historical price unavailable','{"pricing_state":"unpriced"}')`)
				require.NoError(t, err)
			}
			var jobsBefore, receiptsBefore, jobsAfter, receiptsAfter string
			require.NoError(t, db.QueryRowContext(ctx, evidenceSQL).Scan(&jobsBefore, &receiptsBefore))
			output.Reset()
			require.NoError(t, collector.collectBillingBacklog(ctx))
			record := readBillingBacklogLog(t, output)
			require.Equal(t, float64(wantReview), record["terminal_jobs_needs_review_count"])
			require.Equal(t, float64(2), record["terminal_jobs_manual_waiver_count"])
			require.Equal(t, float64(1), record["needs_review_count"])
			require.Equal(t, float64(0), record["pending_jobs"])
			require.Equal(t, float64(0), record["retry_jobs"])
			require.NoError(t, db.QueryRowContext(ctx, evidenceSQL).Scan(&jobsAfter, &receiptsAfter))
			require.Equal(t, jobsBefore, jobsAfter, "status, error code, message and unpriced evidence are unchanged")
			require.Equal(t, receiptsBefore, receiptsAfter)
		}
	})
	t.Run("partial index on historical successes", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `TRUNCATE pg_temp.upstream_jobs;
		 INSERT INTO upstream_jobs (status,error_code,family,charged_usage_log_id)
		 SELECT 'succeeded','','video',n FROM generate_series(1,100000) n;
		 INSERT INTO upstream_jobs (status,error_code,family,error_message)
		 VALUES ('succeeded','settlement_needs_review','video',''),('failed','settlement_needs_review','realtime_turn',''),
		 ('succeeded','settlement_needs_review','video','MANUAL_WAIVER_NO_DEBIT'),
		 ('succeeded','settlement_needs_review','video','[MANUAL_WAIVER_NO_DEBIT] user approved');`)
		require.NoError(t, err)
		migration, err := migrations.FS.ReadFile("288_upstream_jobs_terminal_review_index_notx.sql")
		require.NoError(t, err)
		for i := 0; i < 2; i++ {
			_, err = db.ExecContext(ctx, string(migration))
			require.NoError(t, err, "migration is executable and idempotent")
		}
		_, err = db.ExecContext(ctx, `ANALYZE upstream_jobs`)
		require.NoError(t, err)
		var reviewCount, waiverCount int64
		require.NoError(t, db.QueryRowContext(ctx, terminalJobsReviewCountsSQL).Scan(&reviewCount, &waiverCount))
		require.Equal(t, int64(2), reviewCount)
		require.Equal(t, int64(2), waiverCount)
		rows, err := db.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+terminalJobsReviewCountsSQL)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		var lines []string
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			lines = append(lines, line)
		}
		require.NoError(t, rows.Err())
		plan := strings.Join(lines, "\n")
		t.Log(plan)
		require.Contains(t, plan, "idx_upstream_jobs_terminal_review", "planner should use the small partial index")
		require.Equal(t, 1, strings.Count(plan, "idx_upstream_jobs_terminal_review"), "both gauges share one candidate scan")
		require.NotContains(t, plan, "Seq Scan on upstream_jobs")
	})
}
