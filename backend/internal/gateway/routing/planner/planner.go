package planner

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

type Features struct {
	Mode        trace.Mode
	ModelID     string
	Seed        uint64
	PinnedID    string
	AffinityIDs []string
}

type EffectivePolicy struct {
	LegacyLRU bool
	Version   string
	Origins   []trace.Origin
	// A nil region list inherits. An explicit empty list denies all.
	Regions                  []string
	RequireZeroRetention     bool
	MaxPrice                 *money.Amount
	RequireKnownQuota        bool
	RequireAuthorizedFunding bool
	AllowEndpointFallback    bool
	AllowModelFallback       bool
	TopK                     int
	IncludeOverflow          bool
	AllowRecheck             bool
}

const (
	eligibility = iota
	model
	region
	retention
	funding
	price
	quota
	ranking
)

var stages = [...]string{"eligibility", "model", "region", "retention", "funding", "price", "quota", "ranking"}

func Plan(features Features, policy EffectivePolicy, in snapshot.CandidateSnapshot) trace.DecisionTrace {
	mode := features.Mode
	if mode != trace.Execution {
		mode = trace.Simulation
	}
	out := trace.DecisionTrace{
		SchemaVersion: 1, Mode: mode, SnapshotID: in.ID, AsOf: in.AsOf,
		PolicyVersion: policy.Version, CatalogVersion: in.CatalogVersion, PriceVersion: in.PriceVersion,
		Seed: features.Seed, Origins: append([]trace.Origin{}, policy.Origins...),
		Stages: []trace.Stage{}, Candidates: []trace.Candidate{}, Uncertainties: []string{},
		Redactions: []string{"connection_identity"},
	}

	ordered := slices.Clone(in.Candidates)
	slices.SortFunc(ordered, func(a, b snapshot.Candidate) int { return cmp.Compare(a.ID, b.ID) })
	eligible := make([]snapshot.Candidate, 0, len(ordered))
	excluded := make([]trace.Candidate, 0)
	var rejected [len(stages)]int
	unknownQuota := false
	requiresRecheck := false
	for _, c := range ordered {
		stage, code := rejection(c, features, policy)
		if code != "" {
			rejected[stage]++
			excluded = append(excluded, trace.Candidate{
				ID: c.ID, State: "excluded", Reasons: []trace.Reason{reason(policy, stage, code)}, Factors: []trace.Factor{},
			})
			continue
		}
		unknownQuota = unknownQuota || c.QuotaRemaining == nil
		eligible = append(eligible, c)
	}
	slices.SortFunc(eligible, func(a, b snapshot.Candidate) int {
		if order := cmp.Compare(fallbackLevel(a, features), fallbackLevel(b, features)); order != 0 {
			return order
		}
		return cmp.Compare(a.Tier, b.Tier)
	})

	count := 0
	appendRank := func(c snapshot.Candidate) {
		requiresRecheck = requiresRecheck || c.RecheckOnly
		count++
		rank := count
		out.Candidates = append(out.Candidates, trace.Candidate{
			ID: c.ID, State: "eligible", Rank: &rank, Reasons: []trace.Reason{},
			Factors: append([]trace.Factor{}, c.Factors...),
		})
		if out.Selected == nil {
			out.Selected = &trace.Selection{ModelID: c.ModelID, CandidateID: c.ID, FundingSource: c.FundingSource}
		}
	}
	for i, c := range eligible {
		if c.ID == features.PinnedID && !c.RecheckOnly && fallbackLevel(c, features) == 0 {
			appendRank(c)
			eligible = slices.Delete(eligible, i, i+1)
			break
		}
	}
	for len(eligible) > 0 {
		end := 1
		for end < len(eligible) && eligible[end].Tier == eligible[0].Tier &&
			fallbackLevel(eligible[end], features) == fallbackLevel(eligible[0], features) {
			end++
		}
		pool := eligible[:end]
		eligible = eligible[end:]
		top := len(pool)
		if !pool[0].RecheckOnly && policy.TopK > 0 {
			top = min(top, policy.TopK)
		}
		if policy.LegacyLRU {
			slices.SortFunc(pool, func(a, b snapshot.Candidate) int { return compareLegacy(a, b, in.AsOf, features.Seed) })
		} else {
			slices.SortFunc(pool, compare)
		}
		primary := pool[:top]
		if !pool[0].RecheckOnly && !policy.LegacyLRU {
			primary = affinityOrder(primary, features)
		}
		for _, c := range primary {
			appendRank(c)
		}
		for _, c := range pool[top:] {
			if policy.IncludeOverflow {
				appendRank(c)
				continue
			}
			rejected[ranking]++
			excluded = append(excluded, trace.Candidate{ID: c.ID, State: "excluded",
				Reasons: []trace.Reason{reason(policy, ranking, "OUTSIDE_TOP_K")}, Factors: append([]trace.Factor{}, c.Factors...)})
		}
	}
	out.Candidates = append(out.Candidates, excluded...)
	remaining := len(ordered)
	for i, name := range stages {
		out.Stages = append(out.Stages, trace.Stage{Name: name, InputCount: remaining, RemainingCount: remaining - rejected[i]})
		remaining -= rejected[i]
	}
	if out.Selected != nil {
		out.Uncertainties = append(out.Uncertainties, "CAPACITY_CAN_CHANGE")
	}
	if unknownQuota {
		out.Uncertainties = append(out.Uncertainties, "QUOTA_UNKNOWN")
	}
	if requiresRecheck {
		out.Uncertainties = append(out.Uncertainties, "COMPATIBILITY_RECHECK_REQUIRED")
	}
	return out
}

