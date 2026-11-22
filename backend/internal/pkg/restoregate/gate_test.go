package restoregate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestEnterCachesClearStateAcrossConcurrentRequests(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillDelayFor(20 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	gate := New(db)
	const requests = 64
	var callers sync.WaitGroup
	callers.Add(requests)
	start := make(chan struct{})
	for range requests {
		go func() {
			defer callers.Done()
			<-start
			if held, err := gate.enter(context.Background()); err == nil {
				gate.leave(t.Context(), held)
			}
		}()
	}
	close(start)
	callers.Wait()
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterBootstrapMissingSchemaDoesNotAbortHeldTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	gate := NewBusinessPool(db)
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnError(&pq.Error{Code: "42P01"})
	_, leave, err := gate.Admit(t.Context())
	require.NoError(t, err)
	defer leave()
	mock.ExpectExec("SELECT 1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	_, err = gate.held.conn.ExecContext(t.Context(), "SELECT 1")
	require.NoError(t, err, "bootstrap query must use a pooled connection, not the fencing transaction")
}

func TestEnterRechecksJournalWhenCachedLockIsReleased(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	gate := New(db)
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	held, err := gate.enter(t.Context())
	require.NoError(t, err)
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(true))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	gate.leave(t.Context(), held)
	_, err = gate.enter(t.Context())
	require.ErrorIs(t, err, ErrMaintenance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterKeepsRestoreLockFailureClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(false))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	gate := New(db)
	_, err = gate.enter(t.Context())
	require.ErrorIs(t, err, ErrMaintenance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterConcurrentLeasesShareOneJournalLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillDelayFor(50 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	gate := New(db)
	const requests = 64
	leaves := make(chan func(), requests)
	start := make(chan struct{})
	var callers sync.WaitGroup
	callers.Add(requests)
	for range requests {
		go func() {
			defer callers.Done()
			<-start
			_, leave, err := gate.Admit(t.Context())
			require.NoError(t, err)
			leaves <- leave
		}()
	}
	close(start)
	callers.Wait()
	close(leaves)
	require.Len(t, leaves, requests)
	gate.mu.Lock()
	require.Equal(t, requests, gate.held.users, "every admitted request holds exactly one fence reference")
	gate.mu.Unlock()
	for leave := range leaves {
		leave()
	}
	gate.mu.Lock()
	require.Nil(t, gate.held)
	gate.mu.Unlock()
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterRefreshesExpiredClearStateAndDoesNotCacheBlocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Unix(100, 0)
	gate := New(db)
	gate.now = func() time.Time { return now }
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	_, leave, err := gate.Admit(t.Context())
	require.NoError(t, err)
	now = now.Add(2 * time.Second)
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(true))
	_, _, err = gate.Admit(t.Context())
	require.ErrorIs(t, err, ErrMaintenance)
	mock.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("journal unavailable"))
	_, _, err = gate.Admit(t.Context())
	require.True(t, IsUnavailable(err))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	leave()
	gate.mu.Lock()
	require.Nil(t, gate.held)
	gate.mu.Unlock()
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterReopenedFenceHoldsOneReferencePerRequest(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	gate := New(db)
	for round := 0; round < 2; round++ {
		mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
		mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
		_, leave, err := gate.Admit(t.Context())
		require.NoError(t, err)
		gate.mu.Lock()
		require.Equal(t, 1, gate.held.users)
		gate.mu.Unlock()
		leave()
		gate.mu.Lock()
		require.Nil(t, gate.held, "the fence must be released once the last request leaves")
		gate.mu.Unlock()
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterFailedCheckHoldsNoReference(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	gate := New(db)
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(true))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	_, _, err = gate.Admit(t.Context())
	require.ErrorIs(t, err, ErrMaintenance)
	gate.mu.Lock()
	require.Nil(t, gate.held)
	gate.mu.Unlock()
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRestoreGateLossCancelsExistingLeaseAndRejectsDetachedStatements(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("restore_journal.operations").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	gate := New(db)
	ctx, leave, err := gate.Admit(t.Context())
	require.NoError(t, err)

	mock.ExpectQuery("pg_catalog.pg_locks").WillReturnRows(sqlmock.NewRows([]string{"held"}).AddRow(false))
	_, err = gate.Statement(ctx)
	require.ErrorIs(t, err, errFenceLost)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("lost fence did not cancel the request")
	}
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
	leave()
	_, err = gate.Statement(context.WithoutCancel(ctx))
	require.ErrorIs(t, err, errFenceLost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRestoreGateBeginHonorsCancellation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectExec("BEGIN").WillDelayFor(time.Second).WillReturnResult(sqlmock.NewResult(0, 0))
	gate := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err = gate.Admit(ctx)
	require.True(t, IsUnavailable(err))
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.NoError(t, mock.ExpectationsWereMet())
}
