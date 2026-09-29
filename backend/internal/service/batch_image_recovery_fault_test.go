//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recoveryScanRepo struct {
	BatchImageRepository
	ids []string
	err error
}

func (r *recoveryScanRepo) ListRecoverableBatchImageJobs(_ context.Context, after string, limit int) ([]string, error) {
	limit = NormalizeBatchImageRecoveryLimit(limit)
	if r.err != nil {
		return nil, r.err
	}
	var out []string
	for _, id := range r.ids {
		if id > after && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func TestBatchImageRecovery_PersistentHeadDoesNotStarveTail(t *testing.T) {
	for _, configured := range []int{0, -1, 100, 1000, 1001, 2000} {
		t.Run(fmt.Sprintf("limit_%d", configured), func(t *testing.T) {
			repo := &recoveryScanRepo{}
			for index := 0; index < 2005; index++ {
				repo.ids = append(repo.ids, fmt.Sprintf("imgbatch_%04d", index))
			}
			queue := &recoveryQueue{fakeBatchImageQueue: newFakeBatchImageQueue(""), seen: map[string]bool{}}
			worker := NewBatchImageWorker(queue, &fakeBatchImageProcessor{}, BatchImageWorkerOptions{RecoverLimit: configured})
			expectedLimit := 100
			if configured == 1000 {
				expectedLimit = 1000
			}
			require.Equal(t, expectedLimit, worker.opts.RecoverLimit)
			runtime := NewBatchImageWorkerRuntime(worker, nil)
			runtime.billingRecovery = &BatchImageBillingRecoveryService{Repo: repo}
			// Every row remains recoverable, even after enqueue. Scan twice to
			// exercise both duplicate head pages and cursor wraparound.
			for round := 0; round < 2; round++ {
				for page := 0; page <= len(repo.ids)/expectedLimit; page++ {
					require.NoError(t, runtime.recoverQueueOnce(context.Background()))
				}
				require.True(t, queue.seen["imgbatch_2004"], "tail must be visited despite persistent head rows")
				require.Len(t, queue.seen, len(repo.ids))
				require.Empty(t, runtime.recoveryAfter)
			}
		})
	}
}

type recoveryQueue struct {
	*fakeBatchImageQueue
	unavailable bool
	seen        map[string]bool
}

func (q *recoveryQueue) Enqueue(_ context.Context, id string) error {
	if q.unavailable {
		return errors.New("redis disconnected")
	}
	if q.seen[id] {
		return ErrBatchImageAlreadyQueued
	}
	q.seen[id] = true
	return nil
}
func TestBatchImageRecovery_DBAndRedisDisconnectResumeWithoutStarvation(t *testing.T) {
	repo := &recoveryScanRepo{ids: []string{"imgbatch_a", "imgbatch_b", "imgbatch_c"}}
	q := &recoveryQueue{fakeBatchImageQueue: newFakeBatchImageQueue(""), seen: map[string]bool{}, unavailable: true}
	r := NewBatchImageWorkerRuntime(NewBatchImageWorker(q, &fakeBatchImageProcessor{}, BatchImageWorkerOptions{RecoverLimit: 2}), nil)
	r.billingRecovery = &BatchImageBillingRecoveryService{Repo: repo}
	require.Error(t, r.recoverQueueOnce(context.Background()))
	require.Empty(t, r.recoveryAfter)
	q.unavailable = false
	require.NoError(t, r.recoverQueueOnce(context.Background()))
	require.Equal(t, "imgbatch_b", r.recoveryAfter)
	repo.err = errors.New("database disconnected")
	require.Error(t, r.recoverQueueOnce(context.Background()))
	require.Equal(t, "imgbatch_b", r.recoveryAfter)
	repo.err = nil
	require.NoError(t, r.recoverQueueOnce(context.Background()))
	require.Empty(t, r.recoveryAfter)
	require.Len(t, q.seen, 3)
	require.NoError(t, r.recoverQueueOnce(context.Background())) // duplicate page is harmless
	require.Equal(t, "imgbatch_b", r.recoveryAfter)
}

func TestBatchImageRecovery_StaleUploadingRetainsHold(t *testing.T) {
	repo := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("imgbatch_unknown_submission")
	job.Status = BatchImageJobStatusUploading
	job.ProviderJobName = nil
	job.UpdatedAt = time.Now().Add(-time.Hour)
	repo.jobs[job.BatchID] = job
	billing := &fakeBatchImageBillingRepo{}
	recovery := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, StaleAfter: time.Minute}
	n, err := recovery.ReleaseStaleUnsubmittedOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, billing.releases)
	require.Equal(t, BatchImageJobStatusUploading, job.Status)
}

