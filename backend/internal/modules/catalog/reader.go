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
	registry := make(map[ModelIdentity][]RegistryModel)
	approvedAliases := make(map[ModelIdentity][]string)
	for _, entry := range input.Registry {
		id := NewModelIdentity(entry.Provider, entry.Model)
		registry[id] = append(registry[id], entry)
		for _, alias := range entry.Aliases {
			aliasID := NewModelIdentity(entry.Provider, alias)
			approvedAliases[aliasID] = append(approvedAliases[aliasID], entry.Model)
		}
	}
	out := Snapshot{AsOf: asOf, Details: make([]Detail, 0, len(input.Models))}
	if !p.Public() {
		out.ScopeVersion = &p.AccessVersion
	}
	canonicals := make(map[ModelIdentity]ModelIdentity, len(input.Models))
	for _, m := range input.Models {
		id := NewModelIdentity(m.Provider, m.Model)
		names := slices.Clone(m.Canonicals)
		for _, entry := range registry[id] {
			if entry.Canonical != "" {
				names = append(names, entry.Canonical)
			}
		}
		if len(names) == 0 && len(registry[id]) == 0 {
			names = slices.Clone(approvedAliases[id])
		}
		canonicals[id] = NewModelIdentity(id.Provider, CanonicalName(id.Model, names))
	}
	aliases := make(map[ModelIdentity][]string)
	for id, target := range canonicals {
		// Keep the relation only when the target is independently visible
		// and is itself canonical.
		if canonicals[target] != target {
			target = id
		}
		aliases[target] = append(aliases[target], id.Model)
	}
	for _, m := range input.Models {
		identity := NewModelIdentity(m.Provider, m.Model)
		m.Provider = identity.Provider
		id := identity.ID()
		detail := projectModel(p, m, registry[identity], window, input.Metrics[id])
		detail.Compatibility = unique(input.Compatibility[id])
		target := canonicals[identity]
		if canonicals[target] != target {
			target = identity
		}
		detail.Model.CanonicalID = target.ID()
		for _, alias := range aliases[target] {
			if alias != m.Model {
				detail.Aliases = append(detail.Aliases, alias)
			}
		}
		detail.Aliases = stringsSorted(detail.Aliases)
		out.Details = append(out.Details, detail)
	}
	sort.Slice(out.Details, func(i, j int) bool { return out.Details[i].Model.ID < out.Details[j].Model.ID })
	projection := slices.Clone(out.Details)
	offerRevisions := make([]string, 0, len(out.Details))
	for i := range projection {
		if projection[i].Metrics.Requests == nil {
			projection[i].Metrics.Start, projection[i].Metrics.End = time.Time{}, time.Time{}
		}
		offers := make([]string, 0, len(projection[i].Offers))
		for _, offer := range projection[i].Offers {
			offers = append(offers, Digest(struct {
				Group, Channel int64
				Availability   [4]bool
				Basis          string
				Bindings       []ModelBinding
				Price          Price
				PriceVersion   *string
				Semantics      string
			}{offer.GroupID, offer.ChannelID, [4]bool{offer.Access, offer.Eligible, offer.Published, offer.Discovered},
				offer.PublicationBasis, unique(offer.Bindings), offer.Price, offer.Price.Version, offer.Price.Semantics}))
		}
		offerRevisions = append(offerRevisions, Digest(stringsSorted(offers)))
	}
	out.Version = "cat_" + Digest(struct {
		Perspective Perspective
		Registry    string
		Revision    string
		Details     []Detail
		Offers      []string
	}{p, input.RegistryVersion, input.Revision, projection, offerRevisions})
	for i := range out.Details {
		out.Details[i].Model.Version = out.Version
	}
	return out
}

