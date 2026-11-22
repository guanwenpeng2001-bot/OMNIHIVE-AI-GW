package quotes

import (
	"context"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

// ResolveSaleVersion is shared by key quotes and execution routing. It never
// substitutes a project-wide price for a key's group, or a PI reference for a
// missing sale. The version is stable across reads of an unchanged tariff.
func ResolveSaleVersion(ctx context.Context, store PublishedVersions, pc pricing.PriceContext, detail catalog.Detail) (pricing.Version, error) {
	if err := pc.Validate(); err != nil {
		return pricing.Version{}, err
	}
	if detail.Model.Provider != pc.Provider || detail.Model.Model != pc.Model {
		return pricing.Version{}, fmt.Errorf("pricing model identity mismatch")
	}
	projected, err := projectedSaleVersion(pc, detail)
	if err != nil {
		return pricing.Version{}, err
	}
	if store != nil {
		versions, err := store.At(ctx, pc.ScopeKey(), pc.PricingAt)
		if err != nil {
			return pricing.Version{}, err
		}
		version, err := pricing.Resolve(pricing.Candidates{Published: versions}, pc.PricingAt)
		if err != nil {
			return pricing.Version{}, err
		}
		if version != nil {
			if err := version.ValidateExecutable(); err != nil {
				return pricing.Version{}, err
			}
			// Operator group/channel commands remain the wallet sale authority.
			// A published exact binding cannot silently quote a different tariff
			// while the established gateway settles the operator price bundle.
			if projected.ID == "" || executableShape(*version) != executableShape(projected) {
				return pricing.Version{}, &fault.Fault{Status: 409, Reason: "PRICE_QUOTE_CHANGED", Message: "Published and operator sale tariffs differ. Reconcile pricing before execution."}
			}
			version.EvaluationContext = &pc
			return *version, nil
		}
	}
	return projected, nil
}

func executableShape(v pricing.Version) string {
	v.ID, v.Source, v.Kind = "", "", ""
	v.EffectiveFrom, v.EffectiveUntil, v.EvaluationContext = time.Time{}, nil, nil
	return catalog.Digest(v)
}

func projectedSaleVersion(pc pricing.PriceContext, detail catalog.Detail) (pricing.Version, error) {
	price, ok := detail.GroupPrices[pc.GroupID]
	if !ok {
		return pricing.Version{}, nil
	}
	if price.Kind == "reference" || pricing.IsReferenceSource(price.Source) {
		return pricing.Version{}, pricing.ErrReferencePrice
	}
	// Context is deliberately excluded from the content version: each quote
	// carries its own admission instant and policy, without inventing a new rate.
	price.Context = nil
	version := "sale_" + catalog.Digest(struct {
		Scope string
		Price catalog.Price
	}{pc.ScopeKey(), price})
	selected := detail
	selected.PriceVersion = &version
	selected.PriceSource = price.Source
	selected.PriceKind = "sale"
	selected.Model.PriceState, selected.Model.Input, selected.Model.Output = price.State, price.Input, price.Output
	selected.ContextBasis, selected.AdditionalRates, selected.TimeBands = price.ContextBasis, price.Additional, price.TimeBands
	result, err := SaleProjection(selected, pc.PricingAt)
	result.EvaluationContext = &pc
	return result, err
}

// Reference is retained as a source-compatible name for older callers. It has
// always represented operator sale projections, never Detail.ReferencePrice.
func Reference(d catalog.Detail, at time.Time) (pricing.Version, error) { return SaleProjection(d, at) }
