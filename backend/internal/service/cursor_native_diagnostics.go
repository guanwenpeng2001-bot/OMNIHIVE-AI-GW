package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
)

func appendCursorNativeFailure(ctx context.Context, requestContext *gin.Context, account *Account, stage string, err error, run ...*cursor.RunDiagnostics) {
	event := OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID,
		Kind: "request_error", Stage: stage, Reason: cursor.DiagnosticCode(err)}
	if len(run) > 0 && run[0] != nil {
		snapshot := *run[0]
		event.CursorRun = &snapshot
	}
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
		event.ConnectMessageClass = classifyCursorConnectMessage(connect.Message)
		messageBytes := len(connect.Message)
		credentialLike := cursorMessageCredentialLike(connect.Message)
		event.ConnectMessageBytes, event.CredentialLike = &messageBytes, &credentialLike
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

func cursorMessageCredentialLike(message string) bool {
	original := strings.TrimSpace(message)
	// The shared redactor canonicalizes JSON. Compare like with like so JSON
	// whitespace or key order alone does not report a credential.
	var value any
	if json.Unmarshal([]byte(original), &value) == nil {
		if canonical, err := json.Marshal(value); err == nil {
			original = string(canonical)
		}
	}
	return logredact.RedactText(original) != original
}

// Inspect a bounded prefix, emit only closed labels. Never use this heuristic
// to change status codes, account health, admission, or retry policy.
func classifyCursorConnectMessage(message string) string {
	if strings.TrimSpace(message) == "" {
		return "empty"
	}
	if len(message) > 16*1024 {
		message = message[:16*1024]
	}
	text := strings.ToLower(message)
	for _, class := range []struct {
		label   string
		markers []string
	}{
		{"system_prompt", []string{"--system-prompt", "system prompt", "system_prompt"}},
		{"quota", []string{"quota", "insufficient credit", "usage limit", "spending limit"}},
		{"rate_limit", []string{"rate limit", "rate_limit", "too many requests"}},
		{"context_length", []string{"context length", "context_length", "context window", "too many tokens", "prompt is too long"}},
		{"tool_or_schema", []string{"tool", "schema"}},
		{"model_unavailable", []string{"model unavailable", "model not found", "model is not available", "unknown model", "unsupported model"}},
		{"permission", []string{"permission", "unauthorized", "forbidden", "access denied", "authentication"}},
		{"timeout", []string{"timeout", "timed out", "deadline exceeded"}},
	} {
		for _, marker := range class.markers {
			if strings.Contains(text, marker) {
				return class.label
			}
		}
	}
	return "unclassified"
}
