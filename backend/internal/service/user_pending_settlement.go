package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
)

// UserPendingSettlementCursor pages receipts independently from settled usage.
type UserPendingSettlementCursor struct {
	Source   string
	BeforeID int64
	Limit    int
}

type UserPendingSettlementItem struct {
	*UsageSettlementReceipt
	InputTokens      *int
	OutputTokens     *int
	CompletionStatus string
}

func PendingSettlementFromReceipt(receipt *UsageSettlementReceipt) *UserPendingSettlementItem {
	return &UserPendingSettlementItem{UsageSettlementReceipt: receipt, InputTokens: &receipt.Usage.InputTokens, OutputTokens: &receipt.Usage.OutputTokens, CompletionStatus: receipt.CompletionState}
}

type UserPendingSettlementPage struct {
	Items      []*UserPendingSettlementItem
	Total      int64
	NextCursor int64
}

// UserPendingSettlementReader exposes only the current user's unresolved receipts.
type UserPendingSettlementReader interface {
	ListUserPendingSettlement(context.Context, int64, usagestats.UsageLogFilters, UserPendingSettlementCursor) (*UserPendingSettlementPage, error)
}

func (s *UsageService) ListUserPendingSettlement(ctx context.Context, userID int64, filter usagestats.UsageLogFilters, cursor UserPendingSettlementCursor) (*UserPendingSettlementPage, error) {
	if userID <= 0 {
		return nil, ErrUsageLogNotFound
	}
	reader, ok := s.usageRepo.(UserPendingSettlementReader)
	if !ok {
		return nil, fmt.Errorf("pending settlement reader unavailable")
	}
	if cursor.Limit <= 0 {
		cursor.Limit = 20
	}
	if cursor.Limit > 100 {
		cursor.Limit = 100
	}
	return reader.ListUserPendingSettlement(ctx, userID, filter, cursor)
}

// PendingSettlementFromRealtimeJob preserves only observed turn facts.
func PendingSettlementFromRealtimeJob(job *UpstreamJob) (*UserPendingSettlementItem, error) {
	var usage struct {
		InputTokens  *int `json:"input_tokens"`
		OutputTokens *int `json:"output_tokens"`
	}
	usageError := json.Unmarshal(job.UsageJSON, &usage)
	if usageError != nil {
		usage.InputTokens, usage.OutputTokens = nil, nil
	}
	var upstream *string
	if job.Billing.Model != "" {
		model := job.Billing.Model
		upstream = &model
	}
	receipt := &UsageSettlementReceipt{ID: job.ID, Key: UsageSettlementKey{RequestID: job.PublicRequestID, UserID: job.UserID, APIKeyID: job.APIKeyID}, State: "needs_review", CreatedAt: job.SubmittedAt,
		Usage: UsageLog{RequestedModel: job.Billing.RequestedModel, UpstreamModel: upstream, PricingState: job.Billing.PricingState}}
	if job.ErrorCode == "unsupported_usage" || usageError != nil {
		receipt.Usage.PricingState = PricingStateError
	}
	return &UserPendingSettlementItem{UsageSettlementReceipt: receipt, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CompletionStatus: job.ResultRef}, nil
}

// PendingSettlementFromRealtimeJSON also reads optional declaration evidence so
// this reader remains compatible with snapshots written by newer realtime adapters.
func PendingSettlementFromRealtimeJSON(raw []byte) (*UserPendingSettlementItem, error) {
	var job UpstreamJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	var evidence struct {
		Billing struct {
			Realtime struct {
				UpstreamResponseModel *string `json:"upstream_response_model"`
			} `json:"realtime"`
		} `json:"billing_snapshot"`
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return nil, err
	}
	item, err := PendingSettlementFromRealtimeJob(&job)
	if err != nil {
		return nil, err
	}
	item.Usage.UpstreamResponseModel = evidence.Billing.Realtime.UpstreamResponseModel
	return item, nil
}
