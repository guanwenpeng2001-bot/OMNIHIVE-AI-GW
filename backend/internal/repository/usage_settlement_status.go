package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Job settlements carry their frozen pricing in upstream_jobs rather than an
// ordinary receipt. Only zero rows without either source need historical review.
const historicalZeroUsageFilter = `usage_log.actual_cost=0
 AND NOT EXISTS (SELECT 1 FROM usage_settlement_receipts receipt WHERE receipt.request_id=usage_log.request_id AND receipt.api_key_id=usage_log.api_key_id)
 AND NOT EXISTS (SELECT 1 FROM upstream_jobs job WHERE job.public_request_id=usage_log.request_id
 AND job.api_key_id=usage_log.api_key_id AND job.user_id=usage_log.user_id AND job.charged_usage_log_id=usage_log.id
 AND job.status='succeeded' AND job.billing_snapshot->>'pricing_state' IN ('priced','free')
 AND COALESCE(job.billing_snapshot->>'pricing_snapshot','')<>'')`

func (repository *usageSettlementReceiptRepository) SettlementStatus(ctx context.Context, requestID string, afterID int64) (*service.UsageSettlementStatus, error) {
	status := &service.UsageSettlementStatus{Items: []service.UsageSettlementStatusItem{}}
	err := repository.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='needs_review') FROM usage_settlement_receipts`).Scan(&status.Pending, &status.NeedsReview)
	if err != nil {
		return nil, err
	}
	// Unknown old zeros are a review population, not evidence of free pricing.
	err = repository.db.QueryRowContext(ctx, `SELECT count(*) FROM usage_logs usage_log WHERE `+historicalZeroUsageFilter).Scan(&status.HistoricalZeroReview)
	if err != nil {
		return nil, err
	}
	rows, err := repository.db.QueryContext(ctx, `SELECT id,request_id,api_key_id,user_id,state,pricing_state,created_at FROM usage_settlement_receipts WHERE id>$1 AND ($2='' AND state<>'settled' OR $2<>'' AND request_id=$2) ORDER BY id LIMIT 100`, afterID, requestID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item service.UsageSettlementStatusItem
		if err := rows.Scan(&item.ID, &item.RequestID, &item.APIKeyID, &item.UserID, &item.State, &item.PricingState, &item.CreatedAt); err != nil {
			return nil, err
		}
		status.Items = append(status.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	status.HistoricalItems = []service.UsageSettlementStatusItem{}
	history, err := repository.db.QueryContext(ctx, `SELECT id,request_id,api_key_id,user_id,created_at FROM usage_logs usage_log WHERE `+historicalZeroUsageFilter+` AND id>$1 AND ($2='' OR request_id=$2) ORDER BY id LIMIT 100`, afterID, requestID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = history.Close() }()
	for history.Next() {
		item := service.UsageSettlementStatusItem{State: "needs_review", PricingState: "unknown"}
		if err := history.Scan(&item.ID, &item.RequestID, &item.APIKeyID, &item.UserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		status.HistoricalItems = append(status.HistoricalItems, item)
	}
	return status, history.Err()
}
