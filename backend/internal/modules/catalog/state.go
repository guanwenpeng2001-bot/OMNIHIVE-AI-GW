package catalog

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

type SourceAxis struct {
	Kinds      []string `json:"kinds"`
	Candidate  *bool    `json:"candidate"`
	Discovered *bool    `json:"discovered"`
}

type Availability struct {
	State       string `json:"state"`
	Publication string `json:"publication"`
	Access      string `json:"access"`
	Reason      string `json:"reason,omitempty"`
}

type Billing struct {
	State          string     `json:"state"`
	Reason         string     `json:"reason,omitempty"`
	SalePrice      *SalePrice `json:"sale_price,omitempty"`
	ReferencePrice *Price     `json:"reference_price,omitempty"`
}

type SalePrice struct {
	Source       string      `json:"source"`
	Scope        string      `json:"scope"`
	Version      *string     `json:"price_version"`
	ValidFrom    *time.Time  `json:"valid_from"`
	ValidUntil   *time.Time  `json:"valid_until"`
	Input        *money.Rate `json:"input_rate"`
	Output       *money.Rate `json:"output_rate"`
	Additional   []RateBand  `json:"additional_rates"`
	ContextBasis string      `json:"context_basis,omitempty"`
	TimeBands    []TimeBand  `json:"time_bands"`
}

type PriceInput struct {
	Perspective Perspective
	Identity    ModelIdentity
	Binding     ModelBinding
	GroupID     int64
	Multiplier  float64
	At          time.Time
}

// PricePort describes a sale tariff for a previously admitted offer. The
// reference price loader is independent of this port.
type PricePort interface {
	Describe(context.Context, PriceInput) (Price, error)
}

type omitSalePricesKey struct{}

func WithoutSalePrices(ctx context.Context) context.Context {
	return context.WithValue(ctx, omitSalePricesKey{}, true)
}

func OmitSalePrices(ctx context.Context) bool {
	omit, _ := ctx.Value(omitSalePricesKey{}).(bool)
	return omit
}

type PriceSnapshotPort interface {
	Snapshot(context.Context, time.Time) context.Context
}

type PriceRevisionPort interface {
	PriceRevision(context.Context) (string, error)
}

// EligibleBindings narrows the snapshot's already admitted bindings to a
// concrete action and protocol. Public callers get only publishable offers.
func (d Detail) EligibleBindings(p Perspective, action, protocol string) []ModelBinding {
	var bindings []ModelBinding
	for _, offer := range d.Offers {
		if !offer.Eligible || offer.CatalogOnly || p.Public() && !offer.Published || !p.Public() && !offer.Access {
			continue
		}
		for _, binding := range offer.Bindings {
			if binding.Eligible && (action == "" || binding.Action == action) && (protocol == "" || binding.Protocol == protocol) {
				bindings = append(bindings, binding)
			}
		}
	}
	return bindings
}

func sourceAxis(p Perspective, candidate bool, offers []Offer, provenance []Provenance) *SourceAxis {
	kinds := []string{}
	for _, e := range provenance {
		if e.SourceKind != "" {
			kinds = append(kinds, e.SourceKind)
		}
	}
	discovered := false
	for _, o := range offers {
		discovered = discovered || o.Discovered
		candidate = candidate || o.Candidate
	}
	if discovered && !p.Public() {
		kinds = append(kinds, "account_discovery")
	}
	source := &SourceAxis{Kinds: stringsSorted(kinds), Candidate: &candidate}
	if !p.Public() {
		source.Discovered = &discovered
	}
	return source
}

func projectAvailability(p Perspective, offers []Offer, source *SourceAxis) (Availability, string) {
	a := Availability{State: "unavailable", Publication: "unpublished", Access: "denied"}
	legacy := "catalog_only"
	unknown := false
	for _, o := range offers {
		if o.Published {
			a.Publication = "published"
		}
		if o.Access {
			a.Access = "allowed"
			unknown = unknown || o.EligibilityUnknown
		}
		if o.Eligible && !o.CatalogOnly && (o.Access || p.Public()) {
			legacy = "live"
			a.State = "live"
		}
	}
	if p.Public() {
		a.State, a.Access = "requires_scope", "requires_scope"
	} else if a.State == "unavailable" && unknown {
		a.State = "unknown"
		a.Reason = "eligibility_incomplete"
		return a, legacy
	}
	if a.State == "live" {
		return a, legacy
	}
	switch {
	case a.Publication == "published" && legacy != "live":
		a.Reason = "published_without_eligible_binding"
	case source.Discovered != nil && *source.Discovered:
		a.Reason = "discovered_without_eligible_binding"
	case source.Candidate != nil && *source.Candidate && a.Publication != "published":
		a.Reason = "candidate_only"
	case p.Public():
		a.Reason = "scope_required"
	case a.Access == "denied":
		a.Reason = "access_denied"
	default:
		a.Reason = "no_eligible_binding"
	}
	return a, legacy
}

