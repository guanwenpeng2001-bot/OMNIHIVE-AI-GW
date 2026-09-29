package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
)

// PricingState is independent of settlement and provider completion state.
type PricingState string

const (
	PricingStatePriced   PricingState = "priced"
	PricingStateFree     PricingState = "free"
	PricingStateUnpriced PricingState = "unpriced"
	PricingStateError    PricingState = "error"
)

var ErrInvalidBillingAmount = errors.New("invalid billing amount or quantity")

// PricingSnapshot is serialized at calculation time, before mutable price cards
// can change. It contains effective rates only, never credentials or prompts.
// Amounts remain raw for the historical fingerprint; ChargedAmount is the
// canonical eight-place amount to use for every customer effect and usage log.
type PricingSnapshot struct {
	// displayProbe skips serialization only after the ordinary amount/rate checks.
	displayProbe                  bool
	ProxyCacheDiscountInputTokens int                    `json:"proxy_cache_discount_input_tokens,omitempty"`
	PricingAt                     time.Time              `json:"pricing_at,omitzero"`
	PricingState                  PricingState           `json:"pricing_state"`
	QuantizeQuoteBeforeMultiplier bool                   `json:"quantize_quote_before_multiplier,omitempty"`
	AdjustmentMultiplier          float64                `json:"adjustment_multiplier,omitempty"`
	AccountRateMultiplier         float64                `json:"account_rate_multiplier,omitempty"`
	AccountStatsCost              *float64               `json:"account_stats_cost,omitempty"`
	FX                            *QianwenPricingOptions `json:"fx,omitempty"`
	Additions                     []json.RawMessage      `json:"additions,omitempty"`
	CustomerPricing               json.RawMessage        `json:"customer_pricing,omitempty"`

	Model               string        `json:"model"`
	Provider            string        `json:"provider,omitempty"`
	Mode                string        `json:"mode"`
	Source              string        `json:"source"`
	PriceCardVersion    string        `json:"price_card_version,omitempty"`
	Tokens              UsageTokens   `json:"tokens"`
	TokenPrices         *ModelPricing `json:"token_prices,omitempty"`
	UnitPrice           *float64      `json:"unit_price,omitempty"`
	Units               float64       `json:"units,omitempty"`
	SizeTier            string        `json:"size_tier,omitempty"`
	ServiceTier         string        `json:"service_tier,omitempty"`
	ThinkingEnabled     *bool         `json:"thinking_enabled,omitempty"`
	LongContextApplied  bool          `json:"long_context_applied,omitempty"`
	RateMultiplier      float64       `json:"rate_multiplier"`
	TimeMultiplier      float64       `json:"time_multiplier"`
	ReasoningMultiplier float64       `json:"reasoning_multiplier"`
	TotalCost           float64       `json:"total_cost"`
	ActualCost          float64       `json:"actual_cost"`
	ChargedAmount       float64       `json:"charged_amount"`
}

func validBillingNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// ValidateUsageBillingAmount rejects values outside NUMERIC(20,8), including
// rounding across its upper bound. Quantization itself remains legacy compatible.
func ValidateUsageBillingAmount(value float64) error {
	if !validBillingNumber(value) || QuantizeUsageBillingAmount(value) >= 1e12 {
		return ErrInvalidBillingAmount
	}
	return nil
}

func pricingFailure(err error) *CostBreakdown {
	state := PricingStateError
	if errors.Is(err, ErrModelPricingUnavailable) {
		state = PricingStateUnpriced
	}
	return &CostBreakdown{PricingState: state}
}

func validateCostInput(input CostInput) error {
	if !validBillingNumber(input.RateMultiplier) || !validBillingNumber(input.UsageUnits) || input.RequestCount < 0 {
		return ErrInvalidBillingAmount
	}
	for _, quantity := range []int{input.Tokens.InputTokens, input.Tokens.OutputTokens, input.Tokens.CacheCreationTokens, input.Tokens.CacheReadTokens, input.Tokens.CacheCreation5mTokens, input.Tokens.CacheCreation1hTokens, input.Tokens.ImageInputTokens, input.Tokens.ImageCacheReadTokens, input.Tokens.ImageOutputTokens} {
		if quantity < 0 {
			return ErrInvalidBillingAmount
		}
	}
	if input.Tokens.InputTokens > math.MaxInt-input.Tokens.CacheCreationTokens || input.Tokens.InputTokens+input.Tokens.CacheCreationTokens > math.MaxInt-input.Tokens.CacheReadTokens {
		return ErrInvalidBillingAmount
	}
	return nil
}

