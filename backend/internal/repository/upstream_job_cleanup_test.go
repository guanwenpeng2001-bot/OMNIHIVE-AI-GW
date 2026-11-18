//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamJobDuePreservesCloseFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	closeErr := errors.New("rows close failed")
	rows := sqlmock.NewRows([]string{"public_request_id"}).AddRow(nil).CloseError(closeErr)
	mock.ExpectQuery("SELECT public_request_id").WillReturnRows(rows).RowsWillBeClosed()
	ids, err := (&upstreamJobRepository{db: db}).Due(context.Background(), time.Now(), 20)
	require.Nil(t, ids)
	require.ErrorContains(t, err, "converting NULL to string")
	require.ErrorIs(t, err, closeErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamJobLockedPreservesTransactionErrors(t *testing.T) {
	for _, scenario := range []string{"callback and rollback", "commit failure", "commit success"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			callbackErr := errors.New("callback failed")
			rollbackErr := errors.New("rollback failed")
			commitErr := errors.New("commit failed")
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT row_to_json").WithArgs("job").WillReturnRows(sqlmock.NewRows([]string{"job"}).AddRow(`{"id":1}`))
			if scenario == "callback and rollback" {
				mock.ExpectRollback().WillReturnError(rollbackErr)
			} else {
				mock.ExpectExec("UPDATE upstream_jobs").WillReturnResult(sqlmock.NewResult(0, 1))
				commit := mock.ExpectCommit()
				if scenario == "commit failure" {
					commit.WillReturnError(commitErr)
				}
			}
			job, err := (&upstreamJobRepository{db: db}).WithLocked(context.Background(), "job", func(*service.UpstreamJob, service.UpstreamJobTx) error {
				if scenario == "callback and rollback" {
					return callbackErr
				}
				return nil
			})
			switch scenario {
			case "callback and rollback":
				require.Nil(t, job)
				require.ErrorIs(t, err, callbackErr)
				require.ErrorIs(t, err, rollbackErr)
			case "commit failure":
				require.Nil(t, job)
				require.ErrorIs(t, err, commitErr)
			default:
				require.NoError(t, err)
				require.NotNil(t, job)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
