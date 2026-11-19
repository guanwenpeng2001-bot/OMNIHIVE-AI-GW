package snapshot

import (
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

type Candidate struct {
	ID                string
	ModelID           string
	FundingSource     string
	FundingAuthorized bool
	EndpointFallback  bool
	Region            string
	RegionVerified    bool
	RetentionVerified bool
	ZeroRetention     bool
	Price             *money.Amount
	QuotaRemaining    *int64

	Rejection   string
	Score       float64
	Priority    int
	LoadRate    int
	Waiting     int
	Tier        int
	RecheckOnly bool
	LastUsedAt  *time.Time
	ResetAt     *time.Time
	PreferOAuth bool
	InputOrder  int
	Factors     []trace.Factor
}

type CandidateSnapshot struct {
	ID             string
	AsOf           time.Time
	CatalogVersion string
	PriceVersion   string
	Candidates     []Candidate
}
