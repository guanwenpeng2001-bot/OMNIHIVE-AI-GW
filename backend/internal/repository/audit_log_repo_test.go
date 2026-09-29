package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAuditClearWithTraceTransaction(t *testing.T) {
	for _, failure := range []string{"", "begin", "lock", "count", "truncate", "insert", "commit"} {
		t.Run("failure_"+failure, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			repo := &auditLogRepository{db: db}
			cause := errors.New("injected failure")
			begin := mock.ExpectBegin()
			if failure == "begin" {
				begin.WillReturnError(cause)
			} else {
				steps := []string{"lock", "count", "truncate", "insert"}
				for _, step := range steps {
					if step == "count" {
						q := mock.ExpectQuery(`SELECT COUNT\(\*\) FROM audit_logs`)
						if failure == step {
							q.WillReturnError(cause)
						} else {
							q.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
						}
					} else {
						queries := map[string]string{"lock": "LOCK TABLE audit_logs IN ACCESS EXCLUSIVE MODE", "truncate": "TRUNCATE TABLE audit_logs", "insert": "INSERT INTO audit_logs"}
						e := mock.ExpectExec(queries[step])
						if step == "insert" {
							e.WithArgs(sqlmock.AnyArg(), nil, "", "", "", "", service.AuditActionAuditLogClear, "", "", "", "", "", "", 0, 0, `{"deleted_rows":7}`)
						}
						if failure == step {
							e.WillReturnError(cause)
						} else {
							e.WillReturnResult(sqlmock.NewResult(1, 1))
						}
					}
					if failure == step {
						break
					}
				}
				switch failure {
				case "":
					mock.ExpectCommit()
				case "commit":
					mock.ExpectCommit().WillReturnError(cause)
				default:
					mock.ExpectRollback()
				}
			}
			deleted, err := repo.ClearWithTrace(context.Background(), &service.AuditLog{Action: service.AuditActionAuditLogClear})
			if failure == "" {
				require.NoError(t, err)
				require.EqualValues(t, 7, deleted)
			} else {
				require.ErrorIs(t, err, cause)
				require.Zero(t, deleted)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAuditClearWithTraceRequiresTrace(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = (&auditLogRepository{db: db}).ClearWithTrace(context.Background(), nil)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
