package models

import (
	"context"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/compare"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

// SalePriceBindings resolves metadata from authorized catalog offers.
type SalePriceBindings struct {
	Catalog catalog.CatalogReader
	Selected quotes.SelectedModelAuthorizer
}

func (b SalePriceBindings) PriceBindings(ctx context.Context, p catalog.Perspective, model string, key int64, protocol, action string, at time.Time) ([]pricing.Binding, error) {
	if key > 0 {
		if b.Selected == nil {
			return nil, fault.NotFound
		}
		authorized, detail, err := b.Selected.AuthorizeSelectedModel(ctx, p, key, 0, model, action, protocol, at)
		if err != nil {
			return nil, err
		}
		p.Actor = authorized.FundingUserID
		return b.PriceBindingsForDetail(p, detail, protocol, action), nil
	}
	snapshot, err := b.Catalog.Snapshot(ctx, p, at)
	if err != nil {
		return nil, err
	}
	detail, ok := snapshot.Find(model)
	if !ok {
		return nil, fault.NotFound
	}
	return b.PriceBindingsForDetail(p, detail, protocol, action), nil
}

func (SalePriceBindings) PriceBindingsForDetail(p catalog.Perspective, detail catalog.Detail, protocol, action string) []pricing.Binding {
	return legacy.SaleBindings(p, detail.EligibleBindings(p, action, protocol))
}

type ComparePrices struct{ compare.Service }

func (p ComparePrices) EstimateCatalog(ctx context.Context, perspective catalog.Perspective, bindings []catalog.ModelBinding, quantities []money.Quantity, at time.Time) (pricing.Result, string, string, error) {
	if len(quantities) != 4 {
		return pricing.Result{}, "", "", fault.Invalid("/scenario")
	}
	scenario := pricing.Scenario{Usage: pricing.Usage{Dimensions: []pricing.Dimension{
		{Kind: "input", Quantity: quantities[0]},
		{Kind: "cache_read", Quantity: quantities[1]},
		{Kind: "output", Quantity: quantities[2]},
		{Kind: "request", Quantity: quantities[3], Optional: true},
	}}}
	return p.EstimateCatalogScenario(ctx, perspective, "", bindings, scenario, at, at)
}

func (p ComparePrices) EstimateCatalogScenario(ctx context.Context, perspective catalog.Perspective, model string, bindings []catalog.ModelBinding, scenario pricing.Scenario, at, asOf time.Time) (pricing.Result, string, string, error) {
	ctx = domain.WithWorkspaceScope(ctx, perspective.Workspace)
	result := pricing.Unavailable(pricing.Missing)
	var err error
	switch {
	case at.Equal(asOf):
		result, err = p.EstimateLive(ctx, legacy.SaleBindings(perspective, bindings), scenario, at)
	case at.Before(asOf) && perspective.KeyID == 0:
		result, err = p.EstimateHistorical(ctx, perspective, model, scenario, at)
	}
	state := result.State
	switch result.Reason {
	case pricing.Conflict:
		state = "conflict"
	case pricing.Partial:
		state = "partial"
	case pricing.Conditional:
		state = "conditional"
	}
	if state == "estimated" {
		state = "priced"
	}
	return result, state, string(result.Reason), err
}
