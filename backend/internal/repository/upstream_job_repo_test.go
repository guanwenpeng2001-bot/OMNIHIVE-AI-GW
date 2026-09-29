//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Opt-in real PostgreSQL proof, also compiled by the required unit suite. The
// dedicated database name guard prevents an accidental shared-database test run.
func TestUpstreamJobPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated job_test PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(32)
	ctx := context.Background()
	var database string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.Equal(t, "job_test", database, "refusing to modify any non-test database")
	require.NoError(t, ApplyMigrations(ctx, db))
	migration, err := migrations.FS.ReadFile("247_upstream_jobs.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must be repeatable")
	repo := NewUpstreamJobRepository(db)
	fixture := func(t *testing.T) *service.UpstreamJob {
		t.Helper()
		var user, key, account int64
		require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@example.test").Scan(&user))
		require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'test') RETURNING id`, user, uuid.NewString()).Scan(&key))
		require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('test','qianwen','apikey') RETURNING id`).Scan(&account))
		return &service.UpstreamJob{UserID: user, APIKeyID: key, AccountID: account, Platform: service.PlatformQianwen, Family: "video", Billing: service.UpstreamJobBilling{Model: "wan2.6-t2v", RequestedModel: "wan2.6-t2v", Resolution: "720p", UnitPrice: 0.1, RateMultiplier: 1, AccountRateMultiplier: 1, KeyQuota: true, KeyRateLimit: true}}
	}
	success := service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "succeeded", ResultRef: "https://example.test/video.mp4", Usage: json.RawMessage(`{"duration":5,"video_count":1,"SR":720}`)}
	makeDue := func(t *testing.T, id string) {
		t.Helper()
		_, err := db.Exec(`UPDATE upstream_jobs SET next_poll_at=NOW()-INTERVAL '1 second' WHERE public_request_id=$1`, id)
		require.NoError(t, err)
	}
	checkMoney := func(t *testing.T, j *service.UpstreamJob, wantLogs int, wantBalance float64) {
		t.Helper()
		var logs int
		var balance, quota, rate float64
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1`, j.PublicRequestID).Scan(&logs))
		require.Equal(t, wantLogs, logs)
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, j.UserID).Scan(&balance))
		require.Equal(t, wantBalance, balance)
		require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h FROM api_keys WHERE id=$1`, j.APIKeyID).Scan(&quota, &rate))
		require.InDelta(t, 100-wantBalance, quota, 1e-8)
		require.Equal(t, quota, rate)
	}
	t.Run("frozen_pricing_survives_database_reload", func(t *testing.T) {
		input := fixture(t)
		svc := service.NewUpstreamJobService(repo, nil, service.QianwenVideoJobCharge)
		job, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v","input":{"prompt":"frozen"}}`), func(context.Context) (service.UpstreamJobUpdate, error) {
			return service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "submitted"}, nil
		})
		require.NoError(t, err)
		_, err = svc.Settle(ctx, job.PublicRequestID, success)
		require.NoError(t, err)
		loaded, err := NewUpstreamJobRepository(db).Get(ctx, job.PublicRequestID, input.UserID, input.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, service.PricingStatePriced, loaded.Billing.PricingState)
		require.NotEmpty(t, loaded.Billing.PricingSnapshot)
		replay, err := service.RecalculatePricingSnapshot(loaded.Billing.PricingSnapshot)
		require.NoError(t, err)
		require.Equal(t, .5, replay.ActualCost)
		frozen := loaded.Billing.PricingSnapshot
		_, err = repo.WithLocked(ctx, job.PublicRequestID, func(updated *service.UpstreamJob, _ service.UpstreamJobTx) error {
			updated.Billing.PricingSnapshot = `{"changed":true}`
			return nil
		})
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		_, err = repo.WithLocked(ctx, job.PublicRequestID, func(updated *service.UpstreamJob, _ service.UpstreamJobTx) error {
			updated.Billing.UnitPrice = 999 // ordinary UPDATE must not rewrite admission rates
			return nil
		})
		require.NoError(t, err)
		reloaded, err := repo.Get(ctx, job.PublicRequestID, input.UserID, input.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, frozen, reloaded.Billing.PricingSnapshot)
		require.Equal(t, .1, reloaded.Billing.UnitPrice)
		checkMoney(t, reloaded, 1, 99.5)
	})
	t.Run("concurrent_submit_poll_settle_restart", func(t *testing.T) {
		input := fixture(t)
		var submits, polls atomic.Int32
		svc := service.NewUpstreamJobService(repo, func(context.Context, *service.UpstreamJob) (service.UpstreamJobUpdate, error) {
			polls.Add(1)
			return success, nil
		}, service.QianwenVideoJobCharge)
		ids := make(chan string, 32)
		errs := make(chan error, 32)
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				j, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v","input":{"prompt":"cat"}}`), func(context.Context) (service.UpstreamJobUpdate, error) {
					submits.Add(1)
					return service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "submitted"}, nil
				})
				if err != nil {
					errs <- err
					return
				}
				ids <- j.PublicRequestID
			}()
		}
		wg.Wait()
		close(ids)
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		id := ""
		for got := range ids {
			if id == "" {
				id = got
			}
			require.Equal(t, id, got)
		}
		require.EqualValues(t, 1, submits.Load())
		// A scheduler change on a replay must retain the original account claim.
		retryInput := *input
		require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('other','qianwen','apikey') RETURNING id`).Scan(&retryInput.AccountID))
		replay, err := svc.Submit(ctx, &retryInput, []byte(`{"input":{"prompt":"cat"},"model":"wan2.6-t2v"}`), func(context.Context) (service.UpstreamJobUpdate, error) {
			submits.Add(1)
			return service.UpstreamJobUpdate{}, errors.New("must not submit")
		})
		require.NoError(t, err)
		require.Equal(t, id, replay.PublicRequestID)
		require.Equal(t, input.AccountID, replay.AccountID)
		require.EqualValues(t, 1, submits.Load())
		// A fresh service/repository pair models restart and another server process.
		svc2 := service.NewUpstreamJobService(NewUpstreamJobRepository(db), func(context.Context, *service.UpstreamJob) (service.UpstreamJobUpdate, error) {
			polls.Add(1)
			return success, nil
		}, service.QianwenVideoJobCharge)
		makeDue(t, id)
		errs = make(chan error, 32)
		for i := range 32 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					_, err = svc.Poll(ctx, id)
				} else {
					_, err = svc2.Poll(ctx, id)
				}
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.EqualValues(t, 1, polls.Load())
		for range 3 {
			j, err := svc2.Settle(ctx, id, success)
			require.NoError(t, err)
			require.Equal(t, "succeeded", j.Status)
			require.NotNil(t, j.ChargedUsageLogID)
			checkMoney(t, j, 1, 99.5)
		}
		_, err = svc.Get(ctx, id, input.UserID+1, input.APIKeyID)
		require.ErrorIs(t, err, service.ErrUpstreamJobNotFound)
		_, err = svc.Get(ctx, id, input.UserID, input.APIKeyID+1)
		require.ErrorIs(t, err, service.ErrUpstreamJobNotFound)
	})
	t.Run("failure_replay_expiry_and_unsupported", func(t *testing.T) {
		for _, mode := range []string{"rejected", "ambiguous", "unsupported", "expired"} {
			t.Run(mode, func(t *testing.T) {
				var submits, polls atomic.Int32
				svc := service.NewUpstreamJobService(repo, func(context.Context, *service.UpstreamJob) (service.UpstreamJobUpdate, error) {
					polls.Add(1)
					u := success
					u.Usage = json.RawMessage(`{"unknown_units":9}`)
					return u, nil
				}, service.QianwenVideoJobCharge)
				input := fixture(t)
				submit := func(context.Context) (service.UpstreamJobUpdate, error) {
					submits.Add(1)
					if mode == "rejected" {
						return service.UpstreamJobUpdate{Status: "failed", ErrorCode: "upstream_rejected"}, nil
					}
					if mode == "ambiguous" {
						return service.UpstreamJobUpdate{}, errors.New("connection lost")
					}
					return service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "submitted"}, nil
				}
				j, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v"}`), submit)
				require.NoError(t, err)
				if mode == "expired" {
					_, err = db.Exec(`UPDATE upstream_jobs SET expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, j.ID)
					require.NoError(t, err)
					_, err = svc.Expire(ctx)
					require.NoError(t, err)
				}
				makeDue(t, j.PublicRequestID)
				for range 3 {
					j, err = svc.Poll(ctx, j.PublicRequestID)
					require.NoError(t, err)
				}
				want := "failed"
				if mode == "expired" {
					want = "expired"
				}
				require.Equal(t, want, j.Status)
				if mode == "unsupported" {
					require.Equal(t, "unsupported_usage", j.ErrorCode)
					require.JSONEq(t, `{"unknown_units":9}`, string(j.UsageJSON))
				} else {
					require.Zero(t, polls.Load())
				}
				replay, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v"}`), submit)
				require.NoError(t, err)
				require.Equal(t, j.ID, replay.ID)
				require.EqualValues(t, 1, submits.Load())
				checkMoney(t, j, 0, 100)
			})
		}
	})
	t.Run("rollback_usage_balance_and_anchor_together", func(t *testing.T) {
		input := fixture(t)
		failCharge := true
		svc := service.NewUpstreamJobService(repo, func(context.Context, *service.UpstreamJob) (service.UpstreamJobUpdate, error) { return success, nil }, func(j *service.UpstreamJob) (*service.UsageLog, *service.UsageBillingCommand, error) {
			log, cmd, err := service.QianwenVideoJobCharge(j)
			if failCharge {
				cmd.UserID = -1
			}
			return log, cmd, err
		})
		j, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v"}`), func(context.Context) (service.UpstreamJobUpdate, error) {
			return service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "running"}, nil
		})
		require.NoError(t, err)
		makeDue(t, j.PublicRequestID)
		_, err = svc.Poll(ctx, j.PublicRequestID)
		require.Error(t, err)
		j, err = svc.Get(ctx, j.PublicRequestID, j.UserID, j.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, "running", j.Status)
		require.Nil(t, j.ChargedUsageLogID)
		checkMoney(t, j, 0, 100)
		failCharge = false
		j, err = svc.Poll(ctx, j.PublicRequestID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", j.Status)
		checkMoney(t, j, 1, 99.5)
	})
	t.Run("poll_backoff_and_crashed_claim_expiry", func(t *testing.T) {
		input := fixture(t)
		var polls int
		svc := service.NewUpstreamJobService(repo, func(context.Context, *service.UpstreamJob) (service.UpstreamJobUpdate, error) {
			polls++
			return service.UpstreamJobUpdate{}, fmt.Errorf("offline")
		}, service.QianwenVideoJobCharge)
		j, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v"}`), func(context.Context) (service.UpstreamJobUpdate, error) {
			return service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "running"}, nil
		})
		require.NoError(t, err)
		for i := 1; i <= 3; i++ {
			makeDue(t, j.PublicRequestID)
			j, err = svc.Poll(ctx, j.PublicRequestID)
			require.NoError(t, err)
			require.Equal(t, i, j.AttemptCount)
			wantDelay := 5 * (1 << (i - 1))
			require.InDelta(t, float64(wantDelay), j.NextPollAt.Sub(*j.LastPolledAt).Seconds(), 0.1)
			_, err = svc.Poll(ctx, j.PublicRequestID)
			require.NoError(t, err)
			require.Equal(t, i, polls)
		}
		_, err = db.Exec(`UPDATE upstream_jobs SET upstream_task_id='',expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, j.ID)
		require.NoError(t, err)
		require.NoError(t, svc.PollDue(ctx, 20))
		j, err = svc.Get(ctx, j.PublicRequestID, j.UserID, j.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, "expired", j.Status)
		checkMoney(t, j, 0, 100)
	})
	t.Run("workspace_owner_binding_and_cross_tenant_reads", func(t *testing.T) {
		input := fixture(t)
		svc := service.NewUpstreamJobService(repo, nil, service.QianwenVideoJobCharge)
		job, err := svc.Submit(ctx, input, []byte(`{"model":"wan2.6-t2v"}`), func(context.Context) (service.UpstreamJobUpdate, error) {
			return service.UpstreamJobUpdate{TaskID: "upstream-1", Status: "submitted"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), job.WorkspaceID, "owner workspace is resolved from the persisted key, never the caller")
		require.Positive(t, job.Version)

		foreign := domain.WithWorkspaceScope(ctx, 424242)
		_, err = svc.Get(foreign, job.PublicRequestID, input.UserID, input.APIKeyID)
		require.ErrorIs(t, err, service.ErrUpstreamJobNotFound, "a foreign workspace scope must not read the task")

		scoped := domain.WithWorkspaceScope(ctx, 1)
		_, err = svc.Get(scoped, job.PublicRequestID, input.UserID, input.APIKeyID)
		require.NoError(t, err)

		// Ownership, upstream binding and workspace are immutable after insert.
		_, err = db.Exec(`UPDATE upstream_jobs SET workspace_id=2 WHERE id=$1`, job.ID)
		require.Error(t, err, "workspace reassignment is rejected")
		_, err = db.Exec(`UPDATE upstream_jobs SET account_id=account_id+1 WHERE id=$1`, job.ID)
		require.Error(t, err, "upstream account binding is immutable")
		_, err = db.Exec(`UPDATE upstream_jobs SET api_key_id=api_key_id+1 WHERE id=$1`, job.ID)
		require.Error(t, err, "owner key binding is immutable")

		// The version predicate fences a stale lock holder even inside the row lock.
		stale, err := repo.Get(ctx, job.PublicRequestID, input.UserID, input.APIKeyID)
		require.NoError(t, err)
		fresh, err := repo.WithLocked(ctx, job.PublicRequestID, func(j *service.UpstreamJob, _ service.UpstreamJobTx) error {
			j.AttemptCount++
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, stale.Version+1, fresh.Version)
	})
	t.Log("isolated PostgreSQL: migration replay, concurrent claims, row-lock settlement, ownership, rollback, terminal replay, backoff and expiry passed", time.Now().UTC())
}
