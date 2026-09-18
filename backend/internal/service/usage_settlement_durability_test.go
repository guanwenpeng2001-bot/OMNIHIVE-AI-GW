package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Simulate the two commit boundaries independently, including lost ACKs.
type durableReceiptStore struct {
	UsageSettlementReceiptRepository
	mu                               sync.Mutex
	row                              *UsageSettlementReceipt
	ack                              *UsageSettlementReceipt
	persistErr, lookupErr, settleErr error
	commitBeforeError                bool
	applied                          int
	contexts                         []context.Context
}

func (r *durableReceiptStore) Persist(ctx context.Context, in *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contexts = append(r.contexts, ctx)
	if r.persistErr != nil && !r.commitBeforeError {
		return nil, r.persistErr
	}
	if r.row == nil {
		copy := *in
		copy.ID = 1
		r.row = &copy
	}
	copy := *r.row
	r.ack = &copy
	return r.ack, r.persistErr
}
func (r *durableReceiptStore) Get(ctx context.Context, key UsageSettlementKey) (*UsageSettlementReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contexts = append(r.contexts, ctx)
	if r.lookupErr != nil {
		return nil, r.lookupErr
	}
	if r.row == nil || r.row.Key != key {
		return nil, ErrUsageSettlementNotFound
	}
	copy := *r.row
	return &copy, nil
}
func (r *durableReceiptStore) Settle(_ context.Context, key UsageSettlementKey, fingerprint string) (*UsageSettlementResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settleErr != nil {
		return nil, r.settleErr
	}
	if r.row == nil || r.row.Key != key || r.row.Command.RequestFingerprint != fingerprint {
		return nil, ErrUsageBillingRequestConflict
	}
	applied := r.row.State == UsageSettlementPending
	if applied {
		r.applied++
		r.row.State = UsageSettlementSettled
	}
	copy := *r.row
	return &UsageSettlementResult{Receipt: &copy, Billing: &UsageBillingApplyResult{Applied: applied}}, nil
}
func (r *durableReceiptStore) ListPending(_ context.Context, cursor int64, _ int) ([]*UsageSettlementReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.row == nil || r.row.ID <= cursor || r.row.State != UsageSettlementPending {
		return nil, nil
	}
	copy := *r.row
	return []*UsageSettlementReceipt{&copy}, nil
}

func durableReceiptFixture() *UsageSettlementReceipt {
	return &UsageSettlementReceipt{
		Key:             UsageSettlementKey{RequestID: "durable", UserID: 1, APIKeyID: 2},
		Command:         UsageBillingCommand{RequestID: "durable", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "m"},
		Usage:           UsageLog{RequestID: "durable", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "m", PricingState: PricingStateFree, PricingSnapshot: `{"pricing_state":"free","unit_price":0,"units":1,"rate_multiplier":1}`},
		ResolutionState: "resolved", CompletionState: "complete",
	}
}

func TestSettlementPersistenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		commit, readFails, writeFails bool
		wantError                     bool
	}{
		{"cancelled request", false, false, false, false},
		{"commit ACK lost", true, false, true, false},
		{"before commit failure", false, false, true, true},
		{"commit outcome unknown", true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &durableReceiptStore{commitBeforeError: tc.commit}
			if tc.writeFails {
				repo.persistErr = errors.New("write ACK lost")
			}
			if tc.readFails {
				repo.lookupErr = errors.New("database unavailable")
			}
			svc := NewUsageSettlementService(repo, nil, nil)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := svc.Submit(ctx, durableReceiptFixture(), nil)
			if tc.wantError {
				require.ErrorContains(t, err, "persistence unconfirmed")
				require.Equal(t, uint64(1), svc.persistFailures.Load())
				require.Zero(t, repo.applied)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, repo.applied)
			}
			for _, used := range repo.contexts {
				deadline, ok := used.Deadline()
				require.True(t, ok)
				require.WithinDuration(t, time.Now(), deadline, time.Minute)
			}
			if tc.commit {
				repo.lookupErr = nil
				restarted := NewUsageSettlementService(repo, nil, nil)
				require.NoError(t, restarted.Recover(context.Background()))
				require.NoError(t, restarted.Recover(context.Background()))
				require.Equal(t, 1, repo.applied)
			}
		})
	}
}

