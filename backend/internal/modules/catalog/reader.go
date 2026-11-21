package catalog

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const SnapshotTTL = 15 * time.Minute

type Reader struct {
	source       Acquisition
	publications CatalogPublisher

	mu          sync.Mutex
	lastVersion string
	lastPublish time.Time
}

// publicationRefresh bounds how often an unchanged public snapshot is republished.
const publicationRefresh = time.Minute

func NewReader(source Acquisition, publications CatalogPublisher) *Reader {
	return &Reader{source: source, publications: publications}
}

func (r *Reader) Snapshot(ctx context.Context, p Perspective, asOf time.Time) (Snapshot, error) {
	now := time.Now().UTC()
	current := asOf.IsZero()
	if current {
		asOf = now
	}
	end := asOf.Truncate(time.Hour)
	window := Window{end.Add(-24 * time.Hour), end}
	input, err := r.source.Load(ctx, p, window, now)
	if err != nil {
		return Snapshot{}, err
	}
	out := Project(p, input, window, asOf)
	if current && p.Public() && r.publications != nil && r.shouldPublish(out.Version, now) {
		if err := r.publications.Publish(ctx, out, input.RegistryVersion, now); err != nil {
			return Snapshot{}, err
		}
		r.markPublished(out.Version, now)
	}
	return out, nil
}

// shouldPublish suppresses the write per request when the public projection has
// not changed. A changed version is always published immediately.
func (r *Reader) shouldPublish(version string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return version != r.lastVersion || now.Sub(r.lastPublish) >= publicationRefresh
}

func (r *Reader) markPublished(version string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastVersion, r.lastPublish = version, now
}

func Project(p Perspective, input Input, window Window, asOf time.Time) Snapshot {
	registry := make(map[string][]RegistryModel)
	for _, entry := range input.Registry {
		key := StableID(entry.Provider, entry.Model)
		registry[key] = append(registry[key], entry)
	}
	out := Snapshot{AsOf: asOf, Details: make([]Detail, 0, len(input.Models))}
	visible := make(map[string]bool, len(input.Models))
	aliases := make(map[string][]string)
	for _, m := range input.Models {
		id := StableID(m.Provider, m.Model)
		visible[id] = true
		canonical := canonicalModel(m.Model, registry[id])
		key := StableID(m.Provider, canonical)
		aliases[key] = append(aliases[key], m.Model)
	}
	for _, m := range input.Models {
		id := StableID(m.Provider, m.Model)
		detail := projectModel(p, m, registry[id], window, input.Metrics[id])
		detail.Compatibility = input.Compatibility[id]
		canonical := canonicalModel(m.Model, registry[id])
		key := StableID(m.Provider, canonical)
		if visible[key] {
			for _, alias := range aliases[key] {
				if alias != m.Model {
					detail.Aliases = append(detail.Aliases, alias)
				}
			}
			slices.Sort(detail.Aliases)
		}
		out.Details = append(out.Details, detail)
	}
	sort.Slice(out.Details, func(i, j int) bool { return out.Details[i].Model.ID < out.Details[j].Model.ID })
	out.Version = "cat_" + Digest(struct {
		Perspective Perspective
		Registry    string
		Details     []Detail
	}{p, input.RegistryVersion, out.Details})
	for i := range out.Details {
		out.Details[i].Model.Version = out.Version
	}
	return out
}

func canonicalModel(name string, entries []RegistryModel) string {
	canonical := ""
	for _, entry := range entries {
		if entry.Canonical == "" || (canonical != "" && entry.Canonical != canonical) {
			return name
		}
		canonical = entry.Canonical
	}
	if canonical == "" {
		return name
	}
	return canonical
}

