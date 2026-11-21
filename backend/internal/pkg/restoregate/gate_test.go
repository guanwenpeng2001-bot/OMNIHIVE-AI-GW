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
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillDelayFor(20 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	gate := New(db)
	const requests = 64
	var callers sync.WaitGroup
	callers.Add(requests)
	start := make(chan struct{})
	for range requests {
		go func() {
			defer callers.Done()
			<-start
			if gate.Enter(context.Background()) {
				gate.Leave()
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
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnError(&pq.Error{Code: "42P01"})
	_, leave, err := gate.Admit(t.Context())
	require.NoError(t, err)
	defer leave()
	mock.ExpectExec("SELECT 1").WillReturnResult(sqlmock.NewResult(0, 0))
	_, err = gate.held.ExecContext(t.Context(), "SELECT 1")
	require.NoError(t, err, "bootstrap query must use a pooled connection, not the fencing transaction")
}

func TestEnterRechecksJournalWhenCachedLockIsReleased(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	gate := New(db)
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	require.NoError(t, gate.enter(t.Context()))
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(true))
	reachedHold := make(chan struct{})
	releaseHold := make(chan struct{})
	gate.beforeHold = func() {
		close(reachedHold)
		<-releaseHold
	}
	entered := make(chan error, 1)
	go func() { entered <- gate.enter(t.Context()) }()
	<-reachedHold
	gate.Leave()
	close(releaseHold)
	err = <-entered
	require.ErrorIs(t, err, ErrMaintenance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterKeepsRestoreLockFailureClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(false))
	mock.ExpectRollback()
	gate := New(db)
	require.False(t, gate.Enter(t.Context()))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterConcurrentLeasesShareOneJournalLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillDelayFor(50 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
	mock.ExpectRollback()
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
	require.Equal(t, requests, gate.users, "every admitted request holds exactly one fence reference")
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
	mock.ExpectBegin()
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
		mock.ExpectBegin()
		mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
		mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(false))
		mock.ExpectRollback()
		_, leave, err := gate.Admit(t.Context())
		require.NoError(t, err)
		gate.mu.Lock()
		require.Equal(t, 1, gate.users)
		gate.mu.Unlock()
		leave()
		gate.mu.Lock()
		require.Nil(t, gate.held, "the fence must be released once the last request leaves")
		require.Zero(t, gate.users)
		gate.mu.Unlock()
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterFailedCheckHoldsNoReference(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	gate := New(db)
	mock.ExpectBegin()
	mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(true))
	mock.ExpectRollback()
	_, _, err = gate.Admit(t.Context())
	require.ErrorIs(t, err, ErrMaintenance)
	gate.mu.Lock()
	require.Nil(t, gate.held)
	require.Zero(t, gate.users)
	gate.mu.Unlock()
	require.NoError(t, mock.ExpectationsWereMet())
}
