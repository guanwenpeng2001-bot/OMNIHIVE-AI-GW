package admin

import (
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type snapshotMeta struct {
	Version      string    `json:"catalog_version"`
	AsOf         time.Time `json:"as_of"`
	ScopeVersion int64     `json:"scope_version,string"`
}

func newSnapshotMeta(s catalog.Snapshot, p catalog.Perspective) snapshotMeta {
	return snapshotMeta{Version: s.Version, AsOf: s.AsOf.UTC(), ScopeVersion: p.AccessVersion}
}

type listResponse struct {
	snapshotMeta
	Items []modelSummary `json:"items"`
	Next  *string        `json:"next_cursor"`
}

type modelSummary struct {
	ID                     string       `json:"id"`
	CanonicalID            string       `json:"canonical_id"`
	Model                  string       `json:"model"`
	DisplayName            string       `json:"display_name"`
	Provider               string       `json:"provider"`
	Source                 modelSource  `json:"source"`
	Availability           availability `json:"availability"`
	Billing                billing      `json:"billing"`
	AliasCount             int          `json:"aliases_count"`
	AccountCount           int          `json:"accounts_count"`
	DiscoveredAccountCount int          `json:"discovered_accounts_count"`
	GroupCount             int          `json:"groups_count"`
	ChannelCount           int          `json:"channels_count"`
}

type modelSource struct {
	Kinds      []string `json:"kinds"`
	Candidate  *bool    `json:"candidate"`
	Discovered *bool    `json:"discovered"`
}

type availability struct {
	State       string `json:"state"`
	Publication string `json:"publication"`
	Access      string `json:"access"`
	Reason      string `json:"reason,omitempty"`
}

func sourceKind(kind string) bool {
	switch kind {
	case catalog.BuiltinReviewed, catalog.PlatformPreset, catalog.BuiltinSupplement, catalog.PiAIReference,
		catalog.AccountDiscovery, catalog.AccountDeclaration, catalog.ExternalReference, "heuristic":
		return true
	default:
		return false
	}
}

func projectAxes(m catalog.Summary, counts bindingSummary) (modelSource, availability, catalog.Billing) {
	s := modelSource{Kinds: []string{}}
	if m.Source != nil {
		s.Candidate, s.Discovered = m.Source.Candidate, m.Source.Discovered
		for _, kind := range m.Source.Kinds {
			if sourceKind(kind) {
				s.Kinds = append(s.Kinds, kind)
			}
		}
	}
	if counts.discovered > 0 && !slices.Contains(s.Kinds, catalog.AccountDiscovery) {
		s.Kinds = append(s.Kinds, catalog.AccountDiscovery)
	}
	slices.Sort(s.Kinds)
	s.Kinds = slices.Compact(s.Kinds)
	a := availability{State: "unknown", Access: "unknown", Publication: "unknown"}
	if v := m.Availability; v != nil {
		a = availability{State: v.State, Publication: v.Publication, Access: v.Access, Reason: v.Reason}
	}
	b := catalog.Billing{State: "unknown"}
	if m.Billing != nil {
		b = *m.Billing
	}
	return s, a, b
}

func projectSummary(d catalog.Detail, counts bindingSummary) modelSummary {
	m := d.Model
	s, a, b := projectAxes(m, counts)
	canonical := m.CanonicalID
	if canonical == "" {
		canonical = m.ID
	}
	return modelSummary{
		ID: m.ID, CanonicalID: canonical, Model: m.Model, DisplayName: m.DisplayName, Provider: m.Provider,
		Source: s, Availability: a, Billing: projectBilling(b), AliasCount: len(d.Aliases),
		AccountCount: counts.accounts, DiscoveredAccountCount: counts.discovered,
		GroupCount: counts.groups, ChannelCount: counts.channels,
	}
}

type bindingSummary struct {
	accounts, discovered, groups, channels int
	hasUnboundCurrent, hasStale            bool
}

func summarizeBindings(offers []catalog.Offer) bindingSummary {
	type status uint8
	const (
		discovered status = 1 << iota
		current
		eligible
	)
	accounts := map[int64]status{}
	groups, channels := map[int64]struct{}{}, map[int64]struct{}{}
	s := bindingSummary{}
	for _, o := range offers {
		if o.GroupID > 0 {
			groups[o.GroupID] = struct{}{}
		}
		if o.ChannelID > 0 {
			channels[o.ChannelID] = struct{}{}
		}
		for _, b := range o.Bindings {
			if b.AccountID <= 0 {
				continue
			}
			state := accounts[b.AccountID]
			if discoveryState(b.DiscoveryState) {
				state |= discovered
			}
			if b.DiscoveryState == "current" {
				state |= current
			}
			if offerEligible(o) && b.Eligible {
				state |= eligible
			}
			accounts[b.AccountID] = state
			s.hasStale = s.hasStale || b.DiscoveryState == "stale"
		}
	}
	s.accounts, s.groups, s.channels = len(accounts), len(groups), len(channels)
	for _, state := range accounts {
		if state&discovered != 0 {
			s.discovered++
		}
		s.hasUnboundCurrent = s.hasUnboundCurrent || (state&(current|eligible) == current)
	}
	return s
}

func discoveryState(state string) bool {
	return state == "current" || state == "stale" || state == "invalidated"
}

func offerEligible(o catalog.Offer) bool {
	return o.Access && o.Eligible && !o.CatalogOnly
}

func stringsList(values []string) []string {
	return append([]string{}, values...)
}
