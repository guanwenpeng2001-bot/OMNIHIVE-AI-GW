//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUsageSettlementReceiptPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated job_test PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(32)
	var database string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.Equal(t, "job_test", database, "refusing to modify any non-test database")
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, db))
	_, err = db.Exec(`CREATE TABLE receipt_test_effects (usage_log_id BIGINT PRIMARY KEY, request_id TEXT NOT NULL)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DROP TABLE receipt_test_effects`)
		require.NoError(t, err)
	})
	effect := func(ctx context.Context, tx *sql.Tx, usage *service.UsageLog) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO receipt_test_effects(usage_log_id,request_id) VALUES($1,$2)`, usage.ID, usage.RequestID)
		return err
	}
	repo := NewUsageSettlementReceiptRepository(db, effect)
	fixture := func(t *testing.T, amount float64) *service.UsageSettlementReceipt {
		t.Helper()
		var userID, keyID, accountID int64
		require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@example.test").Scan(&userID))
		require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'receipt') RETURNING id`, userID, uuid.NewString()).Scan(&keyID))
		require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('receipt','qianwen','apikey') RETURNING id`).Scan(&accountID))
		state := service.PricingStatePriced
		if amount == 0 {
			state = service.PricingStateFree
		}
		snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: state, Model: "receipt-model", Provider: "qianwen", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: service.QuantizeUsageBillingAmount(amount)})
		require.NoError(t, err)
		requestID := uuid.NewString()
		return &service.UsageSettlementReceipt{
			Key:             service.UsageSettlementKey{RequestID: requestID, UserID: userID, APIKeyID: keyID},
			Command:         service.UsageBillingCommand{RequestID: requestID, APIKeyID: keyID, UserID: userID, AccountID: accountID, AccountType: service.AccountTypeAPIKey, Model: "receipt-model", BalanceCost: amount, APIKeyQuotaCost: amount, APIKeyRateLimitCost: amount},
			Usage:           service.UsageLog{RequestID: requestID, UserID: userID, APIKeyID: keyID, AccountID: accountID, Model: "receipt-model", TotalCost: amount, ActualCost: amount, RateMultiplier: 1, PricingState: state, PricingSnapshot: string(snapshot)},
			ResolutionState: "resolved", CompletionState: "complete",
		}
	}
	check := func(t *testing.T, receipt *service.UsageSettlementReceipt, settled bool) {
		t.Helper()
		var balance, quota, rate float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, receipt.Key.UserID).Scan(&balance))
		require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h FROM api_keys WHERE id=$1`, receipt.Key.APIKeyID).Scan(&quota, &rate))
		var logs, dedup, events int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, receipt.Key.RequestID, receipt.Key.APIKeyID).Scan(&logs))
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, receipt.Key.RequestID, receipt.Key.APIKeyID).Scan(&dedup))
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM receipt_test_effects WHERE request_id=$1`, receipt.Key.RequestID).Scan(&events))
		loaded, err := repo.Get(ctx, receipt.Key)
		require.NoError(t, err)
		if settled {
			amount := service.QuantizeUsageBillingAmount(receipt.Command.BalanceCost)
			require.InDelta(t, 100-amount, balance, 1e-10)
			require.Equal(t, amount, quota)
			require.Equal(t, quota, rate)
			require.Equal(t, 1, logs)
			require.Equal(t, 1, dedup)
			require.Equal(t, 1, events)
			require.Equal(t, service.UsageSettlementSettled, loaded.State)
			require.NotNil(t, loaded.UsageLogID)
			require.NotNil(t, loaded.SettledAt)
			var loggedAmount float64
			require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE id=$1`, *loaded.UsageLogID).Scan(&loggedAmount))
			require.Equal(t, amount, loggedAmount)
		} else {
			require.Equal(t, float64(100), balance)
			require.Zero(t, quota)
			require.Zero(t, rate)
			require.Zero(t, logs)
			require.Zero(t, dedup)
			require.Zero(t, events)
			require.Equal(t, service.UsageSettlementPending, loaded.State)
			require.Nil(t, loaded.UsageLogID)
			require.Nil(t, loaded.SettledAt)
		}
	}
	for _, concurrency := range []int{20, 32} {
		t.Run(fmt.Sprintf("same_identity_%d", concurrency), func(t *testing.T) {
			input := fixture(t, .000078125)
			var applied atomic.Int32
			var workers sync.WaitGroup
			errorsCh := make(chan error, concurrency)
			for range concurrency {
				workers.Add(1)
				go func() {
					defer workers.Done()
					stored, err := NewUsageSettlementReceiptRepository(db).Persist(ctx, input)
					if err != nil {
						errorsCh <- err
						return
					}
					result, err := NewUsageSettlementReceiptRepository(db, effect).Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
					if err != nil {
						errorsCh <- err
						return
					}
					if result.Billing.Applied {
						applied.Add(1)
					}
				}()
			}
			workers.Wait()
			close(errorsCh)
			for err := range errorsCh {
				require.NoError(t, err)
			}
			require.Equal(t, int32(1), applied.Load())
			check(t, input, true)
			// Lost response: a fresh repository finds the committed original ID.
			loaded, err := NewUsageSettlementReceiptRepository(db).Get(ctx, input.Key)
			require.NoError(t, err)
			require.Equal(t, input.Usage.PricingSnapshot, loaded.Usage.PricingSnapshot)
			rawCommand := input.Command
			rawCommand.Normalize()
			require.Equal(t, rawCommand.RequestFingerprint, loaded.Command.RequestFingerprint)
			chargedCommand := input.Command
			chargedCommand.BalanceCost = service.QuantizeUsageBillingAmount(chargedCommand.BalanceCost)
			chargedCommand.APIKeyQuotaCost = chargedCommand.BalanceCost
			chargedCommand.APIKeyRateLimitCost = chargedCommand.BalanceCost
			chargedCommand.Normalize()
			require.NotEqual(t, chargedCommand.RequestFingerprint, loaded.Command.RequestFingerprint)
			_, err = repo.Settle(ctx, input.Key, "different")
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			conflicting := *input
			conflicting.Command.RequestPayloadHash = "changed"
			_, err = repo.Persist(ctx, &conflicting)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			wrongOwner := input.Key
			wrongOwner.UserID++
			_, err = repo.Get(ctx, wrongOwner)
			require.ErrorIs(t, err, service.ErrUsageSettlementNotFound)
		})
	}
	for _, failure := range []string{"log_insert", "balance_update", "commit"} {
		t.Run(failure+"_rolls_back_all_effects", func(t *testing.T) {
			input := fixture(t, 1.25)
			stored, err := repo.Persist(ctx, input)
			require.NoError(t, err)
			// A deferred constraint trigger fails COMMIT itself, after the log,
			// balance and completion UPDATE statements have all succeeded.
			name := "receipt_failure_" + failure
			table, timing, condition := "usage_logs", "BEFORE INSERT", fmt.Sprintf("NEW.request_id = '%s'", input.Key.RequestID)
			if failure == "balance_update" {
				table, timing, condition = "users", "BEFORE UPDATE", fmt.Sprintf("NEW.id = %d", input.Key.UserID)
			}
			if failure == "commit" {
				table, timing, condition = "usage_settlement_receipts", "AFTER UPDATE", fmt.Sprintf("NEW.id = %d AND NEW.state = 'settled'", stored.ID)
			}
			_, err = db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'injected receipt %s failure'; END IF; RETURN NEW; END $$`, name, condition, failure))
			require.NoError(t, err)
			constraint, deferred := "", ""
			if failure == "commit" {
				constraint, deferred = "CONSTRAINT ", "DEFERRABLE INITIALLY DEFERRED"
			}
			_, err = db.Exec(fmt.Sprintf(`CREATE %sTRIGGER %s %s ON %s %s FOR EACH ROW EXECUTE FUNCTION %s()`, constraint, name, timing, table, deferred, name))
			require.NoError(t, err)
			cleanup := func() {
				_, err := db.Exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON %s`, name, table))
				require.NoError(t, err)
				_, err = db.Exec(fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, name))
				require.NoError(t, err)
			}
			t.Cleanup(cleanup)
			_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
			require.ErrorContains(t, err, "injected receipt "+failure+" failure")
			check(t, input, false)
			cleanup()
			result, err := NewUsageSettlementReceiptRepository(db, effect).Settle(ctx, input.Key, stored.Command.RequestFingerprint)
			require.NoError(t, err)
			require.True(t, result.Billing.Applied)
			check(t, input, true)
		})
	}
	t.Run("derived_event_failure_rolls_back", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		injected := errors.New("derived event insert failed")
		failing := NewUsageSettlementReceiptRepository(db, func(context.Context, *sql.Tx, *service.UsageLog) error { return injected })
		_, err = failing.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, injected)
		check(t, input, false)
	})
	t.Run("concurrent_different_fingerprints_conflict", func(t *testing.T) {
		input := fixture(t, 1)
		var accepted, conflicts atomic.Int32
		var workers sync.WaitGroup
		errorsCh := make(chan error, 32)
		for worker := range 32 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				candidate := *input
				candidate.Command.RequestPayloadHash = fmt.Sprintf("payload-%d", worker)
				stored, err := repo.Persist(ctx, &candidate)
				if errors.Is(err, service.ErrUsageBillingRequestConflict) {
					conflicts.Add(1)
					return
				}
				if err != nil {
					errorsCh <- err
					return
				}
				_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
				if err != nil {
					errorsCh <- err
					return
				}
				accepted.Add(1)
			}()
		}
		workers.Wait()
		close(errorsCh)
		for err := range errorsCh {
			require.NoError(t, err)
		}
		require.Equal(t, int32(1), accepted.Load())
		require.Equal(t, int32(31), conflicts.Load())
		check(t, input, true)
	})
	t.Run("archived_different_fingerprint_conflicts", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,'other',NOW())`, input.Key.RequestID, input.Key.APIKeyID)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		check(t, input, false)
	})
	t.Run("existing_canonical_log_conflict_rolls_back_claim", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		_, err = (&usageLogRepository{}).createSingle(ctx, db, &input.Usage)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		_, err = db.Exec(`DELETE FROM usage_logs WHERE id=$1`, input.Usage.ID)
		require.NoError(t, err)
		check(t, input, false)
	})
	t.Run("archived_identity_is_never_charged_again", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,$3,NOW())`, input.Key.RequestID, input.Key.APIKeyID, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
		var balance float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, input.Key.UserID).Scan(&balance))
		require.Equal(t, float64(100), balance)
		loaded, err := repo.Get(ctx, input.Key)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementNeedsReview, loaded.State)
		var liveDedup int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, input.Key.RequestID, input.Key.APIKeyID).Scan(&liveDedup))
		require.Zero(t, liveDedup)
	})
	t.Run("free_and_unpriced_remain_distinct", func(t *testing.T) {
		free := fixture(t, 0)
		free.CompletionState = "interrupted"
		stored, err := repo.Persist(ctx, free)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, free.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		check(t, free, true)
		for _, state := range []service.PricingState{service.PricingStateUnpriced, service.PricingStateError} {
			input := fixture(t, 0)
			input.Usage.PricingState, input.Usage.PricingSnapshot = state, ""
			input.ResolutionState = "unresolved"
			stored, err := repo.Persist(ctx, input)
			require.NoError(t, err)
			require.Equal(t, service.UsageSettlementNeedsReview, stored.State)
			_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
			require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
		}
	})
	t.Run("pending_recovery_uses_frozen_input", func(t *testing.T) {
		input := fixture(t, 3)
		input.Usage.Account = &service.Account{Credentials: map[string]any{"api_key": "must-not-persist"}}
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		require.Nil(t, stored.Usage.Account)
		require.NotNil(t, input.Usage.Account)
		pending, err := repo.ListPending(ctx, stored.ID-1, 1)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, stored.ID, pending[0].ID)
		_, err = NewUsageSettlementReceiptRepository(db, effect).Settle(ctx, pending[0].Key, pending[0].Command.RequestFingerprint)
		require.NoError(t, err)
		check(t, input, true)
	})
	t.Run("committed_response_lost_query_original_identity", func(t *testing.T) {
		input := fixture(t, 4)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		responseLost := errors.New("settlement response lost")
		call := func() error {
			_, err := repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
			if err != nil {
				return err
			}
			return responseLost
		}
		require.ErrorIs(t, call(), responseLost)
		loaded, err := NewUsageSettlementReceiptRepository(db).Get(ctx, stored.Key)
		require.NoError(t, err)
		require.Equal(t, stored.ID, loaded.ID)
		require.Equal(t, service.UsageSettlementSettled, loaded.State)
		result, err := repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		require.False(t, result.Billing.Applied)
		check(t, input, true)
	})
}
