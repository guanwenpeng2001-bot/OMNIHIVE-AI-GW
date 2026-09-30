package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const openCodePaymentReason = "opencode_payment_required"

func (s *AccountTestService) observeOpenCodePaymentFailure(ctx context.Context, account *Account, status int, headers http.Header, body []byte, model string) {
	if s.accountRepo == nil || !account.IsOpenCodeGoPlan() {
		return
	}
	reason := openCodePaymentReason
	if status != http.StatusPaymentRequired {
		if status != http.StatusTooManyRequests || !isOpenCodeUsageLimitFailure(body) {
			return
		}
		reason = "opencode_usage_limit"
	}
	limiter := NewRateLimitService(s.accountRepo, nil, s.cfg, nil, nil)
	limiter.handleOpenCodeModelQuotaFailure(withTempUnschedulableModel(WithAccountRuntimeIdentity(ctx, account), []string{model}), account, headers, body,
		fmt.Sprintf("%s: HTTP %d: %s", reason, status, strings.TrimSpace(string(body))))
}

// OpenCodeQuotaProbeWriter persists a probe and its recovery decision under the
// existing runtime identity lock. Implementations must preserve operator state.
type OpenCodeQuotaProbeWriter interface {
	ApplyOpenCodeQuotaProbe(context.Context, *Account, *CNProviderQuotaProbeResult) error
}

// ReconcileOpenCodeQuotaProbe is called with the current locked row. Only the
// error/cooldown observed at probe start may be recovered, never a later failure.
// Schedulable is deliberately untouched: false may be an operator pause.
func ReconcileOpenCodeQuotaProbe(current, expected *Account, result *CNProviderQuotaProbeResult, now time.Time) {
	if current == nil || expected == nil || result == nil || !current.IsOpenCodeGoPlan() {
		return
	}
	if current.Status != expected.Status || current.ErrorMessage != expected.ErrorMessage ||
		current.TempUnschedulableReason != expected.TempUnschedulableReason ||
		!sameQuotaTime(current.TempUnschedulableUntil, expected.TempUnschedulableUntil) ||
		!sameQuotaTime(current.RateLimitResetAt, expected.RateLimitResetAt) {
		return
	}
	if current.Status != StatusActive && current.Status != StatusError {
		return
	}
	if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
		current.Status = StatusError
		current.ErrorMessage = result.Error
		return
	}
	if !result.Success || !result.CredentialValid || len(result.Tiers) == 0 {
		return
	}
	// Authentication failures and arbitrary errors cannot be disproved by usage.
	recoveredError := current.Status == StatusError
	if recoveredError {
		message := strings.TrimSpace(current.ErrorMessage)
		if message != "" && !strings.HasPrefix(message, "Payment required (402):") {
			return
		}
		current.Status = StatusActive
		current.ErrorMessage = ""
	}
	owned := strings.HasPrefix(current.TempUnschedulableReason, openCodePaymentReason) ||
		strings.HasPrefix(current.TempUnschedulableReason, cnQuotaExhaustedReasonPrefix)
	if current.TempUnschedulableUntil != nil && current.TempUnschedulableUntil.After(now) && !owned {
		return
	}
	// Usage is an observation, not proof that every model is blocked. Free,
	// private and overage-funded models can remain available at 100% usage.
	if owned {
		current.TempUnschedulableUntil = nil
		current.TempUnschedulableReason = ""
	}
	for _, window := range []string{"5h", "weekly", "monthly"} {
		reset := parseSchedulingResetAt(current.Extra[cnExtraKey(PlatformOpenCodeGo, window+"_reset_at")])
		if reset != nil && sameQuotaTime(current.RateLimitResetAt, reset) {
			current.RateLimitedAt, current.RateLimitResetAt = nil, nil
			break
		}
	}
}

func sameQuotaTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

// OpenCodeModelQuotaWriter serializes a model failure with identity changes and
// other failures, retaining a later proven retry time for the same wire model.
type OpenCodeModelQuotaWriter interface {
	SetOpenCodeModelQuotaLimit(context.Context, *Account, string, time.Time, string) error
}

// A quota failure proves only that the attempted model cannot currently run.
func (s *RateLimitService) handleOpenCodeModelQuotaFailure(ctx context.Context, account *Account, headers http.Header, body []byte, reason string) {
	model := tempUnschedulableModel(ctx, nil)
	if model == "" || account == nil || s.accountRepo == nil {
		return // Unknown probe model cannot justify an account-wide pause.
	}
	now := time.Now()
	until := now.Add(s.cnBalanceCooldownDuration()) // default 20-minute recheck, never a snapshot-derived monthly ban
	if retry := parseRetryAfterResetTime(headers, now); retry != nil && retry.After(now) {
		until = *retry
	}
	if retry := parseOpenAIRateLimitResetTime(body); retry != nil && time.Unix(*retry, 0).After(until) {
		until = time.Unix(*retry, 0)
	}
	if previous := account.modelRateLimitResetAt(model); previous != nil && previous.After(until) {
		until = *previous
	}
	var err error
	if writer, ok := s.accountRepo.(OpenCodeModelQuotaWriter); ok {
		err = writer.SetOpenCodeModelQuotaLimit(ctx, account, model, until, reason)
	} else {
		err = s.accountRepo.SetModelRateLimit(ctx, account.ID, model, until, reason)
	}
	if err != nil {
		slog.Warn("opencode_model_quota_write_failed", "account_id", account.ID, "model", model, "error", err)
		return
	}
	setAccountModelRateLimitSnapshot(account, model, until, reason, now)
}

func isOpenCodeUsageLimitFailure(body []byte) bool {
	message := strings.ToLower(string(body))
	return strings.Contains(message, "gousagelimiterror") || strings.Contains(message, "usage limit") || strings.Contains(message, "usage_limit") || strings.Contains(message, "quota will reset")
}

// ClearOpenCodeModelQuotaLimits removes only quota-owned model observations on
// identity replacement; unrelated operator/model cooldowns retain their reasons.
func ClearOpenCodeModelQuotaLimits(account *Account) {
	if account == nil || !account.IsOpenCodeGoPlan() {
		return
	}
	limits, ok := account.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return
	}
	remaining := make(map[string]any, len(limits))
	for model, value := range limits {
		limit, _ := value.(map[string]any)
		reason, _ := limit["reason"].(string)
		if !strings.HasPrefix(reason, openCodePaymentReason) && !strings.HasPrefix(reason, "opencode_usage_limit") {
			remaining[model] = value
		}
	}
	account.Extra = shallowCopyMap(account.Extra)
	account.Extra[modelRateLimitsKey] = remaining
}
