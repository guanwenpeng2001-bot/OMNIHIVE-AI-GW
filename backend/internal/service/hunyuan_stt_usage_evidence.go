package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
)

// RecordHunyuanSTTUnresolved retains a successful response whose media duration
// is unknown. It never resolves a price or invokes a monetary settlement.
func (s *OpenAIGatewayService) RecordHunyuanSTTUnresolved(ctx context.Context, result *OpenAIForwardResult, apiKey *APIKey, account *Account, taskID string) error {
	if s == nil || s.usageSettlement == nil {
		return errors.New("hunyuan STT evidence settlement unavailable")
	}
	if result == nil || !result.AudioUsageUnresolved || result.AudioUsage != nil || apiKey == nil || account == nil || account.Platform != PlatformHunyuan {
		return errors.New("invalid hunyuan STT unresolved evidence")
	}
	model := strings.TrimSpace(firstNonEmpty(result.BillingModel, result.Model))
	if model == "" {
		return errors.New("hunyuan STT evidence model unavailable")
	}
	// A separate observation identity cannot occupy the final task charge key.
	requestID := "hunyuan-stt-evidence:" + resolveUsageBillingRequestID(ctx, result.RequestID)
	snapshot, err := json.Marshal(struct {
		Reason string `json:"reason"`
		TaskID string `json:"task_id,omitempty"`
	}{Reason: "missing_or_invalid_duration_ms", TaskID: strings.TrimSpace(taskID)})
	if err != nil {
		return err
	}
	receipt := &UsageSettlementReceipt{
		Key:     UsageSettlementKey{RequestID: requestID, UserID: apiKey.UserID, APIKeyID: apiKey.ID},
		Command: UsageBillingCommand{RequestID: requestID, UserID: apiKey.UserID, APIKeyID: apiKey.ID, AccountID: account.ID, Model: model},
		Usage: UsageLog{RequestID: requestID, UserID: apiKey.UserID, APIKeyID: apiKey.ID, AccountID: account.ID, Model: model,
			GroupID: apiKey.GroupID, PricingState: PricingStateError, PricingSnapshot: string(snapshot)},
		ResolutionState: "unresolved", CompletionState: "complete",
	}
	slog.Warn("hunyuan STT duration unresolved", "request_id", requestID, "task_id", taskID, "account_id", account.ID, "model", model, "reason", "missing_or_invalid_duration_ms")
	err = s.usageSettlement.Submit(ctx, receipt, nil)
	if errors.Is(err, ErrUsageSettlementNeedsReview) {
		return nil // Persisted review is the intended outcome, not a failed write.
	}
	return err
}
