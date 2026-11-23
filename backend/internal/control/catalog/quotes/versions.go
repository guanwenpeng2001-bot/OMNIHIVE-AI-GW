package quotes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type PublishedVersions interface {
	At(context.Context, string, time.Time) ([]pricing.Version, error)
}

// BindingReader returns the authorized current offers for an action. Public
// calls expose publishable offers only. A selected Key must be evaluated for
// scope, expiry, action, model and billing-group access before returning a binding.
type BindingReader interface {
	PriceBindings(context.Context, catalog.Perspective, string, int64, string, string, time.Time) ([]pricing.Binding, error)
}

func (s Service) bindingReader() BindingReader {
	if s.Bindings != nil {
		return s.Bindings
	}
	reader, _ := s.Catalog.(BindingReader)
	return reader
}

func PublicationScope(p catalog.Perspective, modelID string) string {
	if p.Public() {
		return fmt.Sprintf("workspace/%d/public/%s", p.Workspace, modelID)
	}
	return fmt.Sprintf("workspace/%d/project/%d/actor/%d/scope/%d/%s", p.Workspace, p.Project, p.Actor, p.AccessVersion, modelID)
}

func (s Service) Estimate(ctx context.Context, p catalog.Perspective, d catalog.Detail, usage pricing.Usage, at, asOf time.Time) (pricing.Result, error) {
	return s.EstimateFor(ctx, p, d, 0, "", "", pricing.Scenario{Usage: usage}, at, asOf)
}

func (s Service) EstimateFor(ctx context.Context, p catalog.Perspective, d catalog.Detail, key int64, protocol, action string, scenario pricing.Scenario, at, asOf time.Time) (pricing.Result, error) {
	if (!p.Public() && d.Model.AccessState != "allowed") || at.After(asOf) {
		return pricing.Unavailable(pricing.Missing), nil
	}
	if at.Before(asOf) {
		return s.historicalEstimate(ctx, p, d.Model.ID, scenario.Usage, at)
	}
	bindings := s.bindingReader()
	if bindings == nil || d.Model.RouteState != "eligible" {
		if key != 0 {
			return pricing.Result{}, fault.NotFound
		}
		return pricing.Unavailable(pricing.Missing), nil
	}
	authorized, err := bindings.PriceBindings(ctx, p, d.Model.ID, key, protocol, action, at)
	if err != nil {
		return pricing.Result{}, err
	}
	if key != 0 && len(authorized) == 0 {
		return pricing.Result{}, fault.NotFound
	}
	if s.Book == nil {
		return pricing.Unavailable(pricing.Missing), nil
	}
	if snapshotter, ok := s.Book.(interface {
		Snapshot(context.Context, time.Time) context.Context
	}); ok {
		ctx = snapshotter.Snapshot(ctx, at)
	}
	var chosen pricing.Tariff
	var chosenVersion, chosenTerms string
	for _, binding := range authorized {
		binding.At = at
		tariff, err := s.Book.Resolve(ctx, binding)
		if err != nil {
			return pricing.Result{}, err
		}
		description := tariff.Describe()
		if description.Outcome == pricing.Conflict {
			return pricing.Unavailable(pricing.Conflict), nil
		}
		if len(authorized) > 1 && description.VersionID == "" {
			return pricing.Unavailable(pricing.Conflict), nil
		}
		terms := description.TermsID
		if terms == "" {
			terms = description.VersionID
		}
		if chosen != nil && terms != chosenTerms {
			return pricing.Unavailable(pricing.Conflict), nil
		}
		if chosen == nil || description.VersionID < chosenVersion {
			chosen, chosenVersion, chosenTerms = tariff, description.VersionID, terms
		}
	}
	if chosen == nil {
		return pricing.Unavailable(pricing.Missing), nil
	}
	return chosen.Evaluate(scenario)
}

func (s Service) historicalEstimate(ctx context.Context, p catalog.Perspective, modelID string, usage pricing.Usage, at time.Time) (pricing.Result, error) {
	if s.Published == nil {
		return pricing.Unavailable(pricing.Missing), nil
	}
	versions, err := s.Published.At(ctx, PublicationScope(p, modelID), at)
	if err != nil {
		return pricing.Result{}, err
	}
	version, err := pricing.ResolveExact(versions, at)
	if errors.Is(err, pricing.ErrAmbiguous) {
		return pricing.Unavailable(pricing.Conflict), nil
	}
	if err != nil {
		return pricing.Result{}, err
	}
	if version == nil {
		return pricing.Unavailable(pricing.Missing), nil
	}
	return pricing.NewExactTariff(*version, at).Evaluate(pricing.Scenario{Usage: usage})
}
