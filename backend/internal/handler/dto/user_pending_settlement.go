package dto

import (
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// UserPendingSettlement contains receipt facts only; an unknown amount is never zero.
type UserPendingSettlement struct {
	CompletionStatus      string               `json:"completion_status"`
	Source                string               `json:"source"`
	ReceiptID             int64                `json:"receipt_id"`
	RequestID             string               `json:"request_id"`
	APIKeyID              int64                `json:"api_key_id"`
	RequestedModel        string               `json:"requested_model"`
	UpstreamModel         *string              `json:"upstream_model"`
	UpstreamResponseModel *string              `json:"upstream_response_model"`
	InputTokens           *int                 `json:"input_tokens"`
	OutputTokens          *int                 `json:"output_tokens"`
	PricingState          service.PricingState `json:"pricing_state"`
	State                 string               `json:"state"`
	Amount                *float64             `json:"amount"`
	AmountState           string               `json:"amount_state"`
	CreatedAt             time.Time            `json:"created_at"`
}

func UserPendingSettlementFromService(receipt *service.UserPendingSettlementItem) UserPendingSettlement {
	return UserPendingSettlement{
		CompletionStatus: receipt.CompletionStatus, ReceiptID: receipt.ID, RequestID: receipt.Key.RequestID, APIKeyID: receipt.Key.APIKeyID,
		RequestedModel: receipt.Usage.RequestedModel, UpstreamModel: receipt.Usage.UpstreamModel, UpstreamResponseModel: receipt.Usage.UpstreamResponseModel,
		InputTokens: receipt.InputTokens, OutputTokens: receipt.OutputTokens,
		PricingState: receipt.Usage.PricingState, State: "needs_review", AmountState: "pending_pricing", CreatedAt: receipt.CreatedAt,
	}
}
