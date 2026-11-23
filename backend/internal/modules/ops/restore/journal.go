package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
)

var digestFormat = regexp.MustCompile("^sha256:[a-f0-9]{64}$")

// ErrIsolatedTargetUnavailable indicates that the production journal is ready for fencing.
var ErrIsolatedTargetUnavailable = errors.New("isolated restore target is unavailable")

func databaseIdentity(ctx context.Context, db *sql.DB) (cluster, name string, err error) {
	err = db.QueryRowContext(ctx, "SELECT system_identifier::text,current_database() FROM pg_control_system()").Scan(&cluster, &name)
	return
}

func (s *Service) Initialize(ctx context.Context) error {
	j, jn, err := databaseIdentity(ctx, s.Journal)
	if err != nil {
		return err
	}
	p, pn, err := databaseIdentity(ctx, s.Targets[Production].DB)
	if err != nil {
		return err
	}
	if j == p && jn == pn {
		return errors.New("restore needs an independent journal database and a separate rehearsal cluster")
	}
	targets := map[string]string{Production: p + "/" + pn}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	i, in, isolatedErr := databaseIdentity(probe, s.Targets[Isolated].DB)
	cancel()
	if isolatedErr == nil {
		if i == p || i == j {
			return errors.New("restore needs an independent journal database and a separate rehearsal cluster")
		}
		targets[Isolated] = i + "/" + in
	}
	tx, err := s.Journal.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(298,1); SET LOCAL omnihive.restore_journal='on'"); err != nil {
		return err
	}
	ddl, err := migrations.FS.ReadFile("298_restore_journal.sql")
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, string(ddl)); err != nil {
		return err
	}
	for name, id := range targets {
		var bound string
		err = tx.QueryRowContext(ctx, `INSERT INTO restore_journal.targets(name,database_identity) VALUES ($1,$2)
  ON CONFLICT(name) DO UPDATE SET name=excluded.name RETURNING database_identity`, name, id).Scan(&bound)
		if err != nil {
			return err
		}
		if bound != id {
			return errors.New("restore journal belongs to a different target")
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if isolatedErr != nil {
		return ErrIsolatedTargetUnavailable
	}
	return nil
}
func (s *Service) Import(ctx context.Context, id string, record json.RawMessage, review bool) error {
	_, err := s.Journal.ExecContext(ctx, `INSERT INTO restore_journal.backups(id,record,review_required) VALUES($1,$2,$3)
 ON CONFLICT(id) DO UPDATE SET
 record=excluded.record, review_required=restore_journal.backups.review_required OR excluded.review_required
 WHERE (excluded.review_required AND coalesce(restore_journal.backups.record->>'restore_status','') NOT IN ('running','review_required'))
 OR (restore_journal.backups.record->>'status'<>'completed' AND excluded.record->>'status'='completed')`, id, string(record), review)
	return err
}
func (s *Service) authorize(ctx context.Context, a Actor, action string) error {
	if s == nil || s.Access == nil {
		return Unavailable
	}
	_, err := s.Access.Evaluate(ctx, a.ID, a.Workspace, 0, action)
	if err != nil {
		return err
	}
	if s.Journal == nil {
		return Unavailable
	}
	return nil
}
func validKey(key string) bool {
	return len(key) > 0 && len(key) <= 200 && strings.TrimSpace(key) == key
}
func validTarget(t string) bool { return t == Production || t == Isolated }

func (s *Service) Preview(ctx context.Context, a Actor, key string, cmd PreviewInput) (Preview, error) {
	if err := s.authorize(ctx, a, PreviewAction); err != nil {
		return Preview{}, err
	}
	if !validKey(key) {
		return Preview{}, fault.Invalid("/Idempotency-Key")
	}
	if !validTarget(cmd.Target) {
		return Preview{}, fault.Invalid("/target")
	}
	if cmd.BackupID == "" || len(cmd.BackupID) > 200 {
		return Preview{}, fault.Invalid("/backup_id")
	}
	if cmd.ExpectedDigest != "" && !digestFormat.MatchString(cmd.ExpectedDigest) {
		return Preview{}, fault.Invalid("/expected_digest")
	}
	fp, _ := json.Marshal(cmd)
	read := func() (Preview, error) {
		var p Preview
		var existing, digest string
		var counts []byte
		err := s.Journal.QueryRowContext(ctx, `SELECT id,fingerprint,digest,evidence,expires_at FROM restore_journal.previews WHERE workspace_id=$1 AND actor_id=$2 AND request_key=$3`, a.Workspace, a.ID, key).Scan(&p.ID, &existing, &digest, &counts, &p.ExpiresAt)
		if err != nil {
			return p, err
		}
		if existing != string(fp) {
			return p, fault.Conflict
		}
		if err = json.Unmarshal(counts, &p.Impact); err != nil {
			return p, err
		}
		p.Impact = reportCounts(p.Impact)
		p.Verified = true
		p.ExpiresAt = p.ExpiresAt.UTC()
		p.Impact["databases"] = "1"
		p.Impact["source_digest"] = digest
		return p, nil
	}
	if p, err := read(); !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	record, err := s.Archives.Record(ctx, cmd.BackupID)
	if err != nil {
		return Preview{}, err
	}
	raw, err := s.Archives.Open(ctx, record)
	if err != nil {
		return Preview{}, err
	}
	digest, counts, err := inspectArchive(ctx, raw, io.Discard)
	err = errors.Join(err, raw.Close())
	if err != nil {
		return Preview{}, err
	}
	if cmd.ExpectedDigest != "" && cmd.ExpectedDigest != digest {
		return Preview{}, mismatch
	}
	if err = s.Import(ctx, cmd.BackupID, record, false); err != nil {
		return Preview{}, err
	}
	data, _ := json.Marshal(counts)
	_, err = s.Journal.ExecContext(ctx, `INSERT INTO restore_journal.previews(id,workspace_id,actor_id,request_key,fingerprint,backup_id,target,digest,evidence,record)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(workspace_id,actor_id,request_key) DO NOTHING`, "rp_"+uuid.NewString(), a.Workspace, a.ID, key, string(fp), cmd.BackupID, cmd.Target, digest, string(data), string(record))
	if err != nil {
		return Preview{}, err
	}
	return read()
}

const columns = "id,generation,state,stage,target,report"

type scanner interface{ Scan(...any) error }

func scanOperation(row scanner, extra ...any) (Operation, error) {
	var op Operation
	var report []byte
	dest := []any{&op.ID, &op.Generation, &op.State, &op.Stage, &op.Target, &report}
	err := row.Scan(append(dest, extra...)...)
	if len(report) > 0 && err == nil {
		err = json.Unmarshal(report, &op.Report)
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = fault.NotFound
	}
	return op, err
}

func (s *Service) Execute(ctx context.Context, a Actor, key string, cmd ExecuteInput) (Operation, error) {
	if err := s.authorize(ctx, a, ExecuteAction); err != nil {
		return Operation{}, err
	}
	if !validKey(key) {
		return Operation{}, fault.Invalid("/Idempotency-Key")
	}
	if !validTarget(cmd.Target) {
		return Operation{}, fault.Invalid("/target")
	}
	if cmd.Target == Production {
		if err := s.RequireStepUp(ctx, a); err != nil {
			return Operation{}, err
		}
	}
	if (cmd.Target == Production) != (cmd.RehearsalID != "") {
		return Operation{}, prerequisite
	}
	tx, err := s.Journal.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	// Serialize admission of producers across instances, including same-key requests for different targets.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(298,2)"); err != nil {
		return Operation{}, err
	}
	var id, p, r, t string
	err = tx.QueryRowContext(ctx, `SELECT id,preview_id,coalesce(rehearsal_id,''),target FROM restore_journal.operations WHERE workspace_id=$1 AND actor_id=$2 AND request_key=$3`, a.Workspace, a.ID, key).Scan(&id, &p, &r, &t)
	if err == nil {
		if p != cmd.PreviewID || r != cmd.RehearsalID || t != cmd.Target {
			return Operation{}, fault.Conflict
		}
		return scanOperation(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM restore_journal.operations WHERE id=$1", id))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT p.target=$4 AND p.expires_at>clock_timestamp()
 AND NOT EXISTS(SELECT FROM restore_journal.operations o WHERE o.preview_id=p.id)
 AND (p.target='isolated' OR EXISTS(SELECT FROM restore_journal.operations r JOIN restore_journal.previews rp ON rp.id=r.preview_id
 WHERE r.id=$5 AND r.workspace_id=p.workspace_id AND r.state='succeeded' AND r.target='isolated'
 AND rp.backup_id=p.backup_id AND rp.digest=p.digest AND r.updated_at <= p.created_at))
 FROM restore_journal.previews p WHERE p.id=$1 AND p.workspace_id=$2 AND p.actor_id=$3`, cmd.PreviewID, a.Workspace, a.ID, cmd.Target, cmd.RehearsalID).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, fault.NotFound
	}
	if err != nil {
		return Operation{}, err
	}
	if !valid {
		return Operation{}, prerequisite
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM restore_journal.operations WHERE target=$1 AND (state IN ('queued','running','unknown') OR stage='manual_recovery'))
 OR ($1='production' AND EXISTS(SELECT FROM restore_journal.backups WHERE review_required))`, cmd.Target).Scan(&active)
	if err != nil {
		return Operation{}, err
	}
	if active {
		return Operation{}, busy
	}
	op, err := scanOperation(tx.QueryRowContext(ctx, `INSERT INTO restore_journal.operations(id,workspace_id,actor_id,request_key,preview_id,target,rehearsal_id,session_key)
 VALUES($1,$2,$3,$4,$5,$6,nullif($7,''),$8) RETURNING `+columns, "rs_"+uuid.NewString(), a.Workspace, a.ID, key, cmd.PreviewID, cmd.Target, cmd.RehearsalID, a.Session))
	if err != nil {
		return Operation{}, err
	}
	return op, tx.Commit()
}
func validOperationID(id string) bool {
	prefix, value, ok := strings.Cut(id, "_")
	if !ok || (prefix != "rs" && prefix != "ri") {
		return false
	}
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func (s *Service) Get(ctx context.Context, a Actor, id string) (Operation, error) {
	if !validOperationID(id) {
		return Operation{}, fault.NotFound
	}
	if err := s.authorize(ctx, a, ExecuteAction); err != nil {
		return Operation{}, err
	}
	op, _, err := s.lookup(ctx, a.Workspace, id)
	return op, err
}

func (s *Service) lookup(ctx context.Context, w int64, id string) (Operation, int64, error) {
	query := "SELECT " + columns + ",actor_id FROM restore_journal.operations WHERE workspace_id=$1 AND id=$2"
	if strings.HasPrefix(id, "ri_") {
		query = `SELECT r.id,coalesce(o.generation,1),coalesce(o.state, CASE r.state WHEN 'pending' THEN 'queued' ELSE 'failed' END),
  coalesce(o.stage,'queued'),'production',coalesce(o.report,CASE WHEN r.reason IS NOT NULL THEN jsonb_build_object('reason',r.reason) END),r.actor_id
  FROM restore_journal.legacy_requests r LEFT JOIN restore_journal.operations o ON o.id=r.production_id WHERE r.workspace_id=$1 AND r.id=$2`
	}
	var owner int64
	op, err := scanOperation(s.Journal.QueryRowContext(ctx, query, w, id), &owner)
	return op, owner, err
}

func (s *Service) ReadOperation(ctx context.Context, w, actor int64, id string) (identity.Operation, error) {
	if !validOperationID(id) {
		return identity.Operation{}, fault.NotFound
	}
	if s.Journal == nil {
		return identity.Operation{}, Unavailable
	}
	access, err := s.Access.Evaluate(ctx, actor, w, 0, "")
	if err != nil {
		return identity.Operation{}, err
	}
	result, owner, err := s.lookup(ctx, w, id)
	if err != nil {
		return identity.Operation{}, err
	}
	op := identity.Operation{ID: id, OwnerID: owner, State: result.State, DelegatedAction: ExecuteAction, StatusURL: fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", w, id)}
	if access.Require(ExecuteAction) != nil {
		if owner != actor {
			return identity.Operation{}, fault.NotFound
		}
		return op, nil
	}
	op.Result = result
	return op, nil
}

func (s *Service) RequireStepUp(ctx context.Context, a Actor) error {
	if a.Session == "" || s.StepUp == nil {
		return fault.Forbidden
	}
	return s.StepUp.Require(ctx, a)
}