func projectModel(p Perspective, m Candidate, entries []RegistryModel, w Window, metrics Metrics) Detail {
	summary := Summary{
		ID: StableID(m.Provider, m.Model), Model: m.Model, DisplayName: m.Model, Provider: m.Provider,
		Maturity: "unknown", CatalogState: "listed", AccessState: "requires_scope", RouteState: "requires_scope",
		Modalities: []string{}, Protocols: []string{}, Actions: []string{}, Regions: []string{},
		EvidenceState: "unknown",
	}
	d := Detail{Model: summary, Aliases: []string{}, Facts: []ModelFacts{}, Capabilities: []Capability{},
		Endpoints: []Endpoint{}, AdditionalRates: []RateBand{}, TimeBands: []TimeBand{}, Benchmarks: []Benchmark{}, Redactions: []string{}}
	var contexts []ModelFacts
	for _, entry := range entries {
		if entry.Facts.Source != "approved_registry" {
			continue
		}
		if len(entries) == 1 && entry.DisplayName != "" {
			d.Model.DisplayName = entry.DisplayName
		}
		if d.Model.Maturity == "unknown" || entry.Facts.Maturity == "deprecated" {
			d.Model.Maturity = entry.Facts.Maturity
		}
		contexts = append(contexts, entry.Facts)
		d.Facts = append(d.Facts, entry.Facts)
		d.Capabilities = append(d.Capabilities, entry.Capabilities...)
	}
	if p.Public() {
		d.Redactions = []string{"endpoints", "scoped_evidence", "scoped_metrics", "scoped_prices"}
		d.Metrics = unavailableMetrics(w, "withheld")
	} else {
		d.Model.Scope = &EffectiveScope{p.Workspace, p.Project, p.AccessVersion}
		d.Model.AccessState, d.Model.RouteState = "denied", "unknown"
		var scopedFacts []ModelFacts
		for _, offer := range m.Offers {
			if offer.Access && offer.Eligible && offer.GroupID > 0 && !slices.Contains(d.EligibleGroups, offer.GroupID) {
				d.EligibleGroups = append(d.EligibleGroups, offer.GroupID)
			}
			if offer.Access {
				d.Model.AccessState = "allowed"
			}
			if offer.Eligible {
				d.Model.RouteState = "eligible"
			}
			scopedFacts = append(scopedFacts, offer.Facts...)
			d.Capabilities = append(d.Capabilities, offer.Capabilities...)
			if p.EndpointMetadata {
				d.Endpoints = append(d.Endpoints, offer.Endpoints...)
				for _, endpoint := range offer.Endpoints {
					d.Model.Regions = append(d.Model.Regions, endpoint.Regions...)
				}
			}
		}
		if !p.EndpointMetadata {
			d.Redactions = append(d.Redactions, "endpoints")
		}
		d.Facts = append(d.Facts, scopedFacts...)
		if len(scopedFacts) > 0 {
			contexts = scopedFacts
		}
		d.Metrics = metrics
		if d.Metrics.State == "" {
			d.Metrics = unavailableMetrics(w, "unavailable")
		}
	}
	for _, f := range d.Facts {
		d.Model.Modalities = append(d.Model.Modalities, f.Modalities...)
	}
	d.Model.Context = commonContext(contexts)
	for _, c := range d.Capabilities {
		if c.State != "stale" {
			d.Model.Protocols = append(d.Model.Protocols, c.Protocol)
			d.Model.Actions = append(d.Model.Actions, c.Action)
		}
	}
	fillBuiltinFacts(&d, contexts)
	d.Facts = unique(d.Facts)
	d.Capabilities = unique(d.Capabilities)
	d.Endpoints = mergeEndpoints(d.Endpoints)
	d.Model.Protocols = stringsSorted(d.Model.Protocols)
	d.Model.Actions = stringsSorted(d.Model.Actions)
	d.Model.Modalities = stringsSorted(d.Model.Modalities)
	d.Model.Regions = stringsSorted(d.Model.Regions)
	for _, fact := range d.Facts {
		if d.Model.ObservedAt == nil || fact.ObservedAt != nil && fact.ObservedAt.After(*d.Model.ObservedAt) {
			d.Model.ObservedAt = fact.ObservedAt
		}
		if fact.State == "stale" || (d.Model.EvidenceState == "unknown" && fact.State == "fresh") {
			d.Model.EvidenceState = fact.State
		}
	}
	for _, capability := range d.Capabilities {
		if capability.State == "stale" || (d.Model.EvidenceState == "unknown" && capability.State == "fresh") {
			d.Model.EvidenceState = capability.State
		}
		if d.Model.ObservedAt == nil || capability.ObservedAt != nil && capability.ObservedAt.After(*d.Model.ObservedAt) {
			d.Model.ObservedAt = capability.ObservedAt
		}
	}

	price := commonPrice(m.Offers, p.Public())
	if price.State == "unpriced" {
		d.ReferencePrice = piReferencePrice(d.Model.Provider, d.Model.Model)
	}
	d.Model.PriceState, d.Model.Input, d.Model.Output = price.State, price.Input, price.Output
	d.PriceSource, d.ContextBasis = price.Source, price.ContextBasis
	d.AdditionalRates, d.TimeBands = price.Additional, price.TimeBands
	if price.State == "priced" || price.State == "free" {
		version := "ref_" + Digest(price)
		d.PriceVersion = &version
	}
	return d
}

