package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

var _ service.TPMReservationRepository = (*usageSettlementReceiptRepository)(nil)

type tpmScopePolicy struct {
	kind  service.TPMScope
	id    int64
	limit int64
}

type tpmReservationRow struct {
	kind        service.TPMScope
	scopeID     int64
	window      time.Time
	estimate    int64
	actual      sql.NullInt64
	fingerprint string
	state       string
}

// Policy rows precede token identity locks. Receipt settlement already owns the
// monetary/key locks and appends only token locks; token settlement never takes
// receipt, key or money locks. All token writers then lock identity -> windows.
func readTPMPolicies(ctx context.Context, tx *sql.Tx, command service.TPMReservationCommand) ([]tpmScopePolicy, error) {
	policies := []tpmScopePolicy{
		{kind: service.TPMScopeAPIKey, id: command.APIKeyID},
		{kind: service.TPMScopeGroup, id: command.BillingGroupID},
		{kind: service.TPMScopeAccount, id: command.AccountID},
	}
	tables := []string{"api_keys", "groups", "accounts"}
	for index := range policies {
		if policies[index].id == 0 && policies[index].kind == service.TPMScopeGroup {
			continue
		}
		query := `SELECT tpm_limit FROM ` + tables[index] + ` WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR SHARE`
		if err := tx.QueryRowContext(ctx, query, policies[index].id, command.WorkspaceID).Scan(&policies[index].limit); err != nil {
			return nil, err
		}
	}
	// This is also the token lock order: key -> group -> account. Check shared
	// caller limits first so an exhausted account cannot mask a key/group denial.
	return policies, nil
}

func lockTPMIdentity(ctx context.Context, tx *sql.Tx, identity service.TPMReservationIdentity) error {
	if identity.WorkspaceID <= 0 || identity.APIKeyID <= 0 || strings.TrimSpace(identity.ExecutionID) == "" || identity.AttemptIndex <= 0 {
		return service.ErrTPMReservationConflict
	}
	// Transaction advisory locks serialize the absent-row case without creating
	// an extra identity table. A hash collision only serializes unrelated work.
	key := fmt.Sprintf("gateway_tpm:%d:%d:%s:%d", identity.WorkspaceID, identity.APIKeyID, identity.ExecutionID, identity.AttemptIndex)
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
	return err
}

