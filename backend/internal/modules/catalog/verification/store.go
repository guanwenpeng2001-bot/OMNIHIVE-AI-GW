package verification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

type Store struct {
	db     *sql.DB
	access identity.AccessEvaluator
}

func NewStore(db *sql.DB, access identity.AccessEvaluator) *Store {
	return &Store{db: db, access: access}
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type scanner interface{ Scan(...any) error }

func (s *Store) beginMutation(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// The gate spans short mutations only. Workers release it before any I/O
	// to an upstream. It makes the queue's bounds global across replicas.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(316, 1)`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func validateKey(key string) error {
	if key == "" || len(key) > 128 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return fault.Invalid("/Idempotency-Key")
	}
	return nil
}

type command struct {
	actor                  int64
	kind, key, fingerprint string
}

func newCommand(actor int64, kind, key string, in any) command {
	return command{actor: actor, kind: kind, key: key, fingerprint: catalog.Digest(in)}
}

func (c command) replay(ctx context.Context, tx *sql.Tx) (uuid.UUID, error) {
	var fingerprint string
	var id uuid.UUID
	err := tx.QueryRowContext(ctx, `SELECT fingerprint, result_id FROM catalog_verification_commands
		WHERE actor_id=$1 AND kind=$2 AND key=$3`, c.actor, c.kind, c.key).Scan(&fingerprint, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err == nil && c.fingerprint != fingerprint {
		return uuid.Nil, fault.Conflict
	}
	return id, err
}

func (c command) save(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO catalog_verification_commands(actor_id,kind,key,fingerprint,result_id)
		VALUES ($1,$2,$3,$4,$5)`, c.actor, c.kind, c.key, c.fingerprint, id)
	return err
}

const targetFields = `id, owner_id, connection_id, entry_id, provider, model, action, protocol,
	account_mode, adapter_version, interval_seconds, schedule_version, next_due_at, created_at`

func scanTarget(row scanner) (Target, error) {
	var id uuid.UUID
	var t Target
	err := row.Scan(&id, &t.OwnerID, &t.ConnectionID, &t.EntryID, &t.Provider, &t.Model, &t.Action, &t.Protocol,
		&t.AccountMode, &t.AdapterVersion, &t.IntervalSeconds, &t.ScheduleVersion, &t.NextDueAt, &t.CreatedAt)
	t.ID = "vt_" + id.String()
	t.CreatedAt = t.CreatedAt.UTC()
	if t.NextDueAt != nil {
		due := t.NextDueAt.UTC()
		t.NextDueAt = &due
	}
	return t, err
}

func loadTarget(ctx context.Context, q queryer, id uuid.UUID) (Target, error) {
	t, err := scanTarget(q.QueryRowContext(ctx, `SELECT `+targetFields+` FROM catalog_verification_targets WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = fault.NotFound
	}
	return t, err
}

const nextDueSQL = `CASE WHEN $1::int > 0 THEN statement_timestamp()+make_interval(secs=>$1) END`

func (s *Store) CreateTarget(ctx context.Context, actor int64, key string, in CreateTarget) (Target, error) {
	if err := authorize(ctx, s.access, actor, VerifyGrant); err != nil {
		return Target{}, err
	}
	if err := validateKey(key); err != nil {
		return Target{}, err
	}
	if err := in.validate(); err != nil {
		return Target{}, err
	}

	cmd, matrix := newCommand(actor, "target", key, in), in.matrix()
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return Target{}, err
	}
	defer tx.Rollback()
	id, err := cmd.replay(ctx, tx)
	if err != nil {
		return Target{}, err
	}
	if id != uuid.Nil {
		return loadTarget(ctx, tx, id)
	}

	source, err := loadSource(ctx, tx, Target{ConnectionID: in.ConnectionID, Provider: in.Provider, Matrix: matrix})
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, targetUnavailable
	}
	if err != nil {
		return Target{}, err
	}
	if !accountMatchesMode(&source.account, matrix.AccountMode) {
		return Target{}, targetUnavailable
	}
	if !source.implements(matrix) {
		return Target{}, fault.Invalid("/protocol")
	}
	id = uuid.New()
	t, err := scanTarget(tx.QueryRowContext(ctx, `INSERT INTO catalog_verification_targets
		(interval_seconds,next_due_at,id,owner_id,connection_id,cell_id,entry_id,provider,model,action,protocol,account_mode,adapter_version)
		VALUES ($1,`+nextDueSQL+`,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (entry_id,action,protocol,account_mode,adapter_version) DO NOTHING
		RETURNING `+targetFields, in.IntervalSeconds, id, actor, in.ConnectionID, matrix.cellID(), matrix.EntryID, in.Provider,
		in.Model, in.Action, in.Protocol, in.AccountMode, in.AdapterVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, targetExists
	}
	if err != nil {
		return Target{}, err
	}
	if err := importConnectionEvidence(ctx, tx, id, t, source); err != nil {
		return Target{}, err
	}
	if err := cmd.save(ctx, tx, id); err != nil {
		return Target{}, err
	}
	return t, tx.Commit()
}

