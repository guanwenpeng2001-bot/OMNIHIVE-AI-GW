package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/planner"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/lib/pq"
)

var NativePolicyDenied = &fault.Fault{Status: 403, Reason: "ROUTING_POLICY_DENIED", Message: "No endpoint satisfies the routing policy"}

// NativeAccounts preserves the unconstrained caller contract.
func (s *Store) NativeAccounts(ctx context.Context, a scope.Authorized, model string) (map[int64]bool, error) {
	return s.NativeRequestAccounts(ctx, a, model, nil)
}

// NativeRequestAccounts narrows the normal scheduler's candidates for one authorized
// selection. The native scheduler still checks model bindings, protocol, quota
// and capacity. Unverified facts cannot satisfy an explicit constraint.
func (s *Store) NativeRequestAccounts(ctx context.Context, a scope.Authorized, model string, request *Layer) (map[int64]bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t := Target{a.WorkspaceID, a.ProjectID, a.KeyID}
	chain, err := layers(ctx, tx, t)
	if err != nil {
		return nil, err
	}
	if request != nil {
		if request.Definition.Scope.Type != "request" {
			return nil, fault.Invalid("/request/scope")
		}
		chain = append(chain, *request)
	}
	if len(chain) == 0 {
		return nil, tx.Commit()
	}
	e, err := Compose(t, chain)
	if err != nil {
		return nil, err
	}
	if err = readAccess(ctx, tx, Caller{a.ActorID, a.WorkspaceID, a.Version}, Scenario{Project: strconv.FormatInt(a.ProjectID, 10)}); err != nil {
		return nil, err
	}
	var observations map[ConnectionFence]nativeEndpointObservation
	if e.Planner.Regions != nil || e.Planner.RequireZeroRetention || e.NoTraining {
		observations, err = nativeEndpointObservations(ctx, tx, a, model)
		if err != nil {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,coalesce(pc.provider,a.platform),coalesce(pc.verified_id::text,''),
	 ARRAY(SELECT pm.pool_id::text FROM connection_pool_members pm WHERE pm.workspace_id=a.workspace_id AND pm.account_id=pc.account_id)
	 FROM accounts a JOIN projects d ON d.workspace_id=a.workspace_id AND d.is_default
	 LEFT JOIN project_resources r ON r.workspace_id=a.workspace_id AND r.kind='accounts' AND r.resource_id=a.id
	 LEFT JOIN provider_connections pc ON pc.workspace_id=a.workspace_id AND pc.account_id=a.id
	 WHERE a.workspace_id=$1 AND coalesce(r.project_id,d.id)=$2 AND a.deleted_at IS NULL AND a.status='active' AND a.schedulable
	 AND (pc.account_id IS NULL OR (pc.state='active' AND NOT pc.legacy_dirty AND EXISTS (
	   SELECT FROM connection_verifications v JOIN connection_credential_versions cv ON cv.workspace_id=v.workspace_id
	   AND cv.account_id=v.account_id AND cv.version=v.credential_version AND cv.revoked_at IS NULL
	   WHERE v.id=pc.verified_id AND v.workspace_id=pc.workspace_id AND v.account_id=pc.account_id AND v.eligible AND v.expires_at>clock_timestamp()
	   AND `+verifiedProxy+`)))
	 AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind='accounts' AND q.resource_id=a.id)`, t.Workspace, t.Project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	g := Graph{Facts: map[string]Evidence{}}
	scenario := Scenario{Model: model}
	// Native execution checks the actual protocol and action against the
	// account's final model binding in the scheduler. This pass applies
	// the composed policy constraints to that candidate set.
	for rows.Next() {
		var account int64
		var f Evidence
		var verification string
		if err = rows.Scan(&account, &f.Provider, &verification, pq.Array(&f.Pools)); err != nil {
			return nil, err
		}
		id := strconv.FormatInt(account, 10)
		obs := observations[ConnectionFence{account, verification}]
		f.VerifiedNoTraining = obs.trainingOff
		g.Facts[id] = f
		g.Snapshot.Candidates = append(g.Snapshot.Candidates, snapshot.Candidate{ID: id, ModelID: model, FundingAuthorized: true,
			Region: obs.region, RegionVerified: obs.region != "", RetentionVerified: obs.strictZero, ZeroRetention: obs.strictZero})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	decision := planner.Plan(planner.Features{Mode: trace.Execution, ModelID: model}, e.Planner, e.prepare(g, scenario, false))
	allowed := make(map[int64]bool, len(decision.Candidates))
	for _, c := range decision.Candidates {
		if c.State == "eligible" {
			allowed[ID(c.ID)] = true
		}
	}
	if len(allowed) == 0 && (request != nil || e.Planner.Regions != nil || e.NoTraining || e.Planner.RequireZeroRetention) {
		return nil, NativePolicyDenied
	}
	return allowed, tx.Commit()
}

// Only the trusted frozen execution graph carries verified privacy and region
// evidence. A connection probe by itself proves authorization and reachability.
type nativeEndpointObservation struct {
	region                  string
	strictZero, trainingOff bool
}

func nativeEndpointObservations(ctx context.Context, tx *sql.Tx, a scope.Authorized, model string) (map[ConnectionFence]nativeEndpointObservation, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT graph FROM routing_candidate_snapshots
	WHERE workspace_id=$1 AND project_id=$2 AND actor_id=$3 AND coalesce(key_id,0)=$4 AND scope_version=$5
	AND as_of<=clock_timestamp() AND expires_at>clock_timestamp() AND NOT(graph ? 'simulation_only')
	AND graph->'Snapshot'->'Candidates' @> jsonb_build_array(jsonb_build_object('ModelID',$6::text))
	ORDER BY as_of DESC,id DESC LIMIT 1`, a.WorkspaceID, a.ProjectID, a.ActorID, a.KeyID, a.Version, model).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var graph Graph
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, err
	}
	return verifiedNativeObservations(graph, model), nil
}

func verifiedNativeObservations(graph Graph, model string) map[ConnectionFence]nativeEndpointObservation {
	result := map[ConnectionFence]nativeEndpointObservation{}
	for _, candidate := range graph.Snapshot.Candidates {
		fact := graph.Facts[candidate.ID]
		if candidate.ModelID != model || fact.Connection == nil || fact.Connection.VerificationID == "" || graph.SimulationOnly {
			continue
		}
		obs := nativeEndpointObservation{strictZero: candidate.RetentionVerified && candidate.ZeroRetention, trainingOff: fact.VerifiedNoTraining}
		if candidate.RegionVerified {
			obs.region = candidate.Region
		}
		if previous, ok := result[*fact.Connection]; ok {
			if obs.region != previous.region {
				obs.region = ""
			}
			obs.strictZero = obs.strictZero && previous.strictZero
			obs.trainingOff = obs.trainingOff && previous.trainingOff
		}
		result[*fact.Connection] = obs
	}
	return result
}
