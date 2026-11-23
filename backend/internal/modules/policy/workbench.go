package policy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/lib/pq"
)

var errNeedWorkbenchGraph = errors.New("workbench snapshot needed")

func needsWorkbenchGraph(in SimulationInput, g Graph, err error) bool {
	return in.Snapshot == "" && errors.Is(err, Expired) &&
		(g.Snapshot.ID == "" || g.SimulationOnly && !g.ExpiresAt.After(time.Now()))
}

func (s *Store) captureWorkbench(ctx context.Context, c Caller, scenario Scenario) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hash, err := scenario.fingerprint()
	if err != nil {
		return err
	}
	lock := fmt.Sprintf("routing/workbench/%d/%d/%d/%x", c.Workspace, c.Actor, c.AccessVersion, hash)
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lock); err != nil {
		return err
	}
	g, err := currentGraph(ctx, tx, c, scenario, "", true)
	if !needsWorkbenchGraph(SimulationInput{Scenario: scenario}, g, err) {
		return err
	}
	if err = readAccess(ctx, tx, c, scenario); err != nil {
		return err
	}
	if scenario.Key != "" {
		owner, err := keyOwner(ctx, tx, c.Workspace, ID(scenario.Project), ID(scenario.Key))
		if err != nil {
			return err
		}
		if owner != c.Actor {
			return Expired
		}
	}
	g, err = connectionGraph(ctx, tx, scenario.target(c.Workspace), scenario)
	if err != nil {
		return err
	}
	g.SimulationOnly = true
	if err = capture(ctx, tx, c, scenario, g); err != nil {
		return err
	}
	return tx.Commit()
}

// connectionGraph reads project-owned, currently verified model bindings.
// Missing method, quota, price, region and privacy evidence remains unknown.
func connectionGraph(ctx context.Context, q queryer, t Target, s Scenario) (Graph, error) {
	now := time.Now().UTC()
	g := Graph{Snapshot: snapshot.CandidateSnapshot{ID: uuid.NewString(), AsOf: now}, Facts: map[string]Evidence{}, ExpiresAt: now.Add(time.Minute)}
	rows, err := q.QueryContext(ctx, `SELECT pc.account_id,pc.provider,a.type,v.id,model.id,coalesce(a.extra ? 'upstream_model_catalog',false),
		ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(m->'endpoints')='array' THEN m->'endpoints' ELSE '[]' END)
		    FROM jsonb_array_elements(CASE WHEN jsonb_typeof(a.extra->'upstream_model_catalog'->'eligible_models')='array'
		     THEN a.extra->'upstream_model_catalog'->'eligible_models' ELSE '[]' END) m WHERE m->>'id'=model.id),
		ARRAY(SELECT pm.pool_id::text FROM connection_pool_members pm WHERE pm.workspace_id=pc.workspace_id AND pm.account_id=pc.account_id ORDER BY pm.pool_id)
	FROM provider_connections pc
	JOIN accounts a ON a.id=pc.account_id AND a.workspace_id=pc.workspace_id AND a.deleted_at IS NULL AND a.status='active' AND a.schedulable
	JOIN projects d ON d.workspace_id=pc.workspace_id AND d.is_default
	LEFT JOIN project_resources r ON r.workspace_id=pc.workspace_id AND r.kind='accounts' AND r.resource_id=pc.account_id
	JOIN connection_verifications v ON v.workspace_id=pc.workspace_id AND v.account_id=pc.account_id AND v.id=pc.verified_id AND v.eligible AND v.expires_at>clock_timestamp()
	JOIN connection_credential_versions cv ON cv.workspace_id=v.workspace_id AND cv.account_id=v.account_id AND cv.version=v.credential_version AND cv.revoked_at IS NULL
	CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(v.snapshot->'models')='array' THEN v.snapshot->'models' ELSE '[]' END) model(id)
	WHERE pc.workspace_id=$1 AND coalesce(r.project_id,d.id)=$2 AND pc.state='active' AND NOT pc.legacy_dirty
	AND ($4::bigint=0 OR EXISTS(SELECT FROM api_keys k WHERE k.workspace_id=pc.workspace_id AND k.id=$4
	    AND (EXISTS(SELECT FROM account_groups ag WHERE ag.group_id=k.group_id AND ag.account_id=pc.account_id)
	    OR k.group_id IS NULL AND NOT EXISTS(SELECT FROM account_groups ag WHERE ag.account_id=pc.account_id))))
	AND (model.id=$3 OR left($3,2)='m_') AND (cardinality(pc.model_ids)=0 OR model.id=ANY(pc.model_ids))
	AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind='accounts' AND q.resource_id=pc.account_id)
	AND `+verifiedProxy+`
	ORDER BY pc.account_id,model.id`, t.Workspace, t.Project, s.Model, t.Key)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	for rows.Next() {
		var binding ConnectionFence
		var provider, auth, model string
		var methods, pools []string
		var hasCatalog bool
		if err = rows.Scan(&binding.AccountID, &provider, &auth, &binding.VerificationID, &model, &hasCatalog, pq.Array(&methods), pq.Array(&pools)); err != nil {
			return g, err
		}
		if s.Model != model && s.Model != catalog.StableID(provider, model) {
			continue
		}
		id := strconv.FormatInt(binding.AccountID, 10)
		if _, ok := g.Facts[id]; ok {
			continue
		}
		facts := Evidence{Provider: provider, Pools: pools, Connection: &binding}
		identity := domain.ActionIdentity{Platform: provider, HostedAccountType: auth}
		if !hasCatalog {
			methods = catalog.ReviewedMethods(ctx, provider, model)
		}
		for _, method := range methods {
			action, protocol, adapter := catalog.GatewayAction(method)
			if protocol == s.Protocol && action == s.Action && domain.PlatformAction(identity, adapter).Status == domain.ActionImplemented {
				facts.Protocols = []string{protocol}
				facts.Actions = []string{action}
				break
			}
		}
		g.Facts[id] = facts
		g.Snapshot.Candidates = append(g.Snapshot.Candidates, snapshot.Candidate{ID: id, ModelID: s.Model, FundingSource: "wallet", FundingAuthorized: true})
	}
	return g, rows.Err()
}
