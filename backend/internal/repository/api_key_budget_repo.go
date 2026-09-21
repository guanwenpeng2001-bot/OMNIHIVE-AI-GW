package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// This file owns the period-budget persistence: the policy projection on
// api_keys, the frozen period ledger, request-level holds and audited
// adjustments. It is deliberately separate from api_key_repo.go so the auth and
// CRUD projections stay untouched.

const apiKeyBudgetPolicyColumns = `id,budget_period_mode,budget_period_seconds,budget_period_anchor,
	budget_calendar_unit,budget_timezone,budget_limit,budget_enforced`

// LoadAPIKeyBudgetStates returns the canonical policy and current period
// counters of each requested key. Keys without a period budget are omitted.
func (r *apiKeyRepository) LoadAPIKeyBudgetStates(ctx context.Context, apiKeyIDs []int64) (map[int64]service.APIKeyBudgetState, error) {
	out := make(map[int64]service.APIKeyBudgetState, len(apiKeyIDs))
	if len(apiKeyIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, 0, len(apiKeyIDs))
	args := make([]any, 0, len(apiKeyIDs))
	for i, id := range apiKeyIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, id)
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT `+apiKeyBudgetPolicyColumns+`,budget_used,budget_reserved,budget_overrun,
			budget_period_start,budget_period_end
		FROM api_keys
		WHERE id IN (`+strings.Join(placeholders, ",")+`) AND deleted_at IS NULL`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	now := time.Now().UTC()
	for rows.Next() {
		var (
			policy      service.APIKeyBudgetPolicy
			used        float64
			reserved    float64
			overrun     float64
			periodStart *time.Time
			periodEnd   *time.Time
			apiKeyID    int64
		)
		if err := rows.Scan(&apiKeyID, &policy.Mode, &policy.DurationSeconds, &policy.Anchor,
			&policy.CalendarUnit, &policy.Timezone, &policy.LimitUSD, &policy.Enforced,
			&used, &reserved, &overrun, &periodStart, &periodEnd); err != nil {
			return nil, err
		}
		policy = policy.Normalize()
		if !policy.IsActive() {
			continue
		}
		start, end, rolled := policy.PeriodBounds(now, periodStart)
		if rolled {
			used, reserved, overrun = 0, 0, 0
		}
		out[apiKeyID] = service.NewAPIKeyBudgetState(policy, &start, &end, used, reserved, overrun)
	}
	return out, rows.Err()
}

// attachBudgetStates fills the period budget policy and counters on
// already-loaded keys in one query. It is used by every projection that can be
// served to the gateway or the panel.
func (r *apiKeyRepository) attachBudgetStates(ctx context.Context, keys []*service.APIKey) error {
	ids := make([]int64, 0, len(keys))
	for _, key := range keys {
		if key != nil && key.ID > 0 {
			ids = append(ids, key.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	states, err := r.LoadAPIKeyBudgetStates(ctx, ids)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key == nil {
			continue
		}
		state, ok := states[key.ID]
		if !ok {
			key.BudgetPolicy = service.APIKeyBudgetPolicy{Mode: service.APIKeyBudgetModeOff}
			key.BudgetState = nil
			continue
		}
		copied := state
		key.BudgetPolicy = state.Policy
		key.BudgetState = &copied
	}
	return nil
}

// SaveAPIKeyBudgetPolicy persists the canonical policy of one key. Changing the
// policy never resets consumed usage: the current period keeps its counters and
// only the limit/period definition moves.
func (r *apiKeyRepository) SaveAPIKeyBudgetPolicy(ctx context.Context, apiKeyID int64, policy service.APIKeyBudgetPolicy) error {
	policy = policy.Normalize()
	res, err := r.sql.ExecContext(ctx, `
		UPDATE api_keys SET
			budget_period_mode = $2,
			budget_period_seconds = $3,
			budget_period_anchor = $4,
			budget_calendar_unit = $5,
			budget_timezone = $6,
			budget_limit = $7,
			budget_enforced = $8,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		apiKeyID, policy.Mode, policy.DurationSeconds, policy.Anchor, policy.CalendarUnit,
		policy.EffectiveTimezoneName(), policy.LimitUSD, policy.Enforced)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

