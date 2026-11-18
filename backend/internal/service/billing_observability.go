package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
)

// Only bounded categories enter operational logs; raw provider/database errors
// may contain credentials, SQL values, URLs or response bodies.
func billingFailureCategory(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrUsageBillingRequestConflict), errors.Is(err, ErrQianwenRealtimeConflict):
		return "conflict"
	case errors.Is(err, ErrUsageSettlementNeedsReview), errors.Is(err, ErrInvalidBillingAmount), errors.Is(err, ErrUpstreamJobUnsupportedUsage), errors.Is(err, ErrUpstreamJobTariff):
		return "needs_review"
	default:
		return "dependency_unavailable"
	}
}

func logJobEvent(ctx context.Context, j *UpstreamJob, category string) {
	if j == nil {
		return
	}
	requestID, _ := ctx.Value(ctxkey.RequestID).(string)
	if requestID == "" {
		requestID = j.PublicRequestID
	}
	correlationID := j.PublicRequestID
	if j.Billing.Realtime != nil {
		correlationID = j.Billing.Realtime.SessionID
	}
	fields := []any{"component", "billing", "request_id", requestID, "job_id", j.PublicRequestID, "family", j.Family, "failure_category", category, "attempt", j.AttemptCount}
	// Reuse Ops' existing request/client-request indexes for background lookups.
	fields = append(fields, "client_request_id", correlationID)
	if j.Billing.Realtime != nil {
		fields = append(fields, "session_id", j.Billing.Realtime.SessionID, "turn_id", j.UpstreamTaskID)
	}
	if category == "admitted" || category == "receipt_received" {
		slog.Info("billing job event", fields...)
	} else {
		slog.Error("billing job event", fields...)
	}
}

// Preserve errors.Is/As without exposing dependency error text to a handler.
type billingObservedError struct{ cause error }

func (e billingObservedError) Error() string { return "usage receipt persistence unconfirmed" }
func (e billingObservedError) Unwrap() error { return e.cause }
