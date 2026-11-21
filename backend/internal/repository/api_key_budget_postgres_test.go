//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

// Period-budget execution against a real PostgreSQL instance: admission must be
// atomic across instances, settlement must adjust to the real cost and a closed
// period must keep the spend that arrived after it rolled.
//
// Set UPSTREAM_JOB_TEST_DSN to an isolated job_test database to run this suite.
func TestKeyBudgetReservationPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	ctx := context.Background()
	database, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, ApplyMigrations(ctx, database))

	repo := &usageSettlementReceiptRepository{db: database}
	userID := createKeyBudgetTestUser(t, ctx, database, 10)

	t.Run("concurrent holds never overdraw the period budget", func(t *testing.T) {
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 1, DurationSeconds: 3600,
		})
		const workers = 24
		const hold = 0.25
		results := make([]error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, reserveErr := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
					RequestID:  fmt.Sprintf("concurrent-%d", i),
					APIKeyID:   keyID,
					UserID:     userID,
					HoldAmount: hold,
					Now:        time.Now().UTC(),
				})
				results[i] = reserveErr
			}(i)
		}
		wg.Wait()

		admitted := 0
		for _, reserveErr := range results {
			if reserveErr == nil {
				admitted++
				continue
			}
			require.ErrorIs(t, reserveErr, service.ErrAPIKeyBudgetExceeded)
		}
		require.Equal(t, 4, admitted, "a 1 USD budget admits exactly four 0.25 USD holds")

		used, reserved, limit := readKeyBudgetCounters(t, ctx, database, keyID)
		require.InDelta(t, limit, used+reserved, 1e-9, "holds must fill the budget exactly, never beyond it")
		require.LessOrEqual(t, used+reserved, limit+1e-9)
		require.InDelta(t, 10-4*hold, readUserBalance(t, ctx, database, userID), 1e-9)
	})

	t.Run("concurrent holds never overdraw the payer balance", func(t *testing.T) {
		poorUserID := createKeyBudgetTestUser(t, ctx, database, 1)
		keyID := createKeyBudgetTestKey(t, ctx, database, poorUserID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 100, DurationSeconds: 3600,
		})
		const workers = 12
		const hold = 0.3
		results := make([]error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, reserveErr := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
					RequestID:  fmt.Sprintf("balance-%d", i),
					APIKeyID:   keyID,
					UserID:     poorUserID,
					HoldAmount: hold,
					Now:        time.Now().UTC(),
				})
				results[i] = reserveErr
			}(i)
		}
		wg.Wait()

		admitted := 0
		for _, reserveErr := range results {
			if reserveErr == nil {
				admitted++
				continue
			}
			require.ErrorIs(t, reserveErr, service.ErrAPIKeyBudgetInsufficientBalance)
		}
		require.Equal(t, 3, admitted, "a 1 USD balance admits exactly three 0.3 USD holds")
		require.GreaterOrEqual(t, readUserBalance(t, ctx, database, poorUserID), 0.0, "the balance must never go negative")
	})

	t.Run("concurrent holds and settlements share one lock order", func(t *testing.T) {
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 3, DurationSeconds: 3600,
		})
		const workers = 12
		var wg sync.WaitGroup
		errs := make([]error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				outcome, reserveErr := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
					RequestID:  fmt.Sprintf("mixed-%d", i),
					APIKeyID:   keyID,
					UserID:     userID,
					HoldAmount: 0.2,
				})
				if reserveErr != nil {
					errs[i] = reserveErr
					return
				}
				if outcome.Reservation == nil {
					errs[i] = errors.New("hold missing without an error")
					return
				}
				// Half of the admitted requests settle immediately, so holds and
				// settlements contend on the same payer and key rows.
				if i%2 == 0 {
					settleKeyBudgetReservationForTest(t, ctx, database, keyID, userID, outcome.Reservation.ID, 0.15)
				}
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err == nil {
				continue
			}
			require.ErrorIs(t, err, service.ErrAPIKeyBudgetExceeded, "worker %d", i)
		}
		used, reserved, limit := readKeyBudgetCounters(t, ctx, database, keyID)
		require.LessOrEqual(t, used+reserved, limit+1e-9)
		require.GreaterOrEqual(t, readUserBalance(t, ctx, database, userID), 0.0)
	})

	t.Run("reservation identity is idempotent", func(t *testing.T) {
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 3600,
		})
		command := &service.KeyBudgetReservationCommand{RequestID: "same-request", APIKeyID: keyID, UserID: userID, HoldAmount: 0.5}
		first, err := repo.ReserveKeyBudget(ctx, command)
		require.NoError(t, err)
		require.NotNil(t, first.Reservation)

		second, err := repo.ReserveKeyBudget(ctx, command)
		require.NoError(t, err)
		require.True(t, second.Duplicate)
		require.Nil(t, second.Reservation)

		_, reserved, _ := readKeyBudgetCounters(t, ctx, database, keyID)
		require.InDelta(t, 0.5, reserved, 1e-9, "the same request must not hold twice")
	})

	t.Run("settlement adjusts the hold to the actual cost", func(t *testing.T) {
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 3600,
		})
		balanceBefore := readUserBalance(t, ctx, database, userID)
		outcome, err := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
			RequestID: "settle-1", APIKeyID: keyID, UserID: userID, HoldAmount: 0.4,
		})
		require.NoError(t, err)
		require.NotNil(t, outcome.Reservation)
		require.InDelta(t, balanceBefore-0.4, readUserBalance(t, ctx, database, userID), 1e-9)

		result := settleKeyBudgetReservationForTest(t, ctx, database, keyID, userID, outcome.Reservation.ID, 0.3)
		require.True(t, result.BudgetReservationSettled)
		require.Zero(t, result.BalanceUncovered)

		used, reserved, _ := readKeyBudgetCounters(t, ctx, database, keyID)
		require.InDelta(t, 0.3, used, 1e-9, "the period records the actual cost")
		require.InDelta(t, 0, reserved, 1e-9, "the hold is released into the actual cost")
		require.InDelta(t, balanceBefore-0.3, readUserBalance(t, ctx, database, userID), 1e-9)

		// Releasing an already settled hold must not refund it again.
		require.NoError(t, repo.ReleaseKeyBudgetReservation(ctx, keyID, "settle-1"))
		require.InDelta(t, balanceBefore-0.3, readUserBalance(t, ctx, database, userID), 1e-9)
		require.InDelta(t, 0.3, readKeyBudgetUsed(t, ctx, database, keyID), 1e-9)
	})

	t.Run("released holds fall back to the receipt deduction", func(t *testing.T) {
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 3600,
		})
		balanceBefore := readUserBalance(t, ctx, database, userID)
		outcome, err := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
			RequestID: "released-1", APIKeyID: keyID, UserID: userID, HoldAmount: 0.6,
		})
		require.NoError(t, err)
		require.NoError(t, repo.ReleaseKeyBudgetReservation(ctx, keyID, "released-1"))
		require.InDelta(t, balanceBefore, readUserBalance(t, ctx, database, userID), 1e-9, "a released hold refunds the estimate")

		result := settleKeyBudgetReservationForTest(t, ctx, database, keyID, userID, outcome.Reservation.ID, 0.25)
		require.False(t, result.BudgetReservationSettled, "a released hold is not consumed twice")
		require.InDelta(t, balanceBefore-0.25, readUserBalance(t, ctx, database, userID), 1e-9, "the actual cost is still charged")
		require.InDelta(t, 0.25, readKeyBudgetUsed(t, ctx, database, keyID), 1e-9, "the period still records the actual cost")
	})

	t.Run("late settlement lands in the frozen period", func(t *testing.T) {
		// A one second window lets the period genuinely expire, so the roll is the
		// production path rather than a hand-edited window start.
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 1,
		})
		balanceBefore := readUserBalance(t, ctx, database, userID)
		first, err := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
			RequestID: "frozen-1", APIKeyID: keyID, UserID: userID, HoldAmount: 0.5,
		})
		require.NoError(t, err)
		frozenStart := first.Reservation.PeriodStart

		time.Sleep(1100 * time.Millisecond)
		second, err := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
			RequestID: "frozen-2", APIKeyID: keyID, UserID: userID, HoldAmount: 0.5,
		})
		require.NoError(t, err)
		require.False(t, second.Reservation.PeriodStart.Equal(frozenStart), "the window rolled")

		var frozenUsed float64
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT used_amount FROM api_key_budget_periods WHERE api_key_id = $1 AND period_start = $2`,
			keyID, frozenStart).Scan(&frozenUsed))
		require.InDelta(t, 0, frozenUsed, 1e-9)

		result := settleKeyBudgetReservationForTest(t, ctx, database, keyID, userID, first.Reservation.ID, 0.4)
		require.True(t, result.BudgetReservationSettled)

		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT used_amount FROM api_key_budget_periods WHERE api_key_id = $1 AND period_start = $2`,
			keyID, frozenStart).Scan(&frozenUsed))
		require.InDelta(t, 0.4, frozenUsed, 1e-9, "late settlement is attributed to the period that owned the hold")

		used, reserved, _ := readKeyBudgetCounters(t, ctx, database, keyID)
		require.InDelta(t, 0, used, 1e-9, "the open period is not charged by a closed period's settlement")
		require.InDelta(t, 0.5, reserved, 1e-9, "the open period keeps its own hold")
		// Both holds left the balance; the closed period's hold is refunded and
		// charged at its actual cost, the open period's hold stays live.
		require.InDelta(t, balanceBefore-0.5-0.5+0.5-0.4, readUserBalance(t, ctx, database, userID), 1e-9)
	})

	t.Run("legacy day anchor still rolls at the UTC day boundary", func(t *testing.T) {
		policy, ok := service.LegacyAPIKeyBudgetPolicy(service.APIKeyLegacyBudgetAnchor1d, 10, false)
		require.True(t, ok)
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, policy)

		_, err := database.ExecContext(ctx, `
			UPDATE api_keys SET
				budget_period_start = date_trunc('day', NOW() - INTERVAL '1 day'),
				budget_period_end = date_trunc('day', NOW()),
				budget_used = 2
			WHERE id = $1`, keyID)
		require.NoError(t, err)

		outcome, err := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
			RequestID: "legacy-1d", APIKeyID: keyID, UserID: userID, HoldAmount: 0.25,
		})
		require.NoError(t, err)
		require.NotNil(t, outcome.Reservation)

		var wantStart time.Time
		require.NoError(t, database.QueryRowContext(ctx, `SELECT date_trunc('day', NOW())`).Scan(&wantStart))
		require.True(t, outcome.Reservation.PeriodStart.Equal(wantStart.UTC()),
			"the legacy 1d anchor starts at the UTC day boundary, got %s want %s", outcome.Reservation.PeriodStart, wantStart.UTC())

		var frozenUsed float64
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT used_amount FROM api_key_budget_periods WHERE api_key_id = $1 AND period_start = $2`,
			keyID, wantStart.UTC().AddDate(0, 0, -1)).Scan(&frozenUsed))
		require.InDelta(t, 2, frozenUsed, 1e-9, "the closed day keeps its spend")

		used, reserved, _ := readKeyBudgetCounters(t, ctx, database, keyID)
		require.InDelta(t, 0, used, 1e-9)
		require.InDelta(t, 0.25, reserved, 1e-9)
	})

	t.Run("expired holds are refunded by the sweeper", func(t *testing.T) {
		keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{
			Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 3600,
		})
		balanceBefore := readUserBalance(t, ctx, database, userID)
		_, err := repo.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{
			RequestID: "expired-1", APIKeyID: keyID, UserID: userID, HoldAmount: 0.7,
			TTL: time.Second, Now: time.Now().UTC().Add(-2 * time.Hour),
		})
		require.NoError(t, err)
		require.InDelta(t, balanceBefore-0.7, readUserBalance(t, ctx, database, userID), 1e-9)

		released, err := repo.ReleaseExpiredKeyBudgetReservations(ctx, time.Now().UTC(), 10)
		require.NoError(t, err)
		require.GreaterOrEqual(t, released, 1)
		require.InDelta(t, balanceBefore, readUserBalance(t, ctx, database, userID), 1e-9)
		_, reserved, _ := readKeyBudgetCounters(t, ctx, database, keyID)
		require.InDelta(t, 0, reserved, 1e-9)
	})
}

// settleKeyBudgetReservationForTest consumes a hold through the production
// billing effect path, exactly like a settlement receipt does.
func settleKeyBudgetReservationForTest(t *testing.T, ctx context.Context, database *sql.DB, apiKeyID, userID, reservationID int64, actual float64) *service.UsageBillingApplyResult {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	result := &service.UsageBillingApplyResult{Applied: true}
	require.NoError(t, (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		UserID:              userID,
		APIKeyID:            apiKeyID,
		BalanceCost:         actual,
		BudgetReservationID: reservationID,
	}, result))
	require.NoError(t, tx.Commit())
	return result
}

func createKeyBudgetTestUser(t *testing.T, ctx context.Context, database *sql.DB, balance float64) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, database.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, role, status, balance, concurrency)
		VALUES ($1, 'fixture', 'user', 'active', $2, 1)
		RETURNING id`, fmt.Sprintf("key-budget-%d@test", time.Now().UnixNano()), balance).Scan(&userID))
	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(), `DELETE FROM api_keys WHERE user_id = $1`, userID)
		_, _ = database.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

