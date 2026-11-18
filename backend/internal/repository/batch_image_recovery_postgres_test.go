//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// These decorators exit the entire child process after a real database commit:
// no defers, lock release, queue completion or in-memory state survives.
type batchRecoveryCrashBilling struct{ service.UsageBillingRepository }

func (r batchRecoveryCrashBilling) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	result, err := r.UsageBillingRepository.CaptureBatchImageBalance(ctx, cmd)
	if err == nil && os.Getenv("BATCH_RECOVERY_CRASH") == "capture" {
		os.Exit(71)
	}
	return result, err
}

type batchRecoveryCrashUsage struct{ service.UsageLogRepository }

func (r batchRecoveryCrashUsage) Create(ctx context.Context, log *service.UsageLog) (bool, error) {
	if os.Getenv("BATCH_RECOVERY_CRASH") == "usage_error" {
		return false, errors.New("injected usage write failure")
	}
	inserted, err := r.UsageLogRepository.Create(ctx, log)
	if err == nil && os.Getenv("BATCH_RECOVERY_CRASH") == "usage" {
		os.Exit(71)
	}
	return inserted, err
}

type batchRecoveryCrashRepo struct{ service.BatchImageRepository }

func (r batchRecoveryCrashRepo) MarkBatchImageJobSettled(ctx context.Context, params service.MarkBatchImageJobSettledParams) error {
	if os.Getenv("BATCH_RECOVERY_CRASH") == "terminal_error" {
		return errors.New("injected terminal write failure")
	}
	err := r.BatchImageRepository.MarkBatchImageJobSettled(ctx, params)
	if err == nil && os.Getenv("BATCH_RECOVERY_CRASH") == "terminal" {
		os.Exit(71)
	}
	return err
}

type batchRecoveryPricing struct{}

func (batchRecoveryPricing) BatchImageUnitPrice(context.Context, *service.BatchImageJob) (float64, error) {
	return .25, nil
}

func batchRecoveryQueue(rdb *redis.Client, prefix string) *batchImageQueue {
	return newBatchImageQueueWithOptions(rdb, batchImageQueueOptions{
		ReadyKey: prefix + ":ready", ActiveKey: prefix + ":active", DelayedKey: prefix + ":delayed",
		InflightPrefix: prefix + ":inflight:", LockPrefix: prefix + ":lock:",
	})
}

func TestBatchImageRecoveryCrashChild(t *testing.T) {
	id := os.Getenv("BATCH_RECOVERY_CHILD")
	if id == "" {
		t.Skip("subprocess only")
	}
	db, err := sql.Open("postgres", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	var database string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&database))
	require.Equal(t, "job_test", database)
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("ACCOUNTING_TEST_REDIS_ADDR")})
	ctx := context.Background()
	q := batchRecoveryQueue(rdb, id)
	reserved, err := q.Reserve(ctx, time.Second)
	require.NoError(t, err)
	require.Equal(t, id, reserved.BatchID)
	_, acquired, err := q.TryAcquireJobLock(ctx, id, 100*time.Millisecond)
	require.NoError(t, err)
	require.True(t, acquired)
	svc := &service.BatchImageSettlementService{
		Repo:         batchRecoveryCrashRepo{NewBatchImageRepository(db)},
		BillingRepo:  batchRecoveryCrashBilling{NewUsageBillingRepository(nil, db)},
		UsageLogRepo: batchRecoveryCrashUsage{NewUsageLogRepository(nil, db)}, Pricing: batchRecoveryPricing{},
	}
	_, err = svc.Settle(ctx, id)
	if os.Getenv("BATCH_RECOVERY_CRASH") == "usage_error" || os.Getenv("BATCH_RECOVERY_CRASH") == "terminal_error" {
		require.ErrorContains(t, err, "injected")
		os.Exit(71)
	}
	t.Fatalf("expected crash hook; settlement returned %v", err)
}

