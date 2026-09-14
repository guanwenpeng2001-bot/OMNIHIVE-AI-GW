//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQianwenRealtimeDistributedAdmission(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		total, user, account int
		blocked              bool
	}{{"allowed", 15, 0, 1, false}, {"global", 16, 0, 0, true}, {"user", 1, 1, 0, true}, {"account", 2, 0, 2, true}} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			j := &service.UpstreamJob{PublicRequestID: "session", Platform: service.PlatformQianwen, UserID: 1, AccountID: 2, APIKeyID: 3}
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT count\(\*\),count\(\*\) FILTER`).WithArgs(int64(1), int64(2)).WillReturnRows(sqlmock.NewRows([]string{"total", "user", "account"}).AddRow(tt.total, tt.user, tt.account))
			if tt.blocked {
				mock.ExpectRollback()
			} else {
				raw, _ := json.Marshal(j)
				mock.ExpectQuery(`INSERT INTO upstream_jobs`).WillReturnRows(sqlmock.NewRows([]string{"row_to_json"}).AddRow(raw))
				mock.ExpectCommit()
			}
			_, err = (&upstreamJobRepository{db: db}).CreateRealtimeSession(context.Background(), j)
			if tt.blocked {
				require.ErrorIs(t, err, service.ErrQianwenRealtimeCapacity)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestQianwenRealtimeAnchorAndChargeShareTransaction(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "anchor_update_rolls_back_money"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			j := &service.UpstreamJob{ID: 7, PublicRequestID: "turn", Family: "realtime_turn", Status: "running", UsageJSON: json.RawMessage(`{"characters":25}`)}
			raw, _ := json.Marshal(j)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT row_to_json\(upstreamJob\).*FOR UPDATE`).WithArgs("turn").WillReturnRows(sqlmock.NewRows([]string{"row_to_json"}).AddRow(raw))
			mock.ExpectQuery(`INSERT INTO usage_logs`).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(11, time.Now()))
			mock.ExpectQuery(conditionalBalanceDeductSQL).WithArgs(0.000325, int64(1)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(9.999675))
			update := mock.ExpectExec(`UPDATE upstream_jobs SET`)
			if fail {
				update.WillReturnError(errors.New("injected anchor update failure"))
				mock.ExpectRollback()
			} else {
				update.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			settled, err := (&upstreamJobRepository{db: db}).WithLocked(context.Background(), "turn", func(job *service.UpstreamJob, tx service.UpstreamJobTx) error {
				log := &service.UsageLog{RequestID: "turn", APIKeyID: 3, UserID: 1, AccountID: 2, Model: "qwen3-tts-flash-realtime", RateMultiplier: 1}
				if err := tx.Charge(context.Background(), log, &service.UsageBillingCommand{UserID: 1, BalanceCost: 0.000325}); err != nil {
					return err
				}
				job.ChargedUsageLogID = &log.ID
				job.Status = "succeeded"
				now := time.Now()
				job.FinishedAt = &now
				return nil
			})
			if fail {
				require.Error(t, err)
				require.Nil(t, settled)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(11), *settled.ChargedUsageLogID)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
