package byok

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type Planner interface {
	PlanExecution(context.Context, scope.Authorized, policy.Scenario, *policy.Layer, uint64) (trace.DecisionTrace, error)
}
type Reservations interface {
	ReserveKeyBudget(context.Context, *service.KeyBudgetReservationCommand) (*service.KeyBudgetReservationOutcome, error)
	ReleaseKeyBudgetReservation(context.Context, int64, string) error
}

// ExternalBudget reserves only the external estimate against an opted-in budget
// in the same transaction as the attempt. It must never debit the platform wallet.
type ExternalBudget interface {
	AdmitExternal(context.Context, *sql.Tx, scope.Authorized, string, money.Money) error
}
type Gateway struct {
	Store   *Store
	Planner Planner
	Wallet  Reservations
	Budget  ExternalBudget
}
type Dispatch struct {
	RequestID                        string
	Ordinal                          int
	CredentialID, Version, AccountID int64
	Source                           string
	Consent                          bool
	Scenario                         policy.Scenario
	RequestPolicy                    *policy.Layer
	Seed                             uint64
}
type Funding struct {
	Estimate
	Attempt       journal.Attempt
	ReservationID int64
}

// Transport receives an ephemeral native API key for a BYOK dispatch. Wallet
// attempts resolve their own authorized native account through the usual gateway.
type Transport func(context.Context, Funding, map[string]any) error
type check struct {
	consent        bool
	approvedWallet []string
	credential     Credential
	cost           Estimate
	frozen         frozenPrice
	snapshot       string
	deadline       time.Time
}