func reason(policy EffectivePolicy, stage int, code string) trace.Reason {
	r := trace.Reason{Code: code, Stage: stages[stage]}
	for _, origin := range policy.Origins {
		if origin.Constraint == r.Stage || (r.Stage == "region" && origin.Constraint == "regions") {
			r.Origin = origin.Version
		}
	}
	return r
}

func fallbackLevel(c snapshot.Candidate, f Features) int {
	if c.ModelID != f.ModelID {
		return 2
	}
	if c.EndpointFallback {
		return 1
	}
	return 0
}

func rejection(c snapshot.Candidate, f Features, p EffectivePolicy) (int, string) {
	switch {
	case c.Rejection != "":
		return eligibility, c.Rejection
	case c.RecheckOnly && !p.AllowRecheck:
		return eligibility, "COMPATIBILITY_RECHECK_REQUIRED"
	case c.ModelID != f.ModelID && !p.AllowModelFallback:
		return model, "MODEL_DENIED"
	case c.EndpointFallback && !p.AllowEndpointFallback:
		return model, "ENDPOINT_FALLBACK_DENIED"
	case p.Regions != nil && !c.RegionVerified:
		return region, "REGION_UNVERIFIED"
	case p.Regions != nil && !slices.Contains(p.Regions, c.Region):
		return region, "REGION_DENIED"
	case p.RequireZeroRetention && (!c.RetentionVerified || !c.ZeroRetention):
		return retention, "RETENTION_INELIGIBLE"
	case (p.RequireAuthorizedFunding || fallbackLevel(c, f) != 0) && !c.FundingAuthorized:
		return funding, "FUNDING_NOT_AUTHORIZED"
	case p.MaxPrice != nil && c.Price == nil:
		return price, "PRICE_UNKNOWN"
	case p.MaxPrice != nil && c.Price.Compare(*p.MaxPrice) > 0:
		return price, "PRICE_CEILING"
	case p.RequireKnownQuota && c.QuotaRemaining == nil:
		return quota, "QUOTA_UNKNOWN"
	case c.QuotaRemaining != nil && *c.QuotaRemaining <= 0:
		return quota, "QUOTA_EXHAUSTED"
	default:
		return 0, ""
	}
}

