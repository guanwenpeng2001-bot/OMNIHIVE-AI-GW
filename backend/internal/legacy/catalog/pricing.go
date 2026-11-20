package legacy

import (
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func priceFor(model service.PlazaModel, group service.PlazaGroup, userRates map[int64]float64, public bool) catalog.Price {
	price := catalog.Price{State: "unpriced", Source: "unavailable", Additional: []catalog.RateBand{}, TimeBands: []catalog.TimeBand{}}
	raw := model.Pricing
	if raw == nil {
		return price
	}
	multiplier := group.RateMultiplier
	if personal, ok := userRates[group.ID]; ok && !public {
		multiplier = personal
	}
	requestUnit := money.Unit("request")
	if raw.BillingMode == service.BillingModeImage {
		requestUnit = "image"
		if group.ImageRateIndependent {
			multiplier = group.ImageRateMultiplier
		}
	}
	if group.VideoRateIndependent && raw.BillingMode == service.BillingModeVideo {
		multiplier = group.VideoRateMultiplier
	}
	factor, err := money.Parse(decimalText(multiplier))
	if err != nil || factor.Compare(money.Amount{}) < 0 {
		price.State = "error"
		return price
	}
	var faulty, nonzero, present bool
	// Channel prices and the catalog fallback carry a USD amount per individual
	// token, request, or image, including the prices on context tiers.
	rate := func(value *float64, unit money.Unit) *money.Rate {
		if value == nil {
			return nil
		}
		converted, err := legacyRate(*value, unit, "1", factor)
		if err != nil {
			faulty = true
			return nil
		}
		present = true
		nonzero = nonzero || converted.Amount().Compare(money.Amount{}) != 0
		return &converted
	}
	add := func(r *money.Rate, kind string, min, max *int64, label string) {
		if r != nil {
			price.Additional = append(price.Additional, catalog.RateBand{Rate: *r, Kind: kind, MinTokens: min, MaxTokens: max, Label: label})
		}
	}
	price.Input = rate(raw.InputPrice, "input_token")
	price.Output = rate(raw.OutputPrice, "output_token")
	add(rate(raw.CacheReadPrice, "cache_read_token"), "cache_read", nil, nil, "")
	add(rate(raw.CacheWritePrice, "cache_write_token"), "cache_write", nil, nil, "")
	add(rate(raw.CacheWrite1hPrice, "cache_write_token"), "cache_write_1h", nil, nil, "")
	add(rate(raw.ImageInputPrice, "image_input_token"), "image_input", nil, nil, "")
	add(rate(raw.ImageOutputPrice, "image_output_token"), "image_output", nil, nil, "")
	request := rate(raw.PerRequestPrice, requestUnit)
	add(request, "request", nil, nil, "")
	for _, tier := range raw.Intervals {
		var min, max *int64
		if raw.BillingMode == service.BillingModeToken || raw.BillingMode == "" {
			lower := int64(tier.MinTokens)
			min = &lower
			if tier.MaxTokens != nil {
				upper := int64(*tier.MaxTokens)
				max = &upper
			}
		}
		add(rate(tier.InputPrice, "input_token"), "tier_input", min, max, "")
		add(rate(tier.OutputPrice, "output_token"), "tier_output", min, max, "")
		add(rate(tier.CacheReadPrice, "cache_read_token"), "tier_cache_read", min, max, "")
		add(rate(tier.CacheWritePrice, "cache_write_token"), "tier_cache_write", min, max, "")
		add(rate(tier.CacheWrite1hPrice, "cache_write_token"), "tier_cache_write_1h", min, max, "")
		switch tier.TierLabel {
		case "1K", "2K", "4K":
			add(rate(tier.PerRequestPrice, requestUnit), "tier_request", min, max, tier.TierLabel)
		default:
			add(rate(tier.PerRequestPrice, requestUnit), "tier_request", min, max, "")
		}
	}
	if model.TimePricing != nil {
		for _, period := range model.TimePricing.Periods {
			m, err := money.Parse(decimalText(period.Multiplier))
			if err != nil {
				faulty = true
				continue
			}
			price.TimeBands = append(price.TimeBands, catalog.TimeBand{
				Timezone: model.TimePricing.Timezone, WeekdaysOnly: model.TimePricing.WeekdaysOnly,
				Start: period.StartTime, End: period.EndTime, Multiplier: m,
			})
		}
	}
	if group.PeakRateEnabled && group.PeakRateMultiplier != 1 &&
		(raw.BillingMode == "" || raw.BillingMode == service.BillingModeToken) &&
		service.ValidatePeakRateConfig(group.SubscriptionType, true, group.PeakStart, group.PeakEnd, group.PeakRateMultiplier) == nil {
		m, err := money.Parse(decimalText(group.PeakRateMultiplier))
		if err != nil {
			faulty = true
		} else {
			price.TimeBands = append(price.TimeBands, catalog.TimeBand{Timezone: timezone.Location().String(),
				Start: group.PeakStart, End: group.PeakEnd, Multiplier: m})
		}
	}
	if faulty {
		return catalog.Price{State: "error", Source: "unavailable", Additional: []catalog.RateBand{}, TimeBands: []catalog.TimeBand{}}
	}
	if present {
		price.State = "priced"
		if !nonzero && ((price.Input != nil && price.Output != nil) ||
			(request != nil && raw.BillingMode != "" && raw.BillingMode != service.BillingModeToken)) {
			price.State = "free"
		}
		price.Source = "scoped_plaza_reference"
		if public {
			price.Source = "public_plaza_reference"
		}
		price.ContextBasis = string(model.LongContextBasis)
	}
	return price
}

func legacyRate(value float64, unit money.Unit, per string, multiplier money.Amount) (money.Rate, error) {
	base, err := money.NewRate("USD", decimalText(value), unit, per)
	if err != nil {
		return money.Rate{}, err
	}
	exact, err := base.Amount().Exact("USD")
	if err != nil {
		return money.Rate{}, err
	}
	scaled, err := exact.Multiply(multiplier)
	if err != nil {
		return money.Rate{}, err
	}
	amount := strings.TrimRight(strings.TrimRight(scaled.Diagnostic(), "0"), ".")
	return money.NewRate("USD", amount, unit, per)
}