func (s *Store) SetSchedule(ctx context.Context, actor int64, rawID string, expected int64, interval int) (Target, error) {
	if err := authorize(ctx, s.access, actor, VerifyGrant); err != nil {
		return Target{}, err
	}
	if err := validateInterval(interval); err != nil {
		return Target{}, err
	}
	id, err := prefixedID(rawID, "vt_")
	if err != nil {
		return Target{}, err
	}
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return Target{}, err
	}
	defer tx.Rollback()
	current, err := loadTarget(ctx, tx, id)
	if err != nil {
		return Target{}, err
	}
	if current.ScheduleVersion != expected {
		return Target{}, fault.Stale(current.ScheduleVersion)
	}
	if current.IntervalSeconds == interval && current.OwnerID == actor {
		return current, nil
	}
	t, err := scanTarget(tx.QueryRowContext(ctx, `UPDATE catalog_verification_targets
		SET interval_seconds=$1, next_due_at=`+nextDueSQL+`, schedule_version=schedule_version+1, owner_id=$3
		WHERE id=$2 RETURNING `+targetFields, interval, id, actor))
	if err != nil {
		return Target{}, err
	}
	return t, tx.Commit()
}

func (s *Store) Target(ctx context.Context, actor int64, rawID string) (TargetDetail, error) {
	if err := authorize(ctx, s.access, actor, VerifyGrant); err != nil {
		return TargetDetail{}, err
	}
	id, err := prefixedID(rawID, "vt_")
	if err != nil {
		return TargetDetail{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return TargetDetail{}, err
	}
	defer tx.Rollback()
	t, err := loadTarget(ctx, tx, id)
	if err != nil {
		return TargetDetail{}, err
	}
	now := time.Now()
	latest, err := latestObservation(ctx, tx, id, t.Matrix, now, false)
	if err != nil {
		return TargetDetail{}, err
	}
	lastSuccess, err := latestObservation(ctx, tx, id, t.Matrix, now, true)
	if err != nil {
		return TargetDetail{}, err
	}
	release, err := currentPublication(ctx, tx, t.cellID(), now)
	if err != nil {
		return TargetDetail{}, err
	}
	return TargetDetail{Target: t, LatestAttempt: latest, LastSuccess: lastSuccess, Publication: release}, tx.Commit()
}

const observationFields = `id,origin,state,reason,sample_count,window_start,observed_at,expires_at,fixture_id`

func scanObservation(row scanner, m Matrix, now time.Time) (*Observation, error) {
	var id uuid.UUID
	out := Observation{Matrix: m}
	e := &out.CompatibilityEvidence
	err := row.Scan(&id, &e.Source, &e.State, &out.Reason, &e.SampleCount, &e.WindowStart,
		&e.ObservedAt, &e.ExpiresAt, &e.FixtureID)
	if err != nil {
		return nil, err
	}
	e.ID = "ve_" + id.String()
	e.WindowStart, e.ObservedAt, e.ExpiresAt = e.WindowStart.UTC(), e.ObservedAt.UTC(), e.ExpiresAt.UTC()
	*e = e.At(now)
	return &out, nil
}

func latestObservation(ctx context.Context, q queryer, id uuid.UUID, m Matrix, now time.Time, successful bool) (*Observation, error) {
	obs, err := scanObservation(q.QueryRowContext(ctx, `SELECT `+observationFields+`
		FROM catalog_verification_evidence WHERE target_id=$1 AND (NOT $2::boolean OR state='success')
		ORDER BY observed_at DESC, id DESC LIMIT 1`, id, successful), m, now)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return obs, err
}

func loadOperation(ctx context.Context, q queryer, id uuid.UUID, now time.Time) (identity.Operation, error) {
	var op identity.Operation
	var target uuid.UUID
	var m Matrix
	err := q.QueryRowContext(ctx, `SELECT j.state, j.actor_id, t.id, t.entry_id, t.adapter_version, t.action, t.account_mode, t.protocol
		FROM catalog_verification_jobs j JOIN catalog_verification_targets t ON t.id=j.target_id
		WHERE j.id=$1`, id).Scan(&op.State, &op.OwnerID, &target, &m.EntryID, &m.AdapterVersion, &m.Action, &m.AccountMode, &m.Protocol)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Operation{}, fault.NotFound
	}
	if err != nil {
		return identity.Operation{}, err
	}
	op.ID = "cv_" + id.String()
	op.StatusURL = fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", domain.DefaultWorkspaceID, op.ID)
	op.ResourceID, op.DelegatedAction = &m.EntryID, VerifyGrant
	if op.State != "running" && op.State != "queued" {
		obs, err := scanObservation(q.QueryRowContext(ctx, `SELECT `+observationFields+`
			FROM catalog_verification_evidence WHERE operation_id=$1 AND target_id=$2`, id, target), m, now)
		if err != nil {
			return identity.Operation{}, err
		}
		op.Result = obs
	}
	return op, nil
}

func (s *Store) ReadOperation(ctx context.Context, workspace, actor int64, rawID string) (identity.Operation, error) {
	id, err := prefixedID(rawID, "cv_")
	if err != nil || workspace != domain.DefaultWorkspaceID {
		return identity.Operation{}, fault.NotFound
	}
	if err := authorize(ctx, s.access, actor, VerifyGrant); err != nil {
		if denied(err) {
			err = fault.NotFound
		}
		return identity.Operation{}, err
	}
	return loadOperation(ctx, s.db, id, time.Now())
}

var _ identity.OperationReader = (*Store)(nil)

func proxyPolicy(proxyID sql.NullInt64) string {
	if !proxyID.Valid {
		return ""
	}
	return strconv.FormatInt(proxyID.Int64, 10)
}
