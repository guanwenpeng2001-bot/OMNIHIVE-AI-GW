package verification

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

const (
	maxPending = 64
	maxRunning = 4

	observationHours   = 24
	queueWaitMinutes   = 5
	leaseSeconds       = 30
	probeTimeout       = 15 * time.Second
	queueDatabaseLimit = 5 * time.Second
)

func (s *Store) Enqueue(ctx context.Context, actor int64, key string, m Matrix) (identity.Operation, error) {
	if err := authorize(ctx, s.access, actor, VerifyGrant); err != nil {
		return identity.Operation{}, err
	}
	if err := validateKey(key); err != nil {
		return identity.Operation{}, err
	}
	if err := m.validate(true); err != nil {
		return identity.Operation{}, err
	}

	cmd := newCommand(actor, "verify", key, m)
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return identity.Operation{}, err
	}
	defer tx.Rollback()
	id, err := cmd.replay(ctx, tx)
	if err != nil {
		return identity.Operation{}, err
	}
	if id != uuid.Nil {
		return loadOperation(ctx, tx, id, time.Now())
	}
	target, err := resolveMatrix(ctx, tx, m)
	if err != nil {
		return identity.Operation{}, err
	}
	var maturity string
	err = tx.QueryRowContext(ctx, `SELECT maturity FROM catalog_compatibility_publications
		WHERE cell_id=$1 ORDER BY version DESC LIMIT 1`, target.cellID()).Scan(&maturity)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return identity.Operation{}, err
	}
	if maturity == closed {
		return identity.Operation{}, badTransition
	}
	pending, _, err := reapAndCount(ctx, tx)
	if err != nil {
		return identity.Operation{}, err
	}
	if pending >= maxPending {
		return identity.Operation{}, queueFull
	}

	id = uuid.New()
	targetID, _ := prefixedID(target.ID, "vt_")
	res, err := tx.ExecContext(ctx, `INSERT INTO catalog_verification_jobs(id,target_id,actor_id,state)
		VALUES ($1,$2,$3,'queued') ON CONFLICT DO NOTHING`, id, targetID, actor)
	if err != nil {
		return identity.Operation{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return identity.Operation{}, err
	}
	if n == 0 {
		return identity.Operation{}, inProgress
	}
	if err := cmd.save(ctx, tx, id); err != nil {
		return identity.Operation{}, err
	}
	out, err := loadOperation(ctx, tx, id, time.Now())
	if err != nil {
		return identity.Operation{}, err
	}
	return out, tx.Commit()
}

func resolveMatrix(ctx context.Context, tx *sql.Tx, m Matrix) (Target, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+targetFields+` FROM catalog_verification_targets
		WHERE entry_id=$1 AND adapter_version=$2 AND action=$3 AND account_mode=$4 AND ($5='' OR protocol=$5)
		ORDER BY protocol LIMIT 2`, m.EntryID, m.AdapterVersion, m.Action, m.AccountMode, m.Protocol)
	if err != nil {
		return Target{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Target{}, err
		}
		return Target{}, fault.NotFound
	}
	t, err := scanTarget(rows)
	if err != nil {
		return Target{}, err
	}
	if rows.Next() {
		return Target{}, fault.Invalid("/protocol")
	}
	return t, rows.Err()
}