func freezePricing(cost *CostBreakdown, snapshot PricingSnapshot) error {
	if cost == nil {
		return ErrInvalidBillingAmount
	}
	for _, amount := range []float64{cost.InputCost, cost.ImageInputCost, cost.OutputCost, cost.ImageOutputCost, cost.CacheCreationCost, cost.CacheReadCost, cost.TotalCost, cost.ActualCost} {
		if err := ValidateUsageBillingAmount(amount); err != nil {
			return err
		}
	}
	cost.PricingState = PricingStatePriced
	free := snapshot.UnitPrice != nil && *snapshot.UnitPrice == 0
	if snapshot.TokenPrices != nil {
		free = true
		fields := reflect.ValueOf(*snapshot.TokenPrices)
		for index := 0; index < fields.NumField(); index++ {
			field := fields.Field(index)
			if field.Kind() == reflect.Pointer && !field.IsNil() && field.Elem().Kind() == reflect.Float64 && !validBillingNumber(field.Elem().Float()) {
				return ErrInvalidBillingAmount
			}
			if field.Kind() == reflect.Float64 {
				if !validBillingNumber(field.Float()) {
					return ErrInvalidBillingAmount
				}
				if strings.Contains(fields.Type().Field(index).Name, "Price") && field.Float() > 0 {
					free = false
				}
			}
		}
	}
	if cost.TotalCost == 0 && snapshot.TokenPrices != nil && (snapshot.Tokens.InputTokens > 0 || snapshot.Tokens.OutputTokens > 0 || snapshot.Tokens.CacheReadTokens > 0 || snapshot.Tokens.CacheCreationTokens > 0) {
		free = true
	}
	if free && cost.TotalCost == 0 {
		cost.PricingState = PricingStateFree
	}
	if snapshot.displayProbe {
		return nil
	}
	snapshot.PricingState = cost.PricingState
	snapshot.TotalCost, snapshot.ActualCost = cost.TotalCost, cost.ActualCost
	snapshot.ChargedAmount = QuantizeUsageBillingAmount(cost.ActualCost)
	snapshot.LongContextApplied = cost.LongContextBillingApplied
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("freeze pricing: %w", err)
	}
	cost.PricingSnapshot = string(encoded)
	return nil
}

func calculateUnitCost(model, tier, mode, source string, units, price, multiplier float64) *CostBreakdown {
	if !validBillingNumber(units) || !validBillingNumber(price) || !validBillingNumber(multiplier) {
		return pricingFailure(ErrInvalidBillingAmount)
	}
	cost := &CostBreakdown{TotalCost: units * price, ActualCost: units * price * multiplier, BillingMode: mode, PriceSource: source}
	if err := freezePricing(cost, PricingSnapshot{Model: model, Mode: mode, Source: source, UnitPrice: &price, Units: units, SizeTier: tier, RateMultiplier: multiplier, TimeMultiplier: 1, ReasoningMultiplier: 1}); err != nil {
		return pricingFailure(err)
	}
	return cost
}

// freezeJobPricing persists calculated inputs alongside the existing job tariff.
func freezeJobPricing(job *UpstreamJob, log *UsageLog, snapshot PricingSnapshot) error {
	snapshot.Model, snapshot.Provider = job.Billing.Model, job.Platform
	snapshot.QuantizeQuoteBeforeMultiplier = true
	if !validBillingNumber(job.Billing.AccountRateMultiplier) || ValidateUsageBillingAmount(log.TotalCost*job.Billing.AccountRateMultiplier) != nil {
		return ErrInvalidBillingAmount
	}
	if job.Billing.Async != nil {
		snapshot.FX = &job.Billing.Async.QianwenPricingOptions
	}
	snapshot.RateMultiplier, snapshot.AccountRateMultiplier = job.Billing.RateMultiplier, job.Billing.AccountRateMultiplier
	snapshot.AccountStatsCost = log.AccountStatsCost
	snapshot.TimeMultiplier, snapshot.ReasoningMultiplier = 1, 1
	snapshot.Source, snapshot.PriceCardVersion = optionalStringValue(log.PriceSource), optionalStringValue(log.PriceCardVersion)
	snapshot.Mode = optionalStringValue(log.BillingMode)
	cost := &CostBreakdown{TotalCost: log.TotalCost, ActualCost: log.ActualCost}
	if err := freezePricing(cost, snapshot); err != nil {
		return err
	}
	job.Billing.PricingState, job.Billing.PricingSnapshot = cost.PricingState, cost.PricingSnapshot
	log.PricingState, log.PricingSnapshot = cost.PricingState, cost.PricingSnapshot
	return nil
}

func appendPricingAddition(cost, addition *CostBreakdown, customerOnly bool) {
	if cost == nil || addition == nil || cost.PricingSnapshot == "" || addition.PricingSnapshot == "" {
		return
	}
	var snapshot PricingSnapshot
	if json.Unmarshal([]byte(cost.PricingSnapshot), &snapshot) != nil {
		return
	}
	if customerOnly {
		snapshot.CustomerPricing = json.RawMessage(addition.PricingSnapshot)
	} else {
		snapshot.Additions = append(snapshot.Additions, json.RawMessage(addition.PricingSnapshot))
	}
	if err := freezePricing(cost, snapshot); err != nil {
		cost.PricingState = PricingStateError
	}
}