func compare(a, b snapshot.Candidate) int {
	if order := cmp.Compare(b.Score, a.Score); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Priority, b.Priority); order != 0 {
		return order
	}
	if order := cmp.Compare(a.LoadRate, b.LoadRate); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Waiting, b.Waiting); order != 0 {
		return order
	}
	if a.RecheckOnly && b.RecheckOnly {
		switch {
		case a.LastUsedAt == nil && b.LastUsedAt != nil:
			return -1
		case a.LastUsedAt != nil && b.LastUsedAt == nil:
			return 1
		case a.LastUsedAt != nil && b.LastUsedAt != nil:
			if order := a.LastUsedAt.Compare(*b.LastUsedAt); order != 0 {
				return order
			}
		}
		return cmp.Compare(a.InputOrder, b.InputOrder)
	}
	// Legacy IDs are decimal account IDs.
	aID, aErr := strconv.ParseInt(a.ID, 10, 64)
	bID, bErr := strconv.ParseInt(b.ID, 10, 64)
	if aErr == nil && bErr == nil {
		if order := cmp.Compare(aID, bID); order != 0 {
			return order
		}
	}
	if (aErr == nil) != (bErr == nil) {
		if aErr == nil {
			return -1
		}
		return 1
	}
	return cmp.Compare(a.ID, b.ID)
}

func compareLegacy(a, b snapshot.Candidate, now time.Time, seed uint64) int {
	if order := cmp.Compare(a.Priority, b.Priority); order != 0 {
		return order
	}
	reset := func(c snapshot.Candidate) *time.Time {
		if c.ResetAt != nil && now.Before(*c.ResetAt) {
			return c.ResetAt
		}
		return nil
	}
	aReset, bReset := reset(a), reset(b)
	switch {
	case aReset != nil && bReset == nil:
		return -1
	case aReset == nil && bReset != nil:
		return 1
	case aReset != nil && bReset != nil:
		if order := aReset.Compare(*bReset); order != 0 {
			return order
		}
	}
	if order := cmp.Compare(a.LoadRate, b.LoadRate); order != 0 {
		return order
	}
	if order := compareTime(a.LastUsedAt, b.LastUsedAt); order != 0 {
		return order
	}
	if a.PreferOAuth != b.PreferOAuth {
		if a.PreferOAuth {
			return -1
		}
		return 1
	}
	if order := cmp.Compare(tieBreak(a.ID, seed), tieBreak(b.ID, seed)); order != 0 {
		return order
	}
	return cmp.Compare(a.ID, b.ID)
}

func compareTime(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return a.Compare(*b)
	}
}

func tieBreak(id string, seed uint64) uint64 {
	// Mix the ID and seed independently for each tie.
	for i := range len(id) {
		seed = (seed ^ uint64(id[i])) * 1099511628211
	}
	seed = (seed ^ (seed >> 30)) * 0xbf58476d1ce4e5b9
	seed = (seed ^ (seed >> 27)) * 0x94d049bb133111eb
	return seed ^ (seed >> 31)
}

func affinityOrder(pool []snapshot.Candidate, f Features) []snapshot.Candidate {
	for _, id := range f.AffinityIDs {
		for i, c := range pool {
			if c.ID == id {
				order := append([]snapshot.Candidate{c}, pool[:i]...)
				return append(order, pool[i+1:]...)
			}
		}
	}
	return WeightedOrder(pool, f.Seed, func(c snapshot.Candidate) float64 { return c.Score })
}

// WeightedOrder samples without replacement from a ranked pool.
func WeightedOrder[T any](pool []T, seed uint64, score func(T) float64) []T {
	pool = slices.Clone(pool)
	if len(pool) < 2 {
		return pool
	}
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	minScore := score(pool[0])
	for _, c := range pool[1:] {
		if value := score(c); value < minScore {
			minScore = value
		}
	}
	weights := make([]float64, len(pool))
	for i, c := range pool {
		weight := score(c) - minScore + 1
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
			weight = 1
		}
		weights[i] = weight
	}

	order := make([]T, 0, len(pool))
	for len(pool) > 0 {
		total := 0.0
		for _, w := range weights {
			total += w
		}
		seed ^= seed >> 12
		seed ^= seed << 25
		seed ^= seed >> 27
		draw := float64((seed*2685821657736338717)>>11) / (1 << 53) * total
		selected := 0
		acc := 0.0
		for i, w := range weights {
			acc += w
			if draw <= acc {
				selected = i
				break
			}
		}
		order = append(order, pool[selected])
		pool = append(pool[:selected], pool[selected+1:]...)
		weights = append(weights[:selected], weights[selected+1:]...)
	}
	return order
}
