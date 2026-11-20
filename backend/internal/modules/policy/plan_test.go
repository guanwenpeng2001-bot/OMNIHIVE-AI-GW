package policy

import (
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/planner"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/stretchr/testify/require"
)

func TestScopedComposition(t *testing.T) {
	target := Target{1, 2, 3}
	scopes := []Scope{{"platform", "platform", ""}, {"workspace", "1", ""}, {"project", "2", ""}, {"api_key", "3", "2"}, {"request", "request", ""}}
	modes := []*List{{"allow_list", []string{"EU", "US"}}, {"allow_list", []string{"EU", "AP"}}, {"inherit", nil}, nil, {"allow_list", []string{"AP", "EU"}}}
	var chain []Layer
	yes, no := true, false
	for i, sc := range scopes {
		d := Definition{Name: sc.Type, Scope: sc, Constraints: Constraints{Regions: modes[i]}}
		if i == 0 {
			d.Fallback.Endpoint = &no
			d.Candidates.Pools = []string{"p", "q"}
			d.Constraints.Training = "prohibited"
		}
		if i == 2 {
			d.Fallback.Endpoint = &yes
			d.Candidates.Pools = []string{"q", "r"}
			d.Preferences.Template = "stable"
		}
		if i == 4 {
			d.Preferences.Template = "low_latency"
		}
		chain = append(chain, Layer{sc.Type, d})
	}
	e, err := Compose(target, chain)
	require.NoError(t, err)
	require.Equal(t, []string{"EU"}, e.Planner.Regions)
	require.Equal(t, []string{"q"}, e.Pools)
	require.False(t, e.Planner.AllowEndpointFallback)
	require.True(t, e.NoTraining)
	require.Equal(t, "low_latency", e.Template)
	chain[2].Definition.Constraints.Regions = &List{Mode: "deny_all"}
	e, err = Compose(target, chain)
	require.NoError(t, err)
	require.NotNil(t, e.Planner.Regions)
	require.Empty(t, e.Planner.Regions)
	chain[2].Definition.Constraints.Regions = &List{Mode: "allow_list"}
	_, err = Compose(target, chain)
	require.Error(t, err)
	_, err = Compose(Target{9, 2, 3}, chain)
	require.Error(t, err)
}

func TestFallbackConstraintsAndRanking(t *testing.T) {
	yes := true
	models := []string{"backup"}
	providers := []string{"preferred"}
	l := Layer{"v", Definition{Name: "EU", Scope: Scope{Type: "workspace", ID: "1"}, Constraints: Constraints{Regions: &List{"allow_list", []string{"EU"}}, Required: []string{"tools"}, Training: "prohibited"},
		Preferences: Preferences{Providers: &providers}, Fallback: Fallback{Endpoint: &yes, Models: &models}}}
	e, err := Compose(Target{Workspace: 1}, []Layer{l})
	require.NoError(t, err)
	g := Graph{Facts: map[string]Evidence{}, Snapshot: snapshot.CandidateSnapshot{ID: "snapshot"}}
	for _, id := range []string{"de", "fr", "us", "no-training", "unfunded", "wrong-model", "wrong-protocol"} {
		c := snapshot.Candidate{ID: id, ModelID: "backup", EndpointFallback: true, Region: "EU", RegionVerified: true, FundingAuthorized: true}
		f := Evidence{Provider: "ordinary", Protocols: []string{"chat"}, Actions: []string{"text.chat"}, Capabilities: []string{"tools"}, VerifiedNoTraining: true}
		switch id {
		case "fr":
			f.Provider = "preferred"
		case "us":
			c.Region = "US"
		case "no-training":
			f.VerifiedNoTraining = false
		case "unfunded":
			c.FundingAuthorized = false
		case "wrong-model":
			c.ModelID = "other"
		case "wrong-protocol":
			f.Protocols = nil
		}
		g.Snapshot.Candidates = append(g.Snapshot.Candidates, c)
		g.Facts[id] = f
	}
	scenario := Scenario{Model: "main", Action: "text.chat", Protocol: "chat"}
	for _, mode := range []trace.Mode{trace.Execution, trace.Simulation} {
		f := planner.Features{Mode: mode, ModelID: "main", Seed: 7}
		d := planner.Plan(f, e.Planner, e.Prepare(g, scenario))
		require.Equal(t, "fr", d.Selected.CandidateID)
		require.Equal(t, d, planner.Plan(f, e.Planner, e.Prepare(g, scenario)))
		n := 0
		for _, c := range d.Candidates {
			if c.State == "eligible" {
				n++
			}
		}
		require.Equal(t, 2, n)
	}
}
func TestExactRateUsesUnroundedInput(t *testing.T) {
	now := time.Now().UTC()
	rate, err := money.NewRate("USD", "0.000000000001", "input_token", "1")
	require.NoError(t, err)
	v := pricing.Version{ID: "exact", Currency: "USD", EffectiveFrom: now, Rules: []pricing.Rule{{Kind: "input", Rate: rate}}}
	value, err := ExactInputRate(v, now, "1000", "")
	require.NoError(t, err)
	require.Equal(t, "0.000001", value.String())
}

func TestWorkbenchCapturePreservesHistoricalGraphs(t *testing.T) {
	missing := Graph{}
	expired := Graph{Snapshot: snapshot.CandidateSnapshot{ID: "previous"}, ExpiresAt: time.Now().Add(-time.Minute)}
	require.True(t, needsWorkbenchGraph(SimulationInput{}, missing, Expired))
	require.False(t, needsWorkbenchGraph(SimulationInput{Snapshot: "previous"}, missing, Expired))
	require.False(t, needsWorkbenchGraph(SimulationInput{}, expired, Expired))
	expired.SimulationOnly = true
	require.True(t, needsWorkbenchGraph(SimulationInput{}, expired, Expired))
	require.False(t, needsWorkbenchGraph(SimulationInput{Snapshot: "previous"}, expired, Expired))
	expired.ExpiresAt = time.Now().Add(time.Minute)
	require.False(t, needsWorkbenchGraph(SimulationInput{}, expired, Expired))
}
