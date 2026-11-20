package policy

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/planner"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

type SimulationInput struct {
	Scenario
	Version  string `json:"version"`
	Compare  string `json:"compare_version,omitempty"`
	Snapshot string `json:"snapshot_id,omitempty"`
	Seed     string `json:"seed"`
}
type Diff struct {
	SelectionChanged bool     `json:"selection_changed"`
	BecameEligible   []string `json:"became_eligible"`
	BecameExcluded   []string `json:"became_excluded"`
}
type Simulation struct {
	Impact   Impact               `json:"impact"`
	ID       string               `json:"simulation_id"`
	Expires  time.Time            `json:"expires_at"`
	Trace    trace.DecisionTrace  `json:"trace"`
	Baseline *trace.DecisionTrace `json:"baseline"`
	Diff     *Diff                `json:"diff"`
}
type proof struct {
	GraphDigest                     []byte
	Impact                          Impact
	Policy, Version, Base, Snapshot string
	ETag                            int64
	Caller                          Caller
	Scenario                        Scenario
	Chain                           []string
	Expires                         time.Time
}

func withLayer(chain []Layer, l Layer) []Layer {
	target := slices.Index(levels, l.Definition.Scope.Type)
	out := make([]Layer, 0, len(chain)+1)
	added := false
	for _, v := range chain {
		pos := slices.Index(levels, v.Definition.Scope.Type)
		if !added && pos >= target {
			out = append(out, l)
			added = true
		}
		if pos != target {
			out = append(out, v)
		}
	}
	if !added {
		out = append(out, l)
	}
	return out
}
func chainIDs(chain []Layer) []string {
	out := make([]string, 0, len(chain))
	for _, v := range chain {
		out = append(out, v.Version)
	}
	return out
}

func compareTraces(main, base trace.DecisionTrace) *Diff {
	d := &Diff{BecameEligible: []string{}, BecameExcluded: []string{}}
	d.SelectionChanged = (main.Selected == nil) != (base.Selected == nil) || main.Selected != nil && base.Selected != nil && *main.Selected != *base.Selected
	eligible := map[string]bool{}
	for _, c := range base.Candidates {
		eligible[c.ID] = c.State == "eligible"
	}
	for _, c := range main.Candidates {
		if c.State == "eligible" && !eligible[c.ID] {
			d.BecameEligible = append(d.BecameEligible, c.ID)
		}
		if c.State != "eligible" && eligible[c.ID] {
			d.BecameExcluded = append(d.BecameExcluded, c.ID)
		}
	}
	return d
}

func readAccess(ctx context.Context, q queryer, c Caller, s Scenario) error {
	var ok bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM projects p JOIN workspaces w ON w.id=p.workspace_id
 WHERE w.id=$1 AND p.id=$2 AND p.archived_at IS NULL AND w.scope_version=$3)`, c.Workspace, ID(s.Project), c.AccessVersion).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fault.Forbidden
	}
	if s.Key == "" {
		return nil
	}
	// Check authorization metadata only. Key bytes and credential digests are never selected.
	err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM api_keys k
 JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
 LEFT JOIN project_key_policies x ON x.workspace_id=k.workspace_id AND x.api_key_id=k.id
 LEFT JOIN project_resources r ON r.kind='api_keys' AND r.resource_id=k.id AND r.workspace_id=k.workspace_id
 WHERE k.workspace_id=$1 AND k.id=$2 AND coalesce(x.project_id,r.project_id,d.id)=$3 AND k.status='active' AND k.deleted_at IS NULL
 AND (k.expires_at IS NULL OR k.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT FROM identity_scope_quarantine WHERE kind='api_keys' AND resource_id=k.id)
 AND (x.api_key_id IS NULL OR ((NOT x.member_bound OR x.issued_membership IS NOT NULL) AND $4=ANY(x.actions)
 AND (x.model_mode='inherit' OR x.model_mode='allow_list' AND $5=ANY(x.models))))
 AND EXISTS(SELECT FROM api_key_credentials cr WHERE cr.api_key_id=k.id AND cr.workspace_id=k.workspace_id AND cr.revoked_at IS NULL
 AND (cr.valid_until IS NULL OR cr.valid_until>clock_timestamp()) AND
 (x.api_key_id IS NULL AND cr.status IN ('grace','current') OR x.api_key_id IS NOT NULL AND cr.status='project')))`,
		c.Workspace, ID(s.Key), ID(s.Project), s.Action, s.Model).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fault.Forbidden
	}
	return nil
}

