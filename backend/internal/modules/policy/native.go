package policy

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/planner"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/lib/pq"
)

// NativeAccounts narrows the normal scheduler's candidates for one authorized
// selection. The native scheduler still checks model bindings, protocol, quota
// and capacity. Unverified facts cannot satisfy an explicit constraint.
func (s *Store) NativeAccounts(ctx context.Context, a scope.Authorized, model string) (map[int64]bool, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT a.id,coalesce(pc.provider,a.platform),
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
	// only the published policy constraints to that candidate set.
	for rows.Next() {
		var account int64
		var f Evidence
		if err = rows.Scan(&account, &f.Provider, pq.Array(&f.Pools)); err != nil {
			return nil, err
		}
		id := strconv.FormatInt(account, 10)
		g.Facts[id] = f
		g.Snapshot.Candidates = append(g.Snapshot.Candidates, snapshot.Candidate{ID: id, ModelID: model, FundingAuthorized: true})
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
	return allowed, tx.Commit()
}
