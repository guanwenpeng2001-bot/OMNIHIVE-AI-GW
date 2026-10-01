package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
)

func appendCursorNativeFailure(ctx context.Context, requestContext *gin.Context, account *Account, stage string, err error) {
	event := OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID,
		Kind: "request_error", Stage: stage, Reason: cursor.DiagnosticCode(err)}
	var upstream *cursor.HTTPError
	var connect *cursor.ConnectError
	switch {
	case IsAttemptAdmissionError(err):
		event.Kind, event.Stage = "admission_error", "pre_send"
		switch {
		case errors.Is(err, ErrAttemptBudgetExhausted):
			event.Reason = "attempt_budget_exhausted"
		case errors.Is(err, ErrResponseCommitted):
			event.Reason = "response_committed"
		case errors.Is(err, ErrAttemptOutcomeUnknown):
			event.Reason = "attempt_outcome_unknown"
		case errors.Is(err, ErrTPMEstimateUnavailable):
			event.Reason = "tpm_estimate_unavailable"
		case errors.Is(err, ErrTPMStateUnavailable):
			event.Reason = "tpm_state_unavailable"
		case errors.Is(err, ErrTPMReservationConflict):
			event.Reason = "tpm_reservation_conflict"
		default:
			event.Reason = "tpm_limit_exceeded"
		}
	case errors.As(err, &upstream):
		event.Kind, event.UpstreamStatusCode = "http_error", upstream.Status
	case errors.As(err, &connect):
		event.Kind, event.UpstreamStatusCode = "connect_error", 200
	}
	event.Message = "Cursor native request failed: " + event.Reason
	// Existing budget records contain only numeric identities and fixed labels.
	// Record them even on admission rejection: an empty list means no Run send
	// was admitted, not that an upstream accepted or rejected a generation.
	if budget := AttemptBudgetFromContext(ctx); budget != nil {
		detail, _ := json.Marshal(struct {
			Attempts []AttemptRecord `json:"attempts"`
		}{budget.Snapshot()})
		event.Detail = string(detail)
	}
	appendOpsUpstreamError(requestContext, event)
}