func loadTPMReservations(ctx context.Context, tx *sql.Tx, identity service.TPMReservationIdentity) ([]tpmReservationRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope_kind,scope_id,window_start,reserved_tokens,actual_tokens,request_fingerprint,state
FROM gateway_tpm_reservations WHERE workspace_id=$1 AND api_key_id=$2 AND execution_id=$3 AND attempt_index=$4
ORDER BY CASE scope_kind WHEN 'api_key' THEN 0 WHEN 'group' THEN 1 ELSE 2 END,scope_id FOR UPDATE`, identity.WorkspaceID, identity.APIKeyID, identity.ExecutionID, identity.AttemptIndex)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var reservations []tpmReservationRow
	for rows.Next() {
		var reservation tpmReservationRow
		if err := rows.Scan(&reservation.kind, &reservation.scopeID, &reservation.window, &reservation.estimate, &reservation.actual, &reservation.fingerprint, &reservation.state); err != nil {
			return nil, err
		}
		reservations = append(reservations, reservation)
	}
	return reservations, rows.Err()
}

var errTPMMinuteChanged = errors.New("token admission minute changed while waiting for a lock")

func (r *usageSettlementReceiptRepository) ReserveTPMAttempt(ctx context.Context, command service.TPMReservationCommand) (*service.TPMReservation, error) {
	for retry := 0; retry < 3; retry++ {
		reservation, err := r.reserveTPMTransaction(ctx, command)
		if !errors.Is(err, errTPMMinuteChanged) {
			return reservation, err
		}
	}
	return nil, service.ErrTPMStateUnavailable
}

func (r *usageSettlementReceiptRepository) reserveTPMTransaction(ctx context.Context, command service.TPMReservationCommand) (*service.TPMReservation, error) {
	if command.AccountID <= 0 || command.Fingerprint == "" {
		return nil, service.ErrTPMReservationConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	policies, err := readTPMPolicies(ctx, tx, command)
	if err != nil {
		return nil, err
	}
	if err := lockTPMIdentity(ctx, tx, command.TPMReservationIdentity); err != nil {
		return nil, err
	}
	existing, err := loadTPMReservations(ctx, tx, command.TPMReservationIdentity)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		for _, reservation := range existing {
			if reservation.fingerprint != command.Fingerprint || reservation.state != "reserved" {
				return nil, service.ErrTPMReservationConflict
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.TPMReservation{TPMReservationIdentity: command.TPMReservationIdentity, WindowStart: existing[0].window, Estimate: existing[0].estimate, Fingerprint: command.Fingerprint}, nil
	}
	enabled := false
	for _, policy := range policies {
		enabled = enabled || policy.limit > 0
	}
	if !enabled {
		return nil, tx.Commit()
	}
	estimate, err := command.Estimate.Tokens()
	if err != nil {
		return nil, err
	}
	if command.Estimate.Tokenless {
		return nil, tx.Commit()
	}
	var window, databaseNow time.Time
	// clock_timestamp is evaluated after contention/queueing, not at BEGIN.
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return nil, err
	}
	window = databaseNow.UTC().Truncate(time.Minute)
	expires := command.ExpiresAt
	if minimum := databaseNow.Add(24 * time.Hour); expires.Before(minimum) {
		expires = minimum
	}
	for _, policy := range policies {
		if policy.limit <= 0 {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO gateway_tpm_windows (workspace_id,scope_kind,scope_id,window_start,window_end)
VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, command.WorkspaceID, policy.kind, policy.id, window, window.Add(time.Minute))
		if err != nil {
			return nil, err
		}
		var used, reserved, overrun int64
		if err := tx.QueryRowContext(ctx, `SELECT used_tokens,reserved_tokens,overrun_tokens FROM gateway_tpm_windows
WHERE workspace_id=$1 AND scope_kind=$2 AND scope_id=$3 AND window_start=$4 FOR UPDATE`, command.WorkspaceID, policy.kind, policy.id, window).Scan(&used, &reserved, &overrun); err != nil {
			return nil, err
		}
		// Subtraction avoids overflowing used + reserved + estimate.
		if overrun > 0 || used > policy.limit || reserved > policy.limit-used || estimate > policy.limit-used-reserved {
			return nil, &service.TPMLimitExceeded{Scope: policy.kind, ResetAt: window.Add(time.Minute)}
		}
		_, err = tx.ExecContext(ctx, `UPDATE gateway_tpm_windows SET reserved_tokens=reserved_tokens+$5
WHERE workspace_id=$1 AND scope_kind=$2 AND scope_id=$3 AND window_start=$4`, command.WorkspaceID, policy.kind, policy.id, window, estimate)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO gateway_tpm_reservations
(workspace_id,api_key_id,execution_id,attempt_index,scope_kind,scope_id,window_start,reserved_tokens,request_fingerprint,state,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'reserved',$10)`, command.WorkspaceID, command.APIKeyID, command.ExecutionID, command.AttemptIndex, policy.kind, policy.id, window, estimate, command.Fingerprint, expires)
		if err != nil {
			return nil, err
		}
	}
	// Lock contention may cross a minute. Roll back the uncommitted attempt and
	// re-evaluate in the new DB minute using the same idempotency identity.
	var readyAt time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&readyAt); err != nil {
		return nil, err
	}
	if !readyAt.UTC().Truncate(time.Minute).Equal(window) {
		return nil, errTPMMinuteChanged
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.TPMReservation{TPMReservationIdentity: command.TPMReservationIdentity, WindowStart: window, Estimate: estimate, Fingerprint: command.Fingerprint}, nil
}

// settleTPMAttempt runs after monetary effects in a receipt transaction, or in
// its own token-only transaction for verified usage from a failed attempt.
func settleTPMAttempt(ctx context.Context, tx *sql.Tx, identity service.TPMReservationIdentity, actual int64) error {
	if actual < 0 {
		return service.ErrTPMReservationConflict
	}
	return updateTPMAttempt(ctx, tx, identity, "settled", actual)
}

