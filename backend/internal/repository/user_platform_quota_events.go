package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// NewUsageSettlementReceiptWithQuotaRepository adds the quota effect to the
// canonical receipt transaction. Any effect failure rolls back money and receipt.
func NewUsageSettlementReceiptWithQuotaRepository(db *sql.DB) service.UsageSettlementReceiptRepository {
	return NewUsageSettlementReceiptRepository(db, InsertUserPlatformQuotaEvent)
}

// InsertUserPlatformQuotaEvent records only applicable balance consumption. It
// never calls Redis and must run on the transaction which inserts the usage log.
func InsertUserPlatformQuotaEvent(ctx context.Context, tx *sql.Tx, usage *service.UsageLog) error {
	if usage == nil || usage.BillingType != service.BillingTypeBalance || usage.QuotaPlatform == "" || usage.ActualCost == 0 {
		return nil
	}
	if err := service.ValidateUsageBillingAmount(usage.ActualCost); err != nil {
		return err
	}
	if service.QuantizeUsageBillingAmount(usage.ActualCost) == 0 {
		return nil
	}
	if usage.ID <= 0 || usage.CreatedAt.IsZero() {
		return errors.New("quota event requires a persisted usage identity and consumption time")
	}
	var id, daily, weekly, monthly int64
	var dayStart, weekStart, monthStart sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id,daily_generation,weekly_generation,monthly_generation,daily_window_start,weekly_window_start,monthly_window_start
 FROM user_platform_quotas WHERE user_id=$1 AND platform=$2 AND deleted_at IS NULL AND created_at<=$3
 AND (daily_limit_usd IS NOT NULL OR weekly_limit_usd IS NOT NULL OR monthly_limit_usd IS NOT NULL) FOR UPDATE`, usage.UserID, usage.QuotaPlatform, usage.CreatedAt).
		Scan(&id, &daily, &weekly, &monthly, &dayStart, &weekStart, &monthStart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Pending events also establish a rolling-month anchor before the consumer runs.
	var pendingStart sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT max(monthly_start) FROM user_platform_quota_events WHERE quota_id=$1 AND monthly_generation=$2`, id, monthly).Scan(&pendingStart)
	if err != nil {
		return err
	}
	if pendingStart.Valid && (!monthStart.Valid || pendingStart.Time.After(monthStart.Time)) {
		monthStart = pendingStart
	}
	at := usage.CreatedAt
	day, week := timezone.StartOfDay(at), timezone.StartOfWeek(at)
	if dayStart.Valid && dayStart.Time.After(day) && !dayStart.Time.After(at) {
		day = dayStart.Time
	}
	if weekStart.Valid && weekStart.Time.After(week) && !weekStart.Time.After(at) {
		week = weekStart.Time
	}
	anchor := at
	if monthStart.Valid && !at.Before(monthStart.Time) && at.Sub(monthStart.Time) < 30*24*time.Hour {
		anchor = monthStart.Time
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_platform_quota_events
 (usage_log_id,quota_id,user_id,platform,cost,consumed_at,daily_start,weekly_start,monthly_start,daily_generation,weekly_generation,monthly_generation)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(usage_log_id,effect_kind) DO NOTHING`,
		usage.ID, id, usage.UserID, usage.QuotaPlatform, service.QuantizeUsageBillingAmount(usage.ActualCost), at, day, week, anchor, daily, weekly, monthly)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE user_platform_quotas SET derived_usage=TRUE WHERE id=$1`, id)
	return err
}

// ConsumeQuotaEvents commits counters and applied_at together. Invalidation is
// deliberately outside that transaction, and its durable acknowledgement is
// written only after success. A crash in either gap is safe to replay.
func (r *userPlatformQuotaRepository) ConsumeQuotaEvents(ctx context.Context, limit int, invalidate func(context.Context, int64, string) error) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	processed := 0
	for processed < limit {
		var eventID, userID int64
		var platform string
		err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
			rows, err := client.QueryContext(txCtx, `SELECT id,user_id,platform FROM user_platform_quota_events WHERE cache_invalidated_at IS NULL ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`)
			if err != nil {
				return err
			}
			if !rows.Next() {
				return errors.Join(rows.Err(), rows.Close())
			}
			err = rows.Scan(&eventID, &userID, &platform)
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			// Lock ordering is event -> quota. Producers lock quota but never existing
			// event rows; explicit reset locks quota only.
			for _, window := range []string{"daily", "weekly", "monthly"} {
				query := fmt.Sprintf(`UPDATE user_platform_quotas q SET
     %[1]s_usage_usd=CASE WHEN q.%[1]s_window_start IS NULL OR q.%[1]s_window_start<e.%[1]s_start THEN e.cost ELSE q.%[1]s_usage_usd+e.cost END,
     %[1]s_window_start=e.%[1]s_start, updated_at=NOW()
     FROM user_platform_quota_events e WHERE e.id=$1 AND e.applied_at IS NULL AND q.id=e.quota_id AND q.deleted_at IS NULL
     AND q.%[1]s_generation=e.%[1]s_generation AND (q.%[1]s_reset_at IS NULL OR e.consumed_at>=q.%[1]s_reset_at)
     AND (q.%[1]s_window_start IS NULL OR q.%[1]s_window_start<=e.%[1]s_start)`, window)
				if _, err = client.ExecContext(txCtx, query, eventID); err != nil {
					return err
				}
			}
			_, err = client.ExecContext(txCtx, `UPDATE user_platform_quota_events SET applied_at=COALESCE(applied_at,NOW()) WHERE id=$1`, eventID)
			return err
		})
		if err != nil {
			return processed, err
		}
		if eventID == 0 {
			return processed, nil
		}
		if err = invalidate(ctx, userID, platform); err != nil {
			return processed, err
		}
		if _, err = r.client.ExecContext(ctx, `UPDATE user_platform_quota_events SET cache_invalidated_at=NOW() WHERE id=$1`, eventID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
