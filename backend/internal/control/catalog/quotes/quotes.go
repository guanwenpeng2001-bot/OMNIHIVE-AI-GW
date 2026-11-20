package quotes

import (
	"context"
	"encoding/hex"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type Scenario struct {
	Input        string `json:"input_tokens"`
	Cached       string `json:"cached_input_tokens"`
	Output       string `json:"output_tokens"`
	Requests     string `json:"requests"`
	CacheWrite   string `json:"cache_write_tokens,omitempty"`
	CacheWrite1h string `json:"cache_write_1h_tokens,omitempty"`
	Reasoning    string `json:"reasoning_tokens,omitempty"`
	Images       string `json:"images,omitempty"`
	ImageInput   string `json:"image_input_tokens,omitempty"`
	ImageOutput  string `json:"image_output_tokens,omitempty"`
	Search       string `json:"search_calls,omitempty"`
	AudioSeconds string `json:"audio_seconds,omitempty"`
	VideoSeconds string `json:"video_seconds,omitempty"`
	SizeTier     string `json:"size_tier,omitempty"`
}

var facet = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

func ValidFacet(value string) bool { return value == "" || facet.MatchString(value) }

func ValidModel(id string) bool {
	digits, ok := strings.CutPrefix(id, "m_")
	if !ok || len(digits) != 32 || digits != strings.ToLower(digits) {
		return false
	}
	_, err := hex.DecodeString(digits)
	return err == nil
}

func (s Scenario) Usage() (pricing.Usage, error) {
	requests := s.Requests
	if requests == "" {
		requests = "1"
	}
	count, err := money.NewQuantity("request", requests)
	if err != nil || count.Amount().Compare(money.Amount{}) == 0 {
		return pricing.Usage{}, fault.Invalid("/scenario/requests")
	}
	if !ValidFacet(s.SizeTier) {
		return pricing.Usage{}, fault.Invalid("/scenario/size_tier")
	}
	fields := []struct {
		name, value, kind string
		unit              money.Unit
	}{
		{"input_tokens", s.Input, "input", "input_token"},
		{"cached_input_tokens", s.Cached, "cache_read", "cache_read_token"},
		{"output_tokens", s.Output, "output", "output_token"},
		{"reasoning_tokens", s.Reasoning, "reasoning", "reasoning_token"},
		{"cache_write_tokens", s.CacheWrite, "cache_write", "cache_write_token"},
		{"cache_write_1h_tokens", s.CacheWrite1h, "cache_write_1h", "cache_write_token"},
		{"images", s.Images, "image", "image"},
		{"image_input_tokens", s.ImageInput, "image_input", "image_input_token"},
		{"image_output_tokens", s.ImageOutput, "image_output", "image_output_token"},
		{"search_calls", s.Search, "search", "call"},
		{"audio_seconds", s.AudioSeconds, "audio", "second"},
		{"video_seconds", s.VideoSeconds, "video", "second"},
	}
	values := make([]money.Quantity, len(fields))
	for i, f := range fields {
		if f.value == "" {
			f.value = "0"
		}
		values[i], err = money.NewQuantity(f.unit, f.value)
		if err != nil {
			return pricing.Usage{}, fault.Invalid("/scenario/" + f.name)
		}
	}
	// Cached input is included in the input count. Other dimensions represent
	// disjoint usage so component rates apply exactly once.
	input, cached := integer(values[0]), integer(values[1])
	if input.Cmp(cached) < 0 {
		return pricing.Usage{}, fault.Invalid("/scenario/cached_input_tokens")
	}
	contextTokens := new(big.Int).Set(input)
	for _, i := range []int{4, 5, 7} {
		contextTokens.Add(contextTokens, integer(values[i]))
	}
	ctx, err := money.NewQuantity("token", contextTokens.String())
	if err != nil {
		return pricing.Usage{}, fault.Invalid("/scenario/input_tokens")
	}
	input.Sub(input, cached)
	values[0], _ = money.NewQuantity("input_token", input.String())
	usage := pricing.Usage{Context: ctx, Label: s.SizeTier, Dimensions: make([]pricing.Dimension, 0, len(fields)+1)}
	for i, f := range fields {
		if values[i].Amount().Compare(money.Amount{}) == 0 {
			continue
		}
		amount := new(big.Rat)
		amount.SetString(values[i].Amount().String())
		amount.Mul(amount, new(big.Rat).SetInt(integer(count)))
		total := amount.FloatString(0)
		if f.unit == "second" {
			total = strings.TrimRight(strings.TrimRight(amount.FloatString(10), "0"), ".")
		}
		q, err := money.NewQuantity(f.unit, total)
		if err != nil {
			return pricing.Usage{}, fault.Invalid("/scenario/" + f.name)
		}
		usage.Dimensions = append(usage.Dimensions, pricing.Dimension{Kind: f.kind, Quantity: q})
	}
	usage.Dimensions = append(usage.Dimensions, pricing.Dimension{Kind: "request", Quantity: count, Optional: true})
	return usage, nil
}

func integer(q money.Quantity) *big.Int {
	n, _ := new(big.Int).SetString(q.Amount().String(), 10)
	return n
}

func Matches(c catalog.Capability, action, protocol string) bool {
	return c.State != "stale" && (action == "" || c.Action == action) && (protocol == "" || c.Protocol == protocol)
}

func Reference(d catalog.Detail, at time.Time) (pricing.Version, error) {
	if d.PriceVersion == nil || (d.ContextBasis != "" && d.ContextBasis != "whole_request") || (d.Model.PriceState != "priced" && d.Model.PriceState != "free") {
		return pricing.Version{}, nil
	}
	v := pricing.Version{ID: *d.PriceVersion, Source: d.PriceSource, Currency: "USD"}
	if d.Model.Input != nil {
		v.Rules = append(v.Rules, pricing.Rule{Kind: "input", Rate: *d.Model.Input})
	}
	if d.Model.Output != nil {
		v.Rules = append(v.Rules, pricing.Rule{Kind: "output", Rate: *d.Model.Output})
	}
	for _, band := range d.AdditionalRates {
		kind, _ := strings.CutPrefix(band.Kind, "tier_")
		if kind == "request" && band.Rate.Unit() != "request" {
			kind = string(band.Rate.Unit())
		}
		r := pricing.Rule{Kind: kind, Rate: band.Rate, Label: band.Label}
		for _, limit := range []struct {
			value  *int64
			target **money.Quantity
		}{{band.MinTokens, &r.MinContext}, {band.MaxTokens, &r.MaxContext}} {
			if limit.value == nil {
				continue
			}
			q, err := money.NewQuantity("token", strconv.FormatInt(*limit.value, 10))
			if err != nil {
				return pricing.Version{}, err
			}
			*limit.target = &q
		}
		v.Rules = append(v.Rules, r)
	}
	for _, window := range d.TimeBands {
		v.Schedules = append(v.Schedules, pricing.Schedule{Timezone: window.Timezone, Start: window.Start, End: window.End,
			WeekdaysOnly: window.WeekdaysOnly, Multiplier: window.Multiplier})
	}
	// Reference price cards supply evidence for the current snapshot only.
	v.EffectiveFrom = at
	return v, nil
}

type Input struct {
	ModelID  string    `json:"model_id"`
	APIKeyID *string   `json:"api_key_id,omitempty"`
	Protocol string    `json:"protocol"`
	Action   string    `json:"action"`
	Scenario *Scenario `json:"scenario"`
}

// KeyPrices must authorize the requested key in the supplied perspective and
// resolve only the model/action allowed for that key.
type KeyPrices interface {
	ResolveKeyPrice(context.Context, catalog.Perspective, int64, catalog.Detail, string, string, pricing.Usage, time.Time) (pricing.Version, error)
}

type Service struct {
	Catalog   catalog.CatalogReader
	Keys      KeyPrices
	Published PublishedVersions
}

type Quote struct {
	pricing.Result
	Scope *catalog.EffectiveScope `json:"effective_scope,omitempty"`
}

func (s Service) Quote(ctx context.Context, p catalog.Perspective, in Input) (Quote, error) {
	if p.Public() {
		return Quote{}, fault.NotFound
	}
	if !ValidModel(in.ModelID) {
		return Quote{}, fault.Invalid("/model_id")
	}
	if in.Protocol == "" || !ValidFacet(in.Protocol) {
		return Quote{}, fault.Invalid("/protocol")
	}
	if in.Action == "" || !ValidFacet(in.Action) {
		return Quote{}, fault.Invalid("/action")
	}
	if in.Scenario == nil {
		return Quote{}, fault.Invalid("/scenario")
	}
	usage, err := in.Scenario.Usage()
	if err != nil {
		return Quote{}, err
	}
	var key int64
	if in.APIKeyID != nil {
		key, err = strconv.ParseInt(*in.APIKeyID, 10, 64)
		if err != nil || key <= 0 || strconv.FormatInt(key, 10) != *in.APIKeyID {
			return Quote{}, fault.Invalid("/api_key_id")
		}
		if s.Keys == nil {
			return Quote{}, fault.NotFound
		}
	}
	snap, err := s.Catalog.Snapshot(ctx, p, time.Time{})
	if err != nil {
		return Quote{}, err
	}
	for _, d := range snap.Details {
		if d.Model.ID != in.ModelID || d.Model.AccessState != "allowed" {
			continue
		}
		for _, capability := range d.Capabilities {
			if !Matches(capability, in.Action, in.Protocol) {
				continue
			}
			scope := &catalog.EffectiveScope{Workspace: p.Workspace, Project: p.Project, Version: p.AccessVersion}
			if key == 0 {
				cost, err := s.Estimate(ctx, p, d, usage, snap.AsOf, snap.AsOf)
				return Quote{Result: cost, Scope: scope}, err
			}
			version, err := s.Keys.ResolveKeyPrice(ctx, p, key, d, in.Protocol, in.Action, usage, snap.AsOf)
			if err != nil {
				return Quote{}, err
			}
			cost, err := (pricing.Frozen{Version: version, Usage: usage, At: snap.AsOf}).Calculate()
			return Quote{Result: cost, Scope: scope}, err
		}
	}
	return Quote{}, fault.NotFound
}
