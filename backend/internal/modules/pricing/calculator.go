package pricing

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

var ErrAmbiguous = errors.New("ambiguous pricing rules")

type Dimension struct {
	Kind     string         `json:"kind"`
	Quantity money.Quantity `json:"usage"`
	Optional bool           `json:"optional,omitempty"`
}

type Usage struct {
	Context    money.Quantity `json:"context"`
	Label      string         `json:"label,omitempty"`
	Dimensions []Dimension    `json:"dimensions"`
}

type Rule struct {
	Kind        string          `json:"kind"`
	Rate        money.Rate      `json:"rate"`
	MinContext  *money.Quantity `json:"min_context,omitempty"`
	MaxContext  *money.Quantity `json:"max_context,omitempty"`
	Label       string          `json:"label,omitempty"`
	Multipliers []money.Amount  `json:"multipliers,omitempty"`
}

type Schedule struct {
	Timezone     string       `json:"timezone"`
	Start        string       `json:"start"`
	End          string       `json:"end"`
	WeekdaysOnly bool         `json:"weekdays_only"`
	Multiplier   money.Amount `json:"multiplier"`
}

type Version struct {
	EvaluationContext *PriceContext  `json:"-"`
	Kind              string         `json:"kind,omitempty"`
	ID                string         `json:"id"`
	Source            string         `json:"source"`
	Currency          money.Currency `json:"currency"`
	EffectiveFrom     time.Time      `json:"effective_from"`
	EffectiveUntil    *time.Time     `json:"effective_until,omitempty"`
	Rules             []Rule         `json:"rules"`
	Schedules         []Schedule     `json:"schedules,omitempty"`
	Multipliers       []money.Amount `json:"multipliers,omitempty"`
}

type Candidates struct {
	Group, Channel, Published, Fallback []Version
}

func (v Version) active(at time.Time) bool {
	return !at.Before(v.EffectiveFrom) && (v.EffectiveUntil == nil || at.Before(*v.EffectiveUntil))
}

func Resolve(c Candidates, at time.Time) (*Version, error) {
	for _, versions := range [][]Version{c.Group, c.Channel, c.Published, c.Fallback} {
		var selected *Version
		for _, version := range versions {
			if version.active(at) {
				if selected != nil {
					return nil, ErrAmbiguous
				}
				selected = &version
			}
		}
		if selected != nil {
			return selected, nil
		}
	}
	return nil, nil
}

type Line struct {
	Kind        string `json:"kind"`
	Quantity    string `json:"quantity"`
	RawAmount   string `json:"raw_amount"`
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}

type Result struct {
	State              string       `json:"state"`
	PriceVersion       *string      `json:"price_version"`
	Source             *string      `json:"source"`
	Charge             *money.Money `json:"estimated_charge"`
	Components         []Line       `json:"components"`
	RoundingAdjustment *string      `json:"rounding_adjustment"`
}

func Unpriced() Result {
	return Result{State: "unpriced", Components: []Line{}}
}

type Frozen struct {
	Context *PriceContext `json:"price_context,omitempty"`
	Version Version       `json:"version"`
	Usage   Usage         `json:"usage"`
	At      time.Time     `json:"as_of"`
}