func (s *Store) Simulate(ctx context.Context, c Caller, id string, in SimulationInput) (Simulation, error) {
	out, err := s.simulate(ctx, c, id, in)
	if !errors.Is(err, needWorkbenchGraph) {
		return out, err
	}
	if err = s.captureWorkbench(ctx, c, in.Scenario); err != nil {
		return out, err
	}
	return s.simulate(ctx, c, id, in)
}

func (s *Store) simulate(ctx context.Context, c Caller, id string, in SimulationInput) (Simulation, error) {
	var out Simulation
	if err := in.validate(); err != nil {
		return out, err
	}
	seed, err := strconv.ParseUint(in.Seed, 10, 64)
	if err != nil || strconv.FormatUint(seed, 10) != in.Seed {
		return out, fault.Invalid("/seed")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = readAccess(ctx, tx, c, in.Scenario); err != nil {
		return out, err
	}
	h, err := head(ctx, tx, c.Workspace, id, false)
	if err != nil {
		return out, err
	}
	t := in.target(c.Workspace)
	if !t.includes(h.Definition.Scope) {
		return out, fault.NotFound
	}
	chain, err := layers(ctx, tx, t)
	if err != nil {
		return out, err
	}
	draft, err := revision(ctx, tx, h, in.Version, false)
	if err != nil {
		return out, err
	}
	var comparison Layer
	if in.Compare != "" {
		comparison, err = revision(ctx, tx, h, in.Compare, false)
		if err != nil {
			return out, err
		}
	}
	g, err := currentGraph(ctx, tx, c, in.Scenario, in.Snapshot, true)
	if needsWorkbenchGraph(in, g, err) {
		return out, needWorkbenchGraph
	}
	if err != nil {
		return out, err
	}
	simulate := func(l Layer) (trace.DecisionTrace, error) {
		e, err := Compose(t, withLayer(chain, l))
		if err != nil {
			return trace.DecisionTrace{}, err
		}
		e.Planner.Version = l.Version
		features := planner.Features{Mode: trace.Simulation, ModelID: in.Model, Seed: seed}
		d := planner.Plan(features, e.Planner, e.Prepare(g, in.Scenario))
		// Stable opaque labels across baseline and draft, specific to this graph.
		label := func(id string) string { return "candidate_" + s.mac("candidate/"+g.Snapshot.ID, []byte(id))[:20] }
		for i := range d.Candidates {
			d.Candidates[i].ID = label(d.Candidates[i].ID)
		}
		if d.Selected != nil {
			d.Selected.CandidateID = label(d.Selected.CandidateID)
		}
		return d, nil
	}
	out.Trace, err = simulate(draft)
	if err != nil {
		return out, err
	}
	if in.Compare != "" {
		baseline, err := simulate(comparison)
		if err != nil {
			return out, err
		}
		out.Baseline = &baseline
		out.Diff = compareTraces(out.Trace, baseline)
	}
	out.Impact, err = impact(ctx, tx, c.Workspace, h.Definition.Scope)
	if err != nil {
		return out, err
	}
	out.Expires = g.ExpiresAt.UTC()
	fingerprint, err := digest(g)
	if err != nil {
		return out, err
	}
	out.ID, err = s.sign("simulation", proof{GraphDigest: fingerprint, Impact: out.Impact, Policy: h.ID, Version: in.Version, Base: h.Active, Snapshot: g.Snapshot.ID, ETag: h.ETag, Caller: c, Scenario: in.Scenario, Chain: chainIDs(chain), Expires: out.Expires})
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

type GatewaySnapshot struct {
	Layers          []Layer
	Version         string
	PublishedAt     time.Time
	ExpiresAt       time.Time
	RevocationEpoch int64
}

func readPublished(ctx context.Context, tx *sql.Tx, t Target) (GatewaySnapshot, error) {
	var g GatewaySnapshot
	var err error
	g.Layers, err = layers(ctx, tx, t)
	if err != nil {
		return g, err
	}
	err = tx.QueryRowContext(ctx, `SELECT epoch,transaction_timestamp()+interval '60 seconds' FROM routing_policy_clock WHERE singleton`).Scan(&g.RevocationEpoch, &g.ExpiresAt)
	if err != nil {
		return g, err
	}
	if len(g.Layers) > 0 {
		g.Version = g.Layers[len(g.Layers)-1].Version
		err = tx.QueryRowContext(ctx, `SELECT max(published_at) FROM routing_policy_outbox WHERE version IN (SELECT value::uuid FROM jsonb_array_elements_text($1) AS ids(value))`, jsonIDs(g.Layers)).Scan(&g.PublishedAt)
	}
	return g, err
}

// Published reads the committed active assignments and epoch together. Gateways
// can use it directly on each admission, without a cache or Redis dependency.
func (s *Store) Published(ctx context.Context, t Target) (GatewaySnapshot, error) {
	var g GatewaySnapshot
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return g, err
	}
	defer tx.Rollback()
	g, err = readPublished(ctx, tx, t)
	if err != nil {
		return g, err
	}
	return g, tx.Commit()
}

// PlanExecution is the gateway reader for an already authorized request. Request
// restrictions participate in the same hierarchy as simulated versions.
func (s *Store) PlanExecution(ctx context.Context, a scope.Authorized, scenario Scenario, request *Layer, seed uint64) (trace.DecisionTrace, error) {
	var result trace.DecisionTrace
	if err := scenario.validate(); err != nil {
		return result, err
	}
	t := scenario.target(a.WorkspaceID)
	if a.ActorID <= 0 || t.Project != a.ProjectID || t.Key != a.KeyID {
		return result, fault.Forbidden
	}
	c := Caller{a.ActorID, a.WorkspaceID, a.Version}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := readAccess(ctx, tx, c, scenario); err != nil {
		return result, err
	}
	pub, err := readPublished(ctx, tx, t)
	if err != nil {
		return result, err
	}
	if request != nil {
		if request.Definition.Scope.Type != "request" {
			return result, fault.Invalid("/request/scope")
		}
		pub.Layers = append(pub.Layers, *request)
	}
	e, err := Compose(t, pub.Layers)
	if err != nil {
		return result, err
	}
	graph, err := currentGraph(ctx, tx, c, scenario, "", false)
	if err != nil {
		return result, err
	}
	if time.Since(graph.Snapshot.AsOf) > time.Minute {
		return result, Expired
	}
	result = planner.Plan(planner.Features{Mode: trace.Execution, ModelID: scenario.Model, Seed: seed}, e.Planner, e.Prepare(graph, scenario))
	return result, tx.Commit()
}

func (s *Store) checkProof(ctx context.Context, tx *sql.Tx, c Caller, h Head, token string, currentImpact Impact) error {
	var p proof
	if !s.verify("simulation", token, &p) || p.Policy != h.ID || p.Version != h.Draft || p.Base != h.Active || p.ETag != h.ETag ||
		p.Caller.Workspace != c.Workspace || p.Caller.AccessVersion != c.AccessVersion || !time.Now().Before(p.Expires) {
		return StaleSimulation
	}
	if err := readAccess(ctx, tx, p.Caller, p.Scenario); err != nil {
		return err
	}
	chain, err := layers(ctx, tx, p.Scenario.target(c.Workspace))
	if err != nil {
		return err
	}
	if !slices.Equal(chainIDs(chain), p.Chain) {
		return StaleSimulation
	}
	if currentImpact != p.Impact {
		return StaleSimulation
	}
	g, err := currentGraph(ctx, tx, p.Caller, p.Scenario, p.Snapshot, true)
	if errors.Is(err, Expired) {
		return StaleSimulation
	}
	if err != nil {
		return err
	}
	fingerprint, err := digest(g)
	if err != nil {
		return err
	}
	if !bytes.Equal(fingerprint, p.GraphDigest) {
		return StaleSimulation
	}
	return nil
}
