// Package payments records verified rail events and uses the existing payment
// fulfillment service and BE-11 wallet transaction to apply their effects.
package payments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
)

var (
	ErrMismatch = errors.New("payment evidence does not match the order")
	ErrPending  = errors.New("payment requires fulfillment or reconciliation before reversal")
	ErrConflict = errors.New("financial command identity already has different terms")
)

type Fulfillment interface {
	ExecuteBalanceFulfillment(context.Context, int64) error
	ExecuteSubscriptionFulfillment(context.Context, int64) error
}

// Refresh invalidates wallet or subscription caches after an external reversal.
// It also runs on redelivery after a commit or notification failure.
type Store struct {
	DB          *sql.DB
	Fulfillment Fulfillment
	Refresh     func(context.Context, int64, int64) error
}

type order struct {
	id, payer                    int64
	expected, group              sql.NullInt64
	kind, state, trade, currency string
	merchant, app                string
	captured, withinGrace        bool
}

// HandleVerified accepts only the output of the order instance's successful
// Provider.VerifyNotification. Provider secrets and raw bodies never enter history.
func (s Store) HandleVerified(ctx context.Context, provider string, n *payment.PaymentNotification) error {
	if n == nil {
		return nil
	}
	confirmation := n.Status == "refund_confirmation"
	reversal := n.Status == "refund" || n.Status == "chargeback" || confirmation
	if !reversal && n.Status != payment.ProviderStatusSuccess && n.Status != payment.ProviderStatusFailed {
		return ErrMismatch
	}
	if n.EventID == "" || n.EffectID == "" || n.InstanceID == "" || n.TradeNo == "" || n.Minor <= 0 || n.Currency == "" ||
		len(n.EventID) > 256 || len(n.EffectID) > 256 {
		return ErrMismatch
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var o order
	predicate := "p.out_trade_no=$3"
	key := n.OrderID
	if reversal && key == "" {
		predicate = "p.payment_trade_no=$3"
		key = n.TradeNo
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id,t.payer_id,t.provider_minor,t.group_id,p.order_type,p.status,p.payment_trade_no,
 coalesce(t.currency,''),coalesce(p.provider_snapshot->>'merchant_id',''),coalesce(p.provider_snapshot->>'merchant_app_id',''),
 p.paid_at IS NOT NULL,p.updated_at>=now()-interval '5 minutes'
 FROM payment_orders p JOIN payment_order_terms t ON t.order_id=p.id WHERE t.provider=$1 AND t.instance=$2 AND `+predicate+` ORDER BY p.id LIMIT 2 FOR UPDATE OF p`, provider, n.InstanceID, key)
	if err != nil {
		return err
	}
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if reversal {
			return ErrPending
		}
		return sql.ErrNoRows
	}
	err = rows.Scan(&o.id, &o.payer, &o.expected, &o.group, &o.kind, &o.state, &o.trade, &o.currency, &o.merchant, &o.app, &o.captured, &o.withinGrace)
	ambiguous := rows.Next()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if ambiguous || !o.expected.Valid || o.currency != n.Currency || (!reversal && o.expected.Int64 != n.Minor) || (reversal && o.expected.Int64 < n.Minor) {
		return ErrMismatch
	}
	if !matchesMerchant(provider, n.Metadata, o) || (o.captured && o.trade != n.TradeNo) {
		return ErrMismatch
	}
	cmd := uuid.NewString()
	var canonical string
	err = tx.QueryRowContext(ctx, `INSERT INTO payment_provider_events(provider,instance,event_key,kind,order_id,trade,effect_key,currency,minor,command_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING RETURNING command_id`,
		provider, n.InstanceID, n.EventID, n.Status, o.id, n.TradeNo, n.EffectID, n.Currency, n.Minor, cmd).Scan(&canonical)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT command_id FROM payment_provider_events WHERE provider=$1 AND instance=$2 AND event_key=$3 AND kind=$4
 AND order_id=$5 AND trade=$6 AND effect_key=$7 AND currency=$8 AND minor=$9`, provider, n.InstanceID, n.EventID, n.Status, o.id, n.TradeNo, n.EffectID, n.Currency, n.Minor).Scan(&canonical)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
	}
	if err != nil {
		return err
	}
	if err = useCommand(ctx, tx, canonical); err != nil {
		return err
	}
	if confirmation {
		// The v1 admin flow may deliberately omit a wallet deduction. Its
		// confirmed rail event must never run the deduction a second time.
		if !o.captured || (o.state != payment.OrderStatusRefunded && o.state != payment.OrderStatusPartiallyRefunded) {
			return ErrPending
		}
		var same bool
		err = tx.QueryRowContext(ctx, `SELECT $2::bigint=CASE WHEN t.currency IN ('ISK','UGX')
 THEN round(t.provider_minor*p.refund_amount/nullif(t.source_amount,0)/100,0)*100
 ELSE round(t.provider_minor*p.refund_amount/nullif(t.source_amount,0),0) END
			FROM payment_order_terms t JOIN payment_orders p ON p.id=t.order_id WHERE t.order_id=$1`, o.id, n.Minor).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrMismatch
		}
	} else if reversal {
		if err = reverse(ctx, tx, provider, n, o, canonical); err != nil {
			return err
		}
	} else if n.Status == payment.ProviderStatusSuccess && !o.captured {
		switch o.state {
		case payment.OrderStatusExpired:
			if !o.withinGrace {
				return ErrPending
			}
		case payment.OrderStatusPending, payment.OrderStatusCancelled, payment.OrderStatusFailed:
		default:
			return ErrPending
		}
		// Some v1 providers initially store the merchant order number as a query handle.
		if o.trade != "" && o.trade != n.OrderID && o.trade != n.TradeNo {
			return ErrMismatch
		}
		_, err = tx.ExecContext(ctx, `UPDATE payment_orders SET status='PAID',paid_at=now(),updated_at=now(),payment_trade_no=$2,failed_at=NULL,failed_reason=NULL WHERE id=$1`, o.id, n.TradeNo)
		if err != nil {
			return err
		}
		o.state = payment.OrderStatusPaid
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if reversal {
		if s.Refresh != nil {
			return s.Refresh(ctx, o.payer, o.group.Int64)
		}
		return nil
	}
	if n.Status == payment.ProviderStatusSuccess && (o.state == payment.OrderStatusPaid || o.state == payment.OrderStatusFailed || o.state == payment.OrderStatusRecharging) {
		if s.Fulfillment == nil {
			return ErrPending
		}
		if o.kind == payment.OrderTypeSubscription {
			return s.Fulfillment.ExecuteSubscriptionFulfillment(ctx, o.id)
		}
		if o.kind == payment.OrderTypeBalance {
			return s.Fulfillment.ExecuteBalanceFulfillment(ctx, o.id)
		}
		return ErrMismatch
	}
	return nil
}

func matchesMerchant(provider string, meta map[string]string, o order) bool {
	var app, merchant string
	switch provider {
	case payment.TypeWxpay:
		app, merchant = meta["appid"], meta["mchid"]
	case payment.TypeAlipay:
		app = meta["app_id"]
	case payment.TypeEasyPay:
		merchant = meta["pid"]
	case payment.TypeAirwallex:
		merchant = meta["account_id"]
	case payment.TypeStripe:
	default:
		return false
	}
	return (o.merchant == "" || merchant == o.merchant) && (o.app == "" || app == o.app)
}

func useCommand(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, "SELECT set_config('omnihive.payment_command',$1,true)", id)
	return err
}