func normalizedPrice(price Price) Price {
	if price.State == "" {
		price.State = "unpriced"
	}
	if price.State == "priced" || price.State == "free" {
		switch {
		case len(price.TimeBands) != 0 || price.ContextBasis != "" ||
			slices.ContainsFunc(price.Additional, func(b RateBand) bool { return b.MinTokens != nil || b.Label != "" }):
			price.State, price.Reason, price.LegacyEquivalent = "conditional", "scenario_required", true
		case (price.Input == nil) != (price.Output == nil):
			price.State, price.Reason = "partial", "required_dimension_missing"
		case price.State == "priced" && price.Input == nil && !slices.ContainsFunc(price.Additional, standaloneChargeRate):
			if len(price.Additional) == 0 {
				price.State, price.Reason = "unpriced", "no_sale_tariff"
			} else {
				price.State, price.Reason = "partial", "required_dimension_missing"
			}
		}
	}
	if price.Reason == "" {
		switch price.State {
		case "error":
			price.Reason = "price_read_failed"
		case "unpriced":
			price.Reason = "no_sale_tariff"
		case "unknown":
			price.Reason = "price_not_read"
		case "conditional":
			price.Reason = "scenario_required"
		case "partial":
			price.Reason = "required_dimension_missing"
		case "conflict":
			price.Reason = "offers_disagree"
		}
	}
	if price.Additional == nil {
		price.Additional = []RateBand{}
	}
	if price.TimeBands == nil {
		price.TimeBands = []TimeBand{}
	}
	return price
}

func standaloneChargeRate(band RateBand) bool {
	switch band.Rate.Unit() {
	case "request", "image", "second", "call", "audio_second", "video_second":
		return true
	default:
		return false
	}
}

func offerBilling(offers []Offer, public bool) (Billing, Price) {
	b := Billing{State: "unpriced", Reason: "no_applicable_offer"}
	legacy := Price{State: "unpriced", Source: "unavailable", Additional: []RateBand{}, TimeBands: []TimeBand{}}
	var sale *Price
	signature, missing := "", ""
	unread, failed, conflict := false, false, false
	offers = slices.Clone(offers)
	slices.SortFunc(offers, func(a, b Offer) int {
		return strings.Compare(Digest(a.Price), Digest(b.Price))
	})
	for _, o := range offers {
		if o.CatalogOnly || !o.Eligible || !public && !o.Access {
			continue
		}
		price := normalizedPrice(o.Price)
		switch price.State {
		case "error":
			failed = true
		case "unknown":
			unread = true
		case "unpriced":
			if missing == "" || price.Reason < missing {
				missing = price.Reason
			}
		case "conflict":
			conflict = true
		default:
			key := monetarySignature(price)
			switch {
			case sale == nil:
				sale, signature = &price, key
			case key != signature:
				conflict = true
			default:
				if sale.Version != nil && (price.Version == nil || *sale.Version != *price.Version) {
					sale.Version = nil
				}
				if sale.Source != price.Source {
					sale.Source = "multiple_offers"
				}
				if sale.ValidFrom == nil || price.ValidFrom != nil && price.ValidFrom.After(*sale.ValidFrom) {
					sale.ValidFrom = price.ValidFrom
				}
				if sale.ValidUntil == nil || price.ValidUntil != nil && price.ValidUntil.Before(*sale.ValidUntil) {
					sale.ValidUntil = price.ValidUntil
				}
				sale.LegacyEquivalent = sale.LegacyEquivalent && price.LegacyEquivalent
			}
		}
	}
	switch {
	case failed:
		b.State, b.Reason, legacy.State = "error", "price_read_failed", "error"
	case conflict:
		b.State, b.Reason = "conflict", "offers_disagree"
	case sale != nil:
		b.State, b.Reason = sale.State, sale.Reason
		if missing != "" || unread {
			b.State, b.Reason = "partial", "offer_price_missing"
		}
		scope := "authorized"
		if public {
			scope = "public"
		}
		b.SalePrice = &SalePrice{Source: sale.Source, Scope: scope, Version: sale.Version,
			ValidFrom: sale.ValidFrom, ValidUntil: sale.ValidUntil, Input: sale.Input, Output: sale.Output,
			Additional: sale.Additional, TimeBands: sale.TimeBands, ContextBasis: sale.ContextBasis}
		if b.State == "priced" || b.State == "free" || b.State == "conditional" && sale.LegacyEquivalent {
			legacy = *sale
			if b.State == "conditional" {
				legacy.State = "priced"
			}
		}
	case unread:
		b.State, b.Reason = "unknown", "price_not_read"
	case missing != "":
		b.Reason = missing
	}
	return b, legacy
}

func monetarySignature(p Price) string {
	type band struct {
		Rate, Kind, Label string
		Min, Max          *int64
	}
	bands := make([]band, 0, len(p.Additional))
	for _, b := range p.Additional {
		bands = append(bands, band{rateSignature(&b.Rate), b.Kind, b.Label, b.MinTokens, b.MaxTokens})
	}
	return Digest(struct {
		State, Input, Output, ContextBasis, Semantics string
		Bands                                         []band
		TimeBands                                     []TimeBand
	}{p.State, rateSignature(p.Input), rateSignature(p.Output), p.ContextBasis, p.Semantics, unique(bands), p.TimeBands})
}

func rateSignature(rate *money.Rate) string {
	if rate == nil {
		return ""
	}
	one, err := money.NewQuantity(rate.Unit(), "1")
	if err != nil {
		return Digest(rate)
	}
	value, err := rate.Cost(one)
	if err != nil {
		return Digest(rate)
	}
	return string(rate.Currency()) + "/" + string(rate.Unit()) + "/" + value.Ratio().RatString()
}
