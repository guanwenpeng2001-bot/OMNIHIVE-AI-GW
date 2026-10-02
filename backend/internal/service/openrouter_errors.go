package service

import (
	"context"
	"net/http"
	"time"

	"github.com/tidwall/gjson"
)

func openRouterPaymentSource(body []byte) string {
	switch source := gjson.GetBytes(body, "error.metadata.limit_source").String(); source {
	case "openrouter_in_flight_budget", "openrouter_key_limit":
		return source
	default:
		return "openrouter_credits"
	}
}

// Error state is account/request scoped, never evidence that a model vanished.
func (s *OpenAIGatewayService) handleOpenRouterError(ctx context.Context, account *Account, status int, headers http.Header, body []byte) bool {
	if s == nil || account == nil {
		return false
	}
	if status == http.StatusForbidden {
		return false
	}
	if status == http.StatusPaymentRequired {
		source := openRouterPaymentSource(body)
		if source == "openrouter_in_flight_budget" {
			return false
		}
		// This is a local retry cooldown, not an invented provider reset timestamp.
		until := time.Now().Add(time.Minute)
		if retry := parseRetryAfterResetTime(headers, time.Now()); retry != nil {
			until = *retry
		}
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			if err := s.rateLimitService.accountRepo.SetTempUnschedulable(ctx, account.ID, until, source); err == nil {
				return true
			}
		}
		return false
	}
	if s.rateLimitService != nil && (status == http.StatusUnauthorized || status == http.StatusTooManyRequests) {
		return s.rateLimitService.HandleUpstreamError(ctx, account, status, headers, body)
	}
	return false
}

func (s *OpenAIGatewayService) openRouterFailover(ctx context.Context, account *Account, response *http.Response, body []byte, message string) *UpstreamFailoverError {
	status := response.StatusCode
	if status == http.StatusForbidden {
		return nil
	}
	if status != 401 && status != 402 && status != 429 && status < 500 {
		return nil
	}
	disabled := s.handleOpenRouterError(ctx, account, status, response.Header, body)
	transient := status == 402 && openRouterPaymentSource(body) == "openrouter_in_flight_budget"
	failure := s.newOpenAIAccountFailoverError(account, status, response.Header, body, message, disabled, transient)
	if transient {
		failure.RequestScopedTransient = true
		failure.Scope = GatewayFailureScopeRequest
		failure.ClientStatusCode = http.StatusPaymentRequired
		failure.Reason = GatewayFailureReason("openrouter_in_flight_budget")
		if retry := parseRetryAfterResetTime(response.Header, time.Now()); retry != nil {
			failure.SameAccountRetryDelay = time.Until(*retry)
		}
	} else if status == 402 {
		failure.Reason = GatewayFailureReason(openRouterPaymentSource(body))
	}
	return failure
}

// OpenRouter may report an SSE failure after HTTP 200. It must never become a
// successful synthetic protocol terminal event.
func openRouterStreamError(payload string) bool {
	if gjson.Get(payload, "error").Exists() {
		return true
	}
	for _, choice := range gjson.Get(payload, "choices").Array() {
		if choice.Get("finish_reason").String() == "error" {
			return true
		}
	}
	return false
}
