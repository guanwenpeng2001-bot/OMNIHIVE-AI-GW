package service

// UsageBillingEvidence is read only provenance attached to a canonical log.
// A missing receipt remains unknown, independently of its historical amount.
type UsageBillingEvidence struct {
	PricingState    string `json:"pricing_state"`
	SettlementState string `json:"settlement_state"`
	CompletionState string `json:"completion_state"`
	Snapshot        string `json:"snapshot"`
	JobID           string `json:"job_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	TurnID          string `json:"turn_id,omitempty"`
	Family          string `json:"family,omitempty"`
	CacheHit        bool   `json:"cache_hit,omitempty"`
	CacheSourceID   int64  `json:"cache_source_receipt_id,omitempty"`
}
