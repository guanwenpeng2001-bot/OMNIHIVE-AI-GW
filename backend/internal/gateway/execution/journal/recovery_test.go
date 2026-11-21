package journal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRecoverAbandonedImmediatelyWithoutAttemptDeadline(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectExec("INSERT INTO gateway_execution_recovery").
		WillReturnResult(sqlmock.NewResult(0, 2))

	err = New(db).RecoverAbandoned(t.Context())

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRecoveryRetriesAsSoonAsDatabaseReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &recoveryStoreStub{fail: true}
	recovery := &Recovery{Store: store, Interval: time.Hour}
	require.Error(t, recovery.Run(ctx))
	store.fail = false
	require.NoError(t, recovery.Run(ctx))
	require.EqualValues(t, 1, store.count)
}

type recoveryStoreStub struct {
	fail  bool
	count int64
}

func (s *recoveryStoreStub) RecoverAbandoned(context.Context) error {
	if s.fail {
		return errors.New("database unavailable")
	}
	s.count++
	return nil
}

func TestRecoverAbandonedRequiresStore(t *testing.T) {
	err := New(nil).RecoverAbandoned(t.Context())
	require.ErrorIs(t, err, ErrDispatch)
	require.Error(t, (&Recovery{}).Run(t.Context()))
	require.NoError(t, (&Recovery{Store: &recoveryStoreStub{}}).Run(t.Context()))
}

func (s *recoveryStoreStub) Ping(context.Context) error { return nil }