func commonContext(entries []ModelFacts) *int64 {
	var result *int64
	kind := ""
	for _, fact := range entries {
		if fact.Context == nil || fact.State == "stale" || fact.ContextKind == "" {
			continue
		}
		if result != nil && (kind != fact.ContextKind || *result != *fact.Context) {
			return nil
		}
		result, kind = fact.Context, fact.ContextKind
	}
	return result
}

func commonPrice(offers []Offer, public bool) Price {
	unpriced := Price{State: "unpriced", Source: "unavailable", Additional: []RateBand{}, TimeBands: []TimeBand{}}

	var candidate *Price
	var signature string
	for i := range offers {
		offer := &offers[i]
		if (!public && !offer.Access) || (public && offer.Price.Source != "public_plaza_reference" && offer.Price.State != "error") {
			continue
		}
		switch offer.Price.State {
		case "error":
			unpriced.State = "error"
			return unpriced
		case "priced", "free":
		default:
			return unpriced
		}
		current := Digest(offer.Price)
		if candidate != nil && signature != current {
			return unpriced
		}
		candidate, signature = &offer.Price, current
	}
	if candidate != nil {
		return *candidate
	}
	return unpriced
}

func unavailableMetrics(w Window, state string) Metrics {
	return Metrics{Start: w.Start, End: w.End, Source: "unavailable", State: state}
}

func stringsSorted(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}

func mergeEndpoints(values []Endpoint) []Endpoint {
	byID := make(map[string]Endpoint, len(values))
	ids := []string{}
	for _, v := range values {
		if existing, ok := byID[v.ID]; ok {
			v.Capabilities = append(existing.Capabilities, v.Capabilities...)
		} else {
			ids = append(ids, v.ID)
		}
		byID[v.ID] = v
	}
	slices.Sort(ids)
	out := make([]Endpoint, 0, len(ids))
	for _, id := range ids {
		endpoint := byID[id]
		endpoint.Capabilities = unique(endpoint.Capabilities)
		out = append(out, endpoint)
	}
	return out
}

func unique[T any](values []T) []T {
	byDigest := make(map[string]T, len(values))
	keys := make([]string, 0, len(values))
	for _, v := range values {
		key := Digest(v)
		if _, ok := byDigest[key]; !ok {
			keys = append(keys, key)
		}
		byDigest[key] = v
	}
	slices.Sort(keys)
	out := make([]T, 0, len(keys))
	for _, key := range keys {
		out = append(out, byDigest[key])
	}
	return out
}

func MatchesText(value, text string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(text))
}