func TestSettlementQueueUsesDurableIdentityAndDrains(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, OverflowPolicy: "drop"})
	entered, release := make(chan struct{}), make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	repo := &durableReceiptStore{}
	svc := NewUsageSettlementService(repo, pool, nil)
	require.NoError(t, svc.Submit(context.Background(), durableReceiptFixture(), nil))
	require.NotNil(t, repo.row, "commit must precede enqueue")
	require.Zero(t, repo.applied)
	// Mutating the acknowledgement must never redirect the queued charge.
	repo.ack.Key.RequestID = "wrong"
	repo.ack.Command.RequestFingerprint = "wrong"
	close(release)
	pool.Stop()
	require.Equal(t, 1, repo.applied)
	require.NoError(t, svc.Recover(context.Background()))
	require.Equal(t, 1, repo.applied)
}

func TestSettlementFullAndStoppedPoolRetainsRecoverableReceipt(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "stopped"}[stopped], func(t *testing.T) {
			pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, OverflowPolicy: "drop"})
			if stopped {
				pool.Stop()
			} else {
				entered, release := make(chan struct{}), make(chan struct{})
				pool.Submit(func(context.Context) { close(entered); <-release })
				<-entered
				pool.Submit(func(context.Context) {})
				defer func() { close(release); pool.Stop() }()
			}
			repo := &durableReceiptStore{settleErr: errors.New("settlement DB unavailable")}
			svc := NewUsageSettlementService(repo, pool, nil)
			require.NoError(t, svc.Submit(context.Background(), durableReceiptFixture(), nil))
			require.Equal(t, UsageSettlementPending, repo.row.State)
			require.Zero(t, svc.persistFailures.Load())
			repo.settleErr = nil
			restarted := NewUsageSettlementService(repo, nil, nil)
			require.NoError(t, restarted.Recover(context.Background()))
			require.NoError(t, restarted.Recover(context.Background()))
			require.Equal(t, 1, repo.applied)
		})
	}
}

type invalidReceiptAcknowledgement struct {
	*durableReceiptStore
	alter func(*UsageSettlementReceipt) *UsageSettlementReceipt
}

func (r *invalidReceiptAcknowledgement) Persist(ctx context.Context, input *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	stored, err := r.durableReceiptStore.Persist(ctx, input)
	if err != nil {
		return nil, err
	}
	return r.alter(stored), nil
}

func TestSettlementRejectsInvalidPersistenceAcknowledgement(t *testing.T) {
	for name, alter := range map[string]func(*UsageSettlementReceipt) *UsageSettlementReceipt{
		"missing":     func(*UsageSettlementReceipt) *UsageSettlementReceipt { return nil },
		"wrong owner": func(r *UsageSettlementReceipt) *UsageSettlementReceipt { r.Key.UserID++; return r },
		"wrong fingerprint": func(r *UsageSettlementReceipt) *UsageSettlementReceipt {
			r.Command.RequestFingerprint = "other"
			return r
		},
		"unknown state": func(r *UsageSettlementReceipt) *UsageSettlementReceipt { r.State = "unknown"; return r },
	} {
		t.Run(name, func(t *testing.T) {
			repo := &invalidReceiptAcknowledgement{durableReceiptStore: &durableReceiptStore{}, alter: alter}
			svc := NewUsageSettlementService(repo, nil, nil)
			require.Error(t, svc.Submit(context.Background(), durableReceiptFixture(), func(context.Context, *UsageSettlementResult) { t.Error("unconfirmed receipt finalized") }))
			require.Zero(t, repo.applied)
		})
	}
}
