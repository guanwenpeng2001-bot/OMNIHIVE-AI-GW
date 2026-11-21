package repository

import (
	"context"
	"errors"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// Admission is only a process-local pressure valve, never an idempotency or
// money authority. A hot payer can occupy up to four connections and at most 64
// live slots plus one recovery slot; unrelated payers execute concurrently. Waiting consumes no DB
// connection. All rejected receipts remain durable and recoverable.
var billingAdmission = newBillingAdmission(32, 1024, 64)

type billingWaiter struct {
	ready   chan struct{}
	granted bool
}

type billingPayerQueue struct {
	active   int
	ready    bool
	recovery bool
	waiters  []*billingWaiter
}

type billingAdmissionQueue struct {
	mu                            sync.Mutex
	payers                        map[int64]*billingPayerQueue
	ready                         []int64
	active, total, recovery       int
	maxActive, maxTotal, maxPayer int
	maxPayerActive                int
	rejected                      uint64
	waits                         billingSamples
}

func newBillingAdmission(active, total, payer int) *billingAdmissionQueue {
	return &billingAdmissionQueue{payers: make(map[int64]*billingPayerQueue), maxPayerActive: max(1, min(4, active/4)), maxActive: active, maxTotal: total, maxPayer: payer}
}

func (q *billingAdmissionQueue) acquire(ctx context.Context, payer int64) (func(), error) {
	return q.acquireWithRecovery(ctx, payer, service.IsUsageSettlementRecovery(ctx))
}

func (q *billingAdmissionQueue) acquireWithRecovery(ctx context.Context, payer int64, recovery bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	started := time.Now()
	q.mu.Lock()
	p := q.payers[payer]
	count := 0
	if p != nil {
		count = len(p.waiters) + p.active
	}
	full := q.total >= q.maxTotal || count >= q.maxPayer
	if recovery {
		// Live traffic cannot occupy these extra slots. Recovery joins the same
		// FIFO, so neither live nor backlog traffic can continually jump ahead.
		full = q.total >= q.maxTotal+q.maxActive || count >= q.maxPayer+1 || q.recovery >= q.maxActive || (p != nil && p.recovery)
	}
	if full {
		q.rejected++
		q.mu.Unlock()
		return nil, service.ErrUsageSettlementBackpressure
	}
	if p == nil {
		p = &billingPayerQueue{}
		q.payers[payer] = p
	}
	w := &billingWaiter{ready: make(chan struct{})}

	p.waiters = append(p.waiters, w)
	q.scheduleLocked(payer, p)
	if recovery {
		p.recovery = true
		q.recovery++
	}
	q.total++
	q.dispatchLocked()
	q.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	}
	q.mu.Lock()
	granted := w.granted
	if !granted {
		if recovery {
			p.recovery = false
			q.recovery--
		}
		for i, pending := range p.waiters {
			if pending == w {
				p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
				q.total--
				break
			}
		}
		if len(p.waiters) == 0 {
			if p.active == 0 {
				delete(q.payers, payer)
			}
			p.ready = false
			for i, id := range q.ready {
				if id == payer {
					q.ready = append(q.ready[:i], q.ready[i+1:]...)
					break
				}
			}
		}
	}
	q.waits.add(time.Since(started))
	q.mu.Unlock()
	if !granted {
		return nil, ctx.Err()
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			p.active--
			q.active--
			q.total--
			if recovery {
				p.recovery = false
				q.recovery--
			}
			if len(p.waiters) == 0 && p.active == 0 {
				delete(q.payers, payer)
			} else {
				q.scheduleLocked(payer, p)
			}
			q.dispatchLocked()
		})
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// Limit a hot payer to a fraction of global capacity. PostgreSQL still
// serializes financial rows, while receipt preparation can overlap.
func (q *billingAdmissionQueue) scheduleLocked(id int64, p *billingPayerQueue) {
	if !p.ready && len(p.waiters) > 0 && p.active < q.maxPayerActive {
		p.ready = true
		q.ready = append(q.ready, id)
	}
}

func (q *billingAdmissionQueue) dispatchLocked() {
	for q.active < q.maxActive && len(q.ready) > 0 {
		id := q.ready[0]
		q.ready = q.ready[1:]
		p := q.payers[id]
		w := p.waiters[0]
		p.waiters = p.waiters[1:]
		p.ready = false
		p.active++
		w.granted = true
		q.scheduleLocked(id, p)
		q.active++
		close(w.ready)
	}
}

// Samples are bounded, with no user/request labels or unbounded cardinality.
// Durations include SQL round-trip time; they are not server-only lock time.
type billingSamples struct {
	values [1024]float64
	count  uint64
}

func (s *billingSamples) add(d time.Duration) {
	s.values[s.count%1024] = float64(d) / float64(time.Millisecond)
	s.count++
}
func (s *billingSamples) snapshot() service.UsageSettlementLatency {
	n := min(s.count, uint64(len(s.values)))
	result := service.UsageSettlementLatency{Samples: s.count}
	if n == 0 {
		return result
	}
	values := append([]float64(nil), s.values[:n]...)
	sort.Float64s(values)
	result.P50MS, result.P95MS, result.P99MS = values[(n-1)*50/100], values[(n-1)*95/100], values[(n-1)*99/100]
	return result
}

var billingTimings struct {
	sync.Mutex
	locks, retries            billingSamples
	deadlocks, serializations uint64
}

func observeBillingLock(start time.Time) {
	billingTimings.Lock()
	defer billingTimings.Unlock()
	billingTimings.locks.add(time.Since(start))
}

func billingContentionSnapshot() *service.UsageSettlementContention {
	q := billingAdmission
	q.mu.Lock()
	result := &service.UsageSettlementContention{Active: q.active, Waiting: q.total - q.active, Rejected: q.rejected, Admission: q.waits.snapshot()}
	q.mu.Unlock()
	billingTimings.Lock()
	defer billingTimings.Unlock()
	result.LockAcquisition = billingTimings.locks.snapshot()
	result.RetryBackoff = billingTimings.retries.snapshot()
	result.Deadlocks, result.SerializationRetries = billingTimings.deadlocks, billingTimings.serializations
	return result
}

func (*usageSettlementReceiptRepository) SettlementContention() *service.UsageSettlementContention {
	return billingContentionSnapshot()
}

// Retry only PostgreSQL's definitive transaction-aborted states. In particular,
// a network error or ambiguous COMMIT must return to durable identity recovery.
func withBillingAdmission[T any](ctx context.Context, payer int64, run func(context.Context) (T, error)) (zero T, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := billingAdmission.acquire(ctx, payer)
	if err != nil {
		return zero, err
	}
	defer release()
	for attempt := 0; ; attempt++ {
		result, err := run(ctx)
		var state interface{ SQLState() string }
		if err == nil || attempt >= 2 || !errors.As(err, &state) || (state.SQLState() != "40P01" && state.SQLState() != "40001") {
			return result, err
		}
		started := time.Now()
		delay := time.Duration(10*(1<<attempt)+rand.IntN(10)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
		billingTimings.Lock()
		billingTimings.retries.add(time.Since(started))
		if state.SQLState() == "40P01" {
			billingTimings.deadlocks++
		} else {
			billingTimings.serializations++
		}
		billingTimings.Unlock()
	}
}
