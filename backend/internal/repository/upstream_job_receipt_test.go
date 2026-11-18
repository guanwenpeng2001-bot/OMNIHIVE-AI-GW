//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUpstreamJobReceiptConcurrentIdentityConflict(t *testing.T) {
	for _, insertOutcome := range []string{"submission duplicate", "fingerprint race"} {
		for _, scenario := range []string{"replay", "different fingerprint", "different usage", "different price", "different status", "different owner", "different session", "different submission", "lookup failure"} {
			t.Run(insertOutcome+"/"+scenario, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })
				now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
				receipt := &service.UpstreamJob{
					PublicRequestID: "replay-public-id", SubmissionKey: "session-turn", RequestFingerprint: "session-turn",
					Platform: service.PlatformQianwen, Family: "realtime_turn", UserID: 1, APIKeyID: 2, AccountID: 3,
					UpstreamTaskID: "response-1", UsageJSON: json.RawMessage(`{ "characters": 25 }`), ResultRef: "completed",
					Status: "running", SubmittedAt: now, NextPollAt: now, ExpiresAt: now.Add(time.Hour),
					Billing: service.UpstreamJobBilling{Model: "qwen3-tts-flash-realtime", RateMultiplier: 1,
						Realtime: &service.QianwenRealtimeSnapshot{SessionID: "session", PriceVersion: service.QianwenRealtimePriceVersion}},
				}
				stored := *receipt
				stored.PublicRequestID = "winner-public-id"
				stored.ID, stored.Version = 7, 3
				stored.UsageJSON = json.RawMessage(`{"characters":25}`)
				anchor := int64(11)
				stored.Status, stored.ChargedUsageLogID, stored.FinishedAt = "succeeded", &anchor, &now
				stored.Billing.PricingState = service.PricingStatePriced
				stored.Billing.PricingSnapshot = `{"frozen":"winner"}`
				raw, err := json.Marshal(stored)
				require.NoError(t, err)
				switch scenario {
				case "different fingerprint":
					receipt.RequestFingerprint = "other-fingerprint"
				case "different usage":
					receipt.UsageJSON = json.RawMessage(`{"characters":26}`)
				case "different price":
					receipt.Billing.RateMultiplier = 2
				case "different status":
					receipt.ResultRef = "failed"
				case "different owner":
					receipt.APIKeyID++
				case "different session":
					receipt.Billing.Realtime = &service.QianwenRealtimeSnapshot{SessionID: "other-session", PriceVersion: service.QianwenRealtimePriceVersion}
				case "different submission":
					receipt.SubmissionKey = "other-submission"
				}
				violation := &pq.Error{Code: "23505", Constraint: "upstream_jobs_request_fingerprint_key"}
				insert := mock.ExpectQuery(`INSERT INTO upstream_jobs`)
				if insertOutcome == "fingerprint race" {
					insert.WillReturnError(fmt.Errorf("insert: %w", violation))
				} else {
					insert.WillReturnRows(sqlmock.NewRows([]string{"job"}))
				}
				lookupErr := errors.New("winner lookup unavailable")
				lookup := mock.ExpectQuery(`SELECT row_to_json\(upstreamJob\).*WHERE submission_key=\$1`).WithArgs(receipt.SubmissionKey)
				switch scenario {
				case "different submission":
					lookup.WillReturnRows(sqlmock.NewRows([]string{"job"}))
				case "lookup failure":
					lookup.WillReturnError(lookupErr)
				default:
					lookup.WillReturnRows(sqlmock.NewRows([]string{"job"}).AddRow(raw))
				}
				got, err := (&upstreamJobRepository{db: db}).InsertRealtimeReceipt(context.Background(), receipt)
				switch scenario {
				case "replay":
					require.NoError(t, err)
					require.Equal(t, &stored, got, "return the winner, preserving evidence and the charge anchor")
				case "different submission":
					require.Nil(t, got)
					if insertOutcome == "fingerprint race" {
						require.ErrorIs(t, err, violation, "another submission's fingerprint must remain a database conflict")
					} else {
						require.ErrorIs(t, err, service.ErrUpstreamJobNotFound)
					}
				case "lookup failure":
					require.Nil(t, got)
					require.ErrorIs(t, err, lookupErr)
				default:
					require.ErrorIs(t, err, service.ErrQianwenRealtimeConflict)
					require.Equal(t, &stored, got)
				}
			})
		}
	}
}

func TestUpstreamJobReceiptUnrelatedInsertErrors(t *testing.T) {
	for _, failure := range []error{
		&pq.Error{Code: "23505", Constraint: "upstream_jobs_public_request_id_key"},
		&pq.Error{Code: "23503", Constraint: "upstream_jobs_request_fingerprint_key"},
		errors.New("insert unavailable"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery(`INSERT INTO upstream_jobs`).WillReturnError(failure)
			receipt := &service.UpstreamJob{Family: "realtime_turn", UpstreamTaskID: "turn", UsageJSON: json.RawMessage(`{"characters":25}`),
				Billing: service.UpstreamJobBilling{Realtime: &service.QianwenRealtimeSnapshot{SessionID: "session"}}}
			got, err := (&upstreamJobRepository{db: db}).InsertRealtimeReceipt(context.Background(), receipt)
			require.Nil(t, got)
			require.ErrorIs(t, err, failure, "unrelated errors must not trigger a winner lookup")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