func createKeyBudgetTestKey(t *testing.T, ctx context.Context, database *sql.DB, userID int64, policy service.APIKeyBudgetPolicy) int64 {
	t.Helper()
	policy = policy.Normalize()
	var keyID int64
	require.NoError(t, database.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, name, status, budget_period_mode, budget_period_seconds,
			budget_period_anchor, budget_calendar_unit, budget_timezone, budget_limit, budget_enforced)
		VALUES ($1, $2, 'active', $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		userID, fmt.Sprintf("budget-key-%d", time.Now().UnixNano()), policy.Mode, policy.DurationSeconds,
		policy.Anchor, policy.CalendarUnit, policy.EffectiveTimezoneName(), policy.LimitUSD, policy.Enforced).Scan(&keyID))
	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(), `DELETE FROM api_keys WHERE id = $1`, keyID)
	})
	return keyID
}

func readKeyBudgetCounters(t *testing.T, ctx context.Context, database *sql.DB, apiKeyID int64) (used, reserved, limit float64) {
	t.Helper()
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT budget_used, budget_reserved, budget_limit FROM api_keys WHERE id = $1`, apiKeyID).
		Scan(&used, &reserved, &limit))
	return used, reserved, limit
}

func readKeyBudgetUsed(t *testing.T, ctx context.Context, database *sql.DB, apiKeyID int64) float64 {
	t.Helper()
	used, _, _ := readKeyBudgetCounters(t, ctx, database, apiKeyID)
	return used
}

func readUserBalance(t *testing.T, ctx context.Context, database *sql.DB, userID int64) float64 {
	t.Helper()
	var balance float64
	require.NoError(t, database.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance))
	return balance
}
