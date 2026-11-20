package export

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

const sendTimeout = 4 * time.Second

type timing struct {
	Operation string `json:"operation"`
	Success   bool   `json:"success"`
	LatencyMS int64  `json:"latency_ms"`
}

type claim struct {
	id, exportID, leaseID uuid.UUID
	workspace, version    int64
	attempts              int
}

type delivery struct {
	timing
	endpoint  string
	slot      int
	expiresAt *time.Time
}

type deliveryResult struct {
	reason string
	retry  bool
}

// RunDelivery is a worker-mode loop with independent concurrency and storage.
func (s *Store) RunDelivery(ctx context.Context, onError func()) {
	poll := time.NewTicker(200 * time.Millisecond)
	sweep := time.NewTicker(30 * time.Second)
	defer poll.Stop()
	defer sweep.Stop()
	for ctx.Err() == nil {
		var run func(context.Context) error
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			run = s.deliverOne
		case <-sweep.C:
			run = s.purgeExpired
		}
		work, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := run(work)
		cancel()
		if err != nil && ctx.Err() == nil && onError != nil {
			onError()
		}
	}
}

func (s *Store) take(ctx context.Context) (claim, error) {
	j := claim{leaseID: uuid.New()}
	err := s.db.QueryRowContext(ctx, `WITH due AS (
 SELECT id FROM telemetry_export_jobs WHERE slot IS NOT NULL AND ready_at<=statement_timestamp()
 AND (expires_at IS NULL OR expires_at>statement_timestamp()) ORDER BY ready_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
 )
 UPDATE telemetry_export_jobs j SET lease_id=$1,attempts=least(j.attempts+1,$2),
 ready_at=clock_timestamp()+make_interval(secs => CASE j.attempts
 WHEN 0 THEN 15 WHEN 1 THEN 60 WHEN 2 THEN 300 ELSE 15 END)
 FROM due WHERE j.id=due.id RETURNING j.id,j.workspace_id,j.export_id,j.export_version,j.attempts`,
		j.leaseID, maxAttempts+1).Scan(&j.id, &j.workspace, &j.exportID, &j.version, &j.attempts)
	return j, err
}

