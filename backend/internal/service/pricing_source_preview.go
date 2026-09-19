package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// PricingQuote describes one price dimension, not a bill or permission to apply
// it. Scope must identify the exact deployment/region/plan being compared.
type PricingQuote struct {
	Value      *float64 `json:"value"`
	Currency   string   `json:"currency"`
	Unit       string   `json:"unit"`
	Scope      string   `json:"scope"`
	Conditions string   `json:"conditions"`
}

type PricingDifference struct {
	ID              string       `json:"id"`
	Basis           string       `json:"basis"`
	Model           string       `json:"model"`
	Field           string       `json:"field"`
	Baseline        PricingQuote `json:"baseline"`
	Candidate       PricingQuote `json:"candidate"`
	Status          string       `json:"status"`
	AbsolutePercent *float64     `json:"absolute_percent"`
	Warning         bool         `json:"warning"`
}

// ComparePricingQuotes deliberately does not convert currencies, units or
// conditions. The caller must supply matching, evidenced scopes. Two supported
// bases distinguish reference changes from channel-to-reference differences.
func ComparePricingQuotes(basis, model, field string, baseline, candidate PricingQuote) PricingDifference {
	difference := PricingDifference{Basis: basis, Model: model, Field: field, Baseline: baseline, Candidate: candidate, Status: "incomparable"}
	identity, _ := json.Marshal(difference)
	digest := sha256.Sum256(identity)
	difference.ID = hex.EncodeToString(digest[:])
	if basis != "reference_to_reference" && basis != "channel_to_reference" {
		return difference
	}
	if baseline.Value == nil || candidate.Value == nil {
		difference.Status = "missing"
		return difference
	}
	if !validBillingNumber(*baseline.Value) || !validBillingNumber(*candidate.Value) ||
		baseline.Currency == "" || baseline.Unit == "" || baseline.Scope == "" || baseline.Conditions == "" ||
		baseline.Currency != candidate.Currency || baseline.Unit != candidate.Unit ||
		baseline.Scope != candidate.Scope || baseline.Conditions != candidate.Conditions {
		return difference
	}
	if *baseline.Value == 0 && *candidate.Value != 0 {
		difference.Status = "new_charge"
		difference.Warning = true
		return difference
	}
	percent := 0.0
	if *baseline.Value != 0 {
		percent = math.Abs(*candidate.Value-*baseline.Value) / math.Abs(*baseline.Value) * 100
	}
	if math.IsInf(percent, 0) || math.IsNaN(percent) {
		return difference
	}
	difference.Status = "comparable"
	difference.AbsolutePercent = &percent
	// Decimal 10% can round a few ulps above 10 in binary floating point.
	difference.Warning = percent > 10 && math.Abs(percent-10) > 1e-12
	return difference
}

var pricingPreviewUnits = map[string]string{
	"input_cost_per_token": "token", "output_cost_per_token": "token",
	"input_cost_per_token_priority": "token", "output_cost_per_token_priority": "token",
	"cache_creation_input_token_cost": "token", "cache_creation_input_token_cost_priority": "token",
	"cache_creation_input_token_cost_above_1hr": "token",
	"cache_read_input_token_cost":               "token", "cache_read_input_token_cost_priority": "token",
	"output_cost_per_image": "image", "output_cost_per_image_token": "image_token",
	"input_cost_per_image_token": "image_token", "cache_read_input_image_token_cost": "image_token",
}

func pricingEntryQuotes(model string, raw json.RawMessage) map[string]PricingQuote {
	var entry map[string]json.RawMessage
	if json.Unmarshal(raw, &entry) != nil {
		return nil
	}
	var provider string
	_ = json.Unmarshal(entry["litellm_provider"], &provider)
	currency := "USD" // LiteLLM catalog's documented currency; never used for native imports.
	if value, found := entry["currency"]; found {
		currency = ""
		_ = json.Unmarshal(value, &currency)
	}
	conditions := map[string]json.RawMessage{}
	// Explicit condition schema. Price dimension names (including the 1-hour
	// cache write price) are not billing conditions.
	for _, key := range []string{"mode", "region", "supported_regions", "service_tier",
		"pricing_conditions", "price_unit", "plan", "long_context_input_token_threshold",
		"long_context_input_cost_multiplier", "long_context_output_cost_multiplier"} {
		if value, found := entry[key]; found {
			conditions[key] = value
		}
	}
	// Tiered price keys encode real thresholds. Preserve the tier schema as a
	// condition, but compare its prices as independent dimensions, not conditions.
	units := make(map[string]string, len(pricingPreviewUnits))
	for field, unit := range pricingPreviewUnits {
		units[field] = unit
	}
	var tiers []string
	for field := range entry {
		if aboveTierPricePattern.MatchString(field) || cacheTierPricePattern.MatchString(field) {
			units[field] = "token"
			tiers = append(tiers, field)
		}
	}
	if len(tiers) > 0 {
		sort.Strings(tiers)
		conditions["token_price_tiers"], _ = json.Marshal(tiers)
	}
	conditionJSON, _ := json.Marshal(conditions)
	quotes := make(map[string]PricingQuote, len(units))
	for field, unit := range units {
		var value *float64
		if rawValue, found := entry[field]; found {
			_ = json.Unmarshal(rawValue, &value)
		}
		quotes[field] = PricingQuote{Value: value, Currency: currency, Unit: unit,
			Scope: provider + "/" + model, Conditions: string(conditionJSON)}
	}
	return quotes
}

