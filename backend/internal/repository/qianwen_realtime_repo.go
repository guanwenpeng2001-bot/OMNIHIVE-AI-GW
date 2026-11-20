package repository

import (
	"context"
	"encoding/json"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (r *upstreamJobRepository) CreateRealtimeSession(ctx context.Context, j *service.UpstreamJob) (*service.UpstreamJob, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// A fixed, namespaced advisory lock only covers admission, never network IO.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(717936482019)`); err != nil {
		return nil, err
	}
	var total, user, account int
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE user_id=$1),count(*) FILTER (WHERE account_id=$2) FROM upstream_jobs WHERE family='realtime_session' AND status IN ('submitted','running') AND expires_at>NOW()`, j.UserID, j.AccountID).Scan(&total, &user, &account)
	if err != nil {
		return nil, err
	}
	if total >= 16 || user >= 1 || account >= 2 {
		return nil, service.ErrQianwenRealtimeCapacity
	}
	snapshot, err := json.Marshal(j.Billing)
	if err != nil {
		return nil, err
	}
	created, err := scanUpstreamJob(tx.QueryRowContext(ctx, `INSERT INTO upstream_jobs AS j (public_request_id,request_fingerprint,submission_key,platform,family,account_id,user_id,api_key_id,group_id,submitted_at,next_poll_at,expires_at,billing_snapshot) VALUES ($1,$1,$1,$2,'realtime_session',$3,$4,$5,$6,NOW(),NOW(),NOW()+INTERVAL '150 seconds',$7) RETURNING row_to_json(j)`, j.PublicRequestID, j.Platform, j.AccountID, j.UserID, j.APIKeyID, j.GroupID, string(snapshot)))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}