func (s *Store) deliverOne(ctx context.Context) error {
	j, err := s.take(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.deliver(ctx, j)
}

func (s *Store) deliver(ctx context.Context, j claim) error {
	var approver int64
	var destinationID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT destination_id,approved_by FROM telemetry_exports
 WHERE workspace_id=$1 AND id=$2 AND version=$3 AND active_slot IS NOT NULL`, j.workspace, j.exportID, j.version).
		Scan(&destinationID, &approver)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	authErr := s.authorize(ctx, Caller{approver, j.workspace}, 0, ConfigureGrant)
	if authErr != nil && !denied(authErr) {
		return authErr
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	endpoint, err := lockDestination(ctx, tx, j.workspace, destinationID)
	if errors.Is(err, fault.NotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	// Hold the destination and configuration fences across the bounded send.
	// A successful disable, reconfiguration, or revocation drains this send
	// before returning. Generation and lease checks fence stale workers.
	lock := " FOR NO KEY UPDATE"
	if authErr != nil {
		lock = " FOR UPDATE"
	}
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM telemetry_exports
 WHERE workspace_id=$1 AND id=$2 AND version=$3 AND destination_id=$4 AND active_slot IS NOT NULL`+lock,
		j.workspace, j.exportID, j.version, destinationID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if authErr != nil {
		return revokeApproval(ctx, tx, j)
	}

	data := delivery{endpoint: endpoint}
	err = tx.QueryRowContext(ctx, `SELECT slot,coalesce(operation,''),coalesce(success,false),coalesce(latency_ms,0),expires_at
 FROM telemetry_export_jobs WHERE id=$1 AND lease_id=$2 AND slot IS NOT NULL
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR UPDATE`, j.id, j.leaseID).
		Scan(&data.slot, &data.Operation, &data.Success, &data.LatencyMS, &data.expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if (data.slot == 0 && state != stateValidating) || (data.slot > 0 && state != stateActive) {
		return nil
	}

	result := deliveryResult{reason: reasonExhausted}
	if j.attempts <= maxAttempts {
		result = s.send(ctx, j.id, data)
		if result.retry && j.attempts >= maxAttempts {
			result = deliveryResult{reason: reasonExhausted}
		}
	}
	if err = finish(ctx, tx, j, data, result); err != nil {
		return err
	}
	return tx.Commit()
}

func revokeApproval(ctx context.Context, tx *sql.Tx, j claim) error {
	if err := deleteJobs(ctx, tx, j.workspace, j.exportID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE telemetry_exports
 SET state='disabled',active_slot=NULL,version=version+1,last_error_reason=$3 WHERE workspace_id=$1 AND id=$2`,
		j.workspace, j.exportID, reasonApprovalRevoked); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) send(ctx context.Context, key uuid.UUID, data delivery) deliveryResult {
	deadline := time.Now().Add(sendTimeout)
	if data.expiresAt != nil {
		if !time.Now().Before(*data.expiresAt) {
			return deliveryResult{reason: reasonExhausted}
		}
		if data.expiresAt.Before(deadline) {
			deadline = *data.expiresAt
		}
	}
	send, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	body := []byte("{}")
	if data.slot > 0 {
		body, _ = json.Marshal(data.timing)
	}
	req, err := http.NewRequestWithContext(send, http.MethodPost, data.endpoint, bytes.NewReader(body))
	if err != nil {
		return deliveryResult{reason: reasonRejected}
	}
	// Each transport attempt is accounted for by a persisted claim.
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key.String())
	if data.slot == 0 {
		req.Header.Set("X-OmniHive-Telemetry-Validation", "1")
	} else {
		req.Header.Set("X-OmniHive-Telemetry-Expires-At", data.expiresAt.UTC().Format(time.RFC3339Nano))
	}
	resp, err := s.egress.Do(req)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	switch {
	case err != nil || resp == nil:
		return deliveryResult{reason: reasonUnavailable, retry: true}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return deliveryResult{}
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly ||
		resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return deliveryResult{reason: reasonUnavailable, retry: true}
	default:
		return deliveryResult{reason: reasonRejected}
	}
}

func finish(ctx context.Context, tx *sql.Tx, j claim, data delivery, result deliveryResult) error {
	if result.retry {
		_, err := tx.ExecContext(ctx, `UPDATE telemetry_exports SET last_error_reason=$3 WHERE workspace_id=$1 AND id=$2`,
			j.workspace, j.exportID, result.reason)
		return err
	}

	if data.slot == 0 {
		return finishValidation(ctx, tx, j, result)
	}
	_, err := tx.ExecContext(ctx, `UPDATE telemetry_exports SET last_error_reason=nullif($3,''),
 last_success_at=CASE WHEN $3='' THEN clock_timestamp() ELSE last_success_at END WHERE workspace_id=$1 AND id=$2`,
		j.workspace, j.exportID, result.reason)
	if err != nil {
		return err
	}
	// Preserve the key across an ambiguous acknowledgement while erasing the
	// sample body. Keep only the most recent receipts within the configured TTL.
	_, err = tx.ExecContext(ctx, `UPDATE telemetry_export_jobs SET slot=NULL,operation=NULL,success=NULL,latency_ms=NULL,
 completed_at=clock_timestamp() WHERE id=$1`, j.id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM telemetry_export_jobs WHERE id IN (
 SELECT id FROM telemetry_export_jobs WHERE workspace_id=$1 AND export_id=$2 AND slot IS NULL
 ORDER BY completed_at DESC,id DESC OFFSET $3)`, j.workspace, j.exportID, maxCompleted)
	return err
}

func finishValidation(ctx context.Context, tx *sql.Tx, j claim, result deliveryResult) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM telemetry_export_jobs WHERE id=$1`, j.id); err != nil {
		return err
	}
	if result.reason == "" {
		_, err := tx.ExecContext(ctx, `UPDATE telemetry_exports SET state='active',last_error_reason=NULL WHERE workspace_id=$1 AND id=$2`,
			j.workspace, j.exportID)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE telemetry_exports SET state='failed',active_slot=NULL,last_error_reason=$3 WHERE workspace_id=$1 AND id=$2`,
		j.workspace, j.exportID, result.reason)
	return err
}

func (s *Store) purgeExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `WITH expired AS (
 SELECT id FROM telemetry_export_jobs WHERE expires_at<=statement_timestamp()
 ORDER BY expires_at,id LIMIT 512 FOR UPDATE SKIP LOCKED
 ) DELETE FROM telemetry_export_jobs j USING expired WHERE expired.id=j.id`)
	return err
}
