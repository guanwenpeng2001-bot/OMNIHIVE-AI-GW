package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamJobRepository struct{ db *sql.DB }

func NewUpstreamJobRepository(db *sql.DB) service.UpstreamJobRepository {
	return &upstreamJobRepository{db: db}
}

func scanUpstreamJob(row interface{ Scan(...any) error }) (*service.UpstreamJob, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUpstreamJobNotFound
		}
		return nil, err
	}
	var j service.UpstreamJob
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *upstreamJobRepository) Create(ctx context.Context, j *service.UpstreamJob) (*service.UpstreamJob, bool, error) {
	snapshot, err := json.Marshal(j.Billing)
	if err != nil {
		return nil, false, err
	}
	row := r.db.QueryRowContext(ctx, `INSERT INTO upstream_jobs AS j
 (public_request_id,request_fingerprint,platform,family,account_id,user_id,api_key_id,group_id,submitted_at,next_poll_at,expires_at,billing_snapshot,submission_key)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$10,$11,$12)
 ON CONFLICT DO NOTHING RETURNING row_to_json(j)`, j.PublicRequestID, j.RequestFingerprint, j.Platform, j.Family, j.AccountID, j.UserID, j.APIKeyID, j.GroupID, j.SubmittedAt, j.ExpiresAt, string(snapshot), j.SubmissionKey)
	created, err := scanUpstreamJob(row)
	if errors.Is(err, service.ErrUpstreamJobNotFound) {
		existing, err := scanUpstreamJob(r.db.QueryRowContext(ctx, `SELECT row_to_json(j) FROM upstream_jobs j WHERE submission_key=$1`, j.SubmissionKey))
		return existing, false, err
	}
	return created, err == nil, err
}

// InsertRealtimeReceipt persists every replay input before any money transaction.
// A duplicate never updates the original evidence or its settlement anchor.
func (r *upstreamJobRepository) InsertRealtimeReceipt(ctx context.Context, receipt *service.UpstreamJob) (*service.UpstreamJob, error) {
	if receipt == nil || receipt.Family != "realtime_turn" || receipt.UpstreamTaskID == "" || len(receipt.UsageJSON) == 0 || receipt.Billing.Realtime == nil || receipt.Billing.Realtime.SessionID == "" {
		return nil, service.ErrUpstreamJobUnsupportedUsage
	}
	snapshot, err := json.Marshal(receipt.Billing)
	if err != nil {
		return nil, err
	}
	stored, err := scanUpstreamJob(r.db.QueryRowContext(ctx, `INSERT INTO upstream_jobs AS j
 (public_request_id,request_fingerprint,platform,family,account_id,user_id,api_key_id,group_id,submitted_at,next_poll_at,expires_at,billing_snapshot,submission_key,upstream_task_id,usage_json,result_ref,status,finished_at,error_code,error_message)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
 ON CONFLICT (submission_key) DO NOTHING RETURNING row_to_json(j)`,
		receipt.PublicRequestID, receipt.RequestFingerprint, receipt.Platform, receipt.Family, receipt.AccountID, receipt.UserID, receipt.APIKeyID, receipt.GroupID, receipt.SubmittedAt, receipt.NextPollAt, receipt.ExpiresAt, string(snapshot), receipt.SubmissionKey, receipt.UpstreamTaskID, string(receipt.UsageJSON), receipt.ResultRef, receipt.Status, receipt.FinishedAt, receipt.ErrorCode, receipt.ErrorMessage))
	if !errors.Is(err, service.ErrUpstreamJobNotFound) {
		return stored, err
	}
	// A separate statement sees the winning insert after ON CONFLICT waits for it.
	stored, err = scanUpstreamJob(r.db.QueryRowContext(ctx, `SELECT row_to_json(j) FROM upstream_jobs j WHERE submission_key=$1`, receipt.SubmissionKey))
	if err != nil {
		return nil, err
	}
	if !service.EqualQianwenRealtimeReceipt(stored, receipt) {
		return stored, service.ErrQianwenRealtimeConflict
	}
	return stored, nil
}

