package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SearchKimi uses the existing native adapter with the gateway's HTTP transport.
// The result carries the same per-call billing facts as other search adapters.
func (s *OpenAIGatewayService) SearchKimi(ctx context.Context, account *Account, request KimiWebSearchRequest) (*KimiWebSearchResult, *OpenAIForwardResult, error) {
	started := time.Now()
	search, err := NewKimiActionService(s.httpUpstream, s.cfg).SearchWeb(ctx, account, request)
	if err != nil {
		return nil, nil, err
	}
	model := "kimi-search"
	if request.Pro {
		model = "kimi-search-pro"
	}
	// TrackID is caller-controlled and echoed by Kimi. Like the existing search
	// handler, use a gateway-owned money-event ID so repeated tracking headers
	// cannot collapse distinct successful calls into one billing receipt.
	usage := &OpenAIForwardResult{RequestID: "web_search:" + uuid.NewString(), Model: model, BillingModel: model, UpstreamModel: model, Duration: time.Since(started)}
	if search.BillableCall {
		usage.WebSearchCalls = 1
	}
	return search, usage, nil
}
