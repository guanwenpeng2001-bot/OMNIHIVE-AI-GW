package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const openCodePaymentReason = "opencode_payment_required"

func (s *AccountTestService) observeOpenCodePaymentFailure(ctx context.Context, account *Account, status int, body []byte) {
	if s.accountRepo == nil || !account.IsOpenCodeGoPlan() || status != http.StatusPaymentRequired {
		return
	}
	limiter := NewRateLimitService(s.accountRepo, nil, s.cfg, nil, nil)
	limiter.handleOpenCodePaymentRequired(WithAccountRuntimeIdentity(ctx, account), account,
		fmt.Sprintf("HTTP 402: %s", strings.TrimSpace(string(body))))
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
	complete := map[string]bool{}
	exhausted := false
	until := now.Add(20 * time.Minute)
	var latest *time.Time
	for _, tier := range result.Tiers {
		complete[tier.Window] = true
		if tier.UsedPercent < 100 {
			continue
		}
		exhausted = true
		reset := parseSchedulingResetAt(tier.ResetAt)
		if reset != nil && reset.After(now) && (latest == nil || reset.After(*latest)) {
			latest = reset
		}
	}
	if exhausted {
		if latest != nil {
			until = *latest
		}
		current.TempUnschedulableUntil = &until
		if strings.HasPrefix(current.TempUnschedulableReason, openCodePaymentReason) {
			current.TempUnschedulableReason = cnQuotaExhaustedReasonPrefix + ": " + current.TempUnschedulableReason
		} else if !owned {
			current.TempUnschedulableReason = cnQuotaExhaustedReasonPrefix + ": OpenCode Go usage window exhausted"
		}
	} else if recoveredError && !owned {
		current.TempUnschedulableUntil = &until
		current.TempUnschedulableReason = openCodePaymentReason + ": key valid; generation capacity requires recheck"
	} else if owned && complete["5h"] && complete["weekly"] && complete["monthly"] {
		// A successful key check alone is not proof that generation has funds.
		// Only a quota-owned pause can be lifted by an available quota snapshot.
		if strings.HasPrefix(current.TempUnschedulableReason, cnQuotaExhaustedReasonPrefix) {
			current.TempUnschedulableUntil = nil
			current.TempUnschedulableReason = ""
		}
	}
	if !exhausted && complete["5h"] && complete["weekly"] && complete["monthly"] {
		for _, window := range []string{"5h", "weekly", "monthly"} {
			reset := parseSchedulingResetAt(current.Extra[cnExtraKey(PlatformOpenCodeGo, window+"_reset_at")])
			if reset != nil && sameQuotaTime(current.RateLimitResetAt, reset) {
				current.RateLimitedAt, current.RateLimitResetAt = nil, nil
				break
			}
		}
	}
}

func sameQuotaTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

// A payment response does not invalidate the key. Keep its upstream explanation
// and block generation until the exhausted window resets, or a bounded recheck.
func (s *RateLimitService) handleOpenCodePaymentRequired(ctx context.Context, account *Account, message string) {
	until := time.Now().Add(s.cnBalanceCooldownDuration())
	reason := openCodePaymentReason + ": " + strings.TrimSpace(message)
	if reset := openCodeExhaustedQuotaReset(account, time.Now()); reset != nil {
		until = *reset
		reason = cnQuotaExhaustedReasonPrefix + ": " + reason
	}
	s.setCNProviderTempUnschedulable(ctx, account, until, openCodePaymentReason, reason, "opencode_payment_required")
}

// All exhausted windows must have reset before generation can recover. A zero
// rolling window must not shorten a monthly exhaustion to the rolling reset.
func openCodeExhaustedQuotaReset(account *Account, now time.Time) *time.Time {
	if account == nil || !account.IsOpenCodeGoPlan() {
		return nil
	}
	var latest *time.Time
	for _, window := range []string{"5h", "weekly", "monthly"} {
		used, ok := cnParseF64(account.Extra[cnExtraKey(PlatformOpenCodeGo, window+"_used_percent")])
		if !ok || used < 100 {
			continue
		}
		reset := parseSchedulingResetAt(account.Extra[cnExtraKey(PlatformOpenCodeGo, window+"_reset_at")])
		if reset != nil && reset.After(now) && (latest == nil || reset.After(*latest)) {
			latest = reset
		}
	}
	return latest
}
