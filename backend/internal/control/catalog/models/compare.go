package models

import (
	"context"
	"math/big"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

// ComparePricePort evaluates a scenario from admitted offer bindings.
type ComparePricePort interface {
	EstimateCatalog(context.Context, catalog.Perspective, []catalog.ModelBinding, []money.Quantity, time.Time) (pricing.Result, string, string, error)
}

type comparisonInput struct {
	IDs      []string   `json:"model_ids"`
	Protocol string     `json:"protocol"`
	Action   string     `json:"action"`
	Scenario *scenario  `json:"scenario"`
	At       *time.Time `json:"price_at"`
	KeyID    *string    `json:"api_key_id"`
}

type scenario struct {
	Input    string `json:"input_tokens"`
	Cached   string `json:"cached_input_tokens"`
	Output   string `json:"output_tokens"`
	Requests string `json:"requests"`
}

type component struct {
	Kind      string `json:"kind"`
	Quantity  string `json:"quantity"`
	RawAmount string `json:"raw_amount"`
}

type quote struct {
	State              string       `json:"state"`
	BillingState       string       `json:"billing_state"`
	Reason             string       `json:"reason,omitempty"`
	PriceAt            time.Time    `json:"price_at"`
	PriceVersion       *string      `json:"price_version"`
	Charge             *money.Money `json:"estimated_charge"`
	Components         []component  `json:"components"`
	RoundingAdjustment *string      `json:"rounding_adjustment"`
}

type comparisonRow struct {
	ID           string              `json:"model_id"`
	Capabilities map[string]string   `json:"capabilities"`
	Metrics      catalog.Metrics     `json:"metrics"`
	Benchmarks   []catalog.Benchmark `json:"benchmarks"`
	Quote        quote               `json:"quote"`
}

type comparison struct {
	Version      string          `json:"catalog_version"`
	AsOf         time.Time       `json:"as_of"`
	Rows         []comparisonRow `json:"rows"`
	ScopeVersion *int64          `json:"scope_version,string,omitempty"`
}

func (h *Handler) compare(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, fault.Invalid("/query")
	}
	var in comparisonInput
	if err := decodeJSON(c, &in); err != nil {
		return nil, err
	}
	if in.KeyID != nil {
		if *in.KeyID == "" {
			return nil, fault.Invalid("/api_key_id")
		}
		var err error
		p, err = withSelectedKey(p, *in.KeyID)
		if err != nil {
			return nil, err
		}
	}
	if len(in.IDs) < 1 || len(in.IDs) > 3 {
		return nil, fault.Invalid("/model_ids")
	}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if !isIdentifier(id, "m_", 32) || seen[id] {
			return nil, fault.Invalid("/model_ids")
		}
		seen[id] = true
	}
	if (in.Protocol != "" && !facetValue.MatchString(in.Protocol)) || (in.Action != "" && !facetValue.MatchString(in.Action)) {
		return nil, fault.Invalid("/protocol")
	}
	var quantities []money.Quantity
	if in.Scenario != nil {
		var err error
		quantities, err = scenarioQuantities(*in.Scenario)
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := h.reader.Snapshot(ctx, p, time.Time{})
	if err != nil {
		return nil, err
	}
	at := snapshot.AsOf.UTC()
	if in.At != nil {
		at = in.At.UTC()
	}
	result := comparison{Version: snapshot.Version, AsOf: snapshot.AsOf, ScopeVersion: snapshot.ScopeVersion, Rows: make([]comparisonRow, 0, len(in.IDs))}
	catalogModels := map[string]catalog.Detail{}
	for _, d := range snapshot.Details {
		catalogModels[d.Model.ID] = d
	}
	for _, id := range in.IDs {
		d, ok := catalogModels[id]
		if !ok {
			return nil, fault.NotFound
		}
		row := comparisonRow{ID: id, Metrics: d.Metrics, Benchmarks: d.Benchmarks,
			Capabilities: comparisonCapabilities(d, in.Action, in.Protocol), Quote: quote{State: "unpriced", BillingState: "unpriced", PriceAt: at, Components: []component{}}}
		if d.Model.Billing != nil {
			row.Quote.BillingState, row.Quote.Reason = d.Model.Billing.State, d.Model.Billing.Reason
		}
		bindings := d.EligibleBindings(p, in.Action, in.Protocol)
		if len(bindings) == 0 {
			row.Quote.BillingState, row.Quote.Reason = "unpriced", "no_applicable_offer"
		}
		if quantities != nil && len(bindings) > 0 && h.comparePrices != nil && !at.Before(snapshot.AsOf.Add(-catalog.SnapshotTTL)) && !at.After(snapshot.AsOf) {
			estimate, state, reason, err := h.comparePrices.EstimateCatalog(ctx, p, bindings, quantities, at)
			if err != nil {
				return nil, err
			}
			row.Quote.State, row.Quote.BillingState, row.Quote.Reason = estimate.State, state, reason
			row.Quote.PriceVersion, row.Quote.Charge, row.Quote.RoundingAdjustment = estimate.PriceVersion, estimate.Charge, estimate.RoundingAdjustment
			for _, line := range estimate.Components {
				row.Quote.Components = append(row.Quote.Components, component{Kind: line.Kind, Quantity: line.Quantity, RawAmount: line.RawAmount})
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func matchesCapability(c catalog.Capability, action, protocol string) bool {
	return c.State != "stale" && (action == "" || c.Action == action) && (protocol == "" || c.Protocol == protocol)
}

func comparisonCapabilities(d catalog.Detail, action, protocol string) map[string]string {
	tools, stream := []string{}, []string{}
	for _, c := range d.Capabilities {
		if matchesCapability(c, action, protocol) {
			tools = append(tools, c.Tools)
			stream = append(stream, c.Stream)
		}
	}
	if action == "" && protocol == "" {
		for _, fact := range d.Facts {
			if fact.State != "stale" && fact.Tools != nil {
				tools = append(tools, catalog.Support(fact.Tools))
			}
		}
	}
	return map[string]string{"tools": consensus(tools), "stream": consensus(stream)}
}

func consensus(values []string) string {
	v := "unknown"
	for _, value := range values {
		if value == "unknown" {
			continue
		}
		if v != "unknown" && v != value {
			return "unknown"
		}
		v = value
	}
	return v
}

func scenarioQuantities(s scenario) ([]money.Quantity, error) {
	fields := []string{"input_tokens", "cached_input_tokens", "output_tokens", "requests"}
	values := []string{s.Input, s.Cached, s.Output, s.Requests}
	units := []money.Unit{"input_token", "cache_read_token", "output_token", "request"}
	for i, def := range []string{"0", "0", "0", "1"} {
		if values[i] == "" {
			values[i] = def
		}
	}
	numbers := make([]*big.Int, len(fields))
	for i, f := range fields {
		q, err := money.NewQuantity(units[i], values[i])
		if err != nil {
			return nil, fault.Invalid("/scenario/" + f)
		}
		numbers[i], _ = new(big.Int).SetString(q.Amount().String(), 10)
	}
	if numbers[1].Cmp(numbers[0]) > 0 {
		return nil, fault.Invalid("/scenario/cached_input_tokens")
	}
	if numbers[3].Sign() == 0 {
		return nil, fault.Invalid("/scenario/requests")
	}
	numbers[0].Sub(numbers[0], numbers[1])
	result := make([]money.Quantity, len(fields))
	for i := 0; i < 3; i++ {
		total := new(big.Int).Mul(numbers[i], numbers[3])
		var err error
		result[i], err = money.NewQuantity(units[i], total.String())
		if err != nil {
			return nil, fault.Invalid("/scenario/" + fields[i])
		}
	}
	result[3], _ = money.NewQuantity(units[3], numbers[3].String())
	return result, nil
}
