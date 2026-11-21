package requests

import (
	"fmt"
	"net/url"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

const (
	ReadGrant       = "requests.read"
	ExportGrant     = "requests.export"
	FinanceGrant    = "billing.ledger.read"
	ConnectionGrant = "connections.read"
)

type Scope struct {
	WorkspaceID int64
	ProjectID   int64
}

func (s Scope) RequestURL(id string) string {
	return fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/requests/%s", s.WorkspaceID, s.ProjectID, url.PathEscape(id))
}

type Projection struct {
	Scope
	Finance     bool
	Connections bool
	Export      bool
}

type Filter struct {
	From, To       *time.Time
	Timezone       string
	KeyIDs         []int64
	Models         []string
	Protocols      []string
	Outcomes       []string
	Settlement     []string
	FundingSources []string
	Reasons        []string
}

type Seek struct {
	StartedAt time.Time
	ID        string
}

type Page struct {
	Filter
	AsOf  time.Time
	After Seek
	Limit int
}

type Summary struct {
	ID              string       `json:"id"`
	StartedAt       time.Time    `json:"started_at"`
	StartSource     string       `json:"start_source"`
	RequestedModel  *string      `json:"requested_model"`
	Protocol        *string      `json:"protocol"`
	Outcome         string       `json:"outcome"`
	Reason          *string      `json:"reason"`
	SettlementState string       `json:"settlement_state"`
	ChargedAmount   *money.Money `json:"charged_amount"`
	AttemptCount    *int64       `json:"attempt_count,string"`
	DetailURL       string       `json:"detail_url"`
	Redactions      []string     `json:"redactions"`
}

type Missing struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
}

type Completeness struct {
	State   string    `json:"state"`
	Missing []Missing `json:"missing"`
}

func complete() Completeness { return Completeness{State: "complete", Missing: []Missing{}} }

func (d *Completeness) missing(section, reason string) {
	d.State = "partial"
	d.Missing = append(d.Missing, Missing{Section: section, Reason: reason})
}

type Milestone struct {
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	Source string    `json:"source"`
}

type Timings struct {
	Queue        *int64 `json:"queue"`
	Route        *int64 `json:"route"`
	UpstreamTTFT *int64 `json:"upstream_ttft"`
	Total        *int64 `json:"total"`
}

type Route struct {
	PolicyVersion *string `json:"policy_version"`
	SnapshotID    *string `json:"snapshot_id"`
	CandidateID   *string `json:"candidate_id"`
	ExplainURL    string  `json:"explain_url"`
}

type Attempt struct {
	ID                 string                   `json:"id"`
	Number             int                      `json:"number"`
	CandidateID        *string                  `json:"candidate_id"`
	ConnectionID       *int64                   `json:"connection_id,string"`
	ConnectionName     *string                  `json:"connection_name"`
	StartedAt          *time.Time               `json:"started_at"`
	DurationMS         *int64                   `json:"duration_ms"`
	UpstreamTTFTMS     *int64                   `json:"upstream_ttft_ms"`
	Acceptance         string                   `json:"acceptance"`
	Outcome            string                   `json:"outcome"`
	Status             *int                     `json:"status"`
	Reason             *string                  `json:"reason"`
	SemanticOutput     *bool                    `json:"semantic_output"`
	AbandonedUncertain bool                     `json:"abandoned_uncertain"`
	ReceiptState       string                   `json:"receipt_state"`
	Conversion         *protocol.ConversionPlan `json:"conversion"`
}

type Unit struct {
	Kind     string `json:"kind"`
	Quantity int64  `json:"quantity,string"`
}

type Usage struct {
	Level  string `json:"level"`
	Source string `json:"source"`
	Units  []Unit `json:"units"`
}

type Billing struct {
	Source                 *string      `json:"billing_source"`
	State                  string       `json:"state"`
	Currency               *string      `json:"currency"`
	PriceVersion           *string      `json:"price_version"`
	PricingState           *string      `json:"pricing_state"`
	ReservationID          *int64       `json:"reservation_id,string"`
	ReservationState       *string      `json:"reservation_state"`
	ReleasePending         *bool        `json:"release_pending"`
	Reserved               *money.Money `json:"reserved"`
	Estimate               *money.Money `json:"estimate"`
	PlatformCharge         *money.Money `json:"platform_charge"`
	CatalogReference       *money.Money `json:"catalog_reference"`
	EntitlementConsumption *money.Money `json:"entitlement_consumption"`
	BYOKExternalEstimate   *money.Money `json:"byok_external_estimate"`
	PlatformServiceFee     *money.Money `json:"platform_service_fee"`
	SettlementID           *int64       `json:"settlement_id,string"`
	ReceiptState           string       `json:"receipt_state"`
	LedgerEntryIDs         []string     `json:"ledger_entry_ids"`
	RoundingRule           *string      `json:"rounding_rule"`
}

type Diagnostic struct {
	Reason    string  `json:"reason"`
	AttemptID *string `json:"attempt_id,omitempty"`
}

type Detail struct {
	Summary
	WorkspaceID         int64                    `json:"workspace_id,string"`
	ProjectID           int64                    `json:"project_id,string"`
	ClientCorrelationID *string                  `json:"client_correlation_id"`
	APIKeyID            int64                    `json:"api_key_id,string"`
	ActorID             *int64                   `json:"actor_id,string"`
	KeyOwnerID          *int64                   `json:"key_owner_id,string"`
	FundingAccountID    *int64                   `json:"funding_account_id,string"`
	TraceID             *string                  `json:"trace_id"`
	Origin              string                   `json:"origin"`
	Action              *string                  `json:"action"`
	EffectiveModel      *string                  `json:"effective_model"`
	EndedAt             *time.Time               `json:"ended_at"`
	Timings             Timings                  `json:"timings_ms"`
	Timeline            []Milestone              `json:"timeline"`
	Route               Route                    `json:"route"`
	Attempts            []Attempt                `json:"attempts"`
	AbandonedAttempts   []Attempt                `json:"abandoned_attempts"`
	Conversion          *protocol.ConversionPlan `json:"conversion"`
	Usage               Usage                    `json:"usage"`
	Billing             Billing                  `json:"billing"`
	TaskIDs             []string                 `json:"task_ids"`
	Diagnostics         []Diagnostic             `json:"diagnostics"`
	DataCompleteness    Completeness             `json:"data_completeness"`
	AllowedActions      []string                 `json:"allowed_actions"`
}

type Explanation struct {
	RequestID        string               `json:"request_id"`
	Trace            *trace.DecisionTrace `json:"trace"`
	DataCompleteness Completeness         `json:"data_completeness"`
}
