//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Stop exactly after the receipt INSERT commits, before the service can settle.
type realtimeReceiptCommitBoundary struct {
	service.UpstreamJobRepository
	receipts service.QianwenRealtimeReceiptRepository
	stop     error
}

func (boundary *realtimeReceiptCommitBoundary) InsertRealtimeReceipt(ctx context.Context, receipt *service.UpstreamJob) (*service.UpstreamJob, error) {
	stored, err := boundary.receipts.InsertRealtimeReceipt(ctx, receipt)
	if err != nil {
		return stored, err
	}
	return stored, boundary.stop
}

func TestQianwenRealtimeReceiptPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires UPSTREAM_JOB_TEST_DSN")
		}
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer database.Close()
	var databaseName string
	require.NoError(t, database.QueryRow(`SELECT current_database()`).Scan(&databaseName))
	require.Equal(t, "job_test", databaseName)
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, database))
	repository := &upstreamJobRepository{db: database}
	fixture := func(t *testing.T) *service.UpstreamJob {
		t.Helper()
		session := &service.UpstreamJob{PublicRequestID: uuid.NewString(), Platform: service.PlatformQianwen, Family: "realtime_session"}
		require.NoError(t, database.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@example.test").Scan(&session.UserID))
		require.NoError(t, database.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'test') RETURNING id`, session.UserID, uuid.NewString()).Scan(&session.APIKeyID))
		require.NoError(t, database.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('test','qianwen','apikey') RETURNING id`).Scan(&session.AccountID))
		session.Billing = service.UpstreamJobBilling{Model: "qwen3-tts-flash-realtime", RequestedModel: "qwen3-tts-flash-realtime", UnitPrice: 0.13 / 10000, RateMultiplier: 1, AccountRateMultiplier: 1, Realtime: &service.QianwenRealtimeSnapshot{SessionID: session.PublicRequestID, Region: "singapore", PriceVersion: service.QianwenRealtimePriceVersion}}
		return session
	}
	newService := func(repo service.UpstreamJobRepository) *service.UpstreamJobService {
		return service.NewUpstreamJobService(repo, func(context.Context, *service.UpstreamJob) (service.UpstreamJobUpdate, error) {
			t.Error("recovery must not contact provider")
			return service.UpstreamJobUpdate{}, errors.New("unexpected provider poll")
		}, service.QianwenRealtimeCharge)
	}
	usage := []byte(`{"characters":25}`)
	t.Run("first_commit_restart_concurrent_duplicate_and_conflicts", func(t *testing.T) {
		session := fixture(t)
		stopped := errors.New("process stopped after insert commit")
		boundary := &realtimeReceiptCommitBoundary{UpstreamJobRepository: repository, receipts: repository, stop: stopped}
		receipt, err := newService(boundary).RecordQianwenRealtimeTurn(ctx, session, "response-first", usage, "completed")
		require.ErrorIs(t, err, stopped)
		require.NotNil(t, receipt)
		// Read through a new database connection, before any settlement has run.
		restartedDB, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		defer restartedDB.Close()
		restartedRepo := &upstreamJobRepository{db: restartedDB}
		stored, err := restartedRepo.Get(ctx, receipt.PublicRequestID, session.UserID, session.APIKeyID)
		require.NoError(t, err)
		require.JSONEq(t, string(usage), string(stored.UsageJSON))
		require.Equal(t, "response-first", stored.UpstreamTaskID)
		require.Equal(t, "completed", stored.ResultRef)
		require.Equal(t, session.Billing, stored.Billing)
		require.Equal(t, "running", stored.Status)
		require.False(t, stored.NextPollAt.IsZero())
		require.Nil(t, stored.ChargedUsageLogID)
		due, err := restartedRepo.Due(ctx, stored.NextPollAt, 100)
		require.NoError(t, err)
		require.Contains(t, due, stored.PublicRequestID)
		restarted := newService(restartedRepo)
		require.NoError(t, restarted.PollDue(ctx, 100))
		beforeReplay, err := restartedRepo.Get(ctx, stored.PublicRequestID, session.UserID, session.APIKeyID)
		require.NoError(t, err)
		require.NotEmpty(t, beforeReplay.Billing.PricingState)
		require.NotEmpty(t, beforeReplay.Billing.PricingSnapshot)
		var workers sync.WaitGroup
		failures := make(chan error, 16)
		for range 16 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				_, err := restarted.RecordQianwenRealtimeTurn(ctx, session, "response-first", []byte(`{ "characters" : 25 }`), "completed")
				failures <- err
			}()
		}
		workers.Wait()
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
		settled, err := restartedRepo.Get(ctx, stored.PublicRequestID, session.UserID, session.APIKeyID)
		require.NoError(t, err)
		require.NotNil(t, settled.ChargedUsageLogID)
		require.Equal(t, beforeReplay.Billing, settled.Billing, "duplicate receipt must preserve persisted pricing outputs")
		recalculated, err := service.RecalculatePricingSnapshot(settled.Billing.PricingSnapshot)
		require.NoError(t, err)
		require.Equal(t, 0.000325, recalculated.ActualCost)
		var logs int
		var balance float64
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1`, stored.PublicRequestID).Scan(&logs))
		require.Equal(t, 1, logs)
		require.NoError(t, database.QueryRow(`SELECT balance FROM users WHERE id=$1`, session.UserID).Scan(&balance))
		require.InDelta(t, 99.999675, balance, 1e-8)
		for _, conflict := range []struct {
			name       string
			usage      []byte
			status     string
			multiplier float64
		}{
			{"usage", []byte(`{"characters":26}`), "completed", 1},
			{"provider_status", usage, "failed", 1},
			{"frozen_price", usage, "completed", 2},
		} {
			t.Run(conflict.name, func(t *testing.T) {
				changed := *session
				changed.Billing.RateMultiplier = conflict.multiplier
				original, err := restarted.RecordQianwenRealtimeTurn(ctx, &changed, "response-first", conflict.usage, conflict.status)
				require.ErrorIs(t, err, service.ErrQianwenRealtimeConflict)
				require.Equal(t, settled, original)
				after, err := restartedRepo.Get(ctx, stored.PublicRequestID, session.UserID, session.APIKeyID)
				require.NoError(t, err)
				require.Equal(t, settled, after)
			})
		}
	})

	t.Run("concurrent_first_insert_charges_once", func(t *testing.T) {
		session := fixture(t)
		start := make(chan struct{})
		failures := make(chan error, 16)
		for range 16 {
			go func() {
				<-start
				_, err := newService(repository).RecordQianwenRealtimeTurn(ctx, session, "response-concurrent", usage, "completed")
				failures <- err
			}()
		}
		close(start)
		for range 16 {
			require.NoError(t, <-failures)
		}
		var receipts, logs int
		var balance float64
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM upstream_jobs WHERE user_id=$1 AND charged_usage_log_id IS NOT NULL`, session.UserID).Scan(&receipts))
		require.Equal(t, 1, receipts)
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, session.UserID).Scan(&logs))
		require.Equal(t, 1, logs)
		require.NoError(t, database.QueryRow(`SELECT balance FROM users WHERE id=$1`, session.UserID).Scan(&balance))
		require.InDelta(t, 99.999675, balance, 1e-8)
	})
	t.Run("insert_failure_leaves_no_empty_receipt", func(t *testing.T) {
		session := fixture(t)
		// Reject this user's INSERT in PostgreSQL after all values reach the server.
		_, err := database.Exec(`CREATE FUNCTION reject_realtime_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.user_id = ` + fmt.Sprint(session.UserID) + ` THEN RAISE EXCEPTION 'injected receipt insert failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_realtime_receipt BEFORE INSERT ON upstream_jobs FOR EACH ROW EXECUTE FUNCTION reject_realtime_receipt()`)
		require.NoError(t, err)
		defer func() {
			_, err := database.Exec(`DROP TRIGGER reject_realtime_receipt ON upstream_jobs; DROP FUNCTION reject_realtime_receipt()`)
			require.NoError(t, err)
		}()
		_, err = newService(repository).RecordQianwenRealtimeTurn(ctx, session, "response-rejected", usage, "completed")
		require.ErrorContains(t, err, "injected receipt insert failure")
		var receipts int
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM upstream_jobs WHERE user_id=$1`, session.UserID).Scan(&receipts))
		require.Zero(t, receipts)
	})
	t.Run("billing_failure_keeps_receipt_for_restart", func(t *testing.T) {
		session := fixture(t)
		_, err := database.Exec(`CREATE FUNCTION reject_realtime_anchor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.user_id = ` + fmt.Sprint(session.UserID) + ` AND NEW.charged_usage_log_id IS NOT NULL THEN RAISE EXCEPTION 'injected billing anchor failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_realtime_anchor BEFORE UPDATE ON upstream_jobs FOR EACH ROW EXECUTE FUNCTION reject_realtime_anchor()`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := database.Exec(`DROP TRIGGER IF EXISTS reject_realtime_anchor ON upstream_jobs; DROP FUNCTION IF EXISTS reject_realtime_anchor()`)
			require.NoError(t, err)
		})
		failing := newService(repository)
		_, err = failing.RecordQianwenRealtimeTurn(ctx, session, "response-billing", usage, "completed")
		require.ErrorContains(t, err, "injected billing anchor failure")
		var logs int
		var balance float64
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, session.UserID).Scan(&logs))
		require.Zero(t, logs)
		require.NoError(t, database.QueryRow(`SELECT balance FROM users WHERE id=$1`, session.UserID).Scan(&balance))
		require.Equal(t, float64(100), balance)
		_, err = database.Exec(`DROP TRIGGER reject_realtime_anchor ON upstream_jobs; DROP FUNCTION reject_realtime_anchor()`)
		require.NoError(t, err)
		var requestID string
		require.NoError(t, database.QueryRow(`SELECT public_request_id FROM upstream_jobs WHERE user_id=$1`, session.UserID).Scan(&requestID))
		stored, err := repository.Get(ctx, requestID, session.UserID, session.APIKeyID)
		require.NoError(t, err)
		require.Nil(t, stored.ChargedUsageLogID)
		require.JSONEq(t, string(usage), string(stored.UsageJSON))
		require.Equal(t, 1, stored.AttemptCount)
		require.Equal(t, "settlement_pending", stored.ErrorCode)
		_, err = database.Exec(`UPDATE upstream_jobs SET next_poll_at=now() WHERE public_request_id=$1`, requestID)
		require.NoError(t, err)
		recovered, err := newService(&upstreamJobRepository{db: database}).Poll(ctx, requestID)
		require.NoError(t, err)
		require.NotNil(t, recovered.ChargedUsageLogID)
	})
	t.Run("persistent_failures_do_not_starve_healthy_receipt", func(t *testing.T) {
		session := fixture(t)
		stop := errors.New("receipt committed")
		boundary := &realtimeReceiptCommitBoundary{UpstreamJobRepository: repository, receipts: repository, stop: stop}
		var healthy *service.UpstreamJob
		for i := 0; i < 21; i++ {
			id := fmt.Sprintf("fair-%02d", i)
			receipt, err := newService(boundary).RecordQianwenRealtimeTurn(ctx, session, id, usage, "completed")
			require.ErrorIs(t, err, stop)
			if i == 20 {
				healthy = receipt
			}
		}
		recovering := service.NewUpstreamJobService(repository, nil, func(j *service.UpstreamJob) (*service.UsageLog, *service.UsageBillingCommand, error) {
			if j.UpstreamTaskID != "fair-20" {
				return nil, nil, errors.New("injected settlement outage")
			}
			return service.QianwenAsyncJobCharge(j)
		})
		require.Error(t, recovering.PollDue(ctx, 20))
		require.NoError(t, recovering.PollDue(ctx, 20))
		stored, err := repository.Get(ctx, healthy.PublicRequestID, session.UserID, session.APIKeyID)
		require.NoError(t, err)
		require.NotNil(t, stored.ChargedUsageLogID)
		for attempt := 1; attempt < 20; attempt++ {
			_, err := database.Exec(`UPDATE upstream_jobs SET next_poll_at=now() WHERE user_id=$1 AND status='running'`, session.UserID)
			require.NoError(t, err)
			require.Error(t, recovering.PollDue(ctx, 20))
		}
		var review, charged int
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM upstream_jobs WHERE user_id=$1 AND error_code='settlement_needs_review' AND status='failed' AND attempt_count=20 AND usage_json IS NOT NULL AND charged_usage_log_id IS NULL`, session.UserID).Scan(&review))
		require.Equal(t, 20, review)
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, session.UserID).Scan(&charged))
		require.Equal(t, 1, charged)
		due, err := repository.Due(ctx, time.Now().Add(48*time.Hour), 100)
		require.NoError(t, err)
		require.Empty(t, due)
	})

}