func (r *upstreamJobRepository) Get(ctx context.Context, id string, userID, keyID int64) (*service.UpstreamJob, error) {
	return scanUpstreamJob(r.db.QueryRowContext(ctx, `SELECT row_to_json(j) FROM upstream_jobs j WHERE public_request_id=$1 AND user_id=$2 AND api_key_id=$3`, id, userID, keyID))
}
func (r *upstreamJobRepository) Due(ctx context.Context, now time.Time, limit int) ([]string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `SELECT public_request_id FROM upstream_jobs WHERE status IN ('submitted','running') AND next_poll_at<=$1 AND upstream_task_id<>'' AND error_code NOT IN ('usage_conflict','settlement_needs_review') ORDER BY next_poll_at,id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *upstreamJobRepository) WithLocked(ctx context.Context, id string, fn func(*service.UpstreamJob, service.UpstreamJobTx) error) (*service.UpstreamJob, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	j, err := scanUpstreamJob(tx.QueryRowContext(ctx, `SELECT row_to_json(j) FROM upstream_jobs j WHERE public_request_id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	frozenPricing := j.Billing.PricingSnapshot
	if err = fn(j, &upstreamJobTx{tx: tx}); err != nil {
		return nil, err
	}
	if frozenPricing != "" && j.Billing.PricingSnapshot != frozenPricing {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if j.Billing.PricingSnapshot != "" && !json.Valid([]byte(j.Billing.PricingSnapshot)) {
		return nil, errors.New("invalid frozen pricing snapshot")
	}
	var usage any
	if len(j.UsageJSON) > 0 {
		usage = string(j.UsageJSON)
	}
	resourceState, err := json.Marshal(j.ResourceState)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_jobs SET upstream_task_id=$2,status=$3,attempt_count=$4,last_polled_at=$5,next_poll_at=$6,finished_at=$7,result_ref=$8,usage_json=$9,charged_usage_log_id=$10,error_code=$11,error_message=$12,resource_state=$13,
 billing_snapshot=CASE WHEN $14::text='' THEN billing_snapshot ELSE
 jsonb_set(
  CASE WHEN COALESCE(billing_snapshot->>'pricing_snapshot','')='' AND $15::text<>''
   THEN jsonb_set(billing_snapshot,'{pricing_snapshot}',to_jsonb($15::text),true)
   ELSE billing_snapshot END,
  '{pricing_state}',to_jsonb($14::text),true) END
 WHERE id=$1`, j.ID, j.UpstreamTaskID, j.Status, j.AttemptCount, j.LastPolledAt, j.NextPollAt, j.FinishedAt, j.ResultRef, usage, j.ChargedUsageLogID, j.ErrorCode, j.ErrorMessage, string(resourceState), string(j.Billing.PricingState), j.Billing.PricingSnapshot)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return j, nil
}
func (r *upstreamJobRepository) Expire(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE upstream_jobs SET status='expired',finished_at=$1,error_code=CASE WHEN error_code='usage_pending' THEN error_code ELSE 'expired' END,error_message=CASE WHEN error_code='usage_pending' THEN error_message ELSE 'Task expired' END WHERE status IN ('submitted','running') AND expires_at<=$1 AND NOT (family='realtime_turn' AND usage_json IS NOT NULL AND error_code<>'usage_conflict')`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type upstreamJobTx struct{ tx *sql.Tx }

func (t *upstreamJobTx) Charge(ctx context.Context, log *service.UsageLog, cmd *service.UsageBillingCommand) error {
	// Reuse the canonical insert shape and billing effects on THIS transaction.
	// Job lock + anchor guard the whole operation; no best-effort worker is used.
	inserted, err := (&usageLogRepository{}).createSingle(ctx, t.tx, log)
	if err != nil {
		return err
	}
	if !inserted {
		return errors.New("unexpected existing usage log for unsettled job")
	}
	cmd.Normalize()
	return (&usageBillingRepository{}).applyUsageBillingEffects(ctx, t.tx, cmd, &service.UsageBillingApplyResult{Applied: true})
}