type lostLeaseQueue struct {
	*fakeBatchImageQueue
	lock *lostLeaseLock
}
type lostLeaseLock struct{ calls int }

func (l *lostLeaseLock) Release(context.Context) error { return nil }
func (l *lostLeaseLock) Refresh(context.Context, time.Duration) error {
	l.calls++
	if l.calls > 1 {
		return ErrBatchImageLockNotAcquired
	}
	return nil
}
func (q *lostLeaseQueue) TryAcquireJobLock(context.Context, string, time.Duration) (BatchImageJobLock, bool, error) {
	return q.lock, true, nil
}

type cancelledProcessor struct{ cancelled bool }

func (p *cancelledProcessor) Process(ctx context.Context, _ string) (BatchImageProcessResult, error) {
	<-ctx.Done()
	p.cancelled = true
	return BatchImageProcessResult{Terminal: true}, nil // late success must not Ack
}
func TestBatchImageWorker_LostLeaseCancelsAndRejectsLateCompletion(t *testing.T) {
	q := &lostLeaseQueue{fakeBatchImageQueue: newFakeBatchImageQueue("imgbatch_lost"), lock: &lostLeaseLock{}}
	p := &cancelledProcessor{}
	w := NewBatchImageWorker(q, p, BatchImageWorkerOptions{JobLockTTL: 60 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.Error(t, w.RunOnce(ctx))
	require.True(t, p.cancelled)
	require.Empty(t, q.acked)
	require.Empty(t, q.requeued)
}

func TestBatchImageCapture_ExpiredLeaseCannotTouchMoney(t *testing.T) {
	billing := &fakeBatchImageBillingRepo{}
	ctx := context.WithValue(context.Background(), batchImageLeaseContextKey{}, func() error { return ErrBatchImageLockNotAcquired })
	err := captureBatchImageBalanceHold(ctx, billing, testSettlingBatchImageJob("imgbatch_expired"), .25, "hash")
	require.ErrorIs(t, err, ErrBatchImageLockNotAcquired)
	require.Empty(t, billing.captures)
}

type batchRecoveryUsageWriter struct {
	UsageLogRepository
	err        error
	afterWrite func()
}

func (r batchRecoveryUsageWriter) Create(context.Context, *UsageLog) (bool, error) {
	if r.afterWrite != nil {
		r.afterWrite()
	}
	return r.err == nil, r.err
}

func TestBatchImageSettlement_UsageAcknowledgementAndLeaseRequired(t *testing.T) {
	for _, failure := range []string{"missing_writer", "write_error", "lease_lost_after_usage"} {
		t.Run(failure, func(t *testing.T) {
			repo := newFakeBatchImageRepository()
			job := testSettlingBatchImageJob("imgbatch_usage_ack")
			repo.jobs[job.BatchID] = job
			billing := &fakeBatchImageBillingRepo{}
			svc := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: .25}}
			leaseLost := false
			ctx := context.WithValue(context.Background(), batchImageLeaseContextKey{}, func() error {
				if leaseLost {
					return ErrBatchImageLockNotAcquired
				}
				return nil
			})
			switch failure {
			case "write_error":
				svc.UsageLogRepo = batchRecoveryUsageWriter{err: errors.New("usage unavailable")}
			case "lease_lost_after_usage":
				svc.UsageLogRepo = batchRecoveryUsageWriter{afterWrite: func() { leaseLost = true }}
			}
			_, err := svc.Settle(ctx, job.BatchID)
			require.Error(t, err)
			require.Equal(t, BatchImageJobStatusSettling, repo.jobs[job.BatchID].Status)
			require.Empty(t, billing.releases)
			// Repair and retry with the same capture identity.
			svc.UsageLogRepo = batchRecoveryUsageWriter{}
			_, err = svc.Settle(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.Equal(t, BatchImageJobStatusCompleted, repo.jobs[job.BatchID].Status)
			require.Len(t, billing.seen, 1)
		})
	}
}