func (g Gateway) inspect(ctx context.Context, tx *sql.Tx, a scope.Authorized, d Dispatch, plan trace.DecisionTrace) (check, error) {
	var v check
	if d.Scenario.Action != "text.chat" && d.Scenario.Action != "text.embeddings" {
		return v, fault.Forbidden
	}
	if d.Source != "byok" && d.Source != "wallet" || d.Version < 1 || d.AccountID < 1 || d.Ordinal < 1 || d.Scenario.Project != decimalID(a.ProjectID) || d.Scenario.Key != decimalID(a.KeyID) {
		return v, fault.Forbidden
	}
	if plan.Mode != trace.Execution || plan.Selected == nil {
		return v, unavailable
	}
	var at time.Time
	var version string
	e := tx.QueryRowContext(ctx, `SELECT e.pricing_at,e.price_version,e.deadline FROM gateway_executions e
    WHERE e.request_id=$1 AND e.workspace_id=$2 AND e.project_id=$3 AND e.actor_id=$4 AND e.api_key_id=$5 AND e.owner_id=$6 AND e.funding_user_id=$7 AND e.authorization_version=$8
    AND e.model=$9 AND $10 BETWEEN 1 AND e.attempt_budget AND e.finished_at IS NULL AND e.deadline>clock_timestamp()
    AND NOT EXISTS(SELECT FROM gateway_execution_attempts sent WHERE sent.request_id=e.request_id AND sent.ordinal=$10 AND sent.dispatched_at IS NOT NULL)
    AND ($10=1 OR EXISTS(SELECT FROM gateway_execution_attempts prev
        JOIN byok_attempt_funding pf ON pf.attempt_id=prev.attempt_id
        WHERE prev.request_id=e.request_id AND prev.ordinal=$10-1
        AND ($12<>'wallet' OR (pf.credential_id=$13 AND pf.grant_version=$14))
        AND ((prev.decision->>'action'='switch_account' AND prev.account_id<>$11)
             OR (prev.decision->>'action'='retry_same_account' AND prev.account_id=$11))))
    AND NOT EXISTS(SELECT FROM gateway_execution_attempts earlier WHERE earlier.request_id=e.request_id
        AND earlier.ordinal<$10 AND earlier.decision->'excluded' @> jsonb_build_array($11::bigint))
    FOR UPDATE OF e`, d.RequestID, a.WorkspaceID, a.ProjectID, a.ActorID, a.KeyID, a.KeyOwnerID, a.FundingUserID, a.Version, d.Scenario.Model, d.Ordinal, d.AccountID, d.Source, d.CredentialID, d.Version).Scan(&at, &version, &v.deadline)
	if errors.Is(e, sql.ErrNoRows) {
		return v, journal.ErrDispatch
	}
	if e != nil {
		return v, e
	}
	c, e := eligibleOne(ctx, tx, a, d.Scenario.Model, d.CredentialID)
	if e != nil {
		return v, e
	}
	if c.Version != d.Version {
		return v, fault.Stale(c.Version)
	}
	if d.Source == "wallet" && (!c.Fallback || !d.Consent) || d.Source == "byok" && d.AccountID != c.ID {
		return v, fault.Forbidden
	}
	candidate := CandidateID(c, d.AccountID, d.Source)
	if d.Ordinal == 1 && plan.Selected.CandidateID != candidate || !slices.ContainsFunc(plan.Candidates, func(x trace.Candidate) bool { return x.ID == candidate && x.State == "eligible" && x.Rank != nil }) {
		return v, fault.Forbidden
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT graph FROM routing_candidate_snapshots WHERE id=$1 AND workspace_id=$2 AND project_id=$3 AND key_id=$4 AND actor_id=$5 AND scope_version=$6 AND expires_at>clock_timestamp() AND as_of>=clock_timestamp()-interval '60 seconds'`, plan.SnapshotID, a.WorkspaceID, a.ProjectID, a.KeyID, a.ActorID, a.Version).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return v, unavailable
	}
	if e != nil {
		return v, e
	}
	var graph policy.Graph
	if e = json.Unmarshal(raw, &graph); e != nil {
		return v, e
	}
	// Freeze funding consent and the eligible shared candidates on the first
	// permit. A later replan can prune that set, never add a funding source.
	v.consent = d.Consent && c.Fallback
	v.approvedWallet = []string{}
	if d.Ordinal == 1 && v.consent {
		for _, entry := range graph.Snapshot.Candidates {
			f := graph.Facts[entry.ID]
			if !entry.FundingAuthorized || entry.FundingSource != "wallet" || !entry.EndpointFallback || !f.BYOKToPlatform || f.Connection == nil {
				continue
			}
			if entry.ID != CandidateID(c, f.Connection.AccountID, "wallet") || entry.ModelID != d.Scenario.Model {
				continue
			}
			if slices.ContainsFunc(plan.Candidates, func(r trace.Candidate) bool { return r.ID == entry.ID && r.State == "eligible" && r.Rank != nil }) {
				v.approvedWallet = append(v.approvedWallet, entry.ID)
			}
		}
	}
	if d.Ordinal > 1 {
		e = tx.QueryRowContext(ctx, `SELECT f.request_fallback_consent,f.approved_wallet_candidates FROM byok_attempt_funding f JOIN gateway_execution_attempts first USING(attempt_id) WHERE first.request_id=$1 AND first.ordinal=1 AND f.workspace_id=$2`, d.RequestID, a.WorkspaceID).Scan(&v.consent, pq.Array(&v.approvedWallet))
		if errors.Is(e, sql.ErrNoRows) {
			return v, unfunded
		}
		if e != nil {
			return v, e
		}
	}
	if d.Source == "wallet" && (!v.consent || !slices.Contains(v.approvedWallet, candidate)) {
		return v, unfunded
	}
	fact, ok := graph.Facts[candidate]
	if !ok || fact.BYOKToPlatform != (d.Source == "wallet") || !slices.ContainsFunc(graph.Snapshot.Candidates, func(x snapshot.Candidate) bool {
		return x.ID == candidate && x.ModelID == d.Scenario.Model && x.FundingSource == d.Source && x.FundingAuthorized && (d.Source != "wallet" || x.EndpointFallback)
	}) {
		return v, fault.Forbidden
	}
	if d.Source == "wallet" {
		if fact.Connection == nil || fact.Connection.AccountID != d.AccountID {
			return v, fault.Forbidden
		}
		// PlanExecution checked project, pool and proxy evidence. Fence the
		// active account and verified credential again after obtaining a hold.
		var allowed bool
		e = tx.QueryRowContext(ctx, `SELECT true FROM provider_connections pc
        JOIN accounts a ON a.workspace_id=pc.workspace_id AND a.id=pc.account_id AND a.deleted_at IS NULL AND a.status='active' AND a.schedulable
        JOIN connection_verifications v ON v.workspace_id=pc.workspace_id AND v.account_id=pc.account_id AND v.id=pc.verified_id AND v.eligible AND v.expires_at>clock_timestamp()
        JOIN connection_credential_versions k ON k.workspace_id=pc.workspace_id AND k.account_id=pc.account_id AND k.version=v.credential_version AND k.revoked_at IS NULL
        WHERE pc.workspace_id=$1 AND pc.account_id=$2 AND pc.state='active' AND NOT pc.legacy_dirty AND pc.verified_id=$3
        AND NOT EXISTS(SELECT FROM byok_credentials tenant WHERE tenant.id=pc.account_id)
        FOR SHARE OF pc,a,v,k`, a.WorkspaceID, d.AccountID, fact.Connection.VerificationID).Scan(&allowed)
		if errors.Is(e, sql.ErrNoRows) {
			return v, unavailable
		}
		if e != nil {
			return v, e
		}
	} else if fact.Provider != c.Provider {
		return v, fault.Forbidden
	}
	input, e := strconv.ParseInt(d.Scenario.Input, 10, 64)
	if e != nil {
		return v, fault.Invalid("/input_token_bound")
	}
	output, e := strconv.ParseInt(d.Scenario.Output, 10, 64)
	if e != nil {
		return v, fault.Invalid("/output_token_bound")
	}
	v.credential = c
	v.snapshot = plan.SnapshotID
	v.cost, v.frozen, e = price(ctx, tx, c, a.ProjectID, d.AccountID, d.Source, d.Scenario.Model, Bounds{input, output}, at)
	if e != nil {
		return v, e
	}
	if d.Source == "wallet" && (v.cost.PriceVersion == nil || *v.cost.PriceVersion != version) {
		return v, unfunded
	}
	if v.cost.IncludeExternal && (v.cost.External == nil || g.Budget == nil) {
		return v, unfunded
	}
	return v, nil
}

func (v check) holdAmount() *money.Money {
	if v.cost.Source == "wallet" {
		return v.cost.Platform
	}
	if v.cost.Fee.Amount().Compare(money.Amount{}) > 0 {
		return v.cost.Fee
	}
	return nil
}

// Send is the pilot's physical dispatch boundary. It takes a distinct BE-11
// hold for each paid attempt and commits the BE-06 permit with its frozen costs
// before handing a single invocation to transport.
func (g Gateway) Send(ctx context.Context, d Dispatch, send Transport) error {
	a, ok := scope.From(ctx)
	if !ok || g.Planner == nil || send == nil {
		return fault.Forbidden
	}
	if e := journal.Authorize(ctx, a, d.Scenario.Model); e != nil {
		return e
	}
	plan, e := g.Planner.PlanExecution(ctx, a, d.Scenario, d.RequestPolicy, d.Seed)
	if e != nil {
		return e
	}
	tx, e := g.Store.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	v, e := g.inspect(ctx, tx, a, d, plan)
	if e != nil {
		return e
	}
	var reservation int64
	var ref string
	if hold := v.holdAmount(); hold != nil {
		// BE-11 owns its own transaction and locks keys and users. Release
		// preflight locks before taking the hold, then recheck every fence.
		if e = tx.Rollback(); e != nil {
			return e
		}
		if g.Wallet == nil {
			return unfunded
		}
		amount, e := strconv.ParseFloat(hold.String(), 64)
		if e != nil || strconv.FormatFloat(amount, 'f', 8, 64) != hold.String() {
			return unfunded
		}
		ref = fmt.Sprintf("byok:%s:%d:%s", d.RequestID, d.Ordinal, uuid.NewString())
		out, e := g.Wallet.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{RequestID: ref, APIKeyID: a.KeyID, UserID: a.FundingUserID, HoldAmount: amount, TTL: time.Until(v.deadline) + service.DefaultKeyBudgetReservationTTL})
		if e != nil {
			return e
		}
		if out == nil || out.Reservation == nil {
			return unfunded
		}
		reservation = out.Reservation.ID
		release := true
		defer func() {
			if release {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				defer cancel()
				_ = g.Wallet.ReleaseKeyBudgetReservation(cleanup, a.KeyID, ref)
			}
		}()
		plan, e = g.Planner.PlanExecution(ctx, a, d.Scenario, d.RequestPolicy, d.Seed)
		if e != nil {
			return e
		}
		if e = journal.Authorize(ctx, a, d.Scenario.Model); e != nil {
			return e
		}
		tx, e = g.Store.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		v, e = g.inspect(ctx, tx, a, d, plan)
		if e != nil {
			return e
		}
		required := v.holdAmount()
		if required == nil {
			return unfunded
		}
		var valid bool
		e = tx.QueryRowContext(ctx, `SELECT true FROM api_key_budget_reservations WHERE id=$1 AND api_key_id=$2 AND user_id=$3 AND request_id=$4 AND status='reserved' AND balance_held AND expires_at>=$6 AND hold_amount >= $5::numeric FOR UPDATE`, reservation, a.KeyID, a.FundingUserID, ref, required.String(), v.deadline).Scan(&valid)
		if errors.Is(e, sql.ErrNoRows) {
			return unfunded
		}
		if e != nil {
			return e
		}
		// Once COMMIT has been attempted its outcome can be uncertain. Let the
		// BE-11 expiry worker recover an orphan rather than release a live hold.
		return g.commitSend(ctx, tx, a, d, v, reservation, func() { release = false }, send)
	}
	return g.commitSend(ctx, tx, a, d, v, 0, func() {}, send)
}

func (g Gateway) commitSend(ctx context.Context, tx *sql.Tx, a scope.Authorized, d Dispatch, v check, hold int64, committing func(), send Transport) error {
	var secret map[string]any
	var e error
	if d.Source == "byok" {
		secret, e = g.Store.resolve(ctx, tx, v.credential)
		if e != nil {
			return e
		}
	}
	at := journal.Attempt{RequestID: d.RequestID, Number: d.Ordinal, AccountID: d.AccountID, Acceptance: attempt.Unknown, Outcome: "dispatching"}
	e = tx.QueryRowContext(ctx, `INSERT INTO gateway_execution_attempts AS t(attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome)
    SELECT $1,e.request_id,$3,$4,clock_timestamp(),'unknown','dispatching' FROM gateway_executions e WHERE request_id=$2 AND deadline>clock_timestamp() AND finished_at IS NULL
    ON CONFLICT(request_id,ordinal) DO UPDATE SET account_id=excluded.account_id,dispatched_at=excluded.dispatched_at,acceptance='unknown',outcome='dispatching'
    WHERE t.dispatched_at IS NULL RETURNING attempt_id`, uuid.NewString(), d.RequestID, d.Ordinal, d.AccountID).Scan(&at.ID)
	if errors.Is(e, sql.ErrNoRows) {
		return journal.ErrDispatch
	}
	if e != nil {
		return e
	}
	if v.cost.IncludeExternal {
		if e = g.Budget.AdmitExternal(ctx, tx, a, at.ID, *v.cost.External); e != nil {
			return e
		}
	}
	frozen, e := json.Marshal(v.frozen)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO byok_attempt_funding(attempt_id,workspace_id,credential_id,grant_version,secret_version,source,snapshot_id,price_version,reservation_id,byok_external_estimate,platform_service_fee,platform_estimate,frozen_price,include_external_in_budget,request_fallback_consent,approved_wallet_candidates)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,nullif($9::bigint,0),$10,$11,$12,$13,$14,$15,$16)`, at.ID, a.WorkspaceID, d.CredentialID, d.Version, v.credential.SecretVersion, d.Source, v.snapshot, v.cost.PriceVersion, hold, v.cost.External, v.cost.Fee, v.cost.Platform, string(frozen), v.cost.IncludeExternal, v.consent, pq.Array(v.approvedWallet))
	if e != nil {
		return e
	}
	committing()
	if e = tx.Commit(); e != nil {
		return e
	}
	transport, stop := context.WithCancel(ctx)
	defer stop()
	checkRevoked := func() bool {
		var forced bool
		err := g.Store.db.QueryRowContext(transport, `SELECT force_revoked_at IS NOT NULL FROM byok_credentials WHERE workspace_id=$1 AND id=$2`, a.WorkspaceID, d.CredentialID).Scan(&forced)
		return err != nil || forced
	}
	if checkRevoked() {
		// This process has not entered transport. The recorded permit is safe
		// to close as unsent even if a force revocation won after its commit.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		at.Acceptance, at.Outcome = attempt.NotSent, "not_sent"
		if e = journal.New(g.Store.db).Observe(cleanup, at); e != nil {
			return e
		}
		if hold > 0 {
			var ref string
			if e = g.Store.db.QueryRowContext(cleanup, `SELECT request_id FROM api_key_budget_reservations WHERE id=$1 AND api_key_id=$2`, hold, a.KeyID).Scan(&ref); e != nil {
				return e
			}
			if e = g.Wallet.ReleaseKeyBudgetReservation(cleanup, a.KeyID, ref); e != nil {
				return e
			}
		}
		return unavailable
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-transport.Done():
				return
			case <-ticker.C:
				if checkRevoked() {
					stop()
					return
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	return send(transport, Funding{v.cost, at, hold}, secret)
}