func reapAndCount(ctx context.Context, tx *sql.Tx) (pending, running int, err error) {
	if _, err = tx.ExecContext(ctx, `WITH expired AS (
		UPDATE catalog_verification_jobs SET state='unknown',lease_until=NULL
		WHERE (state='running' AND lease_until<=statement_timestamp()) OR
			(state='queued' AND queued_at<=statement_timestamp()-make_interval(mins=>$1))
		RETURNING id,target_id,queued_at,started_at
	)
	INSERT INTO catalog_verification_evidence (id,target_id,operation_id,origin,state,reason,sample_count,window_start,observed_at,expires_at,source_revision)
	SELECT gen_random_uuid(),target_id,id,'endpoint_probe','unknown',CASE WHEN started_at IS NULL THEN 'QUEUE_EXPIRED' ELSE 'WORKER_LEASE_EXPIRED' END,
		0,coalesce(started_at,queued_at),statement_timestamp(),statement_timestamp()+make_interval(hours=>$2),''
	FROM expired`, queueWaitMinutes, observationHours); err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE state='running')
		FROM catalog_verification_jobs WHERE state IN ('queued','running')`).Scan(&pending, &running)
	return pending, running, err
}

func enqueueDue(ctx context.Context, tx *sql.Tx, slots int) error {
	_, err := tx.ExecContext(ctx, `WITH due AS (
		SELECT t.id,t.owner_id FROM catalog_verification_targets t
		JOIN users u ON u.id=t.owner_id AND u.status='active' AND u.deleted_at IS NULL AND u.role='admin'
		WHERE t.next_due_at<=statement_timestamp()
			AND NOT EXISTS (SELECT FROM catalog_verification_jobs j WHERE j.target_id=t.id AND j.state IN ('queued','running'))
			AND coalesce((SELECT maturity FROM catalog_compatibility_publications p
				WHERE p.cell_id=t.cell_id ORDER BY version DESC LIMIT 1),'candidate')<>'closed'
		ORDER BY t.next_due_at,t.id LIMIT $1
	), admitted AS (
		INSERT INTO catalog_verification_jobs(id,target_id,actor_id,state)
		SELECT gen_random_uuid(),id,owner_id,'queued' FROM due RETURNING target_id
	)
	UPDATE catalog_verification_targets t SET next_due_at=statement_timestamp()+make_interval(secs=>t.interval_seconds)
	FROM admitted a WHERE t.id=a.target_id`, slots)
	return err
}

type job struct {
	id     uuid.UUID
	actor  int64
	target Target
}

func (s *Store) claim(ctx context.Context) (*job, error) {
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	pending, running, err := reapAndCount(ctx, tx)
	if err != nil {
		return nil, err
	}
	if pending < maxPending {
		if err := enqueueDue(ctx, tx, maxPending-pending); err != nil {
			return nil, err
		}
	}
	if running >= maxRunning {
		return nil, tx.Commit()
	}

	var j job
	var target uuid.UUID
	err = tx.QueryRowContext(ctx, `UPDATE catalog_verification_jobs SET state='running',
		started_at=statement_timestamp(),lease_until=statement_timestamp()+make_interval(secs=>$1)
		WHERE id=(SELECT id FROM catalog_verification_jobs WHERE state='queued' ORDER BY queued_at,id LIMIT 1)
		RETURNING id,actor_id,target_id`, leaseSeconds).Scan(&j.id, &j.actor, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	j.target, err = loadTarget(ctx, tx, target)
	if err != nil {
		return nil, err
	}
	return &j, tx.Commit()
}

func (s *Store) finish(ctx context.Context, j *job, found probeResult) error {
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := found.state
	if found.state == success {
		state = "succeeded"
	}
	_, err = tx.ExecContext(ctx, `WITH done AS (
		UPDATE catalog_verification_jobs SET state=$2,lease_until=NULL
		WHERE id=$1 AND state='running' AND lease_until>statement_timestamp()
		RETURNING id,target_id,started_at
	)
	INSERT INTO catalog_verification_evidence (id,target_id,operation_id,origin,state,reason,sample_count,window_start,observed_at,expires_at,fixture_id,source_revision)
	SELECT gen_random_uuid(),target_id,id,'endpoint_probe',$3,$4,$5,started_at,statement_timestamp(),
		statement_timestamp()+make_interval(hours=>$6),nullif($7,''),$8 FROM done`,
		j.id, state, found.state, found.reason, found.samples, observationHours, found.fixtureID, found.revision)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type observer interface {
	observe(context.Context, Target) (probeResult, error)
}

type Worker struct {
	store *Store
	probe observer
}

func NewWorker(store *Store, probe *ConnectionProber) *Worker {
	return &Worker{store: store, probe: probe}
}

// Run belongs to the process worker lifecycle. A lost worker's lease expires
// into a terminal unknown operation, so a second worker cannot overwrite it.
func (w *Worker) Run(ctx context.Context) {
	var workers sync.WaitGroup
	for range maxRunning {
		workers.Go(func() {
			for ctx.Err() == nil {
				worked, err := w.runOne(ctx)
				if worked && err == nil {
					continue
				}
				delay := 2 * time.Second
				if err != nil && ctx.Err() == nil {
					slog.WarnContext(ctx, "catalog verification worker storage unavailable", "error", err)
					delay = 10 * time.Second
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
				case <-timer.C:
				}
				timer.Stop()
			}
		})
	}
	workers.Wait()
}

func (w *Worker) runOne(ctx context.Context) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, queueDatabaseLimit)
	j, err := w.store.claim(claimCtx)
	cancel()
	if err != nil || j == nil {
		return false, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	result := w.runProbe(probeCtx, j)
	switch probeCtx.Err() {
	case context.DeadlineExceeded:
		result = inconclusive("PROBE_TIMEOUT")
	case context.Canceled:
		result = inconclusive("WORKER_STOPPED")
	}
	cancel()

	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), queueDatabaseLimit)
	defer cancel()
	return true, w.store.finish(cleanup, j, result)
}

func (w *Worker) runProbe(ctx context.Context, j *job) probeResult {
	if err := authorize(ctx, w.store.access, j.actor, VerifyGrant); err != nil {
		if denied(err) {
			return inconclusive("VERIFICATION_ACCESS_REVOKED")
		}
		return inconclusive("DEPENDENCY_UNAVAILABLE")
	}
	result, err := w.probe.observe(ctx, j.target)
	if err != nil {
		return inconclusive("DEPENDENCY_UNAVAILABLE")
	}
	if err := authorize(ctx, w.store.access, j.actor, VerifyGrant); err != nil {
		if denied(err) {
			return inconclusive("VERIFICATION_ACCESS_REVOKED")
		}
		return inconclusive("DEPENDENCY_UNAVAILABLE")
	}
	return result
}
