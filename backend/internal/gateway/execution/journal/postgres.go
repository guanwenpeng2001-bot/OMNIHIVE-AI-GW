package journal

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
)

type Postgres struct{ db *sql.DB }

func New(db *sql.DB) *Postgres { return &Postgres{db} }

func (s *Postgres) Admit(ctx context.Context, in Admission) (AdmittedExecution, bool, error) {
	if s == nil || s.db == nil {
		return AdmittedExecution{}, false, ErrDispatch
	}
	a, ok := scope.From(ctx)
	if !ok || a.ActorID <= 0 || a.KeyOwnerID <= 0 || a.KeyID <= 0 || a.FundingUserID <= 0 || a.ProjectID <= 0 || a.Version <= 0 {
		return AdmittedExecution{}, false, ErrScope
	}
	var key, payload any
	var fingerprint []byte
	if in.Registration == nil && in.Kind == "job" {
		in.Registration = JobRegistrationFrom(ctx, in.Route)
	}
	if in.Registration != nil {
		key, payload = in.Registration.key[:], in.Registration.payload[:]
		fingerprint = in.Registration.payload[:]
	}
	e := AdmittedExecution{RequestID: in.RequestID, Scope: a, AttemptBudget: in.AttemptBudget, Deadline: in.Deadline, Versions: in.Versions, ReservationRef: in.RequestID}
	var id string
	err := s.db.QueryRowContext(ctx, `WITH admitted AS (
 INSERT INTO gateway_executions(request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,
 kind,route,model,legacy_id,reservation_ref,attempt_budget,deadline,model_version,price_version,pricing_at,idempotency_hash,fingerprint)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
 ON CONFLICT DO NOTHING RETURNING request_id)
 INSERT INTO gateway_execution_attempts(attempt_id,request_id,ordinal)
 SELECT $21,request_id,1 FROM admitted RETURNING request_id`,
		e.RequestID, a.WorkspaceID, a.ProjectID, a.ActorID, a.KeyID, a.KeyOwnerID, a.FundingUserID, a.Version, in.Kind, in.Route, in.Model, in.LegacyID, e.ReservationRef, in.AttemptBudget, in.Deadline, in.Versions.Model, in.Versions.Price, in.Versions.PricingAt, key, payload, NewID()).Scan(&id)
	if err == nil {
		return e, false, nil
	}
	writeErr := err
	if !errors.Is(writeErr, sql.ErrNoRows) {
		// The server may have committed before the driver lost its acknowledgement.
		lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		ctx = lookup
	}
	var original AdmittedExecution
	var storedFingerprint []byte
	var kind, model, legacy string
	o := &original.Scope
	err = s.db.QueryRowContext(ctx, `SELECT request_id,project_id,workspace_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,
 attempt_budget,deadline,model_version,price_version,pricing_at,reservation_ref,fingerprint,kind,model,legacy_id FROM gateway_executions
 WHERE workspace_id=$1 AND actor_id=$2 AND route=$3 AND api_key_id=$8 AND project_id=$9 AND
 (idempotency_hash=$4 OR request_id=$5 OR (kind='job' AND $6='job' AND legacy_id<>'' AND legacy_id=$7))`,
		a.WorkspaceID, a.ActorID, in.Route, key, e.RequestID, in.Kind, in.LegacyID, a.KeyID, a.ProjectID).Scan(&original.RequestID, &o.ProjectID, &o.WorkspaceID, &o.ActorID, &o.KeyID, &o.KeyOwnerID, &o.FundingUserID, &o.Version,
		&original.AttemptBudget, &original.Deadline, &original.Versions.Model, &original.Versions.Price, &original.Versions.PricingAt, &original.ReservationRef, &storedFingerprint, &kind, &model, &legacy)
	if err != nil {
		if !errors.Is(writeErr, sql.ErrNoRows) {
			return AdmittedExecution{}, false, writeErr
		}
		return AdmittedExecution{}, false, err
	}
	if o.ProjectID != a.ProjectID || o.KeyID != a.KeyID || o.KeyOwnerID != a.KeyOwnerID || o.FundingUserID != a.FundingUserID || !bytes.Equal(storedFingerprint, fingerprint) || kind != in.Kind || model != in.Model || (kind == "job" && legacy != in.LegacyID) {
		return AdmittedExecution{}, false, ErrConflict
	}
	// Only this invocation knows its newly generated request ID. A confirmed
	// commit after a lost acknowledgement may proceed to the single-use permit.
	replay := errors.Is(writeErr, sql.ErrNoRows) || original.RequestID != in.RequestID
	return original, replay, nil
}

func (s *Postgres) Dispatch(ctx context.Context, e AdmittedExecution, n int, account int64) (Attempt, error) {
	v := Attempt{RequestID: e.RequestID, Number: n, AccountID: account, Acceptance: attempt.Unknown, Outcome: "dispatching"}
	var conversionPlan any
	if plan, ok := protocol.ConversionPlanFromContext(ctx); ok {
		if data, err := json.Marshal(plan); err == nil {
			conversionPlan = string(data)
		}
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO gateway_execution_attempts(attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome,conversion_plan)
 SELECT $1,e.request_id,$3::int,$4::bigint,clock_timestamp(),'unknown','dispatching',$5::jsonb FROM gateway_executions e
 WHERE e.request_id=$2 AND e.finished_at IS NULL AND e.deadline>clock_timestamp() AND $3 BETWEEN 1 AND e.attempt_budget FOR UPDATE OF e
 ON CONFLICT(request_id,ordinal) DO UPDATE SET account_id=excluded.account_id,dispatched_at=excluded.dispatched_at,acceptance='unknown',outcome='dispatching',conversion_plan=COALESCE(excluded.conversion_plan,gateway_execution_attempts.conversion_plan)
 WHERE gateway_execution_attempts.dispatched_at IS NULL RETURNING attempt_id`, NewID(), e.RequestID, n, account, conversionPlan).Scan(&v.ID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrDispatch
	}
	return v, err
}

func (s *Postgres) Observe(ctx context.Context, a Attempt) error {
	_, err := s.db.ExecContext(ctx, `UPDATE gateway_execution_attempts SET acceptance=$3,outcome=$4
 WHERE request_id=$1 AND attempt_id=$2 AND NOT abandoned_uncertain AND decision IS NULL
 AND (outcome IN ('dispatching','unknown') OR $4='rejected')`, a.RequestID, a.ID, a.Acceptance, a.Outcome)
	return err
}

func (s *Postgres) Decision(ctx context.Context, id string, d attempt.AttemptDecision) error {
	if d.AttemptNumber == 0 {
		return nil
	}
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE gateway_execution_attempts SET decision=CASE WHEN abandoned_uncertain THEN decision ELSE $3 END,abandoned_uncertain=abandoned_uncertain OR $4,
 acceptance=CASE WHEN abandoned_uncertain OR $4 THEN 'unknown'
                 WHEN acceptance IN ('accepted','rejected') THEN acceptance ELSE $5 END,
 outcome=CASE WHEN $4 THEN 'unknown' ELSE outcome END WHERE request_id=$1 AND ordinal=$2 AND dispatched_at IS NOT NULL`, id, d.AttemptNumber, body, d.AbandonedUncertain, d.Acceptance)
	return updated(result, err)
}

func updated(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrDispatch
	}
	return err
}

func (s *Postgres) Finish(ctx context.Context, id string, state lifecycle.State, canceled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE gateway_executions SET finished_at=coalesce(finished_at,now()),
 response_state=greatest(response_state,$2),canceled=canceled OR $3 WHERE request_id=$1`, id, state, canceled)
	return err
}
