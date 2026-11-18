package dto

import (
	"encoding/json"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type UsageBillingExplanation struct {
	PriceSource      *string                  `json:"price_source,omitempty"`
	PriceCardVersion *string                  `json:"price_card_version,omitempty"`
	PricingState     string                   `json:"pricing_state"`
	SettlementState  string                   `json:"settlement_state"`
	CompletionState  string                   `json:"completion_state"`
	Snapshot         *service.PricingSnapshot `json:"snapshot"`
	JobID            string                   `json:"job_id,omitempty"`
	SessionID        string                   `json:"session_id,omitempty"`
	TurnID           string                   `json:"turn_id,omitempty"`
	Family           string                   `json:"family,omitempty"`
}

func usageBillingExplanation(log *service.UsageLog, admin bool) *UsageBillingExplanation {
	evidence := log.BillingEvidence
	if evidence == nil {
		if log.PriceSource == nil && log.PriceCardVersion == nil {
			return nil
		}
		return &UsageBillingExplanation{PriceSource: log.PriceSource, PriceCardVersion: log.PriceCardVersion}
	}
	result := &UsageBillingExplanation{PriceSource: log.PriceSource, PriceCardVersion: log.PriceCardVersion, PricingState: evidence.PricingState, SettlementState: evidence.SettlementState, CompletionState: evidence.CompletionState, JobID: evidence.JobID, SessionID: evidence.SessionID, TurnID: evidence.TurnID, Family: evidence.Family}
	if evidence.Snapshot == "" {
		return result
	}
	var snapshot service.PricingSnapshot
	if err := json.Unmarshal([]byte(evidence.Snapshot), &snapshot); err != nil {
		return result
	}
	// Keep the existing owner/admin boundary: price evidence must not expose
	// account costs or routing models via an otherwise nested user DTO.
	if !admin {
		snapshot.Model = log.RequestedModel
		if snapshot.Model == "" {
			snapshot.Model = log.Model
		}
		snapshot.Provider = ""
		snapshot.AccountRateMultiplier = 0
		snapshot.AccountStatsCost = nil
		snapshot.CustomerPricing = userPricingSnapshot(snapshot.CustomerPricing, log)
		for i, addition := range snapshot.Additions {
			snapshot.Additions[i] = userPricingSnapshot(addition, log)
		}
	}
	result.Snapshot = &snapshot
	return result
}

func userPricingSnapshot(raw json.RawMessage, log *service.UsageLog) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	nested := *log
	nested.BillingEvidence = &service.UsageBillingEvidence{Snapshot: string(raw)}
	evidence := usageBillingExplanation(&nested, false)
	if evidence.Snapshot == nil {
		return nil
	}
	encoded, err := json.Marshal(evidence.Snapshot)
	if err != nil {
		return nil
	}
	return encoded
}