func reverse(ctx context.Context, tx *sql.Tx, provider string, n *payment.PaymentNotification, o order, cmd string) error {
	var same bool
	err := tx.QueryRowContext(ctx, `SELECT order_id=$4 AND minor=$5 AND kind=$6 FROM payment_reversals
 WHERE provider=$1 AND instance=$2 AND effect_key=$3`, provider, n.InstanceID, n.EffectID, o.id, n.Minor, n.Status).Scan(&same)
	if err == nil {
		if same {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !o.captured || (o.state != payment.OrderStatusCompleted && o.state != payment.OrderStatusPartiallyRefunded) {
		return ErrPending
	}
	var delta, total string
	var days int
	var full bool
	// Cumulative allocation conserves the original wallet amount even across
	// several partial refunds. Compare the v1 refunded amount inside the lock.
	err = tx.QueryRowContext(ctx, `WITH used AS (SELECT coalesce(sum(minor),0) minor,coalesce(sum(source_amount),0) source FROM payment_reversals WHERE order_id=$1),
 totals AS (SELECT t.*,u.minor prior,(u.minor+$2) cumulative,u.source used FROM payment_order_terms t CROSS JOIN used u WHERE t.order_id=$1)
 SELECT round(source_amount*cumulative/provider_minor,8)-used,round(source_amount*cumulative/provider_minor,8),
 (trunc(coalesce(days,0)*cumulative/provider_minor)-trunc(coalesce(days,0)*prior/provider_minor))::int,cumulative=provider_minor
 FROM totals WHERE cumulative<=provider_minor AND used=(SELECT refund_amount FROM payment_orders WHERE id=$1)`, o.id, n.Minor).Scan(&delta, &total, &days, &full)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	var original, sourceID int64
	var priorWallet, wallet sql.NullInt64
	sourceKind := "redeem"
	if o.kind == payment.OrderTypeSubscription {
		sourceKind = "plan_assignment"
	}
	err = tx.QueryRowContext(ctx, `SELECT e.id,e.source_id,l.wallet_event_id FROM payment_entitlement_events e
 LEFT JOIN payment_wallet_links l ON l.entitlement_event_id=e.id WHERE e.order_id=$1 AND e.kind=$2 AND e.payer_id=$3`, o.id, sourceKind, o.payer).Scan(&original, &sourceID, &priorWallet)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPending
	}
	if err != nil {
		return err
	}
	if sourceKind == "redeem" {
		if !priorWallet.Valid {
			return ErrPending
		}
		// Legacy refunds may force a capped deduction. An external reversal keeps
		// the rail liability even when the spendable wallet has already been used.
		_, err = tx.ExecContext(ctx, "SELECT payment_write_balance($1,'reversal',$2)", o.payer, delta)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT max(e.id) FROM billing_wallet_events e JOIN billing_funding_accounts f ON f.id=e.funding_account_id
 WHERE f.user_id=$1 AND e.kind='balance_change' AND e.transaction_id=pg_current_xact_id()`, o.payer).Scan(&wallet)
	} else if days > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET expires_at=greatest(starts_at,expires_at-$2*interval '1 day'),
 status=CASE WHEN expires_at-$2*interval '1 day'<=now() THEN 'expired' ELSE status END,updated_at=now()
 WHERE id=$1 AND deleted_at IS NULL`, sourceID, days)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_reversals(provider,instance,effect_key,order_id,command_id,kind,minor,source_amount,
 original_entitlement_id,original_wallet_event_id,wallet_event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		provider, n.InstanceID, n.EffectID, o.id, cmd, n.Status, n.Minor, delta, original, priorWallet, wallet)
	if err != nil {
		return err
	}
	state := payment.OrderStatusPartiallyRefunded
	if full {
		state = payment.OrderStatusRefunded
	}
	_, err = tx.ExecContext(ctx, `UPDATE payment_orders SET status=$2,refund_amount=$3,refund_at=now(),updated_at=now() WHERE id=$1`, o.id, state, total)
	return err
}

// command claims the caller's durable identity in the same transaction as its
// source transition. A retry after a lost COMMIT response skips the mutation.
func (s Store) command(ctx context.Context, id uuid.UUID, payer int64, kind, subject string, apply func(*sql.Tx) error) error {
	if id == uuid.Nil || payer <= 0 || strings.TrimSpace(subject) == "" {
		return ErrMismatch
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_commands(id,kind,payer_id,subject) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, kind, payer, subject)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var same bool
		err = tx.QueryRowContext(ctx, "SELECT kind=$2 AND payer_id=$3 AND subject=$4 FROM payment_commands WHERE id=$1", id, kind, payer, subject).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrConflict
		}
		return nil
	}
	if err = useCommand(ctx, tx, id.String()); err != nil {
		return err
	}
	if err = apply(tx); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return tx.Commit()
}
