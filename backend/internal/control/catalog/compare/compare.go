package compare

import (
	"context"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type Input struct {
	IDs      []string         `json:"model_ids"`
	Protocol string           `json:"protocol"`
	Action   string           `json:"action"`
	Scenario *quotes.Scenario `json:"scenario"`
	At       *time.Time       `json:"price_at"`
}

type Row struct {
	ID           string              `json:"model_id"`
	Capabilities map[string]string   `json:"capabilities"`
	Quote        pricing.Result      `json:"quote"`
	Metrics      catalog.Metrics     `json:"metrics"`
	Benchmarks   []catalog.Benchmark `json:"benchmarks"`
}

type Result struct {
	Version string    `json:"catalog_version"`
	AsOf    time.Time `json:"as_of"`
	Rows    []Row     `json:"rows"`
}

type Service struct {
	Catalog   catalog.CatalogReader
	Published quotes.PublishedVersions
	Book      pricing.PriceBook
	Bindings  quotes.BindingReader
}

func (s Service) EstimateLive(ctx context.Context, bindings []pricing.Binding, scenario pricing.Scenario, at time.Time) (pricing.Result, error) {
	return (quotes.Service{Book: s.Book}).EstimateLive(ctx, bindings, scenario, at)
}

func (s Service) EstimateHistorical(ctx context.Context, p catalog.Perspective, model string, scenario pricing.Scenario, at time.Time) (pricing.Result, error) {
	return (quotes.Service{Published: s.Published}).EstimateHistorical(ctx, p, model, scenario.Usage, at)
}

func (s Service) Compare(ctx context.Context, p catalog.Perspective, in Input) (Result, error) {
	if len(in.IDs) < 1 || len(in.IDs) > 3 {
		return Result{}, fault.Invalid("/model_ids")
	}
	for i, id := range in.IDs {
		if !quotes.ValidModel(id) || slices.Contains(in.IDs[:i], id) {
			return Result{}, fault.Invalid("/model_ids")
		}
	}
	if !quotes.ValidFacet(in.Protocol) {
		return Result{}, fault.Invalid("/protocol")
	}
	if !quotes.ValidFacet(in.Action) {
		return Result{}, fault.Invalid("/action")
	}
	var usage pricing.Usage
	if in.Scenario != nil {
		var err error
		usage, err = in.Scenario.Usage()
		if err != nil {
			return Result{}, err
		}
	}
	snap, err := s.Catalog.Snapshot(ctx, p, time.Time{})
	if err != nil {
		return Result{}, err
	}
	result := Result{Version: snap.Version, AsOf: snap.AsOf, Rows: make([]Row, 0, len(in.IDs))}
	for _, id := range in.IDs {
		i := slices.IndexFunc(snap.Details, func(d catalog.Detail) bool { return d.Model.ID == id })
		if i < 0 {
			return Result{}, fault.NotFound
		}
		d := snap.Details[i]
		row := Row{ID: id, Capabilities: capabilities(d, in.Action, in.Protocol), Quote: pricing.Unpriced(),
			Metrics: d.Metrics, Benchmarks: []catalog.Benchmark{}}
		for _, b := range d.Benchmarks {
			if b.Source != "" && !b.VerifiedAt.IsZero() && !b.VerifiedAt.After(snap.AsOf) {
				row.Benchmarks = append(row.Benchmarks, b)
			}
		}
		matching := slices.ContainsFunc(d.Capabilities, func(c catalog.Capability) bool { return quotes.Matches(c, in.Action, in.Protocol) })
		if in.Scenario != nil && (matching || (in.Action == "" && in.Protocol == "")) {
			at := snap.AsOf
			if in.At != nil {
				at = *in.At
			}
			row.Quote, err = (quotes.Service{Catalog: s.Catalog, Published: s.Published, Book: s.Book, Bindings: s.Bindings}).
				EstimateFor(ctx, p, d, 0, in.Protocol, in.Action, in.Scenario.PriceScenario(usage), at, snap.AsOf)
			if err != nil {
				return Result{}, err
			}
			// BE-05 represents a free estimate as a known zero charge.
			if row.Quote.State == "free" {
				row.Quote.State = "estimated"
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func capabilities(d catalog.Detail, action, protocol string) map[string]string {
	var tools, stream []string
	for _, c := range d.Capabilities {
		if quotes.Matches(c, action, protocol) {
			tools, stream = append(tools, c.Tools), append(stream, c.Stream)
		}
	}
	if action == "" && protocol == "" {
		for _, f := range d.Facts {
			if f.State != "stale" && f.Tools != nil {
				tools = append(tools, catalog.Support(f.Tools))
			}
		}
	}
	return map[string]string{"tools": consensus(tools), "stream": consensus(stream)}
}

func consensus(values []string) string {
	result := "unknown"
	for _, value := range values {
		if value != "supported" && value != "unsupported" {
			continue
		}
		if result != "unknown" && result != value {
			return "unknown"
		}
		result = value
	}
	return result
}
