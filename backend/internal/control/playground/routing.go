package playground

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

var unavailableModel = &fault.Fault{Status: 422, Reason: "NO_ELIGIBLE_ENDPOINT", Message: "No verified endpoint supports this call in the selected scope."}

func selectModel(s catalog.Snapshot, id string, _ bool) (catalog.Detail, error) {
	var exact, bySlug []catalog.Detail
	for _, m := range s.Details {
		if m.Model.AccessState != "allowed" || m.Model.RouteState != "eligible" {
			continue
		}
		if m.Model.ID == id {
			exact = append(exact, m)
		} else if m.Model.Model == id || slices.Contains(m.Aliases, id) {
			bySlug = append(bySlug, m)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) == 0 && len(bySlug) == 1 {
		return bySlug[0], nil
	}
	return catalog.Detail{}, unavailableModel
}
func (g *Gateway) route(ctx context.Context, a scope.Authorized, key *service.APIKey, s policy.Scenario, version string, m catalog.Detail, admittedAccounts map[string]int64, funding string) (trace.DecisionTrace, int64, service.PlaygroundPrice, error) {
	now := time.Now().UTC()
	priceCtx := g.Plans.PlaygroundPriceContext(ctx, now)
	var noPrice service.PlaygroundPrice
	graph := policy.Graph{Snapshot: snapshot.CandidateSnapshot{ID: uuid.NewString(), AsOf: now, CatalogVersion: version}, ExpiresAt: now.Add(time.Minute), Facts: map[string]policy.Evidence{}}
	// Only attest candidates in the selected billing group. BE-10 rechecks
	// credential/proxy versions and ownership against these verification IDs.
	rows, err := g.DB.QueryContext(ctx, `SELECT pc.account_id,pc.verified_id::text,ARRAY(SELECT pm.pool_id::text FROM connection_pool_members pm WHERE pm.workspace_id=pc.workspace_id AND pm.account_id=pc.account_id)
 FROM provider_connections pc JOIN account_groups ag ON ag.account_id=pc.account_id AND ag.group_id=$2
 JOIN projects d ON d.workspace_id=pc.workspace_id AND d.is_default
 LEFT JOIN project_resources pr ON pr.workspace_id=pc.workspace_id AND pr.kind='accounts' AND pr.resource_id=pc.account_id
 WHERE pc.workspace_id=$1 AND coalesce(pr.project_id,d.id)=$3 AND pc.state='active' AND NOT pc.legacy_dirty AND pc.verified_id IS NOT NULL`, a.WorkspaceID, key.Group.ID, a.ProjectID)
	if err != nil {
		return trace.DecisionTrace{}, 0, noPrice, err
	}
	type binding struct {
		account      int64
		verification string
		pools        []string
	}
	ids := map[string]binding{}
	prices := map[string]service.PlaygroundPrice{}
	for rows.Next() {
		var b binding
		if err = rows.Scan(&b.account, &b.verification, pq.Array(&b.pools)); err != nil {
			rows.Close()
			return trace.DecisionTrace{}, 0, noPrice, err
		}
		ids["ep_"+catalog.Digest([2]int64{a.ProjectID, b.account})[:32]] = b
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return trace.DecisionTrace{}, 0, noPrice, err
	}
	for _, ep := range m.Endpoints {
		b, ok := ids[ep.ID]
		if !ok || admittedAccounts[ep.ID] != b.account {
			continue
		}
		if _, seen := graph.Facts[ep.ID]; seen {
			continue
		}
		var caps []string
		ok = false
		for _, c := range ep.Capabilities {
			if c.Action == s.Action && c.Protocol == s.Protocol && c.VerifiedAt != nil && c.State == "fresh" && c.Stream != "unsupported" {
				ok = true
				if c.Tools == "supported" {
					caps = append(caps, "tools")
				}
			}
		}
		if !ok {
			continue
		}
		price, err := g.Plans.ResolvePlaygroundPrice(priceCtx, key, b.account, ep.Provider, m.Model.Model, funding, s.Action, s.Protocol, now)
		if err != nil {
			return trace.DecisionTrace{}, 0, noPrice, err
		}
		rate, err := price.InputRate(s.Input)
		if err != nil {
			return trace.DecisionTrace{}, 0, noPrice, err
		}
		prices[ep.ID] = price
		candidate := snapshot.Candidate{ID: ep.ID, ModelID: s.Model, FundingSource: funding, FundingAuthorized: true, Price: rate}
		if ep.RegionEvidence != nil && ep.RegionEvidence.State == "fresh" && len(ep.Regions) == 1 {
			candidate.Region = ep.Regions[0]
			candidate.RegionVerified = true
		}
		if ep.PrivacyEvidence != nil && ep.PrivacyEvidence.State == "fresh" {
			candidate.RetentionVerified = true
			candidate.ZeroRetention = ep.Retention == "strict_zero"
		}
		graph.Facts[ep.ID] = policy.Evidence{Provider: ep.Provider, Connection: &policy.ConnectionFence{AccountID: b.account, VerificationID: b.verification}, Pools: b.pools, Protocols: []string{s.Protocol}, Actions: []string{s.Action}, Capabilities: slices.Compact(caps), VerifiedNoTraining: ep.PrivacyEvidence != nil && ep.PrivacyEvidence.State == "fresh" && ep.Training == "disabled"}
		graph.Snapshot.Candidates = append(graph.Snapshot.Candidates, candidate)
	}
	if len(graph.Snapshot.Candidates) == 0 {
		return trace.DecisionTrace{}, 0, noPrice, unavailableModel
	}
	if err = (policy.SnapshotWriter{DB: g.DB}).Capture(ctx, policy.Caller{Actor: a.ActorID, Workspace: a.WorkspaceID, AccessVersion: a.Version}, s, graph); err != nil {
		return trace.DecisionTrace{}, 0, noPrice, err
	}
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	decision, err := g.Policies.PlanExecution(ctx, a, s, service.GatewayRequestRoutingLayer(ctx), binary.BigEndian.Uint64(seed[:]))
	if err != nil {
		return decision, 0, noPrice, err
	}
	if decision.Selected == nil {
		if service.HasGatewayRoutingConstraints(ctx) {
			return decision, 0, noPrice, policy.NativePolicyDenied
		}
		return decision, 0, noPrice, unavailableModel
	}
	b, ok := ids[decision.Selected.CandidateID]
	if !ok || decision.Selected.FundingSource != funding || decision.Selected.ModelID != s.Model {
		return decision, 0, noPrice, unavailableModel
	}
	price := prices[decision.Selected.CandidateID]
	decision.PriceVersion = price.Version()
	return decision, b.account, price, nil
}
