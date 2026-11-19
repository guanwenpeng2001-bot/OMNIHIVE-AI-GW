package policy

import (
	"cmp"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/planner"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

type Target struct{ Workspace, Project, Key int64 }

func (t Target) includes(s Scope) bool {
	switch s.Type {
	case "platform", "request":
		return true
	case "workspace":
		return ID(s.ID) == t.Workspace
	case "project":
		return ID(s.ID) == t.Project
	case "api_key":
		return ID(s.ID) == t.Key && s.ProjectID() == t.Project
	}
	return false
}

type Effective struct {
	Planner                                                                    planner.EffectivePolicy
	Models, Pools, DeniedProviders, Required, FallbackModels, OrderedProviders []string
	Template                                                                   string
	NoTraining, Expansion, PlatformFallback                                    bool
}

func intersect(a, b []string) []string {
	if a == nil {
		return append([]string{}, b...)
	}
	result := []string{}
	for _, x := range a {
		if slices.Contains(b, x) {
			result = append(result, x)
		}
	}
	return result
}
func restrict(a []string, b *List) []string {
	if b == nil || b.Mode == "inherit" {
		return a
	}
	return intersect(a, b.Values)
}
func permit(previous, next *bool) *bool {
	if previous != nil && !*previous {
		return previous
	}
	if next != nil {
		return next
	}
	return previous
}
func enabled(p *bool) bool { return p != nil && *p }

var levels = []string{"platform", "workspace", "project", "api_key", "request"}

// Compose accepts one layer per scope in precedence order. Nil slices inherit.
// Non-nil empty effective sets deny every member, including on fallback.
func Compose(t Target, layers []Layer) (Effective, error) {
	var e Effective
	e.Planner.RequireAuthorizedFunding = true
	var endpoint, expansion, platform *bool
	last := -1
	for _, l := range layers {
		d := l.Definition
		w := t.Workspace
		if d.Scope.Type == "platform" {
			w = 0
		}
		level := slices.Index(levels, d.Scope.Type)
		if level <= last || !t.includes(d.Scope) || l.Version == "" {
			return e, fault.Invalid("/hierarchy")
		}
		if err := d.Validate(w); err != nil {
			return e, err
		}
		last = level
		origin := func(name string) {
			e.Planner.Origins = append(e.Planner.Origins, trace.Origin{Constraint: name, ScopeType: d.Scope.Type, ScopeID: d.Scope.ID, Version: l.Version})
		}
		e.Planner.Version = l.Version
		c := d.Constraints
		if c.Models != nil && c.Models.Mode != "inherit" {
			e.Models = restrict(e.Models, c.Models)
			origin("model")
		}
		if c.Regions != nil && c.Regions.Mode != "inherit" {
			e.Planner.Regions = restrict(e.Planner.Regions, c.Regions)
			origin("regions")
		}
		if c.Retention == "strict_zero" {
			e.Planner.RequireZeroRetention = true
			origin("retention")
		}
		if c.Training == "prohibited" {
			e.NoTraining = true
			origin("training")
		}
		if c.MaxInputRate != nil {
			v := c.MaxInputRate.Amount()
			if e.Planner.MaxPrice == nil || v.Compare(*e.Planner.MaxPrice) < 0 {
				e.Planner.MaxPrice = &v
				origin("price")
			}
		}
		if c.RequireKnownQuota {
			e.Planner.RequireKnownQuota = true
			origin("quota")
		}
		if len(c.Required) > 0 {
			e.Required = append(e.Required, c.Required...)
			origin("required_capabilities")
		}
		if len(d.Candidates.Pools) > 0 {
			e.Pools = intersect(e.Pools, d.Candidates.Pools)
			origin("pool_ids")
		}
		if len(d.Candidates.DeniedProviders) > 0 {
			e.DeniedProviders = append(e.DeniedProviders, d.Candidates.DeniedProviders...)
			origin("denied_provider_ids")
		}
		if d.Candidates.Expansion != nil {
			expansion = permit(expansion, d.Candidates.Expansion)
			origin("allow_expansion")
		}
		if d.Preferences.Template != "" {
			e.Template = d.Preferences.Template
			origin("template")
		}
		if d.Preferences.Providers != nil {
			e.OrderedProviders = slices.Clone(*d.Preferences.Providers)
			origin("ordered_provider_ids")
		}
		if d.Fallback.Endpoint != nil {
			endpoint = permit(endpoint, d.Fallback.Endpoint)
			origin("endpoint_fallback")
		}
		if d.Fallback.Models != nil {
			e.FallbackModels = intersect(e.FallbackModels, *d.Fallback.Models)
			origin("fallback_models")
		}
		if d.Fallback.Platform != nil {
			platform = permit(platform, d.Fallback.Platform)
			origin("byok_to_platform")
		}
	}
	e.Expansion, e.PlatformFallback = enabled(expansion), enabled(platform)
	e.Planner.AllowModelFallback = len(e.FallbackModels) > 0
	e.Planner.AllowEndpointFallback = enabled(endpoint)
	return e, nil
}

// Evidence is populated by the authorized candidate snapshot producer. These
// facts contain no resolver, upstream client, credentials or dispatch leases.
type ConnectionFence struct {
	AccountID      int64  `json:"account_id"`
	VerificationID string `json:"verification_id"`
}
type Evidence struct {
	Connection                                   *ConnectionFence
	Provider                                     string
	Pools, Protocols, Actions, Capabilities      []string
	VerifiedNoTraining, Expanded, BYOKToPlatform bool
	TTFTMillis, SuccessPPM                       *int64
}
type Graph struct {
	Snapshot  snapshot.CandidateSnapshot
	ExpiresAt time.Time
	Facts     map[string]Evidence
}

type Scenario struct {
	Project  string   `json:"project_id"`
	Key      string   `json:"api_key_id,omitempty"`
	Model    string   `json:"model_id"`
	Protocol string   `json:"protocol"`
	Action   string   `json:"action"`
	Required []string `json:"required_capabilities"`
	Input    string   `json:"input_token_bound"`
	Output   string   `json:"output_token_bound"`
}

func (e Effective) rejection(c snapshot.Candidate, f Evidence, s Scenario) string {
	switch {
	case c.Rejection != "":
		return c.Rejection
	case e.Models != nil && (!slices.Contains(e.Models, s.Model) || !slices.Contains(e.Models, c.ModelID)):
		return "MODEL_DENIED"
	case c.ModelID != s.Model && !slices.Contains(e.FallbackModels, c.ModelID):
		return "MODEL_DENIED"
	case !slices.Contains(f.Protocols, s.Protocol) || !slices.Contains(f.Actions, s.Action):
		return "ACTION_UNSUPPORTED"
	case e.NoTraining && !f.VerifiedNoTraining:
		return "TRAINING_UNVERIFIED"
	case f.Provider == "" && len(e.DeniedProviders) > 0 || slices.Contains(e.DeniedProviders, f.Provider):
		return "PROVIDER_DENIED"
	case e.Pools != nil && !slices.ContainsFunc(f.Pools, func(p string) bool { return slices.Contains(e.Pools, p) }):
		return "POOL_DENIED"
	case f.Expanded && !e.Expansion:
		return "EXPANSION_DENIED"
	case f.BYOKToPlatform && !e.PlatformFallback:
		return "FUNDING_NOT_AUTHORIZED"
	}
	for _, required := range [][]string{e.Required, s.Required} {
		for _, v := range required {
			if !slices.Contains(f.Capabilities, v) {
				return "CAPABILITY_UNVERIFIED"
			}
		}
	}
	return ""
}

func compareObservation(a, b *int64, higher bool) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	if higher {
		return cmp.Compare(*b, *a)
	}
	return cmp.Compare(*a, *b)
}

