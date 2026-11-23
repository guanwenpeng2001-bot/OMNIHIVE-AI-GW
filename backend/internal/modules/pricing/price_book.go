package pricing

import (
	"context"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

type Outcome string

const (
	Available   Outcome = "available"
	Missing     Outcome = "missing_price"
	Partial     Outcome = "partial_price"
	Conflict    Outcome = "conflicting_prices"
	Conditional Outcome = "scenario_required"
)

// Binding is produced by the caller's authorization and routing layer. Model
// is the billing identity after mappings, and Provider is the outbound platform.
type Binding struct {
	Provider, Model    string
	GroupID, ChannelID int64
	Workspace, Project int64
	Funding            string
	Mode, Action       string
	Protocol           string
	RateMultiplier     float64
	At                 time.Time
}

type Scenario struct {
	Usage
	ServiceTier, ReasoningEffort string
	ThinkingEnabled              *bool
	ExplicitCache, Batch         bool
}

type RateFact struct {
	Kind, Origin string
	Rate         money.Rate
}

type Description struct {
	Outcome                       Outcome
	Source, BaseSource, VersionID string
	TermsID                       string
	Currency                      money.Currency
	Evaluator                     string
	Mode                          string
	RateMultiplier                float64
	EffectiveFrom                 time.Time
	EffectiveUntil                *time.Time
	Rates                         []RateFact
}

// ReferencePrice is display evidence. None of the PriceBook entry points take
// reference rates as input.
type ReferencePrice struct {
	Provider, Model, Source string
	Input, Output           *money.Rate
}

// Tariff owns the frozen evaluator. Description may require a scenario to
// expose effective rates for conditional tariffs.
type Tariff interface {
	Describe() Description
	Evaluate(Scenario) (Result, error)
}

type PriceBook interface {
	Resolve(context.Context, Binding) (Tariff, error)
	Describe(context.Context, Binding) (Description, error)
}

func Unavailable(reason Outcome) Result {
	r := Unpriced()
	r.Reason = reason
	return r
}

// SaleVersion validates an existing exact tariff at monetary read boundaries.
// Synthetic catalog references were never authoritative tariff versions.
func SaleVersion(v Version) bool {
	return v.ID != "" && !strings.HasPrefix(strings.ToLower(v.ID), "ref_") &&
		strings.TrimSpace(v.Source) != "" && !IsReferenceSource(v.Source)
}

func IsReferenceSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return strings.Contains(source, "reference") || source == "pi" || strings.HasPrefix(source, "pi-") || strings.HasPrefix(source, "pi_")
}

func ResolveExact(versions []Version, at time.Time) (*Version, error) {
	var sale []Version
	for _, version := range versions {
		if SaleVersion(version) {
			sale = append(sale, version)
		}
	}
	return Resolve(Candidates{Published: sale}, at)
}

type ExactTariff struct {
	version Version
	at      time.Time
}

func NewExactTariff(version Version, at time.Time) ExactTariff {
	if !SaleVersion(version) {
		version = Version{}
	}
	return ExactTariff{version: version, at: at}
}

func (t ExactTariff) Describe() Description {
	d := Description{Outcome: Missing, Evaluator: "exact"}
	if t.version.ID == "" || !t.version.active(t.at) {
		return d
	}
	d.Outcome, d.Source, d.VersionID, d.TermsID, d.Currency = Available, t.version.Source, t.version.ID, t.version.ID, t.version.Currency
	d.EffectiveFrom, d.EffectiveUntil = t.version.EffectiveFrom, t.version.EffectiveUntil
	for _, rule := range t.version.Rules {
		d.Rates = append(d.Rates, RateFact{Kind: rule.Kind, Origin: t.version.Source, Rate: rule.Rate})
	}
	return d
}

func (t ExactTariff) Freeze(usage Usage) Frozen {
	return Frozen{Version: t.version, Usage: usage, At: t.at}
}

func (t ExactTariff) Evaluate(scenario Scenario) (Result, error) {
	return t.Freeze(scenario.Usage).Calculate()
}
