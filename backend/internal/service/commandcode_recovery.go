package service

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// A bare HTTP denial can come from a relay or edge; require credential evidence.
func isCommandCodeInvalidCredential(status int, body []byte) bool {
	if (status != http.StatusUnauthorized && status != http.StatusForbidden) || !gjson.ValidBytes(body) {
		return false
	}
	if commandCodePlanError(status, body) != "" {
		return false
	}
	code := strings.ToUpper(strings.TrimSpace(gjson.GetBytes(body, "error.code").String()))
	switch code {
	case "INVALID_API_KEY", "INVALID_KEY", "API_KEY_REVOKED", "KEY_REVOKED":
		return true
	}
	message := strings.ToLower(gjson.GetBytes(body, "error.message").String())
	return strings.Contains(message, "invalid api key") || strings.Contains(message, "invalid key") || strings.Contains(message, "api key has been revoked")
}

// This runs after pool/custom-code gates and explicit temporary rules.
func (s *RateLimitService) handleCommandCodeError(ctx context.Context, account *Account, status int, body []byte) (handled, disable bool) {
	if isCommandCodeInvalidCredential(status, body) {
		s.handleAuthError(ctx, account, "Command Code authentication failed: "+extractUpstreamErrorMessage(body))
		return true, true
	}
	switch status {
	case http.StatusPaymentRequired:
		until := time.Now().Add(20 * time.Minute)
		if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, "commandcode_payment_required: payment or balance must recover"); err != nil {
			slog.Warn("commandcode_payment_cooldown_failed", "account_id", account.ID, "error", err)
			return true, false
		}
		s.notifyAccountSchedulingBlocked(account, until, "commandcode_payment_required")
		return true, true
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest:
		// Plan/model restrictions, edge failures and unknown denials do not
		// establish account-wide credential failure.
		return true, false
	case http.StatusTooManyRequests:
		winner := pickLatestResetSchedulingCandidate(commandCodeUsageWindows(account, time.Now()), 100, time.Now())
		if winner == nil {
			s.apply429FallbackRateLimit(ctx, account, "commandcode_usage_window_unknown")
			return true, false
		}
		if err := s.accountRepo.SetRateLimited(ctx, account.ID, *winner.until); err != nil {
			slog.Warn("commandcode_rate_limit_failed", "account_id", account.ID, "error", err)
			return true, false
		}
		s.notifyAccountSchedulingBlocked(account, *winner.until, "429")
		return true, false
	}
	return false, false
}

// Only observed, supported windows with finite usage and a future reset may
// affect scheduling. Failed probes invalidate support without inventing usage.
func commandCodeUsageWindows(account *Account, now time.Time) []*accountSchedulingThresholdCandidate {
	if !account.IsCommandCode() || account.Extra["commandcode_usage_supported"] != true {
		return nil
	}
	var windows []*accountSchedulingThresholdCandidate
	for _, window := range []string{"5h", "weekly"} {
		used, valid := cnParseF64(account.Extra["commandcode_"+window+"_used_percent"])
		reset := parseSchedulingResetAt(account.Extra["commandcode_"+window+"_reset_at"])
		if !valid || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || reset == nil || !reset.After(now) {
			continue
		}
		windows = append(windows, &accountSchedulingThresholdCandidate{window: window, scope: PlatformCommandCode, usedPercent: used, until: reset})
	}
	return windows
}
