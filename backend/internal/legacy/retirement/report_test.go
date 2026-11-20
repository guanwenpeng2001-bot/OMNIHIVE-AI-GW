package retirement

import (
	"slices"
	"testing"
	"time"
)

func TestCatalogAndReviewOrder(t *testing.T) {
	cs, err := contracts(nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range cs {
		if seen[c.ID] || c.ID == "" || !slices.Contains(domains, c.Domain) || !slices.Contains([]string{"projection", "wrapper", "schema", "credential", "plugin", "identity", "preserve", "baseline"}, c.Kind) || c.Owner == "" || c.Legacy == "" || c.Replacement == "" || c.ExitGate == "" {
			t.Fatalf("invalid contract %q", c.ID)
		}
		seen[c.ID] = true
		if c.Kind == "projection" && (c.State != "hold" || c.RemainingCallers != nil) {
			t.Fatalf("unmeasured projection %s", c.ID)
		}
	}
	p := Report{Entries: cs}.Plan()
	n := 0
	for _, d := range p.Domains {
		n += len(d.Hold) + len(d.Review) + len(d.Preserve) + len(d.Baseline)
	}
	if n != len(cs) {
		t.Fatal("plan lost contracts")
	}
}

func TestProjectionRequiresCurrentFleetCensusAndFullPublishedWindow(t *testing.T) {
	now := time.Now().UTC()
	opened := now.AddDate(0, -4, 0)
	closed := now.AddDate(0, 0, -2)
	observed := now.Add(-time.Hour)
	until := now.Add(time.Hour)
	ref := "a871ef8f-37dc-42f9-a0c8-12341d612346"
	binary := "v2.0.1"
	schema := 319
	zero := int64(0)
	one := int64(1)
	base := Observation{EvidenceID: &ref, ObservedAt: &observed, DeployedVersions: []string{binary}, ObservedCallers: &zero, Complete: true, CoverageFrom: &opened, ValidUntil: &until, NoticeID: &ref, WindowStart: &opened, WindowEnd: &closed, MinimumBinary: &binary, MinimumSchema: &schema, ReconciliationID: &ref, RecoveryDrillID: &ref}
	for _, test := range []struct {
		name, kind, state string
		change            func(*Observation)
	}{
		{"attested", "projection", "review", func(o *Observation) {}},
		{"partial", "projection", "hold", func(o *Observation) { o.Complete = false }},
		{"future report", "projection", "hold", func(o *Observation) { o.ObservedAt = &until }},
		{"expired", "projection", "hold", func(o *Observation) { o.ValidUntil = &now }},
		{"no binary", "projection", "hold", func(o *Observation) { o.DeployedVersions = nil }},
		{"live caller", "projection", "hold", func(o *Observation) { o.ObservedCallers = &one }},
		{"unknown count", "projection", "hold", func(o *Observation) { o.ObservedCallers = nil }},
		{"unpublished", "projection", "hold", func(o *Observation) { o.NoticeID = nil }},
		{"not ended", "projection", "hold", func(o *Observation) { o.WindowEnd = &until }},
		{"partial window", "projection", "hold", func(o *Observation) { o.CoverageFrom = &closed }},
		{"no floor", "projection", "hold", func(o *Observation) { o.MinimumSchema = nil }},
		{"no recovery", "projection", "hold", func(o *Observation) { o.RecoveryDrillID = nil }},
		{"schema", "schema", "hold", func(o *Observation) {}},
		{"retained", "preserve", "preserve", func(o *Observation) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := Contract{Kind: test.kind, Observation: base}
			test.change(&c.Observation)
			c.assess(now)
			if c.State != test.state {
				t.Fatalf("got %s (%s)", c.State, c.Reason)
			}
		})
	}
}
