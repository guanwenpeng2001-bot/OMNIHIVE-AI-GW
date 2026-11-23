package admin

import (
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type billing struct {
	State     string          `json:"state"`
	Reason    string          `json:"reason,omitempty"`
	Sale      *salePrice      `json:"sale_price,omitempty"`
	Reference *referencePrice `json:"reference_price,omitempty"`
}

type priceRates struct {
	Input        *money.Rate `json:"input_rate"`
	Output       *money.Rate `json:"output_rate"`
	Additional   []rateBand  `json:"additional_rates"`
	TimeBands    []timeBand  `json:"time_bands"`
	ContextBasis string      `json:"context_basis,omitempty"`
}

type salePrice struct {
	priceRates
	Source     string     `json:"source"`
	Scope      string     `json:"scope"`
	Version    *string    `json:"price_version"`
	ValidFrom  *time.Time `json:"valid_from"`
	ValidUntil *time.Time `json:"valid_until"`
}

type referencePrice struct {
	priceRates
	State  string `json:"state"`
	Source string `json:"source"`
}

type rateBand struct {
	Rate      money.Rate `json:"rate"`
	Kind      string     `json:"kind"`
	MinTokens *int64     `json:"min_tokens,string"`
	MaxTokens *int64     `json:"max_tokens,string"`
	Label     string     `json:"label,omitempty"`
}

type timeBand struct {
	Timezone     string       `json:"timezone"`
	WeekdaysOnly bool         `json:"weekdays_only"`
	Start        string       `json:"start_time"`
	End          string       `json:"end_time"`
	Multiplier   money.Amount `json:"multiplier"`
}

func projectRates(input, output *money.Rate, additional []catalog.RateBand, bands []catalog.TimeBand, basis string) priceRates {
	out := priceRates{Input: input, Output: output, Additional: make([]rateBand, 0, len(additional)),
		TimeBands: make([]timeBand, 0, len(bands)), ContextBasis: basis}
	for _, b := range additional {
		out.Additional = append(out.Additional, rateBand{Rate: b.Rate, Kind: b.Kind, MinTokens: b.MinTokens, MaxTokens: b.MaxTokens, Label: b.Label})
	}
	for _, b := range bands {
		out.TimeBands = append(out.TimeBands, timeBand{Timezone: b.Timezone, WeekdaysOnly: b.WeekdaysOnly, Start: b.Start, End: b.End, Multiplier: b.Multiplier})
	}
	return out
}

func billingState(state string) bool {
	return slices.Contains([]string{"priced", "free", "conditional", "partial", "conflict", "unpriced", "error", "unknown"}, state)
}

func projectBilling(in catalog.Billing) billing {
	out := billing{State: in.State, Reason: in.Reason}
	if !billingState(out.State) {
		out.State = "unknown"
	}
	if p := in.SalePrice; p != nil {
		version := p.Version
		if version != nil && strings.HasPrefix(*version, "ref_") {
			version = nil
		}
		out.Sale = &salePrice{
			Source: p.Source, Scope: "authorized", Version: version, ValidFrom: p.ValidFrom, ValidUntil: p.ValidUntil,
			priceRates: projectRates(p.Input, p.Output, p.Additional, p.TimeBands, p.ContextBasis),
		}
	}
	if p := in.ReferencePrice; p != nil {
		out.Reference = &referencePrice{State: p.State, Source: p.Source,
			priceRates: projectRates(p.Input, p.Output, p.Additional, p.TimeBands, p.ContextBasis)}
	}
	return out
}

func bindingBilling(price catalog.Price, eligible bool) billing {
	if !eligible {
		return billing{State: "unpriced", Reason: "no_applicable_offer"}
	}
	// The snapshot carries the shared aggregate and raw per-binding descriptors.
	// Apply the catalog's dimensional and scenario checks to each descriptor.
	state, reason := price.State, price.Reason
	if state == "" {
		state = "unpriced"
	}
	if state == "priced" || state == "free" {
		switch {
		case len(price.TimeBands) != 0 || price.ContextBasis != "" ||
			slices.ContainsFunc(price.Additional, func(b catalog.RateBand) bool { return b.MinTokens != nil || b.Label != "" }):
			state, reason = "conditional", "scenario_required"
		case (price.Input == nil) != (price.Output == nil):
			state, reason = "partial", "required_dimension_missing"
		case state == "priced" && price.Input == nil && len(price.Additional) == 0:
			state, reason = "unpriced", "no_sale_tariff"
		case state == "priced" && price.Input == nil && !slices.ContainsFunc(price.Additional, standaloneChargeRate):
			state, reason = "partial", "required_dimension_missing"
		}
	}
	if reason == "" {
		switch state {
		case "unpriced":
			reason = "no_sale_tariff"
		case "partial":
			reason = "required_dimension_missing"
		case "conditional":
			reason = "scenario_required"
		case "conflict":
			reason = "offers_disagree"
		case "error":
			reason = "price_read_failed"
		case "unknown":
			reason = "price_not_read"
		}
	}
	out := catalog.Billing{State: state, Reason: reason}
	switch state {
	case "priced", "free", "partial", "conditional":
		out.SalePrice = &catalog.SalePrice{Source: price.Source, Version: price.Version,
			ValidFrom: price.ValidFrom, ValidUntil: price.ValidUntil, Input: price.Input, Output: price.Output,
			Additional: price.Additional, TimeBands: price.TimeBands, ContextBasis: price.ContextBasis}
	}
	return projectBilling(out)
}

func standaloneChargeRate(band catalog.RateBand) bool {
	switch band.Rate.Unit() {
	case "request", "image", "second", "call", "audio_second", "video_second":
		return true
	default:
		return false
	}
}
