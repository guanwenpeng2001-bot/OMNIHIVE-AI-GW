//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchImageWorker_ProcessesJobOnce(t *testing.T) {
	queue := newFakeBatchImageQueue("imgbatch_worker_once")
	processor := &fakeBatchImageProcessor{}
	worker := NewBatchImageWorker(queue, processor, BatchImageWorkerOptions{ReserveBlockTimeout: time.Millisecond})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, []string{"imgbatch_worker_once"}, processor.processed)
	require.Len(t, queue.requeued, 1)
	require.Equal(t, defaultBatchImageWorkerRequeueDelay, queue.requeued[0].delay)
	require.Equal(t, 1, queue.releaseCount)
}

func TestBatchImageWorker_RequeuesNonTerminalResultWithRequestedDelay(t *testing.T) {
	queue := newFakeBatchImageQueue("imgbatch_worker_requeue")
	processor := &fakeBatchImageProcessor{result: BatchImageProcessResult{RequeueAfter: 42 * time.Second}}
	worker := NewBatchImageWorker(queue, processor, BatchImageWorkerOptions{})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Len(t, queue.requeued, 1)
	require.Equal(t, "imgbatch_worker_requeue", queue.requeued[0].batchID)
	require.Equal(t, 42*time.Second, queue.requeued[0].delay)
	require.Empty(t, queue.acked)
}

func TestBatchImageWorker_AcksTerminalResult(t *testing.T) {
	queue := newFakeBatchImageQueue("imgbatch_worker_terminal")
	processor := &fakeBatchImageProcessor{result: BatchImageProcessResult{Terminal: true}}
	worker := NewBatchImageWorker(queue, processor, BatchImageWorkerOptions{})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, []string{"imgbatch_worker_terminal"}, queue.acked)
	require.Empty(t, queue.requeued)
}

// Shutdown cancellation of the parent context must not lose the completion ACK:
// the worker acknowledges exactly once through an independent short-lived
// context (BE-batch-image-ack-uses-parent-ctx).
func TestBatchImageWorker_AcksOnceAfterParentContextCancelled(t *testing.T) {
	queue := newFakeBatchImageQueue("imgbatch_worker_parent_cancelled")
	parent, cancelParent := context.WithCancel(context.Background())
	processor := &cancelParentContextProcessor{cancel: cancelParent, result: BatchImageProcessResult{Terminal: true}}
	worker := NewBatchImageWorker(queue, processor, BatchImageWorkerOptions{})

	require.NoError(t, worker.RunOnce(parent))
	require.Error(t, parent.Err(), "parent context is cancelled before completion")
	require.Equal(t, []string{"imgbatch_worker_parent_cancelled"}, queue.acked)
	require.Len(t, queue.ackContextErrors, 1)
	require.NoError(t, queue.ackContextErrors[0], "completion ACK context must stay live after parent cancellation")
}

type cancelParentContextProcessor struct {
	cancel context.CancelFunc
	result BatchImageProcessResult
}

func (p *cancelParentContextProcessor) Process(ctx context.Context, batchID string) (BatchImageProcessResult, error) {
	p.cancel()
	if err := ctx.Err(); err == nil {
		return BatchImageProcessResult{}, errors.New("process context must observe parent cancellation")
	}
	return p.result, nil
}

func TestBatchImageWorker_RequeuesOnProcessorError(t *testing.T) {
	queue := newFakeBatchImageQueue("imgbatch_worker_error")
	processor := &fakeBatchImageProcessor{err: errors.New("processor failed")}
	worker := NewBatchImageWorker(queue, processor, BatchImageWorkerOptions{ErrorRetryDelay: 7 * time.Second})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Len(t, queue.requeued, 1)
	require.Equal(t, 7*time.Second, queue.requeued[0].delay)
	require.Empty(t, queue.acked)
}

func TestBatchImageWorker_RequeuesWhenJobLockNotAcquired(t *testing.T) {
	queue := newFakeBatchImageQueue("imgbatch_worker_locked")
	queue.lockAcquired = false
	processor := &fakeBatchImageProcessor{}
	worker := NewBatchImageWorker(queue, processor, BatchImageWorkerOptions{LockConflictDelay: 3 * time.Second})

	// 锁冲突必须按冲突延迟重新入队；直接丢弃会让 job 滞留 active zset，
	// 要等 StaleActiveAfter（默认 10 分钟）才被恢复。
	require.NoError(t, worker.RunOnce(context.Background()))
	require.Empty(t, processor.processed)
	require.Len(t, queue.requeued, 1)
	require.Equal(t, 3*time.Second, queue.requeued[0].delay)
	require.Empty(t, queue.acked)
}

func TestNewBatchImageWorkerOptionsFromConfig_UsesFiniteReserveTimeout(t *testing.T) {
	opts := NewBatchImageWorkerOptionsFromConfig(nil)
	require.Equal(t, defaultBatchImageWorkerReserveBlockTimeout, opts.ReserveBlockTimeout)
	require.Positive(t, opts.ReserveBlockTimeout)
}

type fakeBatchImageQueue struct {
	reserved         ReservedBatchImageJob
	lockAcquired     bool
	acked            []string
	ackContextErrors []error
	requeued         []fakeBatchImageRequeue
	releaseCount     int
}

type fakeBatchImageRequeue struct {
	batchID string
	delay   time.Duration
}

func newFakeBatchImageQueue(batchID string) *fakeBatchImageQueue {
	return &fakeBatchImageQueue{
		reserved:     ReservedBatchImageJob{BatchID: batchID},
		lockAcquired: true,
	}
}

func (q *fakeBatchImageQueue) Enqueue(context.Context, string) error {
	return nil
}

func (q *fakeBatchImageQueue) Reserve(context.Context, time.Duration) (ReservedBatchImageJob, error) {
	return q.reserved, nil
}

func (q *fakeBatchImageQueue) RequeueAfter(_ context.Context, batchID string, delay time.Duration) error {
	q.requeued = append(q.requeued, fakeBatchImageRequeue{batchID: batchID, delay: delay})
	return nil
}

func (q *fakeBatchImageQueue) Ack(ctx context.Context, batchID string) error {
	q.acked = append(q.acked, batchID)
	q.ackContextErrors = append(q.ackContextErrors, ctx.Err())
	return nil
}

func (q *fakeBatchImageQueue) Heartbeat(context.Context, string) error {
	return nil
}

func (q *fakeBatchImageQueue) MoveDueDelayedToReady(context.Context, int) (int, error) {
	return 0, nil
}

func (q *fakeBatchImageQueue) RecoverStaleActive(context.Context, time.Duration, int) (int, error) {
	return 0, nil
}

func (q *fakeBatchImageQueue) TryAcquireJobLock(context.Context, string, time.Duration) (BatchImageJobLock, bool, error) {
	if !q.lockAcquired {
		return nil, false, nil
	}
	return fakeBatchImageLock{release: func() { q.releaseCount++ }}, true, nil
}

type fakeBatchImageLock struct {
	release func()
}

func (l fakeBatchImageLock) Release(context.Context) error {
	if l.release != nil {
		l.release()
	}
	return nil
}

type fakeBatchImageProcessor struct {
	result    BatchImageProcessResult
	err       error
	processed []string
}

func (p *fakeBatchImageProcessor) Process(_ context.Context, batchID string) (BatchImageProcessResult, error) {
	p.processed = append(p.processed, batchID)
	return p.result, p.err
}