// GetAPIKeyBudgetState returns the current period counters for display. A stale
// persisted window is reported as the upcoming window with zero usage; the next
// reservation performs the actual roll.
func (r *apiKeyRepository) GetAPIKeyBudgetState(ctx context.Context, apiKeyID int64) (*service.APIKeyBudgetState, error) {
	var (
		policy      service.APIKeyBudgetPolicy
		used        float64
		reserved    float64
		overrun     float64
		periodStart *time.Time
		periodEnd   *time.Time
	)
	err := scanSingleRow(ctx, r.sql, `
		SELECT `+apiKeyBudgetPolicyColumns+`,budget_used,budget_reserved,budget_overrun,
			budget_period_start,budget_period_end
		FROM api_keys WHERE id = $1 AND deleted_at IS NULL`, []any{apiKeyID},
		&apiKeyID, &policy.Mode, &policy.DurationSeconds, &policy.Anchor, &policy.CalendarUnit,
		&policy.Timezone, &policy.LimitUSD, &policy.Enforced,
		&used, &reserved, &overrun, &periodStart, &periodEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	policy = policy.Normalize()
	if policy.IsActive() {
		start, end, rolled := policy.PeriodBounds(time.Now().UTC(), periodStart)
		if rolled {
			used, reserved, overrun = 0, 0, 0
		}
		periodStart, periodEnd = &start, &end
	} else {
		// Turning a budget off must not look like clearing consumed usage: the
		// recorded counters stay visible until an audited adjustment moves them.
		periodStart, periodEnd = nil, nil
	}
	state := service.NewAPIKeyBudgetState(policy, periodStart, periodEnd, used, reserved, overrun)
	return &state, nil
}

// AdjustAPIKeyBudgetUsage records an audited administrative adjustment and moves
// the current period usage by the same delta in one statement.
func (r *apiKeyRepository) AdjustAPIKeyBudgetUsage(ctx context.Context, cmd *service.KeyBudgetAdjustmentCommand) (*service.APIKeyBudgetState, error) {
	if cmd == nil {
		return nil, errors.New("budget adjustment command is nil")
	}
	copy := *cmd
	copy.Normalize()
	var (
		used      float64
		overrun   float64
		limit     float64
		reserved  float64
		periodEnd *time.Time
	)
	err := scanSingleRow(ctx, r.sql, `
		WITH adjustment AS (
			INSERT INTO api_key_budget_adjustments (api_key_id, actor_user_id, delta_usd, reason)
			VALUES ($1, $2, $3, $4)
		),
		current AS (
			SELECT id, budget_limit, budget_reserved, budget_period_end, budget_overrun,
				GREATEST(budget_used + $3, 0) AS raw_used
			FROM api_keys
			WHERE id = $1 AND deleted_at IS NULL
			FOR NO KEY UPDATE
		)
		UPDATE api_keys a SET
			budget_used = CASE
				WHEN c.budget_limit > 0 AND c.raw_used > c.budget_limit THEN c.budget_limit
				ELSE c.raw_used
			END,
			budget_overrun = CASE
				WHEN c.budget_limit > 0 AND c.raw_used > c.budget_limit
				THEN c.budget_overrun + (c.raw_used - c.budget_limit)
				ELSE c.budget_overrun
			END,
			updated_at = NOW()
		FROM current c
		WHERE a.id = c.id
		RETURNING a.budget_used, a.budget_overrun, a.budget_limit, a.budget_reserved, a.budget_period_end`,
		[]any{copy.APIKeyID, copy.ActorUserID, copy.DeltaUSD, copy.Reason},
		&used, &overrun, &limit, &reserved, &periodEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	state, err := r.GetAPIKeyBudgetState(ctx, copy.APIKeyID)
	if err != nil {
		return nil, err
	}
	state.UsedUSD = used
	state.OverrunUSD = overrun
	state.ReservedUSD = reserved
	state.RemainingUSD = state.RemainingAfterReservations()
	return state, nil
}

// budgetRow is the locked api_keys budget projection used by reservations.
type budgetRow struct {
	apiKeyID    int64
	policy      service.APIKeyBudgetPolicy
	used        float64
	reserved    float64
	overrun     float64
	periodStart *time.Time
	periodEnd   *time.Time
}

func lockBudgetRow(ctx context.Context, tx *sql.Tx, apiKeyID int64) (*budgetRow, error) {
	var row budgetRow
	err := tx.QueryRowContext(ctx, `
		SELECT `+apiKeyBudgetPolicyColumns+`,budget_used,budget_reserved,budget_overrun,
			budget_period_start,budget_period_end
		FROM api_keys WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, apiKeyID).
		Scan(&row.apiKeyID, &row.policy.Mode, &row.policy.DurationSeconds, &row.policy.Anchor,
			&row.policy.CalendarUnit, &row.policy.Timezone, &row.policy.LimitUSD, &row.policy.Enforced,
			&row.used, &row.reserved, &row.overrun, &row.periodStart, &row.periodEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	row.policy = row.policy.Normalize()
	return &row, nil
}

// freezeBudgetPeriod writes the closing counters of the previous window into the
// period ledger and starts a new window. Frozen rows are never zeroed later.
func freezeBudgetPeriod(ctx context.Context, tx *sql.Tx, row *budgetRow, start, end time.Time) error {
	if row.periodStart != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO api_key_budget_periods
				(api_key_id, period_start, period_end, limit_amount, used_amount, overrun_amount, closed_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (api_key_id, period_start) DO UPDATE SET
				used_amount = EXCLUDED.used_amount,
				overrun_amount = EXCLUDED.overrun_amount,
				limit_amount = EXCLUDED.limit_amount,
				closed_at = NOW(),
				updated_at = NOW()`,
			row.apiKeyID, row.periodStart.UTC(), row.periodEnd.UTC(), row.policy.LimitUSD, row.used, row.overrun); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			budget_used = 0,
			budget_reserved = 0,
			budget_overrun = 0,
			budget_period_start = $2,
			budget_period_end = $3,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, row.apiKeyID, start.UTC(), end.UTC()); err != nil {
		return err
	}
	row.used, row.reserved, row.overrun = 0, 0, 0
	row.periodStart, row.periodEnd = &start, &end
	return nil
}

// ReserveKeyBudget takes one atomic request-level hold.
//
// Lock order is users -> api_keys -> reservation, and settlement/release use the
// same order (reservation -> users -> api_keys). Consistency matters: a hold and
// a settlement of the same payer must never deadlock, and no request may overdraw
// the budget or the payer balance.
func (r *usageSettlementReceiptRepository) ReserveKeyBudget(ctx context.Context, cmd *service.KeyBudgetReservationCommand) (*service.KeyBudgetReservationOutcome, error) {
	if cmd == nil {
		return nil, service.ErrAPIKeyBudgetInvalid
	}
	copy := *cmd
	copy.Normalize()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Any early return discards the balance hold below, which is exactly the
	// wanted outcome when the key turns out to have no active period budget.
	defer func() { _ = tx.Rollback() }()

	hold := copy.HoldAmount
	if hold < 0 {
		hold = 0
	}
	balanceHeld := !copy.SkipBalanceHold
	if hold > 0 && balanceHeld {
		// The hold is real money: deduct it from the payer balance now so a
		// concurrent request cannot spend the same balance twice.
		res, err := tx.ExecContext(ctx, `
			UPDATE users SET balance = balance - $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL AND balance >= $1`, hold, copy.UserID)
		if err != nil {
			return nil, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			exists, existsErr := userExistsForBilling(ctx, tx, copy.UserID)
			if existsErr != nil {
				return nil, existsErr
			}
			if !exists {
				return nil, service.ErrUserNotFound
			}
			return nil, service.ErrAPIKeyBudgetInsufficientBalance
		}
	}

	row, err := lockBudgetRow(ctx, tx, copy.APIKeyID)
	if err != nil {
		return nil, err
	}
	if !row.policy.IsActive() {
		return &service.KeyBudgetReservationOutcome{LimitUSD: 0}, nil
	}
	start, end, rolled := row.policy.PeriodBounds(copy.Now, row.periodStart)
	if rolled {
		if err := freezeBudgetPeriod(ctx, tx, row, start, end); err != nil {
			return nil, err
		}
	}

	// Idempotency: the same request can never hold twice.
	var existingID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM api_key_budget_reservations
		WHERE api_key_id = $1 AND request_id = $2`, copy.APIKeyID, copy.RequestID).Scan(&existingID)
	if err == nil {
		return &service.KeyBudgetReservationOutcome{
			Duplicate:   true,
			LimitUSD:    row.policy.LimitUSD,
			UsedUSD:     row.used,
			ReservedUSD: row.reserved,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if row.policy.LimitUSD > 0 && row.used+row.reserved+hold > row.policy.LimitUSD+1e-9 {
		return nil, service.ErrAPIKeyBudgetExceeded
	}
	if hold > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_keys SET budget_reserved = budget_reserved + $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL`, hold, copy.APIKeyID); err != nil {
			return nil, err
		}
	}

	var reservationID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO api_key_budget_reservations
			(api_key_id, user_id, request_id, hold_amount, balance_held, status, period_start, period_end, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'reserved', $6, $7, $8)
		RETURNING id`,
		copy.APIKeyID, copy.UserID, copy.RequestID, hold, balanceHeld, start.UTC(), end.UTC(),
		copy.Now.Add(copy.TTL).UTC()).Scan(&reservationID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.KeyBudgetReservationOutcome{
		Reservation: &service.KeyBudgetReservation{
			ID:          reservationID,
			RequestID:   copy.RequestID,
			APIKeyID:    copy.APIKeyID,
			UserID:      copy.UserID,
			HoldAmount:  hold,
			BalanceHeld: balanceHeld,
			PeriodStart: start.UTC(),
			PeriodEnd:   end.UTC(),
		},
		LimitUSD:    row.policy.LimitUSD,
		UsedUSD:     row.used,
		ReservedUSD: row.reserved,
	}, nil
}

// ReleaseKeyBudgetReservation refunds a hold that will never settle. It is a
// state transition, so replaying it is a no-op.
func (r *usageSettlementReceiptRepository) ReleaseKeyBudgetReservation(ctx context.Context, apiKeyID int64, requestID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	released, err := releaseKeyBudgetReservationTx(ctx, tx, apiKeyID, requestID)
	if err != nil {
		return err
	}
	if !released {
		return nil
	}
	return tx.Commit()
}

func releaseKeyBudgetReservationTx(ctx context.Context, tx *sql.Tx, apiKeyID int64, requestID string) (bool, error) {
	var (
		id          int64
		userID      int64
		hold        float64
		balanceHeld bool
		status      string
		periodStart *time.Time
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id, hold_amount, balance_held, status, period_start
		FROM api_key_budget_reservations
		WHERE api_key_id = $1 AND request_id = $2
		FOR UPDATE`, apiKeyID, requestID).Scan(&id, &userID, &hold, &balanceHeld, &status, &periodStart)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status != keyBudgetReservationStatusReserved {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_key_budget_reservations
		SET status = 'released', updated_at = NOW()
		WHERE id = $1`, id); err != nil {
		return false, err
	}
	if hold > 0 && balanceHeld {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL`, hold, userID); err != nil {
			return false, err
		}
	}
	if hold > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_keys SET budget_reserved = GREATEST(budget_reserved - $1, 0), updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
				AND budget_period_start IS NOT DISTINCT FROM $3`,
			hold, apiKeyID, periodStart); err != nil {
			return false, err
		}
	}
	return true, nil
}

const keyBudgetReservationStatusReserved = "reserved"

// ReleaseExpiredKeyBudgetReservations refunds abandoned holds so a crashed
// process cannot strand budget or balance.
func (r *usageSettlementReceiptRepository) ReleaseExpiredKeyBudgetReservations(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT api_key_id, request_id
		FROM api_key_budget_reservations
		WHERE status = 'reserved' AND expires_at < $1
		ORDER BY id
		LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, err
	}
	type identity struct {
		apiKeyID  int64
		requestID string
	}
	pending := make([]identity, 0, limit)
	for rows.Next() {
		var item identity
		if err := rows.Scan(&item.apiKeyID, &item.requestID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	released := 0
	for _, item := range pending {
		if err := r.ReleaseKeyBudgetReservation(ctx, item.apiKeyID, item.requestID); err != nil {
			slog.Warn("release expired key budget reservation",
				"api_key_id", item.apiKeyID, "error", err)
			continue
		}
		released++
	}
	return released, nil
}

// keyBudgetHold is the locked hold handed from the reservation phase to the
// budget/balance phase of one settlement transaction.
type keyBudgetHold struct {
	reservationID int64
	apiKeyID      int64
	userID        int64
	holdAmount    float64
	balanceHeld   bool
	periodStart   *time.Time
	periodEnd     *time.Time
}

// lockKeyBudgetReservation claims a live hold for the settling request. A hold
// that is absent, already released or already settled returns nil so the caller
// falls back to the ordinary balance deduction: the receipt stays authoritative.
func lockKeyBudgetReservation(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (*keyBudgetHold, error) {
	if cmd == nil || cmd.BudgetReservationID <= 0 {
		return nil, nil
	}
	hold := &keyBudgetHold{}
	var status string
	err := tx.QueryRowContext(ctx, `
		SELECT id, api_key_id, user_id, hold_amount, balance_held, status, period_start, period_end
		FROM api_key_budget_reservations
		WHERE id = $1 AND api_key_id = $2
		FOR UPDATE`, cmd.BudgetReservationID, cmd.APIKeyID).
		Scan(&hold.reservationID, &hold.apiKeyID, &hold.userID, &hold.holdAmount, &hold.balanceHeld, &status, &hold.periodStart, &hold.periodEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status != keyBudgetReservationStatusReserved {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_key_budget_reservations
		SET status = 'settled', settled_amount = $2, updated_at = NOW()
		WHERE id = $1`, hold.reservationID, cmd.BalanceCost); err != nil {
		return nil, err
	}
	return hold, nil
}