func projectModel(p Perspective, m Candidate, entries []RegistryModel, w Window, metrics Metrics) Detail {
	summary := Summary{
		ID: StableID(m.Provider, m.Model), Model: m.Model, DisplayName: m.Model, Provider: m.Provider,
		Maturity: "unknown", CatalogState: "listed", AccessState: "requires_scope", RouteState: "requires_scope",
		Modalities: []string{}, Protocols: []string{}, Actions: []string{}, Regions: []string{}, EvidenceState: "unknown",
	}
	d := Detail{Model: summary, Aliases: []string{}, Facts: []ModelFacts{}, Capabilities: []Capability{},
		Endpoints: []Endpoint{}, AdditionalRates: []RateBand{}, TimeBands: []TimeBand{}, Benchmarks: []Benchmark{}, Redactions: []string{}, Offers: m.Offers}
	var descriptive []ModelFacts
	displayName, displayPriority, conflictingName := "", -1, false
	methodPriority := -1
	for _, entry := range slices.Concat(entries, supplementalFacts(NewModelIdentity(m.Provider, m.Model))) {
		rank := sourcePriority(entry.Facts.Origin.SourceKind)
		if entry.Facts.Source == "approved_registry" && rank == 0 {
			rank = sourcePriority(BuiltinReviewed)
		}
		if entry.DisplayName != "" {
			if rank > displayPriority {
				displayName, displayPriority, conflictingName = entry.DisplayName, rank, false
			} else if rank == displayPriority && displayName != entry.DisplayName {
				conflictingName = true
			}
		}
		if entry.Facts.Maturity != "" && (d.Model.Maturity == "unknown" || entry.Facts.Maturity == "deprecated" ||
			entry.Facts.Maturity == "experimental" && d.Model.Maturity == "stable") {
			d.Model.Maturity = entry.Facts.Maturity
		}
		descriptive = append(descriptive, entry.Facts)
		blocked := slices.Contains(entry.Facts.Suppressed, "supported_methods")
		if entry.Methods != nil || len(entry.Capabilities) > 0 || blocked {
			if rank > methodPriority {
				d.Capabilities, methodPriority = d.Capabilities[:0], rank
			}
			if rank == methodPriority && !blocked {
				d.Capabilities = append(d.Capabilities, entry.Capabilities...)
			}
		}
		d.Model.Actions = append(d.Model.Actions, entry.Actions...)
	}
	if displayName != "" && !conflictingName {
		d.Model.DisplayName = displayName
	}
	d.Facts = append(d.Facts, descriptive...)
	var bindings []BindingFacts
	var historical []Provenance
	if p.Public() {
		d.Redactions = []string{"endpoints", "scoped_evidence", "scoped_metrics", "scoped_prices"}
		d.Metrics = unavailableMetrics(w, "withheld")
	} else {
		d.Model.Scope = &EffectiveScope{p.Workspace, p.Project, p.AccessVersion}
		d.ScopeVersion = &p.AccessVersion
		d.Model.AccessState, d.Model.RouteState = "denied", "unknown"
		for _, offer := range m.Offers {
			if offer.Access && offer.Eligible && !offer.CatalogOnly && offer.GroupID > 0 && !slices.Contains(d.EligibleGroups, offer.GroupID) {
				d.EligibleGroups = append(d.EligibleGroups, offer.GroupID)
			}
			if offer.Access {
				d.Model.AccessState = "allowed"
			}
			if offer.Eligible && !offer.CatalogOnly {
				d.Model.RouteState = "eligible"
			}
			if offer.Access || p.Kind == PerspectiveAdmin {
				bindings = append(bindings, offer.BindingFacts...)
				if len(offer.BindingFacts) == 0 && len(offer.Facts) != 0 {
					bindings = append(bindings, BindingFacts{Facts: offer.Facts})
				}
			} else if p.EndpointMetadata {
				_, observations := resolveFacts(offer.Facts, nil)
				for _, observation := range observations {
					if observation.SourceKind != "" {
						historical = append(historical, observation)
					}
				}
			}
			if p.EndpointMetadata {
				d.Facts = append(d.Facts, offer.Facts...)
				d.Endpoints = append(d.Endpoints, offer.Endpoints...)
				for _, endpoint := range offer.Endpoints {
					d.Model.Regions = append(d.Model.Regions, endpoint.Regions...)
				}
			}
			d.Capabilities = append(d.Capabilities, offer.Capabilities...)
			d.Model.Actions = append(d.Model.Actions, offer.Actions...)
		}
		if !p.EndpointMetadata {
			d.Redactions = append(d.Redactions, "endpoints", "scoped_evidence")
		}
		d.Metrics = metrics
		if d.Metrics.State == "" {
			d.Metrics = unavailableMetrics(w, "unavailable")
		}
	}
	if !p.EndpointMetadata {
		bindings = nil
	}
	common, provenance := resolveFacts(descriptive, bindings)
	reference := piReferencePrice(m.Provider, m.Model)
	if reference != nil {
		provenance = append(provenance, Provenance{Field: "reference_price", State: "known", SourceKind: PiAIReference,
			RecordKey: m.Provider + "/" + m.Model, ArtifactVersion: piTokenPrices().version, Provider: m.Provider,
			MatchRule: "exact_public_name", Trust: "reference", Scope: "public"})
	}
	d.Provenance = redactProvenance(p, append(provenance, historical...))
	d.CommonFacts = &common
	d.Model.Context = common.Context
	d.Model.Modalities = append(d.Model.Modalities, common.Modalities...)
	d.Model.Source = sourceAxis(p, len(entries) > 0, m.Offers, d.Provenance)
	availability, legacy := projectAvailability(p, m.Offers, d.Model.Source)
	d.Model.Availability, d.Model.AvailabilityState = &availability, legacy
	for _, c := range d.Capabilities {
		if c.State != "stale" {
			d.Model.Protocols = append(d.Model.Protocols, c.Protocol)
			d.Model.Actions = append(d.Model.Actions, c.Action)
		}
	}
	d.Facts = unique(d.Facts)
	d.Capabilities = unique(d.Capabilities)
	d.Endpoints = mergeEndpoints(d.Endpoints)
	d.Model.Protocols = stringsSorted(d.Model.Protocols)
	d.Model.Actions = stringsSorted(d.Model.Actions)
	d.Model.Modalities = stringsSorted(d.Model.Modalities)
	d.Model.Regions = stringsSorted(d.Model.Regions)
	slices.Sort(d.EligibleGroups)
	for _, fact := range d.Facts {
		applyEvidence(&d.Model, fact.Evidence)
	}
	for _, capability := range d.Capabilities {
		applyEvidence(&d.Model, capability.Evidence)
	}

	billing, price := offerBilling(m.Offers, p.Public())
	billing.ReferencePrice = reference
	d.Model.Billing = &billing
	if price.State == "unpriced" {
		d.ReferencePrice = billing.ReferencePrice
	}
	d.Model.PriceState, d.Model.Input, d.Model.Output = price.State, price.Input, price.Output
	d.PriceSource, d.ContextBasis = price.Source, price.ContextBasis
	if d.PriceSource == "" {
		d.PriceSource = "unavailable"
	}
	if p.Public() && (price.State == "priced" || price.State == "free") {
		d.PriceSource = "public_plaza_reference"
	}
	d.AdditionalRates, d.TimeBands = price.Additional, price.TimeBands
	if price.State == "priced" || price.State == "free" {
		d.PriceVersion = price.Version
		if d.PriceVersion == nil {
			version := "ref_" + Digest(price)
			d.PriceVersion = &version
		}
	}
	return d
}

func applyEvidence(summary *Summary, evidence Evidence) {
	if summary.ObservedAt == nil || evidence.ObservedAt != nil && evidence.ObservedAt.After(*summary.ObservedAt) {
		summary.ObservedAt = evidence.ObservedAt
	}
	if evidence.State == "stale" || summary.EvidenceState == "unknown" && evidence.State == "fresh" {
		summary.EvidenceState = evidence.State
	}
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
