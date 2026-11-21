package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func waitBillingQueue(t *testing.T, q *billingAdmissionQueue, total int) {
	t.Helper()
	require.Eventually(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.total == total }, time.Second, time.Millisecond)
}

func TestBillingAdmissionFIFOAndRoundRobin(t *testing.T) {
	q := newBillingAdmission(1, 10, 5)
	first, err := q.acquire(context.Background(), 1)
	require.NoError(t, err)
	order := make(chan string, 3)
	releases := make(chan func(), 3)
	enqueue := func(payer int64, name string, total int) {
		go func() {
			release, err := q.acquire(context.Background(), payer)
			if err != nil {
				order <- err.Error()
				return
			}
			order <- name
			releases <- release
		}()
		waitBillingQueue(t, q, total)
	}
	enqueue(1, "hot-1", 2)
	enqueue(1, "hot-2", 3)
	enqueue(2, "cold", 4)
	first()
	for _, expected := range []string{"cold", "hot-1", "hot-2"} {
		select {
		case got := <-order:
			require.Equal(t, expected, got)
		case <-time.After(time.Second):
			t.Fatal("payer starved")
		}
		(<-releases)()
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	require.Empty(t, q.payers)
	require.Zero(t, q.total)
	require.Zero(t, q.active)
}

func TestBillingAdmissionBoundedHotPayerDoesNotBlockOthers(t *testing.T) {
	q := newBillingAdmission(2, 3, 2)
	first, err := q.acquire(context.Background(), 1)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := q.acquire(ctx, 1); done <- err }()
	waitBillingQueue(t, q, 2)
	_, err = q.acquire(context.Background(), 1)
	require.ErrorIs(t, err, service.ErrUsageSettlementBackpressure)
	other, err := q.acquire(context.Background(), 2)
	require.NoError(t, err)
	_, err = q.acquire(context.Background(), 3)
	require.ErrorIs(t, err, service.ErrUsageSettlementBackpressure)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	first()
	other()
	waitBillingQueue(t, q, 0)
	require.Equal(t, uint64(2), q.rejected)
}

func TestBillingAdmissionCancellationGrantRace(t *testing.T) {
	q := newBillingAdmission(1, 2, 2)
	for range 100 {
		release, err := q.acquire(context.Background(), 1)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			release, err := q.acquire(ctx, 1)
			if err == nil {
				release()
				release()
			}
			done <- err
		}()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); cancel() }()
		go func() { defer wg.Done(); release() }()
		wg.Wait()
		err = <-done
		require.True(t, err == nil || errors.Is(err, context.Canceled))
		waitBillingQueue(t, q, 0)
	}
}

func TestBillingAdmissionReservesRecoveryWithoutJumpingFIFO(t *testing.T) {
	q := newBillingAdmission(1, 2, 2)
	first, err := q.acquire(context.Background(), 1)
	require.NoError(t, err)
	live := make(chan func(), 1)
	go func() { release, _ := q.acquire(context.Background(), 1); live <- release }()
	waitBillingQueue(t, q, 2)
	_, err = q.acquire(context.Background(), 1)
	require.ErrorIs(t, err, service.ErrUsageSettlementBackpressure)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recovered := make(chan func(), 1)
	go func() { release, _ := q.acquireWithRecovery(ctx, 1, true); recovered <- release }()
	waitBillingQueue(t, q, 3)
	_, err = q.acquireWithRecovery(ctx, 1, true)
	require.ErrorIs(t, err, service.ErrUsageSettlementBackpressure)
	_, err = q.acquireWithRecovery(ctx, 2, true)
	require.ErrorIs(t, err, service.ErrUsageSettlementBackpressure, "global recovery reserve is bounded")
	first()
	liveRelease := <-live
	require.NotNil(t, liveRelease)
	select {
	case <-recovered:
		t.Fatal("recovery jumped an admitted live request")
	default:
	}
	liveRelease()
	recoveryRelease := <-recovered
	require.NotNil(t, recoveryRelease)
	recoveryRelease()
	waitBillingQueue(t, q, 0)
	require.Zero(t, q.recovery)
	// Cancellation frees the reserve for the next recovery sweep.
	first, err = q.acquire(context.Background(), 1)
	require.NoError(t, err)
	blocked, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := q.acquireWithRecovery(blocked, 1, true); done <- err }()
	waitBillingQueue(t, q, 2)
	stop()
	require.ErrorIs(t, <-done, context.Canceled)
	first()
	require.Zero(t, q.recovery)
}

func TestBillingRetriesOnlyDefinitiveAbort(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failure  error
		attempts int
	}{
		{"deadlock", &pq.Error{Code: "40P01"}, 3},
		{"serialization", fmt.Errorf("wrapped: %w", &pq.Error{Code: "40001"}), 3},
		{"ambiguous commit", errors.New("connection lost during commit"), 1},
		{"constraint", &pq.Error{Code: "23505"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			_, err := withBillingAdmission(context.Background(), 123, func(context.Context) (int, error) { attempts++; return 0, tc.failure })
			require.ErrorIs(t, err, tc.failure)
			require.Equal(t, tc.attempts, attempts)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := withBillingAdmission(ctx, 123, func(context.Context) (int, error) { cancel(); return 0, &pq.Error{Code: "40P01"} })
	require.ErrorIs(t, err, context.Canceled)
	t.Logf("injected abort retry backoff baseline: %+v", billingContentionSnapshot().RetryBackoff)
}

func TestBillingFinancialRowLockOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	for i, table := range []string{"users", "user_subscriptions", "api_keys", "accounts"} {
		mock.ExpectQuery(`SELECT id FROM ` + table + ` WHERE id=\$1 AND deleted_at IS NULL FOR NO KEY UPDATE`).WithArgs(int64(i + 1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(i + 1))
	}
	subscription := int64(2)
	require.NoError(t, lockUsageBillingEffects(context.Background(), tx, &service.UsageBillingCommand{UserID: 1, SubscriptionID: &subscription, APIKeyID: 3, AccountID: 4, AccountType: service.AccountTypeAPIKey, BalanceCost: 1, SubscriptionCost: 1, APIKeyQuotaCost: 1, AccountQuotaCost: 1}, false))
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBillingAdmissionOverlapsIndependentReceiptPreparation(t *testing.T) {
	q := newBillingAdmission(32, 1024, 64)
	first, err := q.acquire(t.Context(), 1)
	require.NoError(t, err)
	defer first()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	second, err := q.acquire(ctx, 1)
	require.NoError(t, err)
	second()
}