// settleKeyBudgetCounters attributes the settled amount to the period it belongs
// to. A period that already rolled is frozen with its own ledger row, so late
// settlement is never dropped and never rewrites a closed period.
//
// A settlement without a live hold (released, expired or never reserved) still
// charges the key's current period: the actual cost must never be dropped from
// the budget just because the hold is gone.
func settleKeyBudgetCounters(ctx context.Context, tx *sql.Tx, hold *keyBudgetHold, apiKeyID int64, actual float64, result *service.UsageBillingApplyResult) error {
	if hold == nil {
		return chargeKeyBudgetPeriodWithoutHold(ctx, tx, apiKeyID, actual, result)
	}
	var (
		currentStart *time.Time
		limit        float64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT budget_period_start, budget_limit
		FROM api_keys WHERE id = $1 AND deleted_at IS NULL
		FOR NO KEY UPDATE`, hold.apiKeyID).Scan(&currentStart, &limit)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAPIKeyNotFound
	}
	if err != nil {
		return err
	}
	samePeriod := hold.periodStart != nil && currentStart != nil && hold.periodStart.UTC().Equal(currentStart.UTC())
	if samePeriod {
		var (
			used    float64
			overrun float64
		)
		if err := tx.QueryRowContext(ctx, `
			UPDATE api_keys SET
				budget_reserved = GREATEST(budget_reserved - $2, 0),
				budget_used = CASE
					WHEN budget_limit > 0 AND budget_used + $3 > budget_limit THEN budget_limit
					ELSE budget_used + $3
				END,
				budget_overrun = CASE
					WHEN budget_limit > 0 AND budget_used + $3 > budget_limit
					THEN budget_overrun + (budget_used + $3 - budget_limit)
					ELSE budget_overrun
				END,
				updated_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING budget_used, budget_overrun`,
			hold.apiKeyID, hold.holdAmount, actual).Scan(&used, &overrun); err != nil {
			return err
		}
		result.BudgetReservationSettled = true
		result.BudgetOverrun = overrun
		return nil
	}
	// Frozen period: record the settled amount against the period that owned the
	// hold, clamped by that period's limit, with the excess kept as overrun.
	if hold.periodStart == nil || hold.periodEnd == nil {
		return nil
	}
	var frozenLimit float64
	err = tx.QueryRowContext(ctx, `
		SELECT limit_amount FROM api_key_budget_periods
		WHERE api_key_id = $1 AND period_start = $2`, hold.apiKeyID, hold.periodStart.UTC()).Scan(&frozenLimit)
	if errors.Is(err, sql.ErrNoRows) {
		frozenLimit = limit
	} else if err != nil {
		return err
	}
	var (
		used    float64
		overrun float64
	)
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO api_key_budget_periods
			(api_key_id, period_start, period_end, limit_amount, used_amount, overrun_amount, closed_at)
		VALUES (
			$1, $2, $3, $4::numeric,
			CASE WHEN $4::numeric > 0 AND $5::numeric > $4::numeric THEN $4::numeric ELSE $5::numeric END,
			CASE WHEN $4::numeric > 0 AND $5::numeric > $4::numeric THEN $5::numeric - $4::numeric ELSE 0 END,
			NOW())
		ON CONFLICT (api_key_id, period_start) DO UPDATE SET
			used_amount = CASE
				WHEN api_key_budget_periods.limit_amount > 0
					AND api_key_budget_periods.used_amount + $5::numeric > api_key_budget_periods.limit_amount
				THEN api_key_budget_periods.limit_amount
				ELSE api_key_budget_periods.used_amount + $5::numeric
			END,
			overrun_amount = CASE
				WHEN api_key_budget_periods.limit_amount > 0
					AND api_key_budget_periods.used_amount + $5::numeric > api_key_budget_periods.limit_amount
				THEN api_key_budget_periods.overrun_amount + (api_key_budget_periods.used_amount + $5::numeric - api_key_budget_periods.limit_amount)
				ELSE api_key_budget_periods.overrun_amount
			END,
			updated_at = NOW()
		RETURNING used_amount, overrun_amount`,
		hold.apiKeyID, hold.periodStart.UTC(), hold.periodEnd.UTC(), frozenLimit, actual).Scan(&used, &overrun); err != nil {
		return err
	}
	result.BudgetReservationSettled = true
	result.BudgetOverrun = overrun
	return nil
}

