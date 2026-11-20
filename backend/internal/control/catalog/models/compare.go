package models

import (
	"context"
	"math/big"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type comparisonInput struct {
	IDs      []string   `json:"model_ids"`
	Protocol string     `json:"protocol"`
	Action   string     `json:"action"`
	Scenario *scenario  `json:"scenario"`
	At       *time.Time `json:"price_at"`
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
	Version string          `json:"catalog_version"`
	AsOf    time.Time       `json:"as_of"`
	Rows    []comparisonRow `json:"rows"`
}

func (h *Handler) compare(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, fault.Invalid("/query")
	}
	var in comparisonInput
	if err := decodeJSON(c, &in); err != nil {
		return nil, err
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
	result := comparison{Version: snapshot.Version, AsOf: snapshot.AsOf, Rows: make([]comparisonRow, 0, len(in.IDs))}
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
			Capabilities: comparisonCapabilities(d, in.Action, in.Protocol), Quote: quote{State: "unpriced", Components: []component{}}}
		matching := slices.ContainsFunc(d.Capabilities, func(c catalog.Capability) bool { return matchesCapability(c, in.Action, in.Protocol) })
		if quantities != nil && (in.At == nil || (!in.At.Before(snapshot.AsOf.Add(-catalog.SnapshotTTL)) && !in.At.After(snapshot.AsOf))) &&
			((in.Action == "" && in.Protocol == "") || matching) {
			row.Quote = simpleEstimate(d, quantities)
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

func simpleEstimate(d catalog.Detail, quantities []money.Quantity) quote {
	missing := quote{State: "unpriced", Components: []component{}}
	if d.PriceVersion == nil || len(d.TimeBands) > 0 || d.ContextBasis != "" {
		return missing
	}
	rates := []*money.Rate{d.Model.Input, nil, d.Model.Output, nil}
	kinds := []string{"input", "cache_read", "output", "request"}
	for _, band := range d.AdditionalRates {
		switch band.Kind {
		case "cache_read":
			rate := band.Rate
			rates[1] = &rate
		case "request":
			if band.Rate.Unit() != "request" {
				return missing
			}
			rate := band.Rate
			rates[3] = &rate
		case "cache_write", "cache_write_1h":
		default:
			return missing
		}
	}
	if rates[3] == nil {
		quantities = quantities[:3]
	}
	total, _ := (money.Amount{}).Exact("USD")
	parts := []component{}
	for i, q := range quantities {
		if q.Amount().Compare(money.Amount{}) == 0 {
			continue
		}
		if rates[i] == nil {
			return missing
		}
		line, err := rates[i].Cost(q)
		if err != nil {
			return missing
		}
		total, err = total.Add(line)
		if err != nil {
			return missing
		}
		parts = append(parts, component{Kind: kinds[i], Quantity: q.Amount().String(), RawAmount: line.Diagnostic()})
	}
	if len(parts) == 0 {
		return missing
	}
	charge, err := total.RoundCharge()
	if err != nil {
		return missing
	}
	rounded, err := charge.Amount().Exact(charge.Currency())
	if err != nil {
		return missing
	}
	delta, err := rounded.Sub(total)
	if err != nil {
		return missing
	}
	adjustment := delta.Diagnostic()
	return quote{State: "estimated", PriceVersion: d.PriceVersion, Charge: &charge, Components: parts, RoundingAdjustment: &adjustment}
}