type PricingSourcePreview struct {
	CandidateHash string              `json:"candidate_hash"`
	BeforeCount   int                 `json:"before_count"`
	AfterCount    int                 `json:"after_count"`
	Added         []string            `json:"added"`
	Removed       []string            `json:"removed"`
	Differences   []PricingDifference `json:"differences"`
}

// PreviewSource parses exactly the same fallback/override layers as a refresh,
// without publishing a snapshot, persisting files, or updating channel prices.
func (s *PricingService) PreviewSource(body []byte) (*PricingSourcePreview, error) {
	candidate, _, err := s.buildPricingData(body)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	preview := &PricingSourcePreview{CandidateHash: capabilityHash(body), BeforeCount: len(s.pricingData), AfterCount: len(candidate),
		Added: []string{}, Removed: []string{}, Differences: []PricingDifference{}}
	for model, pricing := range candidate {
		previous, found := s.pricingData[model]
		if !found {
			preview.Added = append(preview.Added, model)
			continue
		}
		for field, quote := range pricing.sourceQuotes {
			baseline := previous.sourceQuotes[field]
			if baseline.Value == nil && quote.Value == nil {
				continue
			}
			difference := ComparePricingQuotes("reference_to_reference", model, field, baseline, quote)
			// Include source generation in the UI deduplication identity.
			difference.ID = preview.CandidateHash + ":" + difference.ID
			preview.Differences = append(preview.Differences, difference)
		}
	}
	for model := range s.pricingData {
		if _, found := candidate[model]; !found {
			preview.Removed = append(preview.Removed, model)
		}
	}
	sort.Strings(preview.Added)
	sort.Strings(preview.Removed)
	sort.Slice(preview.Differences, func(left, right int) bool {
		if preview.Differences[left].Model != preview.Differences[right].Model {
			return preview.Differences[left].Model < preview.Differences[right].Model
		}
		return preview.Differences[left].Field < preview.Differences[right].Field
	})
	return preview, nil
}

type PricingChargeImpact struct {
	Model         string         `json:"model"`
	GroupID       *int64         `json:"group_id"`
	Before        *CostBreakdown `json:"before"`
	After         *CostBreakdown `json:"after"`
	BeforeError   string         `json:"before_error,omitempty"`
	AfterError    string         `json:"after_error,omitempty"`
	ChargeChanged bool           `json:"charge_changed"`
	Comparable    bool           `json:"comparable"`
}

// PreviewChargeImpact runs the existing billing/resolver chain twice against
// private price snapshots. Samples must be new, explicit usage scenarios, not
// requests carrying a frozen gateway plan or an already-resolved price card.
// No settlement, historical repricing or repository update is performed.
func (s *PricingService) PreviewChargeImpact(body []byte, billing *BillingService, channels *ChannelService, samples []CostInput) ([]PricingChargeImpact, error) {
	if billing == nil {
		return nil, fmt.Errorf("billing service is required")
	}
	candidate, _, err := s.buildPricingData(body)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	previous := make(map[string]*LiteLLMModelPricing, len(s.pricingData))
	for model, pricing := range s.pricingData {
		copy := *pricing
		previous[model] = &copy
	}
	s.mu.RUnlock()
	beforeBilling := &BillingService{cfg: billing.cfg, fallbackPrices: billing.fallbackPrices,
		pricingService: &PricingService{cfg: s.cfg, pricingData: previous}}
	afterBilling := &BillingService{cfg: billing.cfg, fallbackPrices: billing.fallbackPrices,
		pricingService: &PricingService{cfg: s.cfg, pricingData: candidate}}
	beforeResolver := NewModelPricingResolver(channels, beforeBilling)
	afterResolver := NewModelPricingResolver(channels, afterBilling)
	impacts := make([]PricingChargeImpact, 0, len(samples))
	previewAt := time.Now()
	for _, sample := range samples {
		if sample.Ctx == nil {
			sample.Ctx = context.Background()
		}
		if sample.Resolved != nil || GatewayRequestPlanFromContext(sample.Ctx) != nil {
			return nil, fmt.Errorf("preview requires an unfrozen usage scenario")
		}
		if sample.PricingAt.IsZero() {
			sample.PricingAt = previewAt
		}
		impact := PricingChargeImpact{Model: sample.Model, GroupID: sample.GroupID}
		sample.Resolver = beforeResolver
		impact.Before, err = beforeBilling.CalculateCostUnified(sample)
		if err != nil {
			impact.BeforeError = err.Error()
		}
		sample.Resolver = afterResolver
		impact.After, err = afterBilling.CalculateCostUnified(sample)
		if err != nil {
			impact.AfterError = err.Error()
		}
		impact.Comparable = impact.BeforeError == "" && impact.AfterError == ""
		impact.ChargeChanged = impact.Comparable && impact.Before.ActualCost != impact.After.ActualCost
		impacts = append(impacts, impact)
	}
	return impacts, nil
}