// RecalculatePricingSnapshot uses only frozen effective rates. It never resolves
// today's model catalog, group settings, time windows, or FX.
func RecalculatePricingSnapshot(encoded string) (*CostBreakdown, error) {
	var snapshot PricingSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return nil, err
	}
	if err := validateCostInput(CostInput{Tokens: snapshot.Tokens, RateMultiplier: snapshot.RateMultiplier, UsageUnits: snapshot.Units}); err != nil {
		return nil, err
	}
	var cost *CostBreakdown
	if snapshot.TokenPrices != nil {
		cost = (&BillingService{}).computeTokenBreakdown(snapshot.TokenPrices, snapshot.Tokens, snapshot.RateMultiplier, snapshot.ServiceTier, snapshot.LongContextApplied)
		applyCostBreakdownMultiplier(cost, snapshot.TimeMultiplier)
		applyCostBreakdownMultiplier(cost, snapshot.ReasoningMultiplier)
	} else if snapshot.UnitPrice != nil {
		cost = &CostBreakdown{TotalCost: *snapshot.UnitPrice * snapshot.Units, ActualCost: *snapshot.UnitPrice * snapshot.Units * snapshot.RateMultiplier}
	} else {
		return nil, ErrModelPricingUnavailable
	}
	if snapshot.QuantizeQuoteBeforeMultiplier {
		cost.TotalCost = QuantizeUsageBillingAmount(cost.TotalCost)
		cost.ActualCost = QuantizeUsageBillingAmount(cost.TotalCost * snapshot.RateMultiplier)
	}
	if snapshot.AdjustmentMultiplier != 0 {
		applyCostBreakdownMultiplier(cost, snapshot.AdjustmentMultiplier)
	}
	for _, encodedAddition := range snapshot.Additions {
		addition, err := RecalculatePricingSnapshot(string(encodedAddition))
		if err != nil {
			return nil, err
		}
		cost.TotalCost += addition.TotalCost
		cost.ActualCost += addition.ActualCost
	}
	if len(snapshot.CustomerPricing) > 0 {
		customer, err := RecalculatePricingSnapshot(string(snapshot.CustomerPricing))
		if err != nil {
			return nil, err
		}
		cost.ActualCost = customer.ActualCost
	}
	cost.BillingMode, cost.PriceSource, cost.PriceCardVersion = snapshot.Mode, snapshot.Source, snapshot.PriceCardVersion
	if err := freezePricing(cost, snapshot); err != nil {
		return nil, err
	}
	return cost, nil
}

func refreshPricingMultiplier(cost *CostBreakdown, multiplier float64) {
	if cost.PricingSnapshot == "" {
		return
	}
	var snapshot PricingSnapshot
	if json.Unmarshal([]byte(cost.PricingSnapshot), &snapshot) != nil {
		cost.PricingState = PricingStateError
		return
	}
	if snapshot.AdjustmentMultiplier == 0 {
		snapshot.AdjustmentMultiplier = 1
	}
	snapshot.AdjustmentMultiplier *= multiplier
	if err := freezePricing(cost, snapshot); err != nil {
		cost.PricingState = PricingStateError
	}
}

// FreezeUsagePricing attaches final provider and account cost context without
// re-resolving prices. Call before persisting a receipt or constructing effects.
func FreezeUsagePricing(cost *CostBreakdown, model, provider string, accountMultiplier float64, accountStatsCost *float64) error {
	if cost == nil || cost.PricingSnapshot == "" {
		return nil
	}
	if !validBillingNumber(accountMultiplier) {
		return ErrInvalidBillingAmount
	}
	var snapshot PricingSnapshot
	if err := json.Unmarshal([]byte(cost.PricingSnapshot), &snapshot); err != nil {
		return err
	}
	snapshot.Provider, snapshot.AccountRateMultiplier, snapshot.AccountStatsCost = provider, accountMultiplier, accountStatsCost
	if snapshot.Model == "" {
		snapshot.Model = model
	}
	if cost.PriceSource != "" {
		snapshot.Source = cost.PriceSource
	}
	if cost.PriceCardVersion != "" {
		snapshot.PriceCardVersion = cost.PriceCardVersion
	}
	return freezePricing(cost, snapshot)
}

func recordJobPricingFailure(job *UpstreamJob, err error) {
	if job == nil || err == nil {
		return
	}
	job.Billing.PricingState = PricingStateError
	if errors.Is(err, ErrUpstreamJobTariff) || errors.Is(err, ErrModelPricingUnavailable) {
		job.Billing.PricingState = PricingStateUnpriced
	}
}
