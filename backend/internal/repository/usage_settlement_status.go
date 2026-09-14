package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *usageSettlementReceiptRepository) SettlementStatus(ctx context.Context, requestID string, afterID int64) (*service.UsageSettlementStatus, error) {
	s := &service.UsageSettlementStatus{Items: []service.UsageSettlementStatusItem{}}
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='needs_review') FROM usage_settlement_receipts`).Scan(&s.Pending, &s.NeedsReview)
	if err != nil {
		return nil, err
	}
	// Unknown old zeros are a review population, not evidence of free pricing.
	err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM usage_logs u WHERE actual_cost=0 AND NOT EXISTS (SELECT 1 FROM usage_settlement_receipts r WHERE r.request_id=u.request_id AND r.api_key_id=u.api_key_id)`).Scan(&s.HistoricalZeroReview)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,request_id,api_key_id,user_id,state,pricing_state,created_at FROM usage_settlement_receipts WHERE id>$1 AND ($2='' AND state<>'settled' OR $2<>'' AND request_id=$2) ORDER BY id LIMIT 100`, afterID, requestID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item service.UsageSettlementStatusItem
		if err := rows.Scan(&item.ID, &item.RequestID, &item.APIKeyID, &item.UserID, &item.State, &item.PricingState, &item.CreatedAt); err != nil {
			return nil, err
		}
		s.Items = append(s.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	s.HistoricalItems = []service.UsageSettlementStatusItem{}
	history, err := r.db.QueryContext(ctx, `SELECT id,request_id,api_key_id,user_id,created_at FROM usage_logs u WHERE actual_cost=0 AND id>$1 AND ($2='' OR request_id=$2) AND NOT EXISTS (SELECT 1 FROM usage_settlement_receipts r WHERE r.request_id=u.request_id AND r.api_key_id=u.api_key_id) ORDER BY id LIMIT 100`, afterID, requestID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = history.Close() }()
	for history.Next() {
		item := service.UsageSettlementStatusItem{State: "needs_review", PricingState: "unknown"}
		if err := history.Scan(&item.ID, &item.RequestID, &item.APIKeyID, &item.UserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		s.HistoricalItems = append(s.HistoricalItems, item)
	}
	return s, history.Err()
}