func (f Frozen) Calculate() (Result, error) {
	v := f.Version
	if err := v.ValidateExecutable(); err != nil {
		return Unpriced(), err
	}
	if f.Context != nil {
		if err := f.Context.Validate(); err != nil {
			return Unpriced(), err
		}
		if !f.At.Equal(f.Context.PricingAt) {
			return Unpriced(), errors.New("pricing context instant mismatch")
		}
		if f.Context.Funding == "wallet" && (v.Kind == "external_cost" || v.Kind == "platform_fee") {
			return Unpriced(), errors.New("non-sale tariff cannot authorize wallet charging")
		}
	}
	if v.ID == "" || !v.active(f.At) {
		return Unpriced(), nil
	}
	total, err := (money.Amount{}).Exact(v.Currency)
	if err != nil {
		return Result{}, err
	}
	factors := append([]money.Amount(nil), v.Multipliers...)
	for _, band := range v.Schedules {
		match, err := band.matches(f.At)
		if err != nil {
			return Result{}, err
		}
		if match {
			factors = append(factors, band.Multiplier)
		}
	}
	result := Unpriced()
	seen := make(map[string]bool, len(f.Usage.Dimensions))
	for _, dimension := range f.Usage.Dimensions {
		if dimension.Kind == "" || seen[dimension.Kind] {
			return Result{}, ErrAmbiguous
		}
		seen[dimension.Kind] = true
		rule, err := selectRule(v.Rules, f.Usage, dimension.Kind)
		if err != nil {
			return Result{}, err
		}
		if rule == nil {
			if dimension.Quantity.Amount().Compare(money.Amount{}) == 0 ||
				(dimension.Optional && !slices.ContainsFunc(v.Rules, func(r Rule) bool { return r.Kind == dimension.Kind })) {
				continue
			}
			return Unpriced(), nil
		}
		line, err := rule.Rate.Cost(dimension.Quantity)
		if err != nil {
			return Result{}, err
		}
		for _, group := range [][]money.Amount{rule.Multipliers, factors} {
			for _, factor := range group {
				line, err = line.Multiply(factor)
				if err != nil {
					return Result{}, err
				}
			}
		}
		total, err = total.Add(line)
		if err != nil {
			return Result{}, err
		}
		if dimension.Quantity.Amount().Compare(money.Amount{}) != 0 {
			ratio := line.Ratio()
			result.Components = append(result.Components, Line{dimension.Kind, dimension.Quantity.Amount().String(), line.Diagnostic(), ratio.Num().String(), ratio.Denom().String()})
		}
	}
	if len(result.Components) == 0 {
		return Unpriced(), nil
	}
	charge, err := total.RoundCharge()
	if err != nil {
		return Result{}, err
	}
	rounded, err := charge.Amount().Exact(v.Currency)
	if err != nil {
		return Result{}, err
	}
	delta, err := rounded.Sub(total)
	if err != nil {
		return Result{}, err
	}
	adjustment := delta.Diagnostic()
	result.State = "estimated"
	if total.Ratio().Sign() == 0 {
		result.State = "free"
	}
	result.PriceVersion, result.Source, result.Charge, result.RoundingAdjustment = &v.ID, &v.Source, &charge, &adjustment
	return result, nil
}

func selectRule(rules []Rule, usage Usage, kind string) (*Rule, error) {
	var selected *Rule
	rank := -1
	ambiguous := false
	for i := range rules {
		rule := &rules[i]
		if rule.Kind != kind || (rule.Label != "" && rule.Label != usage.Label) {
			continue
		}
		priority := 0
		if rule.MinContext != nil || rule.MaxContext != nil {
			match, err := usage.Context.InTier(rule.MinContext, rule.MaxContext)
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
			priority++
		}
		if rule.Label != "" {
			priority += 2
		}
		if priority > rank {
			selected, rank, ambiguous = rule, priority, false
		} else if priority == rank {
			ambiguous = true
		}
	}
	if ambiguous {
		return nil, fmt.Errorf("%w: %s", ErrAmbiguous, kind)
	}
	return selected, nil
}

func (s Schedule) matches(at time.Time) (bool, error) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return false, err
	}
	for _, clock := range []string{s.Start, s.End} {
		parsed, err := time.Parse("15:04", clock)
		if err != nil || parsed.Format("15:04") != clock {
			return false, fmt.Errorf("invalid pricing schedule clock %q", clock)
		}
	}
	local := at.In(loc)
	clock := local.Format("15:04")
	day := local.Weekday()
	inWindow := clock >= s.Start && clock < s.End
	if s.Start > s.End {
		inWindow = clock >= s.Start || clock < s.End
		if clock < s.End {
			day = local.AddDate(0, 0, -1).Weekday()
		}
	}
	return inWindow && (!s.WeekdaysOnly || (day != time.Saturday && day != time.Sunday)), nil
}