// chargeKeyBudgetPeriodWithoutHold records an actual cost whose request hold is
// gone. It only touches keys that really have an active period budget, clamps at
// the limit and keeps the excess as overrun instead of dropping it.
func chargeKeyBudgetPeriodWithoutHold(ctx context.Context, tx *sql.Tx, apiKeyID int64, actual float64, result *service.UsageBillingApplyResult) error {
	if apiKeyID <= 0 || actual <= 0 {
		return nil
	}
	var (
		used    float64
		overrun float64
	)
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys SET
			budget_used = CASE
				WHEN budget_limit > 0 AND budget_used + $2 > budget_limit THEN budget_limit
				ELSE budget_used + $2
			END,
			budget_overrun = CASE
				WHEN budget_limit > 0 AND budget_used + $2 > budget_limit
				THEN budget_overrun + (budget_used + $2 - budget_limit)
				ELSE budget_overrun
			END,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
			AND budget_period_mode <> 'off' AND budget_limit > 0
		RETURNING budget_used, budget_overrun`, apiKeyID, actual).Scan(&used, &overrun)
	if errors.Is(err, sql.ErrNoRows) {
		// No active period budget: nothing to record, the legacy quota path owns it.
		return nil
	}
	if err != nil {
		return err
	}
	result.BudgetOverrun = overrun
	return nil
}

// refundKeyBudgetHold returns the held balance before the actual cost is applied.
func refundKeyBudgetHold(ctx context.Context, tx *sql.Tx, hold *keyBudgetHold) error {
	if hold == nil || hold.holdAmount <= 0 || !hold.balanceHeld {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE users SET balance = balance + $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL`, hold.holdAmount, hold.userID)
	return err
}

// billingBalanceUncoveredTotal counts money that the payer balance could not
// cover. It is an ops signal, not a silent write-off: the usage row and the
// receipt keep the full actual cost.
var billingBalanceUncoveredTotal atomic.Uint64

func observeBillingBalanceUncovered(amount float64) {
	if amount <= 0 {
		return
	}
	billingBalanceUncoveredTotal.Add(1)
	slog.Warn("billing balance uncovered; actual usage kept on the books",
		"uncovered_amount", amount, "total_events", billingBalanceUncoveredTotal.Load())
}

// recordKeyBudgetUncovered persists the part of the actual cost the payer
// balance could not cover, so the shortfall is on the books instead of being
// silently dropped or written as an unconditional overdraft.
func recordKeyBudgetUncovered(ctx context.Context, tx *sql.Tx, hold *keyBudgetHold, uncovered float64) error {
	if hold == nil || uncovered <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE api_key_budget_reservations
		SET uncovered_amount = uncovered_amount + $2, updated_at = NOW()
		WHERE id = $1`, hold.reservationID, uncovered)
	return err
}