// Prepare projects policy fields absent from the BE-08 input into candidate
// rejection codes and ranking tiers. Both planner modes use this same projection.
func (e Effective) Prepare(g Graph, s Scenario) snapshot.CandidateSnapshot {
	in := g.Snapshot
	in.Candidates = slices.Clone(in.Candidates)
	for i := range in.Candidates {
		c := &in.Candidates[i]
		c.Rejection = e.rejection(*c, g.Facts[c.ID], s)
	}
	pos := func(id string) int {
		rank := slices.Index(e.OrderedProviders, g.Facts[id].Provider)
		if rank < 0 {
			return len(e.OrderedProviders)
		}
		return rank
	}
	compare := func(a, b snapshot.Candidate) int {
		if x := cmp.Compare(a.Tier, b.Tier); x != 0 {
			return x
		}
		if x := cmp.Compare(pos(a.ID), pos(b.ID)); x != 0 {
			return x
		}
		fa, fb := g.Facts[a.ID], g.Facts[b.ID]
		switch e.Template {
		case "stable":
			return compareObservation(fa.SuccessPPM, fb.SuccessPPM, true)
		case "low_latency":
			return compareObservation(fa.TTFTMillis, fb.TTFTMillis, false)
		case "lowest_cost":
			if a.Price != nil && b.Price != nil {
				return a.Price.Compare(*b.Price)
			}
			if a.Price != nil {
				return -1
			}
			if b.Price != nil {
				return 1
			}
		}
		return 0
	}
	slices.SortStableFunc(in.Candidates, compare)
	tier := 0
	for i := len(in.Candidates) - 1; i >= 0; i-- {
		if i > 0 && compare(in.Candidates[i-1], in.Candidates[i]) != 0 {
			in.Candidates[i].Tier = tier
			tier--
			continue
		}
		in.Candidates[i].Tier = tier
	}
	return in
}
