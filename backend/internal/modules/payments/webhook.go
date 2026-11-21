package payments

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
)

// ExactWebhook selects the dispatcher from persisted terms after verification.
// sql.ErrNoRows means a verified notification identifies no local order.
func (s Store) ExactWebhook(ctx context.Context, provider string, n *payment.PaymentNotification) (bool, error) {
	reversal := n.Status == "refund" || n.Status == "chargeback" || n.Status == "refund_confirmation"
	var rail, instance sql.NullString
	var historical sql.NullBool
	var pinnedRail bool
	var err error
	if reversal && n.OrderID == "" {
		if n.InstanceID == "" || n.TradeNo == "" {
			return false, ErrMismatch
		}
		var matches int
		err = s.DB.QueryRowContext(ctx, `SELECT t.provider,t.instance,t.historical,true,count(*) OVER ()
 FROM payment_orders p JOIN payment_order_terms t ON t.order_id=p.id
 WHERE p.payment_trade_no=$1 AND t.provider=$2 AND t.instance=$3
 LIMIT 1`, n.TradeNo, provider, n.InstanceID).
			Scan(&rail, &instance, &historical, &pinnedRail, &matches)
		if errors.Is(err, sql.ErrNoRows) {
			// Stripe may deliver the reversal before we bind its intent to an order.
			return false, ErrPending
		}
		if err == nil && matches != 1 {
			return false, ErrMismatch
		}
	} else {
		orderID := strings.TrimSpace(n.OrderID)
		if orderID == "" {
			return false, ErrMismatch
		}
		var legacyID int64
		if tail, ok := strings.CutPrefix(orderID, "sub2_"); ok {
			if parsed, parseErr := strconv.ParseInt(tail, 10, 64); parseErr == nil {
				legacyID = parsed
			}
		}
		var exactID bool
		err = s.DB.QueryRowContext(ctx, `SELECT t.provider,
 coalesce(nullif(t.instance,''),p.provider_snapshot->>'provider_instance_id',''),
 t.historical,coalesce(nullif(p.provider_snapshot->>'provider_key',''),nullif(p.provider_key,'')) IS NOT NULL,
 coalesce(p.out_trade_no=$1,false)
 FROM payment_orders p LEFT JOIN payment_order_terms t ON t.order_id=p.id
 WHERE p.out_trade_no=$1 OR ($2>0 AND p.id=$2)
 ORDER BY (p.out_trade_no=$1) DESC NULLS LAST,p.id LIMIT 1`, orderID, legacyID).
			Scan(&rail, &instance, &historical, &pinnedRail, &exactID)
		if err == nil && !exactID && (!historical.Valid || !historical.Bool || reversal) {
			return false, ErrMismatch
		}
	}
	if err != nil {
		return false, err
	}
	if !historical.Valid {
		return false, ErrPending
	}
	if ((pinnedRail || !historical.Bool) && rail.String != provider) || (instance.String != "" && instance.String != n.InstanceID) {
		return false, ErrMismatch
	}
	if historical.Bool {
		// A confirmation with a pinned order can record fresh local refund
		// evidence. Historical external debits still require reconciliation.
		if n.Status == "refund_confirmation" && pinnedRail && instance.String != "" {
			return true, nil
		}
		if reversal {
			return false, ErrPending
		}
		return false, nil
	}
	if instance.String == "" {
		return false, ErrMismatch
	}
	return true, nil
}
