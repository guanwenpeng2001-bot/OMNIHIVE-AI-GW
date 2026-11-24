package legacy

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// PriceBookPort projects the same authorized sale bindings used by quotes.
type PriceBookPort struct{ *service.PlatformPriceBook }

func SaleBindings(p catalog.Perspective, bindings []catalog.ModelBinding) []pricing.Binding {
	result := make([]pricing.Binding, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, saleBinding(p, binding))
	}
	return result
}

func saleBinding(p catalog.Perspective, b catalog.ModelBinding) pricing.Binding {
	binding := pricing.Binding{
		Provider: b.BillingIdentity.Provider, Model: b.BillingIdentity.Model,
		AccountID: b.AccountID, WireModel: b.WireModel,
		GroupID: b.BillingGroupID, ChannelID: b.ChannelID, Funding: b.Funding,
		Workspace: p.Workspace, Project: p.Project, RateMultiplier: b.RateMultiplier,
		Action: b.Action, Protocol: b.Protocol,
	}
	switch b.BillingModelSource {
	case service.BillingModelSourceRequested, service.BillingModelSourceChannelMapped:
		binding.WireModel = b.WireModel
	}
	return binding
}

func (p PriceBookPort) Describe(ctx context.Context, in catalog.PriceInput) (catalog.Price, error) {
	binding := saleBinding(in.Perspective, in.Binding)
	binding.At, binding.RateMultiplier = in.At, in.Multiplier
	description, err := p.PlatformPriceBook.Describe(ctx, binding)
	if err != nil {
		return catalog.Price{}, err
	}
	return displayDescription(description)
}

func (p PriceBookPort) DescribeForGroup(ctx context.Context, in catalog.PriceInput, group *service.Group) (catalog.Price, error) {
	binding := saleBinding(in.Perspective, in.Binding)
	binding.At, binding.RateMultiplier = in.At, in.Multiplier
	description, err := p.PlatformPriceBook.DescribeForGroup(ctx, binding, group)
	if err != nil {
		return catalog.Price{}, err
	}
	return displayDescription(description)
}

func displayDescription(d pricing.Description) (catalog.Price, error) {
	out := catalog.Price{Source: d.Source, Semantics: d.TermsID, ValidUntil: d.EffectiveUntil}
	switch d.Outcome {
	case pricing.Available:
		out.State = "priced"
	case pricing.Partial:
		out.State = "partial"
	case pricing.Conditional:
		out.State = "conditional"
	case pricing.Conflict:
		out.State = "conflict"
	default:
		out.State = "unpriced"
	}
	if d.VersionID != "" {
		out.Version = &d.VersionID
	}
	if !d.EffectiveFrom.IsZero() {
		out.ValidFrom = &d.EffectiveFrom
	}
	multiplier := decimal.NewFromFloat(d.RateMultiplier)
	allZero := len(d.Rates) > 0
	for _, fact := range d.Rates {
		base := fact.Rate
		amount := decimal.RequireFromString(base.Amount().String()).Mul(multiplier)
		rate, err := money.NewRate(base.Currency(), amount.String(), base.Unit(), base.Per().String())
		if err != nil {
			return catalog.Price{}, err
		}
		allZero = allZero && amount.IsZero()
		switch fact.Kind {
		case "input":
			out.Input = &rate
		case "output":
			out.Output = &rate
		default:
			out.Additional = append(out.Additional, catalog.RateBand{Kind: fact.Kind, Rate: rate})
		}
	}
	if allZero && out.State == "priced" {
		out.State = "free"
	}
	return out, nil
}
