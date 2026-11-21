package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	rs "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (r *Runtime) startLegacy(ctx context.Context, actor int64, backupID, key, session string) (*service.BackupRecord, error) {
	if r.Journal == nil {
		return nil, rs.Unavailable
	}
	a := rs.Actor{ID: actor, Workspace: domain.DefaultWorkspaceID, Session: session}
	for _, action := range []string{rs.PreviewAction, rs.ExecuteAction} {
		if _, err := r.Access.Evaluate(ctx, actor, a.Workspace, 0, action); err != nil {
			return nil, err
		}
	}
	if err := r.RequireStepUp(ctx, a); err != nil {
		return nil, err
	}
	if key == "" {
		key = uuid.NewString()
	}
	if len(key) > 200 || key != strings.TrimSpace(key) {
		return nil, fault.Invalid("/Idempotency-Key")
	}
	tx, err := r.Journal.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(298,2)"); err != nil {
		return nil, err
	}
	var requested, id string
	err = tx.QueryRowContext(ctx, `SELECT backup_id,id FROM restore_journal.legacy_requests WHERE actor_id=$1 AND workspace_id=$2 AND request_key=$3`, actor, a.Workspace, key).Scan(&requested, &id)
	if err == nil {
		if requested != backupID {
			return nil, fault.Conflict
		}
	} else {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		var active bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM restore_journal.legacy_requests WHERE state='pending') OR
  EXISTS(SELECT FROM restore_journal.operations WHERE (state IN ('queued','running','unknown') OR stage='manual_recovery')) OR
  EXISTS(SELECT FROM restore_journal.backups WHERE review_required)`).Scan(&active)
		if err != nil {
			return nil, err
		}
		if active {
			return nil, &fault.Fault{Status: 409, Reason: "RESTORE_TARGET_BUSY", Message: "A restore is active or requires review."}
		}
		record, err := r.Record(ctx, backupID)
		if err != nil {
			return nil, err
		}
		if err = r.Import(ctx, backupID, record, false); err != nil {
			return nil, err
		}
		id = "ri_" + uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO restore_journal.legacy_requests(id,actor_id,workspace_id,backup_id,request_key,session_key) VALUES($1,$2,$3,$4,$5,$6)`, id, actor, a.Workspace, backupID, key, a.Session)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	var rec service.BackupRecord
	var data []byte
	var state, op string
	var reason sql.NullString
	var completed sql.NullTime
	err = r.Journal.QueryRowContext(ctx, `SELECT b.record,CASE WHEN o.stage='manual_recovery' THEN 'unknown' ELSE coalesce(o.state,r.state) END,r.id,coalesce(o.report->>'reason',r.reason),o.updated_at FROM restore_journal.legacy_requests r
 JOIN restore_journal.backups b ON b.id=r.backup_id LEFT JOIN restore_journal.operations o ON o.id=r.production_id WHERE r.id=$1`, id).Scan(&data, &state, &op, &reason, &completed)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	projectStatus(&rec, op, state, reason.String, completed.Time)
	return &rec, nil
}

func projectStatus(b *service.BackupRecord, id, state, reason string, stamp time.Time) {
	b.RestoreOperationID = id
	b.RestoreError = reason
	b.RestoredAt = ""
	switch state {
	case "pending", "queued", "running":
		b.RestoreStatus = "running"
	case "succeeded":
		b.RestoreStatus = "completed"
		b.RestoredAt = stamp.UTC().Format(time.RFC3339)
	case "unknown":
		b.RestoreStatus = "review_required"
	default:
		b.RestoreStatus = "failed"
	}
}

func (r *Runtime) advanceLegacy(ctx context.Context) error {
	tx, err := r.Journal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, backup string
	var a rs.Actor
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT id,backup_id,actor_id,workspace_id,session_key,authorized_until FROM restore_journal.legacy_requests
 WHERE state='pending' AND next_check<=clock_timestamp() ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &backup, &a.ID, &a.Workspace, &a.Session, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var production string
	err = tx.QueryRowContext(ctx, `SELECT id FROM restore_journal.operations WHERE workspace_id=$1 AND actor_id=$2 AND request_key=$3`, a.Workspace, a.ID, id+"/production").Scan(&production)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	state, reason := "pending", ""
	if production == "" {
		run, cancel := context.WithDeadline(ctx, expires)
		production, err = r.advanceAuthorized(run, a, id, backup)
		cancel()
		var f *fault.Fault
		switch {
		case production != "":
		case !time.Now().Before(expires):
			state, reason = "failed", "RESTORE_APPROVAL_EXPIRED"
		case errors.As(err, &f) && f.Status < 500:
			state, reason = "failed", f.Reason
		case err != nil:
			return err
		}
	}
	if production != "" {
		state = "submitted"
	}
	_, err = tx.ExecContext(ctx, `UPDATE restore_journal.legacy_requests SET state=$2,production_id=nullif($3,''),reason=nullif($4,''),next_check=clock_timestamp()+interval '1 second' WHERE id=$1`, id, state, production, reason)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Runtime) advanceAuthorized(ctx context.Context, a rs.Actor, id, backup string) (string, error) {
	p, err := r.Preview(ctx, a, id+"/isolated", rs.PreviewInput{BackupID: backup, Target: rs.Isolated})
	if err != nil {
		return "", err
	}
	op, err := r.Execute(ctx, a, id+"/isolated", rs.ExecuteInput{PreviewID: p.ID, Target: rs.Isolated})
	if err != nil {
		return "", err
	}
	switch op.State {
	case "queued", "running":
		return "", nil
	case "succeeded":
	default:
		return "", &fault.Fault{Status: 409, Reason: "REHEARSAL_FAILED", Message: "Isolated restore verification failed."}
	}
	p, err = r.Preview(ctx, a, id+"/production", rs.PreviewInput{BackupID: backup, Target: rs.Production, ExpectedDigest: op.Report.Digest})
	if err != nil {
		return "", err
	}
	prod, err := r.Execute(ctx, a, id+"/production", rs.ExecuteInput{PreviewID: p.ID, Target: rs.Production, RehearsalID: op.ID})
	return prod.ID, err
}