func TestBatchImageRecoveryPostgresRedis(t *testing.T) {
	dsn, addr := os.Getenv("UPSTREAM_JOB_TEST_DSN"), os.Getenv("ACCOUNTING_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("requires isolated job_test PostgreSQL and ACCOUNTING_TEST_REDIS_ADDR")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var database string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&database))
	require.Equal(t, "job_test", database)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, ApplyMigrations(ctx, db))
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	require.NoError(t, rdb.Ping(ctx).Err())
	t.Run("expired_worker_fenced", func(t *testing.T) {
		id, err := service.NewBatchImageID()
		require.NoError(t, err)
		q := batchRecoveryQueue(rdb, id)
		t.Cleanup(func() {
			require.NoError(t, rdb.Del(context.Background(), q.readyKey, q.activeKey, q.delayedKey, q.inflightKey(id), q.lockKey(id)).Err())
		})
		require.NoError(t, q.Enqueue(ctx, id))
		_, err = q.Reserve(ctx, time.Second)
		require.NoError(t, err)
		old, acquired, err := q.TryAcquireJobLock(ctx, id, 30*time.Millisecond)
		require.NoError(t, err)
		require.True(t, acquired)
		require.Eventually(t, func() bool { return rdb.Exists(ctx, q.lockKey(id)).Val() == 0 }, time.Second, 10*time.Millisecond)
		next, acquired, err := q.TryAcquireJobLock(ctx, id, time.Minute)
		require.NoError(t, err)
		require.True(t, acquired)
		require.ErrorIs(t, old.(service.BatchImageJobLockRefresher).Refresh(ctx, time.Minute), service.ErrBatchImageLockNotAcquired)
		for _, terminal := range []bool{true, false} {
			require.ErrorIs(t, old.(service.BatchImageJobLockCompletion).Complete(ctx, terminal, time.Minute), service.ErrBatchImageLockNotAcquired)
		}
		require.NoError(t, old.Release(ctx))
		require.EqualValues(t, 1, rdb.Exists(ctx, q.lockKey(id)).Val())
		require.EqualValues(t, 1, rdb.Exists(ctx, q.inflightKey(id)).Val())
		require.NoError(t, rdb.ZScore(ctx, q.activeKey, id).Err())
		require.NoError(t, next.(service.BatchImageJobLockCompletion).Complete(ctx, true, 0))
		require.NoError(t, next.Release(ctx))
	})
	for _, stage := range []string{"capture", "usage", "terminal", "usage_error", "terminal_error"} {
		t.Run(stage, func(t *testing.T) {
			id, err := service.NewBatchImageID()
			require.NoError(t, err)
			var user, key, account int64
			require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, id+"@recovery.test").Scan(&user))
			require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'recovery') RETURNING id`, user, id).Scan(&key))
			require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES($1,'gemini','apikey') RETURNING id`, id).Scan(&account))
			q := batchRecoveryQueue(rdb, id)
			t.Cleanup(func() {
				for _, query := range []string{`DELETE FROM batch_image_events WHERE job_id=$1`, `DELETE FROM batch_image_jobs WHERE batch_id=$1`, `DELETE FROM usage_logs WHERE request_id='batch_image_capture:' || $1`, `DELETE FROM usage_billing_dedup WHERE request_id IN ('batch_image_hold:' || $1,'batch_image_capture:' || $1,'batch_image_terminal:' || $1)`} {
					_, err := db.Exec(query, id)
					require.NoError(t, err)
				}
				_, err := db.Exec(`DELETE FROM users WHERE id=$1`, user)
				require.NoError(t, err)
				_, err = db.Exec(`DELETE FROM accounts WHERE id=$1`, account)
				require.NoError(t, err)
				require.NoError(t, rdb.Del(context.Background(), q.readyKey, q.activeKey, q.delayedKey, q.inflightKey(id), q.lockKey(id)).Err())
			})
			repo := NewBatchImageRepository(db).(*batchImageRepository)
			_, err = repo.CreateBatchImageJob(ctx, service.CreateBatchImageJobParams{BatchID: id, UserID: user, APIKeyID: &key, AccountID: &account, Provider: service.BatchImageProviderGeminiAPI, Model: "test-image", Status: service.BatchImageJobStatusSettling, ItemCount: 4, EstimatedCost: 1})
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `UPDATE batch_image_jobs SET success_count=2,fail_count=2 WHERE batch_id=$1`, id)
			require.NoError(t, err)
			billing := NewUsageBillingRepository(nil, db)
			_, err = billing.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(id), BatchID: id, UserID: user, APIKeyID: key, HoldAmount: 1})
			require.NoError(t, err)
			require.NoError(t, q.Enqueue(ctx, id))
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBatchImageRecoveryCrashChild$", "-test.count=1")
			cmd.Env = append(os.Environ(), "BATCH_RECOVERY_CHILD="+id, "BATCH_RECOVERY_CRASH="+stage)
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, "%s", output)
			require.Equal(t, 71, exitErr.ExitCode(), "%s", output)

			assertDurable := func(completed bool, usageCount int) {
				job, err := repo.GetBatchImageJobByBatchID(ctx, id)
				require.NoError(t, err)
				if completed {
					require.Equal(t, service.BatchImageJobStatusCompleted, job.Status)
				} else {
					require.Equal(t, service.BatchImageJobStatusSettling, job.Status)
				}
				var count int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, service.BatchImageCaptureRequestID(id), key).Scan(&count))
				require.Equal(t, usageCount, count)
				var balance, frozen float64
				require.NoError(t, db.QueryRowContext(ctx, `SELECT balance,frozen_balance FROM users WHERE id=$1`, user).Scan(&balance, &frozen))
				require.Equal(t, 99.5, balance)
				require.Zero(t, frozen)
				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, service.BatchImageCaptureRequestID(id), key).Scan(&count))
				require.Equal(t, 1, count)
			}
			wantUsage := 1
			if stage == "capture" || stage == "usage_error" {
				wantUsage = 0
			}
			assertDurable(stage == "terminal", wantUsage)
			ids, err := repo.ListRecoverableBatchImageJobs(ctx, "", 1000)
			require.NoError(t, err)
			if stage != "terminal" {
				require.Contains(t, ids, id)
			} else {
				require.NotContains(t, ids, id)
			}
			// Recover the Redis reservation left behind by the dead process.
			require.Eventually(t, func() bool { return rdb.Exists(ctx, q.lockKey(id)).Val() == 0 }, time.Second, 10*time.Millisecond)
			_, err = q.RecoverStaleActive(ctx, time.Millisecond, 10)
			require.NoError(t, err)
			reserved, err := q.Reserve(ctx, time.Second)
			require.NoError(t, err)
			require.Equal(t, id, reserved.BatchID)
			lock, acquired, err := q.TryAcquireJobLock(ctx, id, time.Minute)
			require.NoError(t, err)
			require.True(t, acquired)
			defer lock.Release(context.Background())
			svc := &service.BatchImageSettlementService{Repo: repo, BillingRepo: billing, UsageLogRepo: NewUsageLogRepository(nil, db), Pricing: batchRecoveryPricing{}}
			for range 3 {
				_, err := svc.Settle(ctx, id)
				require.NoError(t, err)
			}
			require.NoError(t, lock.(service.BatchImageJobLockCompletion).Complete(ctx, true, 0))
			assertDurable(true, 1)
			var actual float64
			require.NoError(t, db.QueryRowContext(ctx, `SELECT actual_cost FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, service.BatchImageCaptureRequestID(id), key).Scan(&actual))
			require.Equal(t, .5, actual)
			require.Zero(t, rdb.Exists(ctx, q.inflightKey(id)).Val())
		})
	}
}