func updateTPMAttempt(ctx context.Context, tx *sql.Tx, identity service.TPMReservationIdentity, state string, actual int64) error {
	if err := lockTPMIdentity(ctx, tx, identity); err != nil {
		return err
	}
	reservations, err := loadTPMReservations(ctx, tx, identity)
	if err != nil {
		return err
	}
	for _, reservation := range reservations {
		if state == "unknown" {
			if reservation.state != "reserved" {
				continue
			}
		} else {
			if reservation.state == "settled" {
				if state == "settled" && actual < reservation.actual.Int64 {
					return service.ErrTPMReservationConflict
				}
				if state != "settled" || actual == reservation.actual.Int64 {
					continue
				}
			}
			if state == "released" && (reservation.state == "unknown" || reservation.state == "released") {
				continue
			}
			var held int64
			if reservation.state != "released" && reservation.state != "settled" {
				held = reservation.estimate
			}
			used := int64(0)
			if state == "settled" {
				used = actual
			}
			overrun := max(int64(0), used-reservation.estimate)
			if reservation.state == "settled" {
				used -= reservation.actual.Int64
				overrun -= max(int64(0), reservation.actual.Int64-reservation.estimate)
			}
			// PostgreSQL bigint arithmetic and constraints fail the transaction on
			// overflow or a corrupt negative counter; no real usage is truncated.
			_, err = tx.ExecContext(ctx, `UPDATE gateway_tpm_windows SET reserved_tokens=reserved_tokens-$5,
used_tokens=used_tokens+$6,overrun_tokens=overrun_tokens+$7
WHERE workspace_id=$1 AND scope_kind=$2 AND scope_id=$3 AND window_start=$4`, identity.WorkspaceID, reservation.kind, reservation.scopeID, reservation.window, held, used, overrun)
			if err != nil {
				return err
			}
		}
		var quantity any
		if state == "settled" {
			quantity = actual
		}
		_, err = tx.ExecContext(ctx, `UPDATE gateway_tpm_reservations SET state=$7,actual_tokens=$8
WHERE workspace_id=$1 AND api_key_id=$2 AND execution_id=$3 AND attempt_index=$4 AND scope_kind=$5 AND scope_id=$6`, identity.WorkspaceID, identity.APIKeyID, identity.ExecutionID, identity.AttemptIndex, reservation.kind, reservation.scopeID, state, quantity)
		if err != nil {
			return err
		}
	}
	return nil
}

// RetireExpiredTPMReservations uses the existing receipt recovery tick. Expiry
// never refunds unknown consumption and never deletes idempotency evidence.
func (r *usageSettlementReceiptRepository) RetireExpiredTPMReservations(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT workspace_id,api_key_id,execution_id,attempt_index FROM gateway_tpm_reservations WHERE state='reserved' AND expires_at<CURRENT_TIMESTAMP ORDER BY expires_at LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var identities []service.TPMReservationIdentity
	seen := make(map[service.TPMReservationIdentity]bool)
	for rows.Next() {
		var identity service.TPMReservationIdentity
		if err := rows.Scan(&identity.WorkspaceID, &identity.APIKeyID, &identity.ExecutionID, &identity.AttemptIndex); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if !seen[identity] {
			identities = append(identities, identity)
			seen[identity] = true
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	for index, identity := range identities {
		if err := r.MarkTPMAttemptUnknown(ctx, identity); err != nil {
			return index, err
		}
	}
	return len(identities), nil
}

func (r *usageSettlementReceiptRepository) updateTPM(ctx context.Context, identity service.TPMReservationIdentity, state string, actual int64) error {
	if actual < 0 {
		return service.ErrTPMReservationConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := updateTPMAttempt(ctx, tx, identity, state, actual); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *usageSettlementReceiptRepository) SettleTPMAttempt(ctx context.Context, identity service.TPMReservationIdentity, actual int64) error {
	return r.updateTPM(ctx, identity, "settled", actual)
}

func (r *usageSettlementReceiptRepository) ReleaseUnsentTPMAttempt(ctx context.Context, identity service.TPMReservationIdentity) error {
	return r.updateTPM(ctx, identity, "released", 0)
}

func (r *usageSettlementReceiptRepository) MarkTPMAttemptUnknown(ctx context.Context, identity service.TPMReservationIdentity) error {
	return r.updateTPM(ctx, identity, "unknown", 0)
}
