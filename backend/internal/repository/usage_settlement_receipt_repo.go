package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/readmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/shopspring/decimal"
)

// UsageSettlementEffect inserts required derived events in the same transaction.
// It must only do local SQL work; external cache refresh belongs after commit.
type UsageSettlementEffect func(context.Context, *sql.Tx, *service.UsageLog) error

type usageSettlementReceiptRepository struct {
	db      *sql.DB
	effects []UsageSettlementEffect
}

func NewUsageSettlementReceiptRepository(db *sql.DB, effects ...UsageSettlementEffect) service.UsageSettlementReceiptRepository {
	return &usageSettlementReceiptRepository{db: db, effects: append([]UsageSettlementEffect(nil), effects...)}
}

const usageSettlementColumns = `id,request_id,api_key_id,user_id,account_id,request_fingerprint,
usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,usage_log_id,created_at,settled_at`

// Keep the token attempt with the existing durable replay payload. Old command
// JSON remains readable; TPM introduces neither a receipt table nor a queue.
type settlementCommandPayload struct {
	service.UsageBillingCommand
	TPMReservation *service.TPMReservationIdentity `json:"tpm_reservation,omitempty"`
}

func scanUsageSettlement(row interface{ Scan(...any) error }) (*service.UsageSettlementReceipt, error) {
	var receipt service.UsageSettlementReceipt
	var usageJSON, commandJSON []byte
	var accountID int64
	var fingerprint, snapshot string
	var pricingState service.PricingState
	err := row.Scan(&receipt.ID, &receipt.Key.RequestID, &receipt.Key.APIKeyID, &receipt.Key.UserID, &accountID, &fingerprint,
		&usageJSON, &commandJSON, &pricingState, &snapshot, &receipt.ResolutionState, &receipt.CompletionState,
		&receipt.State, &receipt.UsageLogID, &receipt.CreatedAt, &receipt.SettledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageSettlementNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(usageJSON, &receipt.Usage); err != nil {
		return nil, err
	}
	var payload settlementCommandPayload
	if err := json.Unmarshal(commandJSON, &payload); err != nil {
		return nil, err
	}
	receipt.Command, receipt.TPMReservation = payload.UsageBillingCommand, payload.TPMReservation
	if receipt.Command.RequestFingerprint != fingerprint || receipt.Command.AccountID != accountID {
		return nil, service.ErrUsageBillingRequestConflict
	}
	receipt.Usage.PricingState, receipt.Usage.PricingSnapshot = pricingState, snapshot
	return &receipt, nil
}

// insertUsageSettlementReceipt serializes the same frozen evidence for ordinary
// writes and jobs that already own their financial transaction.
func insertUsageSettlementReceipt(ctx context.Context, executor *sql.Tx, receipt *service.UsageSettlementReceipt) (sql.Result, error) {
	usage := receipt.Usage
	// Keep one authoritative copy of frozen pricing in the receipt columns.
	usage.PricingState, usage.PricingSnapshot = "", ""
	usageJSON, err := json.Marshal(usage)
	if err != nil {
		return nil, err
	}
	command := receipt.Command
	if receipt.State == service.UsageSettlementPending {
		if err := command.BindFrozenCharge(receipt.Usage.PricingSnapshot); err != nil {
			return nil, err
		}
	}
	commandJSON, err := json.Marshal(settlementCommandPayload{UsageBillingCommand: command, TPMReservation: receipt.TPMReservation})
	if err != nil {
		return nil, err
	}
	// This commit precedes settlement. ON CONFLICT never overwrites frozen input.
	result, err := executor.ExecContext(ctx, `INSERT INTO usage_settlement_receipts
(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (request_id,api_key_id) DO NOTHING`, receipt.Key.RequestID, receipt.Key.APIKeyID, receipt.Key.UserID,
		receipt.Command.AccountID, receipt.Command.RequestFingerprint, string(usageJSON), string(commandJSON), receipt.Usage.PricingState,
		receipt.Usage.PricingSnapshot, receipt.ResolutionState, receipt.CompletionState, receipt.State)
	if err != nil {
		return nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return result, err
	}
	if receipt.State == service.UsageSettlementNeedsReview {
		if err := releaseReviewHolds(ctx, executor, receipt); err != nil {
			return nil, err
		}
	} else {
		// Publish the receipt while holding its reservation, so an expiry worker
		// waiting on this row will see the pending owner in its next statement.
		if _, err := executor.ExecContext(ctx, `SELECT id FROM api_key_budget_reservations WHERE api_key_id=$2 AND user_id=$3 AND (id=$1 OR ($1=0 AND request_id=$4)) FOR UPDATE`,
			receipt.Command.BudgetReservationID, receipt.Key.APIKeyID, receipt.Key.UserID, receipt.Key.RequestID); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *usageSettlementReceiptRepository) Persist(ctx context.Context, input *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	receipt, err := service.PrepareUsageSettlementReceipt(input)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = insertUsageSettlementReceipt(ctx, tx, receipt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	stored, err := r.Get(ctx, receipt.Key)
	if errors.Is(err, service.ErrUsageSettlementNotFound) {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if err != nil {
		return nil, err
	}
	if stored.Command.RequestFingerprint != receipt.Command.RequestFingerprint || stored.Command.AccountID != receipt.Command.AccountID {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if (stored.TPMReservation == nil) != (receipt.TPMReservation == nil) ||
		(stored.TPMReservation != nil && *stored.TPMReservation != *receipt.TPMReservation) {
		return nil, service.ErrTPMReservationConflict
	}
	return stored, nil
}

func (r *usageSettlementReceiptRepository) Get(ctx context.Context, key service.UsageSettlementKey) (*service.UsageSettlementReceipt, error) {
	return scanUsageSettlement(r.db.QueryRowContext(ctx, `SELECT `+usageSettlementColumns+` FROM usage_settlement_receipts
WHERE request_id=$1 AND api_key_id=$2 AND user_id=$3`, strings.TrimSpace(key.RequestID), key.APIKeyID, key.UserID))
}

func (r *usageSettlementReceiptRepository) ListPending(ctx context.Context, afterID int64, limit int) ([]*service.UsageSettlementReceipt, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+usageSettlementColumns+` FROM usage_settlement_receipts WHERE state='pending' AND id>$1 ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var receipts []*service.UsageSettlementReceipt
	for rows.Next() {
		receipt, err := scanUsageSettlement(rows)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

func (r *usageSettlementReceiptRepository) Settle(ctx context.Context, key service.UsageSettlementKey, fingerprint string) (*service.UsageSettlementResult, error) {
	return withBillingAdmission(ctx, key.UserID, func(ctx context.Context) (*service.UsageSettlementResult, error) {
		return r.settle(ctx, key, fingerprint, false)
	})
}

func (r *usageSettlementReceiptRepository) ReconcileLegacy(ctx context.Context, key service.UsageSettlementKey, fingerprint string) (*service.UsageSettlementResult, error) {
	return withBillingAdmission(ctx, key.UserID, func(ctx context.Context) (*service.UsageSettlementResult, error) {
		return r.settle(ctx, key, fingerprint, true)
	})
}

// Settlement owns the receipt before the key hold, payer, key and project hold.
// Use the frozen reservation identities even if the key moved projects later.
func releaseReviewHolds(ctx context.Context, tx *sql.Tx, r *service.UsageSettlementReceipt) error {
	cmd := &r.Command
	if cmd.BudgetReservationID > 0 {
		var ref string
		err := tx.QueryRowContext(ctx, `SELECT request_id FROM api_key_budget_reservations WHERE id=$1 AND api_key_id=$2 AND user_id=$3 FOR UPDATE`, cmd.BudgetReservationID, r.Key.APIKeyID, r.Key.UserID).Scan(&ref)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err := releaseKeyBudgetReservationTx(ctx, tx, r.Key.APIKeyID, ref); err != nil {
				return err
			}
		}
	}
	if cmd.ProjectReservationRef == "" {
		return nil
	}
	workspace, project := cmd.ProjectWorkspaceID, cmd.ProjectID
	if workspace == 0 || project == 0 {
		// Older receipts stored only the project request reference. Recover the
		// original scope from the journal, the scoped receipt mapping, or a
		// unique frozen UUID in the original key workspace.
		err := tx.QueryRowContext(ctx, `SELECT workspace_id,project_id FROM (
     SELECT 0 AS priority,e.workspace_id,e.project_id FROM gateway_executions e
     WHERE e.request_id::text=$1 AND e.api_key_id=$2 AND e.funding_user_id=$3
     UNION ALL SELECT 1,p.workspace_id,p.project_id FROM project_resources p
     JOIN usage_settlement_receipts r ON p.resource_id=r.id AND p.kind='usage_settlement_receipts'
     JOIN billing_project_holds h ON h.workspace_id=p.workspace_id AND h.project_id=p.project_id AND h.currency='USD' AND h.request_id=$1
     WHERE r.request_id=$4 AND r.api_key_id=$2 AND r.user_id=$3
     UNION ALL SELECT 2,min(h.workspace_id),min(h.project_id) FROM billing_project_holds h
     WHERE h.currency='USD' AND h.request_id=$1
     HAVING count(*)=1 AND min(h.workspace_id)=(SELECT workspace_id FROM api_keys WHERE id=$2)
   ) scoped ORDER BY priority LIMIT 1`, cmd.ProjectReservationRef, r.Key.APIKeyID, r.Key.UserID, r.Key.RequestID).Scan(&workspace, &project)
		if err != nil {
			return err
		}
	}
	return readmodel.Release(ctx, tx, readmodel.Scope{Workspace: workspace, Project: project}, cmd.ProjectReservationRef, "USD")
}

func commitReceiptReview(ctx context.Context, tx *sql.Tx, receipt *service.UsageSettlementReceipt, reason error) error {
	if err := releaseReviewHolds(ctx, tx, receipt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_settlement_receipts SET state='needs_review' WHERE id=$1 AND state='pending'`, receipt.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return reason
}

func (r *usageSettlementReceiptRepository) settle(ctx context.Context, key service.UsageSettlementKey, fingerprint string, reconcile bool) (*service.UsageSettlementResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// All ordinary receipt writers lock receipt -> dedup -> usage -> effects.
	receipt, err := scanUsageSettlement(tx.QueryRowContext(ctx, `SELECT `+usageSettlementColumns+` FROM usage_settlement_receipts
WHERE request_id=$1 AND api_key_id=$2 AND user_id=$3 FOR UPDATE`, strings.TrimSpace(key.RequestID), key.APIKeyID, key.UserID))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(fingerprint) == "" || receipt.Command.RequestFingerprint != strings.TrimSpace(fingerprint) {
		return nil, service.ErrUsageBillingRequestConflict
	}
	result := &service.UsageSettlementResult{Receipt: receipt, Billing: &service.UsageBillingApplyResult{}}
	if receipt.State == service.UsageSettlementSettled {
		return result, nil
	}
	if receipt.State != service.UsageSettlementPending && !reconcile {
		return nil, service.ErrUsageSettlementNeedsReview
	}
	prepared, err := service.PrepareUsageSettlementReceipt(receipt)
	if err != nil {
		return nil, err
	}
	if prepared.State != service.UsageSettlementPending {
		return nil, commitReceiptReview(ctx, tx, receipt, service.ErrUsageSettlementNeedsReview)
	}
	if err := receipt.Command.BindFrozenCharge(receipt.Usage.PricingSnapshot); err != nil {
		return nil, err
	}
	if reconcile && strings.TrimSpace(receipt.Command.RequestPayloadHash) == "" {
		return nil, service.ErrUsageSettlementNeedsReview
	}
	billing := &usageBillingRepository{}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT receipt_dedup`); err != nil {
		return nil, err
	}
	claimed, claimErr := billing.claimUsageBillingKey(ctx, tx, &receipt.Command)
	if claimErr != nil && (reconcile || !errors.Is(claimErr, service.ErrUsageBillingRequestConflict)) {
		return nil, claimErr
	}
	if !claimed {
		if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT receipt_dedup`); err != nil {
			return nil, err
		}
		if !reconcile {
			// A legacy or archived dedup is an existing charge. Never reapply effects
			// or invent a canonical log if its old log is missing/has been retained
			// elsewhere; preserve the receipt for explicit reconciliation instead.
			return nil, commitReceiptReview(ctx, tx, receipt, errors.Join(service.ErrUsageSettlementNeedsReview, claimErr))
		}
	}
	inserted, err := (&usageLogRepository{}).createSingle(ctx, tx, &receipt.Usage)
	if err != nil {
		return nil, err
	}
	if !inserted {
		if !reconcile {
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT receipt_dedup`); err != nil {
				return nil, err
			}
			return nil, commitReceiptReview(ctx, tx, receipt, service.ErrUsageSettlementNeedsReview)
		}
		// Only a same-owner, same-model, same-unit zero row bound to this
		// immutable receipt can advance. Never infer a historical price.
		err = tx.QueryRowContext(ctx, `UPDATE usage_logs SET actual_cost=$1,
        input_cost=$2,output_cost=$3,cache_creation_cost=$4,cache_read_cost=$5
        WHERE request_id=$6 AND api_key_id=$7 AND user_id=$8 AND account_id=$9
        AND model=$10 AND input_tokens=$11 AND output_tokens=$12
        AND cache_creation_tokens=$13 AND cache_read_tokens=$14
        AND image_count=$15 AND total_cost=$16 AND actual_cost=0
        AND billing_type=$17 AND subscription_id IS NOT DISTINCT FROM $18
		AND COALESCE(service_tier,'')=COALESCE($19,'') AND COALESCE(reasoning_effort,'')=COALESCE($20,'')
        RETURNING id`, receipt.Usage.ActualCost, receipt.Usage.InputCost, receipt.Usage.OutputCost,
			receipt.Usage.CacheCreationCost, receipt.Usage.CacheReadCost, key.RequestID, key.APIKeyID, key.UserID,
			receipt.Usage.AccountID, receipt.Usage.Model, receipt.Usage.InputTokens, receipt.Usage.OutputTokens,
			receipt.Usage.CacheCreationTokens, receipt.Usage.CacheReadTokens, receipt.Usage.ImageCount,
			receipt.Usage.TotalCost, receipt.Usage.BillingType, receipt.Usage.SubscriptionID, receipt.Usage.ServiceTier, receipt.Usage.ReasoningEffort).Scan(&receipt.Usage.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUsageSettlementNeedsReview
		}
		if err != nil {
			return nil, err
		}
	}
	if claimed {
		// The historical log DTO is float64. Correct its numeric projection when
		// the frozen eight-place amount cannot round-trip through that DTO.
		if charge := receipt.Command.ChargedAmount; charge != nil && !decimal.RequireFromString(charge.String()).Equal(decimal.NewFromFloat(receipt.Usage.ActualCost)) {
			if _, err := tx.ExecContext(ctx, `UPDATE usage_logs SET actual_cost=$2 WHERE id=$1`, receipt.Usage.ID, charge.String()); err != nil {
				return nil, err
			}
		}
		result.Billing.Applied = true
		if err := billing.applyUsageBillingEffects(ctx, tx, &receipt.Command, result.Billing); err != nil {
			return nil, err
		}
		for _, effect := range r.effects {
			if effect == nil {
				continue
			}
			if err := effect(ctx, tx, &receipt.Usage); err != nil {
				return nil, err
			}
		}
	}
	if receipt.TPMReservation != nil {
		actual, tokenErr := service.TPMUsageTokens(service.UsageTokens{
			InputTokens: receipt.Usage.InputTokens, OutputTokens: receipt.Usage.OutputTokens,
			CacheCreationTokens: receipt.Usage.CacheCreationTokens, CacheReadTokens: receipt.Usage.CacheReadTokens,
		})
		if tokenErr != nil {
			return nil, tokenErr
		}
		if err := settleTPMAttempt(ctx, tx, *receipt.TPMReservation, actual); err != nil {
			return nil, err
		}
	}
	err = tx.QueryRowContext(ctx, `UPDATE usage_settlement_receipts SET state='settled',usage_log_id=$2,settled_at=NOW() WHERE id=$1 RETURNING settled_at`, receipt.ID, receipt.Usage.ID).Scan(&receipt.SettledAt)
	if err != nil {
		return nil, err
	}
	if receipt.Command.ProjectReservationRef != "" {
		if err := releaseReviewHolds(ctx, tx, receipt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	receipt.State, receipt.UsageLogID = service.UsageSettlementSettled, &receipt.Usage.ID
	return result, nil
}

// ListUserPendingSettlement projects unresolved receipts without creating usage rows.
func (r *usageSettlementReceiptRepository) ListUserPendingSettlement(ctx context.Context, userID int64, filter usagestats.UsageLogFilters, cursor service.UserPendingSettlementCursor) (_ *service.UserPendingSettlementPage, resultErr error) {
	if userID <= 0 {
		return nil, service.ErrUsageLogNotFound
	}
	if cursor.Limit <= 0 {
		cursor.Limit = 20
	}
	if cursor.Limit > 100 {
		cursor.Limit = 100
	}
	table, columns, modelColumn, timeColumn := "usage_settlement_receipts", usageSettlementColumns, "usage_json->>'RequestedModel'", "created_at"
	conditions := []string{"user_id=$1", "state='needs_review'", "usage_log_id IS NULL"}
	if cursor.Source == "upstream_job" {
		table, columns, modelColumn, timeColumn = "upstream_jobs", "row_to_json(upstream_jobs)", "billing_snapshot->>'requested_model'", "submitted_at"
		conditions = []string{"user_id=$1", "family='realtime_turn'", "charged_usage_log_id IS NULL", "billing_snapshot->>'pricing_state' IN ('unpriced','error')", "error_code IN ('settlement_needs_review','unsupported_usage')", "result_ref IN ('completed','cancelled','canceled','incomplete','failed')", "jsonb_typeof(usage_json)='object'", "usage_json <> '{}'::jsonb"}
	}
	args := []any{userID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filter.APIKeyID > 0 {
		add("api_key_id=$%d", filter.APIKeyID)
	}
	if filter.Model != "" {
		add(modelColumn+"=$%d", filter.Model)
	}
	if filter.StartTime != nil {
		add(timeColumn+" >= $%d", *filter.StartTime)
	}
	if filter.EndTime != nil {
		add(timeColumn+" < $%d", *filter.EndTime)
	}
	page := &service.UserPendingSettlementPage{Items: []*service.UserPendingSettlementItem{}}
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+strings.Join(conditions, " AND "), args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	if cursor.BeforeID > 0 {
		add("id < $%d", cursor.BeforeID)
	}
	args = append(args, cursor.Limit+1)
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" WHERE "+strings.Join(conditions, " AND ")+fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var receipt *service.UserPendingSettlementItem
		var err error
		if cursor.Source == "upstream_job" {
			var raw []byte
			err = rows.Scan(&raw)
			if err == nil {
				receipt, err = service.PendingSettlementFromRealtimeJSON(raw)
			}
		} else {
			var stored *service.UsageSettlementReceipt
			stored, err = scanUsageSettlement(rows)
			if err == nil {
				receipt = service.PendingSettlementFromReceipt(stored)
			}
		}
		if err != nil {
			return nil, err
		}
		if len(page.Items) == cursor.Limit {
			page.NextCursor = page.Items[len(page.Items)-1].ID
			break
		}
		page.Items = append(page.Items, receipt)
	}
	return page, rows.Err()
}
