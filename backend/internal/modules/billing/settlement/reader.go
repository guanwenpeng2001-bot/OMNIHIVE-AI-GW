package settlement

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

// Scope must come from an authorized project context.
type Scope struct {
	WorkspaceID int64
	ProjectID   int64
}

type Evidence struct {
	AttemptID        string
	FundingAccountID int64
	FundingSource    string
	PriceVersion     string
	ReceiptState     string
	State            string
	SettlementID     *int64
	PlatformCharge   *money.Money
	Reserved         *money.Money
	LedgerEventIDs   []int64
	InputTokens      *int64
	OutputTokens     *int64
	ReviewReason     *string
}

type FinancialReader struct {
	DB *sql.DB
}

// Read returns one attempt's evidence. Amounts come from the persisted command's
// numeric text, and only a linked, settled receipt establishes a charge.
func (r FinancialReader) Read(ctx context.Context, scope Scope, requestID, attemptID string) (Evidence, error) {
	var evidence Evidence
	var charge, reserved *string
	err := r.DB.QueryRowContext(ctx, `
SELECT a.attempt_id::text, f.id,
       CASE
           WHEN receipt.id IS NULL THEN 'unknown'
           WHEN (receipt.billing_command->>'SubscriptionID')::bigint IS NOT NULL THEN 'subscription'
           ELSE 'wallet'
       END,
       e.price_version, COALESCE(u.receipt_state, 'unconfirmed'),
       CASE
           WHEN u.receipt_state IS NULL THEN 'unknown'
           ELSE u.receipt_state
       END,
       receipt.id,
       CASE WHEN u.receipt_state = 'settled' THEN
           CASE WHEN receipt.pricing_state = 'free' THEN '0'
                WHEN receipt.pricing_state = 'priced' THEN
                    (CASE WHEN (receipt.billing_command->>'SubscriptionID')::bigint IS NOT NULL
                          THEN receipt.billing_command->>'SubscriptionCost'
                          ELSE receipt.billing_command->>'BalanceCost' END)::numeric(28,8)::text
           END
       END,
       hold.hold_amount::text,
       u.input_tokens, u.output_tokens, review.reason,
       ARRAY(SELECT event.id FROM billing_wallet_events event
             WHERE event.funding_account_id = f.id
               AND event.transaction_id = settled.transaction_id
             ORDER BY event.id)
FROM gateway_executions e
JOIN gateway_execution_attempts a USING (request_id)
JOIN gateway_execution_usage u USING (request_id, attempt_id)
JOIN billing_funding_accounts f ON f.user_id = e.funding_user_id AND f.currency = 'USD'
LEFT JOIN usage_settlement_receipts receipt ON receipt.id = u.receipt_id
LEFT JOIN api_key_budget_reservations hold ON hold.api_key_id = e.api_key_id
    AND hold.user_id = e.funding_user_id AND hold.request_id = NULLIF(e.reservation_ref, '')
LEFT JOIN billing_settlement_outbox settled ON settled.receipt_id = receipt.id
LEFT JOIN gateway_execution_reconciliation review
    ON review.request_id = e.request_id AND review.attempt_id = a.attempt_id
WHERE e.workspace_id = $1 AND e.project_id = $2
  AND e.request_id = $3::uuid AND a.attempt_id = $4::uuid`,
		scope.WorkspaceID, scope.ProjectID, requestID, attemptID).Scan(
		&evidence.AttemptID, &evidence.FundingAccountID, &evidence.FundingSource,
		&evidence.PriceVersion, &evidence.ReceiptState, &evidence.State, &evidence.SettlementID,
		&charge, &reserved, &evidence.InputTokens, &evidence.OutputTokens, &evidence.ReviewReason, pq.Array(&evidence.LedgerEventIDs))
	if err != nil {
		return Evidence{}, err
	}
	evidence.PlatformCharge, err = nullableUSD(charge)
	if err != nil {
		return Evidence{}, err
	}
	evidence.Reserved, err = nullableUSD(reserved)
	if err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func nullableUSD(value *string) (*money.Money, error) {
	if value == nil {
		return nil, nil
	}
	amount, err := money.NewMoney("USD", *value)
	if err != nil {
		return nil, err
	}
	return &amount, nil
}
