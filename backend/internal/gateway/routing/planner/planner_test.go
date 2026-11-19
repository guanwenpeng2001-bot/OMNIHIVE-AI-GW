package planner

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

func TestStrictPolicyAndFallback(t *testing.T) {
	price, err := money.Parse("0.000000011")
	if err != nil {
		t.Fatal(err)
	}
	expensive, err := money.Parse("0.000000012")
	if err != nil {
		t.Fatal(err)
	}
	quota := int64(1)
	base := snapshot.Candidate{ModelID: "m1", Region: "eu", RegionVerified: true,
		ZeroRetention: true, RetentionVerified: true, Price: &price, QuotaRemaining: &quota, FundingAuthorized: true}
	var pool []snapshot.Candidate
	add := func(id string, update func(*snapshot.Candidate)) {
		c := base
		c.ID = id
		update(&c)
		pool = append(pool, c)
	}
	add("a", func(c *snapshot.Candidate) { c.RegionVerified = false })
	add("b", func(c *snapshot.Candidate) { c.RetentionVerified = false })
	add("c", func(c *snapshot.Candidate) { c.Price = &expensive })
	add("d", func(c *snapshot.Candidate) { c.QuotaRemaining = nil })
	add("e", func(c *snapshot.Candidate) { c.EndpointFallback = true; c.FundingAuthorized = false })
	add("f", func(c *snapshot.Candidate) { c.ModelID = "m2"; c.Score = 100 })
	add("g", func(c *snapshot.Candidate) { c.EndpointFallback = true; c.Score = 200 })
	add("h", func(c *snapshot.Candidate) { c.Price = nil })
	add("i", func(c *snapshot.Candidate) {})
	policy := EffectivePolicy{Regions: []string{"eu"}, RequireZeroRetention: true, MaxPrice: &price,
		RequireKnownQuota: true, AllowEndpointFallback: true, AllowModelFallback: true,
		Origins: []trace.Origin{{Constraint: "regions", ScopeType: "workspace", ScopeID: "w1", Version: "wp8"}},
	}
	features := Features{Mode: trace.Simulation, ModelID: "m1", Seed: 9041, PinnedID: "a"}
	in := snapshot.CandidateSnapshot{ID: "snap1", AsOf: time.Unix(1000, 0).UTC(), Candidates: pool}
	got := Plan(features, policy, in)
	if got.Selected == nil || got.Selected.CandidateID != "i" {
		t.Fatalf("invalid selection: %+v", got.Selected)
	}
	var ranked, reasons []string
	for _, candidate := range got.Candidates {
		if candidate.Rank != nil {
			ranked = append(ranked, candidate.ID)
		} else {
			reasons = append(reasons, candidate.Reasons[0].Code)
		}
	}
	if !slices.Equal(ranked, []string{"i", "g", "f"}) {
		t.Fatalf("fallback ordering: %v", ranked)
	}
	if !slices.Equal(reasons, []string{"REGION_UNVERIFIED", "RETENTION_INELIGIBLE", "PRICE_CEILING",
		"QUOTA_UNKNOWN", "FUNDING_NOT_AUTHORIZED", "PRICE_UNKNOWN"}) {
		t.Fatalf("exclusions: %v", reasons)
	}
	if got.Candidates[3].Reasons[0].Origin != "wp8" {
		t.Fatalf("region policy origin missing: %+v", got.Candidates[3])
	}
	policy.AllowEndpointFallback = false
	policy.AllowModelFallback = false
	policy.Regions = []string{}
	if denied := Plan(features, policy, in); denied.Selected != nil {
		t.Fatalf("empty region intersection admitted a candidate: %+v", denied.Selected)
	}
}

func TestReplayAndTraceJSON(t *testing.T) {
	in := snapshot.CandidateSnapshot{ID: "snap1", Candidates: []snapshot.Candidate{
		{ID: "1", ModelID: "m1", Score: 10},
		{ID: "2", ModelID: "m1", Score: 9},
		{ID: "3", ModelID: "m1", Score: 8},
		{ID: "4", ModelID: "m1", Score: 7},
	}}
	features := Features{ModelID: "m1", Seed: 9041, AffinityIDs: []string{"2"}}
	policy := EffectivePolicy{TopK: 3}
	got := Plan(features, policy, in)
	want := Plan(features, policy, in)
	if !reflect.DeepEqual(got, want) || got.Selected == nil || got.Selected.CandidateID != "2" {
		t.Fatalf("non-reproducible affinity result: %+v", got)
	}
	if !slices.Contains(got.Uncertainties, "QUOTA_UNKNOWN") {
		t.Fatal("missing unknown quota uncertainty")
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mode", "snapshot_id", "seed", "origins", "stages", "candidates", "selected", "uncertainties"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing JSON field %s", key)
		}
	}
	if string(fields["seed"]) != `"9041"` || string(fields["mode"]) != `"simulation"` {
		t.Fatalf("invalid replay metadata: %s", body)
	}
	features.AffinityIDs = nil
	got = Plan(features, policy, in)
	slices.Reverse(in.Candidates)
	features.Mode = trace.Execution
	want = Plan(features, policy, in)
	want.Mode = trace.Simulation
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("simulation and production diverged: %+v / %+v", got, want)
	}

	// The simulation API accepts no context or execution ports. Even with a
	// sink installed in a caller's context, planning performs no emission.
	ctx := trace.WithSink(context.Background(), func(context.Context, trace.DecisionTrace) { t.Fatal("simulation emitted") })
	simulate := func(context.Context) trace.DecisionTrace {
		features.Mode = trace.Simulation
		return Plan(features, policy, in)
	}
	simulate(ctx)
}
